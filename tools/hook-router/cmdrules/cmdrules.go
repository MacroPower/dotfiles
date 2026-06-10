package cmdrules

import (
	"encoding/json"
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// gitFlagsTakingValue lists top-level git flags that consume the
// following argument as their value (e.g. `git -C dir clone url`). The
// rule matcher skips both the flag and its value when locating the
// positional args, but only when [Rule.Command] is exactly
// "git". Other commands match strictly from position 1.
var gitFlagsTakingValue = map[string]bool{
	"-C":             true,
	"-c":             true,
	"--exec-path":    true,
	"--git-dir":      true,
	"--work-tree":    true,
	"--namespace":    true,
	"--super-prefix": true,
	"--config-env":   true,
}

// Command-rule actions. The zero value of Action is the empty string,
// which resolves to deny, so rules that omit the field are deny rules.
const (
	actionDeny = "deny"
	actionAsk  = "ask"
)

// Rule matches a Bash command whose AST contains a call
// matching Command + Args and resolves it to a PreToolUse decision
// per Action: "deny" (or "") blocks the call, "ask" forces a
// permission prompt even when settings or sandbox auto-allow would
// otherwise let it run. When Except is non-empty, the call is
// exempted only if it has at least one further positional arg whose
// literal value appears in Except. A bare `command + args` invocation
// (no further positional args) always fires regardless of Except.
//
// When Command == "git", leading top-level git flags listed in
// [gitFlagsTakingValue] (and their values) are skipped before matching
// Args. Other commands match strictly from position 1.
type Rule struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Except  []string `json:"except,omitempty"`
	Action  string   `json:"action,omitempty"`
	Reason  string   `json:"reason"`
}

// Ask reports whether the rule resolves to an "ask" decision rather
// than a deny.
func (r Rule) Ask() bool {
	return r.Action == actionAsk
}

// Engine is the deny/ask rule engine for PreToolUse:Bash.
// Construct with [New] or [Parse]; both return
// a non-nil zero value for empty input so callers can invoke
// [*Engine.Check] without nil guards.
//
// Rules are evaluated in slice order and the first match wins, so
// the caller building the list is responsible for ordering: deny
// rules before ask rules preserves deny precedence, and within a
// command's ask rules, subcommand-scoped rules must precede a
// catch-all fallback for the same command.
type Engine struct {
	rules []Rule
}

// New builds a [*Engine] from the given rules. A nil
// or empty slice yields an empty engine that matches nothing. Rules
// are evaluated in slice order; the first match wins.
func New(rules []Rule) *Engine {
	return &Engine{rules: rules}
}

// Empty reports whether the engine has no rules. A nil receiver
// reports true, so callers can treat nil and zero-rule engines
// identically (mirrors [*Engine.Check]).
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

// Check walks prog once and returns the first matching rule along with
// its reason. Every rule is evaluated against each [*syntax.CallExpr]
// in declaration order; when two rules could match the same call, the
// first declared wins.
func (r *Engine) Check(prog *syntax.File) (Rule, string, bool) {
	if r == nil || len(r.rules) == 0 {
		return Rule{}, "", false
	}

	var (
		matched Rule
		found   bool
	)

	syntax.Walk(prog, func(node syntax.Node) bool {
		if found {
			return false
		}

		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}

		for _, rule := range r.rules {
			if matchRule(call, rule) {
				matched = rule
				found = true

				return false
			}
		}

		return true
	})

	if !found {
		return Rule{}, "", false
	}

	return matched, matched.Reason, true
}

// matchRule reports whether call satisfies rule. See [Rule]
// for the matching semantics.
func matchRule(call *syntax.CallExpr, rule Rule) bool {
	if len(call.Args) < 1 {
		return false
	}

	parts0 := call.Args[0].Parts
	if len(parts0) != 1 {
		return false
	}

	lit0, ok := parts0[0].(*syntax.Lit)
	if !ok || lit0.Value != rule.Command {
		return false
	}

	// Phase 1 collects rule.Args (with git-specific flag skipping).
	// Phase 2 takes the very next call.Arg as the Except candidate
	// without flag skipping, so e.g. `git stash --keep-index pop`
	// denies because `--keep-index` is the candidate. A non-literal
	// arg ends collection; positionals collected so far still count.
	matched := 0
	skipNext := false
	idx := 0

	for ; idx < len(call.Args)-1; idx++ {
		arg := call.Args[idx+1]

		if matched == len(rule.Args) {
			break
		}

		if len(arg.Parts) != 1 {
			break
		}

		lit, ok := arg.Parts[0].(*syntax.Lit)
		if !ok {
			break
		}

		if skipNext {
			skipNext = false
			continue
		}

		if rule.Command == "git" && strings.HasPrefix(lit.Value, "-") {
			// `--flag=value` carries the value inline, so only
			// consume the next arg for the bare form.
			if gitFlagsTakingValue[lit.Value] {
				skipNext = true
			}

			continue
		}

		if lit.Value != rule.Args[matched] {
			return false
		}

		matched++
	}

	if matched < len(rule.Args) {
		return false
	}

	if len(rule.Except) == 0 {
		return true
	}

	// idx points at the slot after the last args literal
	// (idx == len(call.Args)-1 means we exhausted the call). Bare
	// `command + args` (no further args) ignores Except, since a bare
	// `git stash` is still a save form.
	candidateIdx := idx + 1
	if candidateIdx >= len(call.Args) {
		return true
	}

	cand := call.Args[candidateIdx]
	if len(cand.Parts) != 1 {
		return true
	}

	candLit, ok := cand.Parts[0].(*syntax.Lit)
	if !ok {
		return true
	}

	for _, exempt := range rule.Except {
		if candLit.Value == exempt {
			return false
		}
	}

	return true
}

// Parse decodes the JSON payload passed via --command-rules
// into a [*Engine]. Empty input yields an empty engine; malformed
// JSON or an unknown rule action returns an error so wrapper
// misconfiguration is loud. Unknown fields are silently dropped
// (lenient json.Unmarshal), matching the other hook-router JSON flag
// parsers.
func Parse(s string) (*Engine, error) {
	if s == "" {
		return New(nil), nil
	}

	var rules []Rule

	err := json.Unmarshal([]byte(s), &rules)
	if err != nil {
		return nil, fmt.Errorf("decoding command rules JSON: %w", err)
	}

	for i, rule := range rules {
		switch rule.Action {
		case "", actionDeny, actionAsk:
		default:
			return nil, fmt.Errorf("command rule %d (%s): unknown action %q", i, rule.Command, rule.Action)
		}
	}

	return New(rules), nil
}
