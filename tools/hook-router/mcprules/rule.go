package mcprules

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrInvalidRule reports a conditional rule that [NewRule] rejects.
var ErrInvalidRule = errors.New("invalid conditional rule")

// Tests that a [Condition] applies to its field. A field whose value is
// JSON null counts as absent.
const (
	// TestMatches holds when the field is a string that the regular
	// expression in [Condition.Pattern] matches from start to end.
	TestMatches = "matches"
	// TestTrue holds when the field is the boolean true.
	TestTrue = "true"
	// TestAbsentOrTrue holds when the field is absent or the boolean
	// true.
	TestAbsentOrTrue = "absentOrTrue"
	// TestAbsentOrZero holds when the field is absent, false, an empty
	// string, or an empty list.
	TestAbsentOrZero = "absentOrZero"
)

// Condition is one test on a top-level tool_input field of an MCP tool
// call. Field names the key and Test names one of the Test constants.
// Pattern carries the regular expression of [TestMatches] in Go syntax
// and stays empty for every other test. JSON tags are camelCase because
// builtins.toJSON in home/claude.nix emits attribute names verbatim.
type Condition struct {
	Field   string `json:"field"`
	Test    string `json:"test"`
	Pattern string `json:"pattern,omitempty"`
}

// Rule is a conditional allow rule for one MCP tool. A call of the tool
// satisfies the rule when every one of its conditions holds. Build one
// with [NewRule], and hand it to a [*Ruleset] through [WithRules]. The
// zero value is not a rule, and [WithRules] drops it.
type Rule struct {
	tool       string
	conditions []condition
}

// condition is a [Condition] that [NewRule] accepted. re is set for
// [TestMatches] alone.
type condition struct {
	re *regexp.Regexp
	Condition
}

// NewRule builds the conditional allow [Rule] of tool. Every rejection
// wraps [ErrInvalidRule]. NewRule rejects a tool that is not the exact
// name of one tool, and a rule with no conditions. It rejects a
// condition that names no field, names an unknown test, or carries a
// pattern that its test does not take. A [TestMatches] pattern must be
// a non-empty regular expression that compiles.
func NewRule(tool string, conditions ...Condition) (Rule, error) {
	if !exactTool(tool) {
		return Rule{}, fmt.Errorf("%w: %q is not the exact name of one MCP tool", ErrInvalidRule, tool)
	}

	if len(conditions) == 0 {
		return Rule{}, fmt.Errorf("%w: %s has no conditions", ErrInvalidRule, tool)
	}

	compiled := make([]condition, 0, len(conditions))

	for i, c := range conditions {
		cond, err := compile(c)
		if err != nil {
			return Rule{}, fmt.Errorf("%w: %s condition %d: %w", ErrInvalidRule, tool, i, err)
		}

		compiled = append(compiled, cond)
	}

	return Rule{tool: tool, conditions: compiled}, nil
}

// exactTool reports whether name has the form mcp__<server>__<tool>
// with no glob, so that it names one tool.
func exactTool(name string) bool {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok || strings.Contains(name, "*") {
		return false
	}

	server, tool, ok := strings.Cut(rest, "__")

	return ok && server != "" && tool != ""
}

// compile validates c and compiles the pattern of a [TestMatches]
// condition. The holds method accepts only a match that spans the
// whole value. The compiled expression prefers leftmost-longest
// matches, so it finds such a match whenever the value has one. A
// pattern with no anchors of its own therefore matches only a whole
// value. compile does not add anchors to the pattern text, because
// text such as an unterminated \Q quote swallows them.
func compile(c Condition) (condition, error) {
	if c.Field == "" {
		return condition{}, errors.New("field is empty")
	}

	switch c.Test {
	case TestMatches:
		if c.Pattern == "" {
			return condition{}, fmt.Errorf("field %s: test %q needs a pattern", c.Field, c.Test)
		}

		re, err := regexp.Compile(c.Pattern)
		if err != nil {
			return condition{}, fmt.Errorf("field %s: compiling pattern: %w", c.Field, err)
		}

		re.Longest()

		return condition{Condition: c, re: re}, nil

	case TestTrue, TestAbsentOrTrue, TestAbsentOrZero:
		if c.Pattern != "" {
			return condition{}, fmt.Errorf("field %s: test %q takes no pattern", c.Field, c.Test)
		}

		return condition{Condition: c}, nil

	default:
		return condition{}, fmt.Errorf("field %s: unknown test %q", c.Field, c.Test)
	}
}

// holds reports whether every condition of the rule holds for input.
func (r Rule) holds(input map[string]any) bool {
	for _, c := range r.conditions {
		if !c.holds(input) {
			return false
		}
	}

	return true
}

// describe states the conditions of the rule as one clause, such as
// "`draft` is true and `head` matches `feat/.+`".
func (r Rule) describe() string {
	parts := make([]string, 0, len(r.conditions))
	for _, c := range r.conditions {
		parts = append(parts, c.describe())
	}

	return strings.Join(parts, " and ")
}

// holds reports whether the condition holds for input.
func (c condition) holds(input map[string]any) bool {
	value := input[c.Field]

	switch c.Test {
	case TestMatches:
		s, ok := value.(string)
		if !ok {
			return false
		}

		loc := c.re.FindStringIndex(s)

		return len(loc) == 2 && loc[0] == 0 && loc[1] == len(s)

	case TestTrue:
		return value == true

	case TestAbsentOrTrue:
		return value == nil || value == true

	default:
		return value == nil || zero(value)
	}
}

// zero reports whether value is false, an empty string, or an empty
// list.
func zero(value any) bool {
	switch v := value.(type) {
	case bool:
		return !v
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	default:
		return false
	}
}

// describe states the condition for the reason of a decision.
func (c condition) describe() string {
	switch c.Test {
	case TestMatches:
		return fmt.Sprintf("`%s` matches `%s`", c.Field, c.Pattern)
	case TestTrue:
		return fmt.Sprintf("`%s` is true", c.Field)
	case TestAbsentOrTrue:
		return fmt.Sprintf("`%s` is absent or true", c.Field)
	default:
		return fmt.Sprintf("`%s` is absent, false, or empty", c.Field)
	}
}
