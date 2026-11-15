package mcprules

import (
	"context"
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
// and deny pattern lists, the conditional allow rules, and the
// [BranchResolver] and [PullRequestLookup] that those rules call.
// Construct with [New] or [Parse]. Both return a non-nil ruleset for
// empty input, so callers can invoke [*Ruleset.Match] without nil
// guards.
//
// The deny list decides first. A tool with one or more conditional
// rules then gets its decision from those rules. The ask and allow
// lists do not apply to it. Every other tool resolves through the ask
// list and then the allow list. Within a list the first matching
// pattern wins. The order of the lists mirrors Claude Code's own
// permission-rule precedence.
type Ruleset struct {
	src   sources
	deny  []string
	ask   []string
	allow []string
	rules []Rule
}

// sources holds the functions that conditions call to read state from
// outside a tool call. A nil function fails every condition that calls
// it.
type sources struct {
	branch      BranchResolver
	pullRequest PullRequestLookup
}

// BranchResolver returns the short name of the current branch of the
// git repository at the path repo, such as "feat/x". It returns an
// error when it cannot name that branch, for example because HEAD is
// detached or the path holds no repository.
type BranchResolver func(ctx context.Context, repo string) (string, error)

// PullRequestLookup reports whether the caller holds a record of the
// pull request with the given number in the repository owner/repo. The
// record lists the pull requests that agents opened, so a
// [TestPullRequestRecorded] condition can tell them from every other
// pull request. The lookup must compare owner and repo without regard
// to case, because GitHub does. It returns an error when it cannot
// read the record.
type PullRequestLookup func(ctx context.Context, owner, repo string, number int) (bool, error)

// Option configures a [*Ruleset] that [New] or [Parse] builds.
//
//   - [WithRules]
//   - [WithBranchResolver]
//   - [WithPullRequestLookup]
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

// WithBranchResolver sets the [BranchResolver] that a
// [TestBranchMatches] condition calls when its field names no branch.
// Without a resolver, such a condition fails for every call that names
// no branch, so the call resolves to ask. See [Option].
func WithBranchResolver(resolve BranchResolver) Option {
	return func(r *Ruleset) {
		r.src.branch = resolve
	}
}

// WithPullRequestLookup sets the [PullRequestLookup] that a
// [TestPullRequestRecorded] condition calls. Without a lookup, such a
// condition fails for every call, so the call resolves to ask. See
// [Option].
func WithPullRequestLookup(lookup PullRequestLookup) Option {
	return func(r *Ruleset) {
		r.src.pullRequest = lookup
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
// the normal permission flow. ctx bounds the [BranchResolver] call of a
// [TestBranchMatches] condition and the [PullRequestLookup] call of a
// [TestPullRequestRecorded] condition.
func (r *Ruleset) Match(ctx context.Context, tool string, input map[string]any) (Result, bool) {
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

		if rule.holds(ctx, input, r.src) {
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
// [*Ruleset] and applies opts to it. Empty input yields a ruleset with
// no patterns and no rules. Parse returns an error for malformed JSON
// and for a conditional rule that [NewRule] rejects, such as one whose
// regular expression does not compile. The error makes wrapper
// misconfiguration loud. Like the other hook-router JSON flag parsers,
// Parse drops unknown fields without a report (lenient json.Unmarshal).
func Parse(s string, opts ...Option) (*Ruleset, error) {
	if s == "" {
		return New(nil, nil, nil, opts...), nil
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

	return New(payload.Allow, payload.Ask, payload.Deny, append([]Option{WithRules(rules...)}, opts...)...), nil
}
