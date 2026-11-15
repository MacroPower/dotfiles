package mcprules

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	// TestBranchMatches holds when the field names a branch that the
	// regular expression in [Condition.Pattern] matches from start to
	// end. When the field is absent or an empty string, the test reads
	// the current branch of the git repository whose path the
	// [Condition.RepoField] field holds. The [BranchResolver] of the
	// ruleset resolves that branch, and the test fails when it does
	// not resolve.
	TestBranchMatches = "branchMatches"
	// TestTrue holds when the field is the boolean true.
	TestTrue = "true"
	// TestAbsentOrTrue holds when the field is absent or the boolean
	// true.
	TestAbsentOrTrue = "absentOrTrue"
	// TestAbsentOrZero holds when the field is absent, false, an empty
	// string, or an empty list.
	TestAbsentOrZero = "absentOrZero"
	// TestAbsent holds when the field is absent. Unlike
	// [TestAbsentOrZero], it fails for false, an empty string, and an
	// empty list, so it suits a field that changes something at every
	// value.
	TestAbsent = "absent"
	// TestPullRequestRecorded holds when the field is the number of a
	// pull request that the [PullRequestLookup] of the ruleset reports
	// as recorded. The number must be a whole number greater than
	// zero. The test reads the owner and the name of the repository
	// from the `owner` and `repo` fields of the same call, which must
	// hold non-empty strings. It fails when the ruleset has no lookup
	// and when the lookup returns an error.
	TestPullRequestRecorded = "pullRequestRecorded"
)

// Keys of tool_input that name the repository of a pull request for
// [TestPullRequestRecorded]. The pull request tools of the GitHub MCP
// server take the repository under these keys.
const (
	ownerKey = "owner"
	repoKey  = "repo"
)

// Condition is one test on a top-level tool_input field of an MCP tool
// call. Field names the key and Test names one of the Test constants.
// Pattern carries the regular expression of [TestMatches] and
// [TestBranchMatches] in Go syntax and stays empty for every other
// test. RepoField names the key that holds the repository path of
// [TestBranchMatches] and stays empty for every other test. JSON tags
// are camelCase because builtins.toJSON in home/claude.nix emits
// attribute names verbatim.
type Condition struct {
	Field     string `json:"field"`
	Test      string `json:"test"`
	Pattern   string `json:"pattern,omitempty"`
	RepoField string `json:"repoField,omitempty"`
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
// [TestMatches] and [TestBranchMatches] alone.
type condition struct {
	re *regexp.Regexp
	Condition
}

// NewRule builds the conditional allow [Rule] of tool. Every rejection
// wraps [ErrInvalidRule]. NewRule rejects a tool that is not the exact
// name of one tool, and a rule with no conditions. It rejects a
// condition that names no field, names an unknown test, or carries a
// pattern or a repo field that its test does not take. The pattern of
// [TestMatches] and of [TestBranchMatches] must be a non-empty regular
// expression that compiles, and [TestBranchMatches] must name a repo
// field.
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

// compile validates c and compiles the pattern of a [TestMatches] or
// [TestBranchMatches] condition. Both tests accept only a match that
// spans the whole value. The compiled expression prefers
// leftmost-longest matches, so it finds such a match whenever the
// value has one. A pattern with no anchors of its own therefore
// matches only a whole value. compile does not add anchors to the
// pattern text, because text such as an unterminated \Q quote swallows
// them.
func compile(c Condition) (condition, error) {
	if c.Field == "" {
		return condition{}, errors.New("field is empty")
	}

	var needsPattern, needsRepo bool

	switch c.Test {
	case TestMatches:
		needsPattern = true
	case TestBranchMatches:
		needsPattern, needsRepo = true, true
	case TestTrue, TestAbsentOrTrue, TestAbsentOrZero, TestAbsent, TestPullRequestRecorded:
	default:
		return condition{}, fmt.Errorf("field %s: unknown test %q", c.Field, c.Test)
	}

	switch {
	case needsPattern && c.Pattern == "":
		return condition{}, fmt.Errorf("field %s: test %q needs a pattern", c.Field, c.Test)
	case !needsPattern && c.Pattern != "":
		return condition{}, fmt.Errorf("field %s: test %q takes no pattern", c.Field, c.Test)
	case needsRepo && c.RepoField == "":
		return condition{}, fmt.Errorf("field %s: test %q needs a repo field", c.Field, c.Test)
	case !needsRepo && c.RepoField != "":
		return condition{}, fmt.Errorf("field %s: test %q takes no repo field", c.Field, c.Test)
	}

	if !needsPattern {
		return condition{Condition: c}, nil
	}

	re, err := regexp.Compile(c.Pattern)
	if err != nil {
		return condition{}, fmt.Errorf("field %s: compiling pattern: %w", c.Field, err)
	}

	re.Longest()

	return condition{Condition: c, re: re}, nil
}

// holds reports whether every condition of the rule holds for input.
// It stops at the first condition that fails. A [TestBranchMatches] or
// [TestPullRequestRecorded] condition therefore calls into src only
// when every condition before it holds.
func (r Rule) holds(ctx context.Context, input map[string]any, src sources) bool {
	for _, c := range r.conditions {
		if !c.holds(ctx, input, src) {
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

// holds reports whether the condition holds for input. src serves
// [TestBranchMatches] and [TestPullRequestRecorded] alone.
func (c condition) holds(ctx context.Context, input map[string]any, src sources) bool {
	value := input[c.Field]

	switch c.Test {
	case TestMatches:
		s, ok := value.(string)

		return ok && c.whole(s)

	case TestBranchMatches:
		branch, ok := c.branch(ctx, input, src.branch)

		return ok && c.whole(branch)

	case TestPullRequestRecorded:
		return c.recorded(ctx, input, src.pullRequest)

	case TestTrue:
		return value == true

	case TestAbsentOrTrue:
		return value == nil || value == true

	case TestAbsent:
		return value == nil

	default:
		return value == nil || zero(value)
	}
}

// whole reports whether the pattern of the condition matches s from
// start to end.
func (c condition) whole(s string) bool {
	loc := c.re.FindStringIndex(s)

	return len(loc) == 2 && loc[0] == 0 && loc[1] == len(s)
}

// branch returns the branch name that a [TestBranchMatches] condition
// tests. The field supplies the name when it holds one. When the field
// is absent or an empty string, resolve supplies the current branch of
// the repository that the repo field names. branch reports false when
// neither source yields a name.
func (c condition) branch(ctx context.Context, input map[string]any, resolve BranchResolver) (string, bool) {
	switch v := input[c.Field].(type) {
	case nil:
		// An absent field leaves the name to the repository.
	case string:
		if v != "" {
			return v, true
		}

	default:
		return "", false
	}

	repo, ok := input[c.RepoField].(string)
	if !ok || repo == "" || resolve == nil {
		return "", false
	}

	name, err := resolve(ctx, repo)
	if err != nil || name == "" {
		return "", false
	}

	return name, true
}

// recorded reports whether lookup has a record of the pull request
// that a [TestPullRequestRecorded] condition tests. The field supplies
// the number, and the [ownerKey] and [repoKey] keys of input supply the
// repository. recorded reports false when input does not name a pull
// request, when lookup is nil, and when lookup returns an error.
func (c condition) recorded(ctx context.Context, input map[string]any, lookup PullRequestLookup) bool {
	owner, ok := input[ownerKey].(string)
	if !ok || owner == "" {
		return false
	}

	repo, ok := input[repoKey].(string)
	if !ok || repo == "" {
		return false
	}

	number, ok := pullNumber(input[c.Field])
	if !ok || lookup == nil {
		return false
	}

	found, err := lookup(ctx, owner, repo, number)

	return err == nil && found
}

// pullNumber returns value as the number of a pull request. It accepts
// a JSON number that is whole and between 1 and [math.MaxInt32],
// inclusive.
func pullNumber(value any) (int, bool) {
	v, ok := value.(float64)
	if !ok || v < 1 || v > math.MaxInt32 || v != math.Trunc(v) {
		return 0, false
	}

	return int(v), true
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
	case TestBranchMatches:
		return fmt.Sprintf(
			"`%s`, or the current branch of `%s` when `%s` is empty, matches `%s`",
			c.Field, c.RepoField, c.Field, c.Pattern,
		)

	case TestTrue:
		return fmt.Sprintf("`%s` is true", c.Field)
	case TestAbsentOrTrue:
		return fmt.Sprintf("`%s` is absent or true", c.Field)
	case TestAbsent:
		return fmt.Sprintf("`%s` is absent", c.Field)
	case TestPullRequestRecorded:
		return fmt.Sprintf(
			"`%s` is a pull request of `%s`/`%s` that an agent opened from this host",
			c.Field, ownerKey, repoKey,
		)

	default:
		return fmt.Sprintf("`%s` is absent, false, or empty", c.Field)
	}
}
