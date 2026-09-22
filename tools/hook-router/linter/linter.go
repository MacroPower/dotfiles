package linter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// DefaultTimeout bounds a single linter invocation when a rule omits
// its [Rule.Timeout] override. Harper loads its dictionary and a
// weirpack on every run (~250ms), so this leaves room for a slow disk
// while staying well below the 45s wall-clock budget hook-router
// allots to a whole invocation.
const DefaultTimeout = 10 * time.Second

// MaxFindings is the number of findings [Format] renders before
// truncating. Claude Code truncates hook feedback near 10k characters,
// so the cap keeps the header and the first findings readable.
const MaxFindings = 40

// MaxFormatBytes caps the rendered size of [Format] output for the
// same reason as [MaxFindings]; the first limit reached ends the list.
const MaxFormatBytes = 8 * 1024

var (
	// ErrLinterFailed reports a linter that did not follow the
	// exit-code contract: an exit code of 2 or more, a signal, a
	// timeout, a missing binary, or an unrunnable rule. Callers log it
	// and carry on, since a broken linter must never block a write.
	ErrLinterFailed = errors.New("linter failed")

	// findingLine extracts the 1-based line number from a
	// `path:line:col: message` finding. The lazy path match lets the
	// first `:digits:` pair after it win.
	findingLine = regexp.MustCompile(`^.*?:(\d+):`)
)

// Rule routes a single file path to one external linter. [Rule.Run]
// appends the file path as the final argv element to Command, so the
// linter binary must accept a path positional. [doublestar.PathMatch]
// evaluates PathGlob; `**` matches recursively across path separators
// when used as a full segment (e.g. `/a/**/*.md`).
//
// JSON tags are camelCase because [builtins.toJSON] in home/claude.nix
// emits attribute names verbatim.
type Rule struct {
	PathGlob string   `json:"pathGlob"`
	Command  []string `json:"command"`
	Timeout  string   `json:"timeout,omitempty"`
}

// Finding is one line of linter output. Text is the raw output line
// and Line is the 1-based line the finding refers to, or 0 when the
// line could not be parsed.
type Finding struct {
	Text string
	Line int
}

// LineRange is an inclusive 1-based span of lines in a file.
type LineRange struct {
	Start int
	End   int
}

// ResolveTimeout returns the parsed [time.Duration] for [Rule.Timeout],
// or [DefaultTimeout] when the rule leaves it empty. Malformed
// durations also fall back to the default silently; linter timeouts
// are not load-bearing, so keep running rather than fail closed.
func (r Rule) ResolveTimeout() time.Duration {
	if r.Timeout == "" {
		return DefaultTimeout
	}

	d, err := time.ParseDuration(r.Timeout)
	if err != nil || d <= 0 {
		return DefaultTimeout
	}

	return d
}

// Run invokes the rule's linter against filePath and returns its
// findings. A missing file returns nil (no-op). Exit 0 returns nil.
// Exit 1 returns the findings parsed from stdout, or nil when stdout
// was empty. Any other outcome, including a context-deadline kill
// (which surfaces as an [*exec.ExitError] with code -1), returns an
// error wrapping [ErrLinterFailed] with the linter's stderr in the
// message.
func (r Rule) Run(ctx context.Context, filePath string) ([]Finding, error) {
	if len(r.Command) == 0 {
		return nil, fmt.Errorf("%w: rule has empty command", ErrLinterFailed)
	}

	_, err := os.Stat(filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("%w: stat %s: %w", ErrLinterFailed, filePath, err)
	}

	runCtx, cancel := context.WithTimeout(ctx, r.ResolveTimeout())
	defer cancel()

	// Defensive copy: appending filePath onto r.Command directly would
	// mutate the shared rule slice when it has spare capacity.
	argv := append([]string(nil), r.Command...)
	argv = append(argv, filePath)

	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err == nil {
		return nil, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return ParseFindings(stdout.String()), nil
	}

	return nil, fmt.Errorf("%w: running %s: %w (stderr: %s)",
		ErrLinterFailed, argv[0], err, strings.TrimSpace(stderr.String()))
}

// ParseFindings splits linter stdout into one [Finding] per non-empty
// line. A line without a leading `path:line:` prefix keeps Line 0 so
// [Filter] drops it and a whole-file report still shows it.
func ParseFindings(out string) []Finding {
	var findings []Finding

	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		f := Finding{Text: line}

		if m := findingLine.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[1])
			if err == nil {
				f.Line = n
			}
		}

		findings = append(findings, f)
	}

	return findings
}

// Ranges returns the line span of every occurrence of needle in
// content, in order. An empty needle or no occurrence returns nil.
// The function reports every occurrence because the Edit tool only
// requires old_string to be unique, so new_string may already appear
// elsewhere in the file; over-reporting is the safer failure.
func Ranges(content, needle string) []LineRange {
	if needle == "" {
		return nil
	}

	var ranges []LineRange

	needleLines := strings.Count(needle, "\n")
	offset := 0

	for {
		idx := strings.Index(content[offset:], needle)
		if idx < 0 {
			return ranges
		}

		start := 1 + strings.Count(content[:offset+idx], "\n")
		ranges = append(ranges, LineRange{Start: start, End: start + needleLines})
		offset += idx + len(needle)
	}
}

// Filter keeps the findings whose Line falls inside one of ranges.
// It drops findings with Line 0, since they fit no range.
func Filter(findings []Finding, ranges []LineRange) []Finding {
	var kept []Finding

	for _, f := range findings {
		if f.Line <= 0 {
			continue
		}

		for _, r := range ranges {
			if f.Line >= r.Start && f.Line <= r.End {
				kept = append(kept, f)
				break
			}
		}
	}

	return kept
}

// Format renders findings as the feedback Claude reads: a header line
// naming the file and the count, then one finding per line, truncated
// after limit findings or [MaxFormatBytes] with a trailing
// "... and N more" line. An empty findings slice renders an empty
// string.
func Format(path string, findings []Finding, limit int) string {
	if len(findings) == 0 {
		return ""
	}

	noun := "findings"
	if len(findings) == 1 {
		noun = "finding"
	}

	var b strings.Builder

	fmt.Fprintf(&b, "prose-lint: %d %s in %s. Fix each before continuing.\n", len(findings), noun, path)

	shown := 0

	for _, f := range findings {
		if shown >= limit || b.Len()+len(f.Text)+1 > MaxFormatBytes {
			break
		}

		b.WriteString(f.Text)
		b.WriteByte('\n')

		shown++
	}

	if shown < len(findings) {
		fmt.Fprintf(&b, "... and %d more\n", len(findings)-shown)
	}

	return strings.TrimRight(b.String(), "\n")
}

// Engine routes file paths to per-path linter invocations for
// PostToolUse:Write/Edit/MultiEdit. Construct with [New] or [Parse];
// both return a non-nil engine for empty input, and [*Engine.Empty]
// and [*Engine.Match] also accept a nil receiver so a bare config
// literal in a test needs no guard.
type Engine struct {
	rules []Rule
}

// New builds a [*Engine] from rules. A nil or empty slice yields an
// engine that matches nothing. [*Engine.Match] evaluates rules in slice
// order; the first matching glob wins.
func New(rules []Rule) *Engine {
	return &Engine{rules: rules}
}

// Empty reports whether the engine has no rules. A nil receiver
// reports true.
func (e *Engine) Empty() bool {
	if e == nil {
		return true
	}

	return len(e.rules) == 0
}

// Rules returns a copy of the engine's rules in evaluation order. A
// nil or empty engine returns nil.
func (e *Engine) Rules() []Rule {
	if e == nil || len(e.rules) == 0 {
		return nil
	}

	return append([]Rule(nil), e.rules...)
}

// Match returns the first rule whose [Rule.PathGlob] accepts filePath
// under [doublestar.PathMatch], and true. When no rule matches, returns
// the zero value and false. Match treats a malformed glob as a
// non-match and continues to the next rule; [Parse] rejects malformed
// globs at parse time, so in practice that branch is unreachable.
func (e *Engine) Match(filePath string) (Rule, bool) {
	if e == nil || len(e.rules) == 0 {
		return Rule{}, false
	}

	for _, rule := range e.rules {
		matched, err := doublestar.PathMatch(rule.PathGlob, filePath)
		if err != nil {
			continue
		}

		if matched {
			return rule, true
		}
	}

	return Rule{}, false
}

// Parse decodes the JSON payload passed via --linter-rules into a
// [*Engine]. Empty input yields an empty engine; malformed JSON returns
// an error so wrapper misconfiguration is loud. Each rule must declare
// a non-empty [Rule.PathGlob] and at least one [Rule.Command] element;
// otherwise the rule is unusable and the function returns an error.
func Parse(s string) (*Engine, error) {
	if s == "" {
		return New(nil), nil
	}

	var rules []Rule

	err := json.Unmarshal([]byte(s), &rules)
	if err != nil {
		return nil, fmt.Errorf("decoding linter rules JSON: %w", err)
	}

	for i, rule := range rules {
		if rule.PathGlob == "" {
			return nil, fmt.Errorf("linter rule %d: pathGlob is empty", i)
		}

		if len(rule.Command) == 0 {
			return nil, fmt.Errorf("linter rule %d: command is empty", i)
		}

		_, err := doublestar.PathMatch(rule.PathGlob, "")
		if err != nil {
			return nil, fmt.Errorf("linter rule %d: invalid pathGlob %q: %w", i, rule.PathGlob, err)
		}
	}

	return New(rules), nil
}
