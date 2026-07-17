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

// Ruleset is the allow/ask/deny matcher for PreToolUse:MCP. Construct
// with [New] or [Parse]; both return a non-nil zero value for empty
// input so callers can invoke [*Ruleset.Match] without nil guards.
//
// Lists are evaluated deny first, then ask, then allow, and the first
// matching pattern wins, mirroring Claude Code's own permission-rule
// precedence.
type Ruleset struct {
	deny  []string
	ask   []string
	allow []string
}

// New builds a [*Ruleset] from the given pattern lists. Nil or empty
// slices yield a ruleset that matches nothing.
func New(allow, ask, deny []string) *Ruleset {
	return &Ruleset{deny: deny, ask: ask, allow: allow}
}

// Empty reports whether the ruleset has no patterns in any list. A nil
// receiver reports true, so callers can treat nil and zero-pattern
// rulesets identically (mirrors [*Ruleset.Match]).
func (r *Ruleset) Empty() bool {
	if r == nil {
		return true
	}

	return len(r.deny) == 0 && len(r.ask) == 0 && len(r.allow) == 0
}

// Match resolves an MCP tool name against the deny, ask, and allow
// lists in that order and returns the decision along with the pattern
// that matched. A nil receiver or an unmatched tool returns matched ==
// false, signalling the caller to fall through to the normal
// permission flow.
func (r *Ruleset) Match(tool string) (decision, pattern string, matched bool) {
	if r == nil {
		return "", "", false
	}

	groups := []struct {
		decision string
		patterns []string
	}{
		{DecisionDeny, r.deny},
		{DecisionAsk, r.ask},
		{DecisionAllow, r.allow},
	}

	for _, group := range groups {
		for _, p := range group.patterns {
			if matchPattern(p, tool) {
				return group.decision, p, true
			}
		}
	}

	return "", "", false
}

// matchPattern reports whether tool satisfies pattern. Three forms are
// supported, mirroring Claude Code's MCP permission-rule syntax plus a
// glob extension: a trailing-* pattern prefix-matches the tool name; a
// bare server pattern (no tool segment after the server name, e.g.
// "mcp__github") matches every tool on that server; anything else
// matches exactly.
func matchPattern(pattern, tool string) bool {
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
// [*Ruleset]. Empty input yields an empty ruleset; malformed JSON
// returns an error so wrapper misconfiguration is loud. Unknown fields
// are silently dropped (lenient json.Unmarshal), matching the other
// hook-router JSON flag parsers.
func Parse(s string) (*Ruleset, error) {
	if s == "" {
		return New(nil, nil, nil), nil
	}

	var lists struct {
		Allow []string `json:"allow"`
		Ask   []string `json:"ask"`
		Deny  []string `json:"deny"`
	}

	err := json.Unmarshal([]byte(s), &lists)
	if err != nil {
		return nil, fmt.Errorf("decoding mcp rules JSON: %w", err)
	}

	return New(lists.Allow, lists.Ask, lists.Deny), nil
}
