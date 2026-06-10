package formatter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// DefaultTimeout bounds a single formatter invocation when a
// rule omits its [Rule.Timeout] override. Picked to absorb a
// python-wrapped formatter's startup (mdformat: ~150ms cold) while
// staying short enough that a wedged formatter does not add
// user-visible latency to PostToolUse. Well below the 45s wall-clock
// budget hook-router allots to a whole invocation.
const DefaultTimeout = 5 * time.Second

// Rule routes a single file path to one external formatter.
// [Run] appends the file path as the final argv element to Command,
// so the formatter binary must accept a path positional. PathGlob is
// matched with [doublestar.PathMatch]; `**` matches recursively
// across path separators when used as a full segment (e.g.
// `/a/**/*.md`).
//
// JSON tags are camelCase because [builtins.toJSON] in home/claude.nix
// emits attribute names verbatim.
type Rule struct {
	PathGlob string   `json:"pathGlob"`
	Command  []string `json:"command"`
	Timeout  string   `json:"timeout,omitempty"`
}

// ResolveTimeout returns the parsed [time.Duration] for
// [Rule.Timeout], or [DefaultTimeout] when the rule
// leaves it empty. Malformed durations also fall back to the default
// silently; formatter timeouts are not load-bearing, so keep running
// rather than fail closed.
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

// Run invokes the rule's formatter against filePath. Stdin/stdout/stderr
// are left at their [exec.Cmd] zero-value (discarded), so a chatty
// formatter never leaks into the hook JSON channel Claude Code
// consumes. A missing file returns nil (no-op). Non-zero exit codes
// return an [*exec.ExitError]; callers log and swallow so formatter
// breakage never reaches Claude.
func (r Rule) Run(ctx context.Context, filePath string) error {
	if len(r.Command) == 0 {
		return errors.New("formatter rule has empty command")
	}

	_, err := os.Stat(filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("stat %s: %w", filePath, err)
	}

	runCtx, cancel := context.WithTimeout(ctx, r.ResolveTimeout())
	defer cancel()

	// Defensive copy: appending filePath onto r.Command directly would
	// mutate the shared rule slice when it has spare capacity.
	argv := append([]string(nil), r.Command...)
	argv = append(argv, filePath)

	err = exec.CommandContext(runCtx, argv[0], argv[1:]...).Run()
	if err != nil {
		return fmt.Errorf("running %s: %w", argv[0], err)
	}

	return nil
}

// Engine routes file paths to per-path formatter invocations
// for PostToolUse:Write/Edit/MultiEdit. Construct with
// [New] or [Parse]; both return a non-nil
// engine for empty input so callers can call [*Engine.Match]
// without nil guards.
type Engine struct {
	rules []Rule
}

// New builds a [*Engine] from rules. A nil or
// empty slice yields an engine that matches nothing. Rules are
// evaluated in slice order; the first matching glob wins.
func New(rules []Rule) *Engine {
	return &Engine{rules: rules}
}

// Empty reports whether the engine has no rules. A nil receiver
// reports true.
func (r *Engine) Empty() bool {
	if r == nil {
		return true
	}

	return len(r.rules) == 0
}

// Rules returns a copy of the engine's rules in evaluation order. A
// nil or empty engine returns nil.
func (r *Engine) Rules() []Rule {
	if r == nil || len(r.rules) == 0 {
		return nil
	}

	return append([]Rule(nil), r.rules...)
}

// Match returns the first rule whose [Rule.PathGlob] accepts
// filePath under [doublestar.PathMatch], and true. When no rule
// matches, returns the zero value and false. A malformed glob is
// treated as a non-match and matching continues to the next rule;
// [Parse] rejects malformed globs at parse time, so in
// practice that branch is unreachable.
func (r *Engine) Match(filePath string) (Rule, bool) {
	if r == nil || len(r.rules) == 0 {
		return Rule{}, false
	}

	for _, rule := range r.rules {
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

// Parse decodes the JSON payload passed via
// --formatter-rules into a [*Engine]. Empty input yields an
// empty engine; malformed JSON returns an error so wrapper
// misconfiguration is loud. Each rule must declare a non-empty
// [Rule.PathGlob] and at least one [Rule.Command]
// element; otherwise the rule is unusable and the function returns an
// error.
func Parse(s string) (*Engine, error) {
	if s == "" {
		return New(nil), nil
	}

	var rules []Rule

	err := json.Unmarshal([]byte(s), &rules)
	if err != nil {
		return nil, fmt.Errorf("decoding formatter rules JSON: %w", err)
	}

	for i, rule := range rules {
		if rule.PathGlob == "" {
			return nil, fmt.Errorf("formatter rule %d: pathGlob is empty", i)
		}

		if len(rule.Command) == 0 {
			return nil, fmt.Errorf("formatter rule %d: command is empty", i)
		}

		_, err := doublestar.PathMatch(rule.PathGlob, "")
		if err != nil {
			return nil, fmt.Errorf("formatter rule %d: invalid pathGlob %q: %w", i, rule.PathGlob, err)
		}
	}

	return New(rules), nil
}
