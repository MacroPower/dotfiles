package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// defaultFormatterTimeout bounds a single formatter invocation when a
// rule omits its [FormatterRule.Timeout] override. Picked to absorb a
// python-wrapped formatter's startup (mdformat: ~150ms cold) while
// staying short enough that a wedged formatter does not add
// user-visible latency to PostToolUse. Well below the 45s wall-clock
// budget in [mainErr].
const defaultFormatterTimeout = 5 * time.Second

// FormatterRule routes a single file path to one external formatter.
// The file path is appended as the final argv element when [Run]
// executes Command, so the formatter binary must accept a path
// positional. PathGlob is matched with [filepath.Match] (no recursive
// `**`); a future glob lib swap stays internal to [*FormatterRules.Match].
//
// JSON tag style is camelCase to match the attribute names
// [builtins.toJSON] emits in home/claude.nix.
type FormatterRule struct {
	PathGlob string   `json:"pathGlob"`
	Command  []string `json:"command"`
	Timeout  string   `json:"timeout,omitempty"`
}

// ResolveTimeout returns the parsed [time.Duration] for
// [FormatterRule.Timeout], or [defaultFormatterTimeout] when the rule
// leaves it empty. Malformed durations also fall back to the default
// silently; formatter timeouts are not load-bearing, so keep running
// rather than fail closed.
func (r FormatterRule) ResolveTimeout() time.Duration {
	if r.Timeout == "" {
		return defaultFormatterTimeout
	}

	d, err := time.ParseDuration(r.Timeout)
	if err != nil || d <= 0 {
		return defaultFormatterTimeout
	}

	return d
}

// Run invokes the rule's formatter against filePath. Stdin/stdout/stderr
// are left at their [exec.Cmd] zero-value (discarded), so a chatty
// formatter never leaks into the hook JSON channel Claude Code
// consumes. A missing file returns nil (no-op). Non-zero exit codes
// return an [*exec.ExitError]; callers log and swallow so formatter
// breakage never reaches Claude.
func (r FormatterRule) Run(ctx context.Context, filePath string) error {
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

// FormatterRules routes file paths to per-path formatter invocations
// for PostToolUse:Write/Edit/MultiEdit. Construct with
// [NewFormatterRules] or [parseFormatterRules]; both return a non-nil
// engine for empty input so callers can call [*FormatterRules.Match]
// without nil guards.
type FormatterRules struct {
	rules []FormatterRule
}

// NewFormatterRules builds a [*FormatterRules] from rules. A nil or
// empty slice yields an engine that matches nothing. Rules are
// evaluated in slice order; the first matching glob wins.
func NewFormatterRules(rules []FormatterRule) *FormatterRules {
	return &FormatterRules{rules: rules}
}

// Empty reports whether the engine has no rules. A nil receiver
// reports true.
func (r *FormatterRules) Empty() bool {
	if r == nil {
		return true
	}

	return len(r.rules) == 0
}

// Match returns the first rule whose [FormatterRule.PathGlob] matches
// filePath via [filepath.Match], along with true. When no rule
// matches, returns the zero value and false. A malformed glob in a
// rule counts as "no match" for that rule and matching continues;
// [parseFormatterRules] rejects malformed globs at parse time so this
// path is unreachable in practice.
func (r *FormatterRules) Match(filePath string) (FormatterRule, bool) {
	if r == nil || len(r.rules) == 0 {
		return FormatterRule{}, false
	}

	for _, rule := range r.rules {
		matched, err := filepath.Match(rule.PathGlob, filePath)
		if err != nil {
			continue
		}

		if matched {
			return rule, true
		}
	}

	return FormatterRule{}, false
}

// parseFormatterRules decodes the JSON payload passed via
// --formatter-rules into a [*FormatterRules]. Empty input yields an
// empty engine; malformed JSON returns an error so wrapper
// misconfiguration is loud. Each rule must declare a non-empty
// [FormatterRule.PathGlob] and at least one [FormatterRule.Command]
// element; otherwise the rule is unusable and the function returns an
// error.
func parseFormatterRules(s string) (*FormatterRules, error) {
	if s == "" {
		return NewFormatterRules(nil), nil
	}

	var rules []FormatterRule

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

		_, err := filepath.Match(rule.PathGlob, "")
		if err != nil {
			return nil, fmt.Errorf("formatter rule %d: invalid pathGlob %q: %w", i, rule.PathGlob, err)
		}
	}

	return NewFormatterRules(rules), nil
}
