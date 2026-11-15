package mcprules

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Decisions returned by [*Ruleset.Match], in evaluation order.
const (
	DecisionDeny  = "deny"
	DecisionAsk   = "ask"
	DecisionAllow = "allow"
)

// Ruleset is the matcher for PreToolUse:MCP. It holds the allow, ask,
// and deny pattern lists and the conditional allow rules. Construct
// with [New] or [Parse]. Both return a non-nil zero value for empty
// input, so callers can invoke [*Ruleset.Match] without nil guards.
//
// The deny list decides first. A tool with one or more conditional
// rules then gets its decision from those rules. The ask and allow
// lists do not apply to it. Every other tool resolves through the ask
// list and then the allow list. Within a list the first matching
// pattern wins. The order of the lists mirrors Claude Code's own
// permission-rule precedence.
type Ruleset struct {
	deny  []string
	ask   []string
	allow []string
	rules []Rule
}

// Option configures a [*Ruleset] that [New] builds.
//
//   - [WithRules]
type Option func(*Ruleset)

// WithRules adds conditional allow rules to the ruleset. Rules that
// name the same tool are alternatives, so a call that satisfies one of
// them resolves to allow. It drops a zero [Rule], which has no
// conditions and would allow every call. See [Option].
func WithRules(rules ...Rule) Option {
	return func(r *Ruleset) {
		for _, rule := range rules {
			if len(rule.conditions) > 0 {
				r.rules = append(r.rules, rule)
			}
		}
	}
}

// New builds a [*Ruleset] from the given pattern lists and options. Nil
// or empty slices with no options yield a ruleset that matches nothing.
func New(allow, ask, deny []string, opts ...Option) *Ruleset {
	r := &Ruleset{deny: deny, ask: ask, allow: allow}
	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Empty reports whether the ruleset has no patterns in any list and no
// conditional rules. A nil receiver reports true, so callers can treat
// nil and zero-pattern rulesets identically (mirrors
// [*Ruleset.Match]).
func (r *Ruleset) Empty() bool {
	if r == nil {
		return true
	}

	return len(r.deny) == 0 && len(r.ask) == 0 && len(r.allow) == 0 && len(r.rules) == 0
}

// Result is the decision of [*Ruleset.Match] for one tool call. When
// [*Ruleset.Match] reports a match, Decision is one of the Decision
// constants and exactly one of Pattern and Conditions is set. An
// unmatched call returns the zero Result. Pattern is the list pattern
// that matched the tool name. Conditions describes the conditional
// rules that decided the call. On an allow, it states the conditions of
// the rule that the call satisfied. On an ask, it states the conditions
// of each rule of the tool, one of which a call must satisfy to resolve
// to allow.
type Result struct {
	Decision   string
	Pattern    string
	Conditions string
}

// Match resolves a call of an MCP tool, with its tool_input, to a
// decision. A deny pattern that matches the tool denies the call. A
// tool with conditional rules resolves to allow when input satisfies
// one of them and to ask otherwise. Every other tool resolves through
// the ask and allow lists in that order. A nil receiver or an unmatched
// tool returns matched == false, and the caller then falls through to
// the normal permission flow.
func (r *Ruleset) Match(tool string, input map[string]any) (Result, bool) {
	if r == nil {
		return Result{}, false
	}

	if p, ok := matchList(r.deny, tool); ok {
		return Result{Decision: DecisionDeny, Pattern: p}, true
	}

	var alternatives []string

	for _, rule := range r.rules {
		if rule.tool != tool {
			continue
		}

		if rule.holds(input) {
			return Result{Decision: DecisionAllow, Conditions: rule.describe()}, true
		}

		alternatives = append(alternatives, rule.describe())
	}

	if len(alternatives) > 0 {
		return Result{Decision: DecisionAsk, Conditions: strings.Join(alternatives, ", or when ")}, true
	}

	if p, ok := matchList(r.ask, tool); ok {
		return Result{Decision: DecisionAsk, Pattern: p}, true
	}

	if p, ok := matchList(r.allow, tool); ok {
		return Result{Decision: DecisionAllow, Pattern: p}, true
	}

	return Result{}, false
}

// matchList returns the first pattern in patterns that tool satisfies.
func matchList(patterns []string, tool string) (string, bool) {
	for _, p := range patterns {
		if MatchPattern(p, tool) {
			return p, true
		}
	}

	return "", false
}

// MatchPattern reports whether tool satisfies pattern. The pattern
// syntax mirrors Claude Code's MCP permission rules, plus a glob form:
//
//   - A pattern ending in * matches every tool name with the prefix
//     before it.
//   - A bare server pattern, with no tool segment after the server
//     name (such as "mcp__github"), matches every tool on that server.
//   - Any other pattern matches one tool name exactly.
func MatchPattern(pattern, tool string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(tool, prefix)
	}

	if pattern == tool {
		return true
	}

	if !strings.Contains(strings.TrimPrefix(pattern, "mcp__"), "__") {
		return strings.HasPrefix(tool, pattern+"__")
	}

	return false
}

// Parse decodes the JSON payload passed via --mcp-rules into a
// [*Ruleset]. Empty input yields an empty ruleset. Parse returns an
// error for malformed JSON and for a conditional rule that [NewRule]
// rejects, such as one whose regular expression does not compile. The
// error makes wrapper misconfiguration loud. Like the other hook-router
// JSON flag parsers, Parse drops unknown fields without a report
// (lenient json.Unmarshal).
func Parse(s string) (*Ruleset, error) {
	if s == "" {
		return New(nil, nil, nil), nil
	}

	var payload struct {
		Allow       []string `json:"allow"`
		Ask         []string `json:"ask"`
		Deny        []string `json:"deny"`
		Conditional []struct {
			Tool       string      `json:"tool"`
			Conditions []Condition `json:"conditions"`
		} `json:"conditional"`
	}

	err := json.Unmarshal([]byte(s), &payload)
	if err != nil {
		return nil, fmt.Errorf("decoding mcp rules JSON: %w", err)
	}

	rules := make([]Rule, 0, len(payload.Conditional))

	for i, c := range payload.Conditional {
		rule, err := NewRule(c.Tool, c.Conditions...)
		if err != nil {
			return nil, fmt.Errorf("conditional rule %d: %w", i, err)
		}

		rules = append(rules, rule)
	}

	return New(payload.Allow, payload.Ask, payload.Deny, WithRules(rules...)), nil
}
