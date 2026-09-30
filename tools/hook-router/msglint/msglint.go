package msglint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/linter"
)

// DefaultTimeout bounds a single linter invocation when [Config.Timeout]
// is empty. Harper loads its dictionary and weirpack on every run, so
// this leaves room for a slow disk while staying well below
// hook-router's 45s budget.
const DefaultTimeout = 15 * time.Second

// ErrLintFailed reports a linter that did not follow the exit-code
// contract: an exit code of 2 or more, a signal, a timeout, or a
// missing binary. Callers log it and let the command through, since a
// broken linter must never block a commit.
var ErrLintFailed = errors.New("message lint failed")

// Config declares the message linter. Command runs with the message on
// stdin. JSON tags are camelCase because builtins.toJSON in
// home/claude.nix emits attribute names verbatim. The zero value is a
// disabled linter.
type Config struct {
	Command []string `json:"command"`
	Timeout string   `json:"timeout,omitempty"`
}

// Message is one piece of text a command hands to git or gh. Kind
// names it for the deny reason ("commit message" or "pull request").
type Message struct {
	Kind string
	Text string
}

// Parse decodes the JSON payload passed via --message-lint-config.
// Empty input yields a disabled [Config]; malformed JSON returns an
// error so wrapper misconfiguration is loud.
func Parse(s string) (Config, error) {
	if s == "" {
		return Config{}, nil
	}

	var cfg Config

	err := json.Unmarshal([]byte(s), &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("decoding message lint config JSON: %w", err)
	}

	return cfg, nil
}

// Empty reports whether the linter is disabled.
func (c Config) Empty() bool {
	return len(c.Command) == 0
}

// ResolveTimeout returns the parsed [Config.Timeout], or
// [DefaultTimeout] when it is empty or malformed.
func (c Config) ResolveTimeout() time.Duration {
	if c.Timeout == "" {
		return DefaultTimeout
	}

	d, err := time.ParseDuration(c.Timeout)
	if err != nil || d <= 0 {
		return DefaultTimeout
	}

	return d
}

// Lint runs the linter with text on stdin and returns its findings, one
// per stdout line through [linter.ParseFindings], so each carries the
// tier its `[Tier]` tag names. Exit 0 and exit 1 with empty stdout both
// return nil. Any other outcome returns an error wrapping
// [ErrLintFailed] with the linter's stderr in the message.
func (c Config) Lint(ctx context.Context, text string) ([]linter.Finding, error) {
	if c.Empty() {
		return nil, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, c.ResolveTimeout())
	defer cancel()

	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(runCtx, c.Command[0], c.Command[1:]...)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return nil, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return linter.ParseFindings(stdout.String()), nil
	}

	return nil, fmt.Errorf("%w: running %s: %w (stderr: %s)",
		ErrLintFailed, c.Command[0], err, strings.TrimSpace(stderr.String()))
}

// Result is the outcome of [Check]. Deny is set when a message has a
// Required finding, and the caller refuses the command every time.
// WaiverKey is set when no message has a Required finding but one has
// a Recommended finding. The caller denies the first run and lets the
// identical command through once it has recorded the key, so Claude
// can keep a Recommended finding it judges a false positive by running
// the command again unchanged. Reason is the deny text for either
// case. A message with Optional findings alone leaves all three
// fields empty, and the command passes.
type Result struct {
	Reason    string
	WaiverKey string
	Deny      bool
}

// Check lints every message in prog and reports what to do with the
// command. A disabled config or a command without a literal message
// returns the zero [Result] with no error. A linter crash returns the
// error and no decision, so the caller can log it and fall through.
func Check(ctx context.Context, prog *syntax.File, cfg Config) (Result, error) {
	if cfg.Empty() {
		return Result{}, nil
	}

	type linted struct {
		msg      Message
		findings []linter.Finding
	}

	var (
		res     Result
		waivers []string
		msgs    []linted
	)

	for _, m := range Messages(prog) {
		findings, err := cfg.Lint(ctx, m.Text)
		if err != nil {
			return Result{}, err
		}

		if len(findings) == 0 {
			continue
		}

		msgs = append(msgs, linted{msg: m, findings: findings})

		worst := linter.Optional
		for _, f := range findings {
			worst = min(worst, f.Tier)
		}

		switch worst {
		case linter.Required:
			res.Deny = true
		case linter.Recommended:
			waivers = append(waivers, waiverKey(m))
		}
	}

	if !res.Deny && len(waivers) == 0 {
		return Result{}, nil
	}

	// Every linted message goes into the reason, so a message with
	// Optional findings alone still shows them when another message
	// denies the command.
	var b strings.Builder

	for _, l := range msgs {
		writeReason(&b, l.msg.Kind, l.findings)
	}

	if !res.Deny {
		// One key covers every message in the command, so a rerun of
		// the whole command is what clears it.
		res.WaiverKey = joinKeys(waivers)

		b.WriteString("A Recommended finding that misreads its sentence passes when the command runs again unchanged.\n")
	}

	res.Reason = strings.TrimRight(b.String(), "\n")

	return res, nil
}

// writeReason appends one message's findings to the deny reason,
// grouped under a header per tier in [linter.Tiers] order.
func writeReason(b *strings.Builder, kind string, findings []linter.Finding) {
	noun := "findings"
	if len(findings) == 1 {
		noun = "finding"
	}

	fmt.Fprintf(b, "prose-lint: %d %s in the %s. Line numbers count from the first line of the message. Fix each and run the command again with the corrected text.\n",
		len(findings), noun, kind)

	for _, tier := range linter.Tiers {
		header := false

		for _, f := range findings {
			if f.Tier != tier {
				continue
			}

			if !header {
				fmt.Fprintf(b, "%s: %s\n", tier, tier.Instruction())

				header = true
			}

			b.WriteString(f.Text)
			b.WriteByte('\n')
		}
	}
}

// waiverKey returns the SHA-256 of a message's kind and text, hex
// encoded, so the same text under the same command shape maps to the
// same key on a rerun.
func waiverKey(m Message) string {
	h := sha256.New()
	h.Write([]byte(m.Kind))
	h.Write([]byte{0})
	h.Write([]byte(m.Text))

	return hex.EncodeToString(h.Sum(nil))
}

// joinKeys folds several message keys into one so a command with two
// messages claims one waiver. A single key passes through unchanged.
func joinKeys(keys []string) string {
	if len(keys) == 1 {
		return keys[0]
	}

	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
	}

	return hex.EncodeToString(h.Sum(nil))
}

// Messages returns every commit message and pull request text that
// prog hands to git or gh as literal text, in command order. A message
// the hook cannot read before the command runs (a parameter expansion,
// a command substitution other than `cat` of a heredoc, a missing
// -F file) drops out of the list.
func Messages(prog *syntax.File) []Message {
	var msgs []Message

	syntax.Walk(prog, func(node syntax.Node) bool {
		stmt, ok := node.(*syntax.Stmt)
		if !ok {
			return true
		}

		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		name, ok := literal(call.Args[0])
		if !ok {
			return true
		}

		switch name {
		case "git":
			if m, ok := gitCommit(call, stmt); ok {
				msgs = append(msgs, m)
			}
		case "gh":
			if m, ok := ghPRCreate(call, stmt); ok {
				msgs = append(msgs, m)
			}
		}

		return true
	})

	return msgs
}

// gitGlobalValueFlags lists git's top-level options that take a
// separate value, so the walk to the subcommand skips it.
var gitGlobalValueFlags = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
	"--namespace": true, "--exec-path": true, "--config-env": true,
}

// gitCommitValueFlags lists commit options that take a separate value
// but carry no message text.
var gitCommitValueFlags = map[string]bool{
	"-C": true, "-c": true, "--reuse-message": true, "--reedit-message": true,
	"--fixup": true, "--squash": true, "--author": true, "--date": true,
	"--cleanup": true, "-t": true, "--template": true, "--trailer": true,
	"--pathspec-from-file": true,
}

// gitCommit extracts the message of a `git commit` call. Several -m
// values join with a blank line, as git does. -F reads the named file,
// or the statement's heredoc when the path is "-".
func gitCommit(call *syntax.CallExpr, stmt *syntax.Stmt) (Message, bool) {
	args := call.Args

	i := 1
	for i < len(args) {
		s, ok := literal(args[i])
		if !ok {
			return Message{}, false
		}

		if s == "commit" {
			break
		}

		if gitGlobalValueFlags[s] {
			i += 2

			continue
		}

		if strings.HasPrefix(s, "-") {
			i++

			continue
		}

		return Message{}, false
	}

	if i >= len(args) {
		return Message{}, false
	}

	var parts []string

	for j := i + 1; j < len(args); j++ {
		s, ok := literal(args[j])
		if !ok {
			continue
		}

		if s == "--" {
			break
		}

		switch {
		case s == "-m" || s == "--message":
			text, ok := valueAt(args, j+1)
			if !ok {
				return Message{}, false
			}

			parts = append(parts, text)
			j++
		case strings.HasPrefix(s, "--message="):
			parts = append(parts, strings.TrimPrefix(s, "--message="))
		case s == "-F" || s == "--file":
			text, ok := fileValue(args, j+1, stmt)
			if !ok {
				return Message{}, false
			}

			parts = append(parts, text)
			j++
		case strings.HasPrefix(s, "--file="):
			text, ok := fileText(strings.TrimPrefix(s, "--file="), stmt)
			if !ok {
				return Message{}, false
			}

			parts = append(parts, text)
		case gitCommitValueFlags[s]:
			j++
		case len(s) > 1 && s[0] == '-' && s[1] != '-':
			// A short cluster such as -am or -qm. The first m or F takes
			// the rest of the cluster as its value, or the next argument
			// when it ends the cluster.
			idx := strings.IndexAny(s[1:], "mF")
			if idx < 0 {
				continue
			}

			flag := s[1+idx]
			rest := s[2+idx:]

			if rest == "" {
				var (
					text string
					ok   bool
				)

				if flag == 'm' {
					text, ok = valueAt(args, j+1)
				} else {
					text, ok = fileValue(args, j+1, stmt)
				}

				if !ok {
					return Message{}, false
				}

				parts = append(parts, text)
				j++

				continue
			}

			if flag == 'm' {
				parts = append(parts, rest)

				continue
			}

			text, ok := fileText(rest, stmt)
			if !ok {
				return Message{}, false
			}

			parts = append(parts, text)
		}
	}

	if len(parts) == 0 {
		return Message{}, false
	}

	return Message{Kind: "commit message", Text: strings.Join(parts, "\n\n")}, true
}

// ghValueFlags lists `gh pr create` options that take a separate value
// but carry no message text.
var ghValueFlags = map[string]bool{
	"-B": true, "--base": true, "-H": true, "--head": true, "-a": true, "--assignee": true,
	"-l": true, "--label": true, "-m": true, "--milestone": true, "-p": true, "--project": true,
	"-r": true, "--reviewer": true, "-R": true, "--repo": true, "-T": true, "--template": true,
}

// ghPRCreate extracts the title and body of a `gh pr create` call as
// one text, title first and a blank line between, so the linter sees
// the same shape as a commit message.
func ghPRCreate(call *syntax.CallExpr, stmt *syntax.Stmt) (Message, bool) {
	args := call.Args
	if len(args) < 3 {
		return Message{}, false
	}

	sub, ok := literal(args[1])
	if !ok || sub != "pr" {
		return Message{}, false
	}

	verb, ok := literal(args[2])
	if !ok || verb != "create" {
		return Message{}, false
	}

	var title, body string

	for j := 3; j < len(args); j++ {
		s, ok := literal(args[j])
		if !ok {
			continue
		}

		switch {
		case s == "-t" || s == "--title":
			text, ok := valueAt(args, j+1)
			if !ok {
				return Message{}, false
			}

			title = text
			j++
		case strings.HasPrefix(s, "--title="):
			title = strings.TrimPrefix(s, "--title=")
		case s == "-b" || s == "--body":
			text, ok := valueAt(args, j+1)
			if !ok {
				return Message{}, false
			}

			body = text
			j++
		case strings.HasPrefix(s, "--body="):
			body = strings.TrimPrefix(s, "--body=")
		case s == "-F" || s == "--body-file":
			text, ok := fileValue(args, j+1, stmt)
			if !ok {
				return Message{}, false
			}

			body = text
			j++
		case strings.HasPrefix(s, "--body-file="):
			text, ok := fileText(strings.TrimPrefix(s, "--body-file="), stmt)
			if !ok {
				return Message{}, false
			}

			body = text
		case ghValueFlags[s]:
			j++
		}
	}

	var parts []string

	if title != "" {
		parts = append(parts, title)
	}

	if body != "" {
		parts = append(parts, body)
	}

	if len(parts) == 0 {
		return Message{}, false
	}

	return Message{Kind: "pull request", Text: strings.Join(parts, "\n\n")}, true
}

// valueAt returns the literal text of args[i], or false when i falls
// past the end of args or the word has a non-literal part.
func valueAt(args []*syntax.Word, i int) (string, bool) {
	if i >= len(args) {
		return "", false
	}

	return literal(args[i])
}

// fileValue resolves the -F style argument at args[i] through
// [fileText].
func fileValue(args []*syntax.Word, i int, stmt *syntax.Stmt) (string, bool) {
	path, ok := valueAt(args, i)
	if !ok {
		return "", false
	}

	return fileText(path, stmt)
}

// fileText returns the message a -F path denotes: the statement's
// heredoc for "-", or the file's content when it exists.
func fileText(path string, stmt *syntax.Stmt) (string, bool) {
	if path == "-" {
		return heredoc(stmt.Redirs)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	return string(data), true
}

// heredoc returns the literal body of the first heredoc among redirs.
func heredoc(redirs []*syntax.Redirect) (string, bool) {
	for _, r := range redirs {
		if r.Op != syntax.Hdoc && r.Op != syntax.DashHdoc {
			continue
		}

		if r.Hdoc == nil {
			return "", false
		}

		return literal(r.Hdoc)
	}

	return "", false
}

// literal returns the text a word expands to when every part is a
// plain or quoted literal, or a `$(cat <<EOF)` substitution whose
// heredoc is literal. Any other part (a parameter expansion, an
// arithmetic expansion, another substitution) makes the word
// unreadable before the command runs, and ok is false.
func literal(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}

	var b strings.Builder

	for _, part := range w.Parts {
		s, ok := partLiteral(part)
		if !ok {
			return "", false
		}

		b.WriteString(s)
	}

	return b.String(), true
}

func partLiteral(p syntax.WordPart) (string, bool) {
	switch x := p.(type) {
	case *syntax.Lit:
		return x.Value, true
	case *syntax.SglQuoted:
		return x.Value, true
	case *syntax.DblQuoted:
		var b strings.Builder

		for _, part := range x.Parts {
			s, ok := partLiteral(part)
			if !ok {
				return "", false
			}

			b.WriteString(s)
		}

		return b.String(), true
	case *syntax.CmdSubst:
		return catHeredoc(x)
	default:
		return "", false
	}
}

// catHeredoc returns the heredoc body of a `$(cat <<EOF ... EOF)`
// substitution, the shape Claude Code uses for multi-line messages.
func catHeredoc(sub *syntax.CmdSubst) (string, bool) {
	if len(sub.Stmts) != 1 {
		return "", false
	}

	stmt := sub.Stmts[0]

	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}

	name, ok := literal(call.Args[0])
	if !ok || name != "cat" {
		return "", false
	}

	return heredoc(stmt.Redirs)
}
