package mcprules_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/mcprules"
)

func TestParse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err       error
		input     string
		wantEmpty bool
		wantErr   bool
	}{
		"empty string yields empty ruleset": {
			input:     "",
			wantEmpty: true,
		},
		"all-empty object yields empty ruleset": {
			input:     `{"allow":[],"ask":[],"deny":[],"conditional":[]}`,
			wantEmpty: true,
		},
		"valid object": {
			input: `{"allow":["mcp__fetch__fetch"],"ask":["mcp__spacelift__trigger_stack_run"],"deny":["mcp__spacelift__list_api_keys"]}`,
		},
		"conditional rules alone are not empty": {
			input: `{"conditional":[{"tool":"mcp__git__git_push","conditions":[{"field":"tags","test":"absentOrZero","pattern":""}]}]}`,
		},
		"unknown top-level field dropped": {
			input: `{"allow":["mcp__fetch__fetch"],"bogus":true}`,
		},
		"malformed JSON": {
			input:   `{"allow":`,
			wantErr: true,
		},
		"regular expression that does not compile": {
			input: `{"conditional":[{"tool":"mcp__git__git_push","conditions":[{"field":"ref","test":"matches","pattern":"(feat"}]}]}`,
			err:   mcprules.ErrInvalidRule,
		},
		"conditional rule with an unknown test": {
			input: `{"conditional":[{"tool":"mcp__git__git_push","conditions":[{"field":"ref","test":"equals"}]}]}`,
			err:   mcprules.ErrInvalidRule,
		},
		"conditional rule with no conditions": {
			input: `{"conditional":[{"tool":"mcp__git__git_push"}]}`,
			err:   mcprules.ErrInvalidRule,
		},
		"branch condition with a repo field": {
			input: `{"conditional":[{"tool":"mcp__git__git_push","conditions":[{"field":"ref","test":"branchMatches","pattern":"feat/.+","repoField":"repo"}]}]}`,
		},
		"branch condition with an empty repo field": {
			input: `{"conditional":[{"tool":"mcp__git__git_push","conditions":[{"field":"ref","test":"branchMatches","pattern":"feat/.+","repoField":""}]}]}`,
			err:   mcprules.ErrInvalidRule,
		},
		"empty repo field on a test that takes none": {
			input: `{"conditional":[{"tool":"mcp__git__git_push","conditions":[{"field":"tags","test":"absentOrZero","pattern":"","repoField":""}]}]}`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rs, err := mcprules.Parse(tc.input)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantEmpty, rs.Empty())
		})
	}
}

// TestParseConditional checks that the rules of a parsed payload decide
// a call the way the same rules built with [mcprules.NewRule] do.
func TestParseConditional(t *testing.T) {
	t.Parallel()

	rs, err := mcprules.Parse(`{
		"deny": ["mcp__github__merge_pull_request"],
		"conditional": [
			{
				"tool": "mcp__github__create_pull_request",
				"conditions": [
					{"field": "draft", "test": "true"},
					{"field": "head", "test": "matches", "pattern": "(feat|fix)/.+"},
					{"field": "reviewers", "test": "absentOrZero"}
				]
			},
			{
				"tool": "mcp__github__merge_pull_request",
				"conditions": [{"field": "draft", "test": "absentOrTrue"}]
			}
		]
	}`)
	require.NoError(t, err)

	got, matched := rs.Match(
		t.Context(),
		"mcp__github__create_pull_request",
		map[string]any{"draft": true, "head": "feat/x"},
	)
	require.True(t, matched)
	assert.Equal(t, mcprules.Result{
		Decision:   mcprules.DecisionAllow,
		Conditions: "`draft` is true and `head` matches `(feat|fix)/.+` and `reviewers` is absent, false, or empty",
	}, got)

	got, matched = rs.Match(t.Context(), "mcp__github__create_pull_request", map[string]any{"head": "feat/x"})
	require.True(t, matched)
	assert.Equal(t, mcprules.DecisionAsk, got.Decision)

	got, matched = rs.Match(t.Context(), "mcp__github__merge_pull_request", map[string]any{})
	require.True(t, matched)
	assert.Equal(t, mcprules.Result{
		Decision: mcprules.DecisionDeny,
		Pattern:  "mcp__github__merge_pull_request",
	}, got)
}

func TestNewRule(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err        error
		tool       string
		conditions []mcprules.Condition
	}{
		"each test with a field": {
			tool: "mcp__git__git_push",
			conditions: []mcprules.Condition{
				{Field: "ref", Test: mcprules.TestMatches, Pattern: "^(feat|fix)/.+$"},
				{Field: "ref", Test: mcprules.TestBranchMatches, Pattern: "^(feat|fix)/.+$", RepoField: "repo"},
				{Field: "set_upstream", Test: mcprules.TestTrue},
				{Field: "force_with_lease", Test: mcprules.TestAbsentOrTrue},
				{Field: "tags", Test: mcprules.TestAbsentOrZero},
				{Field: "remote", Test: mcprules.TestAbsent},
				{Field: "pullNumber", Test: mcprules.TestPullRequestRecorded},
			},
		},
		"pattern on absent": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "remote", Test: mcprules.TestAbsent, Pattern: "origin"}},
			err:        mcprules.ErrInvalidRule,
		},
		"pattern on pullRequestRecorded": {
			tool: "mcp__github__update_pull_request",
			conditions: []mcprules.Condition{
				{Field: "pullNumber", Test: mcprules.TestPullRequestRecorded, Pattern: "[0-9]+"},
			},
			err: mcprules.ErrInvalidRule,
		},
		"repo field on pullRequestRecorded": {
			tool: "mcp__github__update_pull_request",
			conditions: []mcprules.Condition{
				{Field: "pullNumber", Test: mcprules.TestPullRequestRecorded, RepoField: "repo"},
			},
			err: mcprules.ErrInvalidRule,
		},
		"pattern that does not compile": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "ref", Test: mcprules.TestMatches, Pattern: "(feat"}},
			err:        mcprules.ErrInvalidRule,
		},
		"pattern with an unbalanced closing parenthesis": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "ref", Test: mcprules.TestMatches, Pattern: "feat/.+)|(.*"}},
			err:        mcprules.ErrInvalidRule,
		},
		"pattern that ends inside a quote": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "ref", Test: mcprules.TestMatches, Pattern: `\Qfeat/`}},
		},
		"matches without a pattern": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "ref", Test: mcprules.TestMatches}},
			err:        mcprules.ErrInvalidRule,
		},
		"pattern on a test that takes none": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "tags", Test: mcprules.TestTrue, Pattern: "true"}},
			err:        mcprules.ErrInvalidRule,
		},
		"branchMatches without a pattern": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "ref", Test: mcprules.TestBranchMatches, RepoField: "repo"}},
			err:        mcprules.ErrInvalidRule,
		},
		"branchMatches without a repo field": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "ref", Test: mcprules.TestBranchMatches, Pattern: "feat/.+"}},
			err:        mcprules.ErrInvalidRule,
		},
		"branchMatches with a pattern that does not compile": {
			tool: "mcp__git__git_push",
			conditions: []mcprules.Condition{
				{Field: "ref", Test: mcprules.TestBranchMatches, Pattern: "(feat", RepoField: "repo"},
			},
			err: mcprules.ErrInvalidRule,
		},
		"repo field on matches": {
			tool: "mcp__git__git_push",
			conditions: []mcprules.Condition{
				{Field: "ref", Test: mcprules.TestMatches, Pattern: "feat/.+", RepoField: "repo"},
			},
			err: mcprules.ErrInvalidRule,
		},
		"repo field on a test that takes no operand": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "tags", Test: mcprules.TestAbsentOrZero, RepoField: "repo"}},
			err:        mcprules.ErrInvalidRule,
		},
		"unknown test": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "tags", Test: "false"}},
			err:        mcprules.ErrInvalidRule,
		},
		"missing test": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Field: "tags"}},
			err:        mcprules.ErrInvalidRule,
		},
		"missing field": {
			tool:       "mcp__git__git_push",
			conditions: []mcprules.Condition{{Test: mcprules.TestTrue}},
			err:        mcprules.ErrInvalidRule,
		},
		"no conditions": {
			tool: "mcp__git__git_push",
			err:  mcprules.ErrInvalidRule,
		},
		"empty tool": {
			conditions: []mcprules.Condition{{Field: "tags", Test: mcprules.TestTrue}},
			err:        mcprules.ErrInvalidRule,
		},
		"bare server as tool": {
			tool:       "mcp__git",
			conditions: []mcprules.Condition{{Field: "tags", Test: mcprules.TestTrue}},
			err:        mcprules.ErrInvalidRule,
		},
		"tool with an empty tool segment": {
			tool:       "mcp__git__",
			conditions: []mcprules.Condition{{Field: "tags", Test: mcprules.TestTrue}},
			err:        mcprules.ErrInvalidRule,
		},
		"tool with an empty server segment": {
			tool:       "mcp____git_push",
			conditions: []mcprules.Condition{{Field: "tags", Test: mcprules.TestTrue}},
			err:        mcprules.ErrInvalidRule,
		},
		"glob as tool": {
			tool:       "mcp__git__git_*",
			conditions: []mcprules.Condition{{Field: "tags", Test: mcprules.TestTrue}},
			err:        mcprules.ErrInvalidRule,
		},
		"tool without the mcp prefix": {
			tool:       "Bash",
			conditions: []mcprules.Condition{{Field: "command", Test: mcprules.TestTrue}},
			err:        mcprules.ErrInvalidRule,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := mcprules.NewRule(tc.tool, tc.conditions...)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestRulesetMatch(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		allow        []string
		ask          []string
		deny         []string
		tool         string
		wantDecision string
		wantPattern  string
		wantMatched  bool
	}{
		"allow exact match": {
			allow:        []string{"mcp__github__search_code"},
			tool:         "mcp__github__search_code",
			wantDecision: mcprules.DecisionAllow,
			wantPattern:  "mcp__github__search_code",
			wantMatched:  true,
		},
		"ask exact match": {
			ask:          []string{"mcp__spacelift__trigger_stack_run"},
			tool:         "mcp__spacelift__trigger_stack_run",
			wantDecision: mcprules.DecisionAsk,
			wantPattern:  "mcp__spacelift__trigger_stack_run",
			wantMatched:  true,
		},
		"deny exact match": {
			deny:         []string{"mcp__spacelift__list_api_keys"},
			tool:         "mcp__spacelift__list_api_keys",
			wantDecision: mcprules.DecisionDeny,
			wantPattern:  "mcp__spacelift__list_api_keys",
			wantMatched:  true,
		},
		"deny wins over ask and allow": {
			allow:        []string{"mcp__x__y"},
			ask:          []string{"mcp__x__y"},
			deny:         []string{"mcp__x__y"},
			tool:         "mcp__x__y",
			wantDecision: mcprules.DecisionDeny,
			wantPattern:  "mcp__x__y",
			wantMatched:  true,
		},
		"ask wins over allow": {
			allow:        []string{"mcp__x__y"},
			ask:          []string{"mcp__x__y"},
			tool:         "mcp__x__y",
			wantDecision: mcprules.DecisionAsk,
			wantPattern:  "mcp__x__y",
			wantMatched:  true,
		},
		"bare server matches its tools": {
			allow:        []string{"mcp__github"},
			tool:         "mcp__github__search_code",
			wantDecision: mcprules.DecisionAllow,
			wantPattern:  "mcp__github",
			wantMatched:  true,
		},
		"bare server does not match other servers": {
			allow: []string{"mcp__github"},
			tool:  "mcp__git__git_clone",
		},
		"glob matches prefix": {
			allow:        []string{"mcp__github__*"},
			tool:         "mcp__github__search_code",
			wantDecision: mcprules.DecisionAllow,
			wantPattern:  "mcp__github__*",
			wantMatched:  true,
		},
		"glob does not match other prefix": {
			allow: []string{"mcp__github__*"},
			tool:  "mcp__git__git_clone",
		},
		"unmatched tool": {
			allow: []string{"mcp__fetch__fetch"},
			tool:  "mcp__leanspec__view",
		},
		"exact pattern is not a prefix": {
			allow: []string{"mcp__github__search"},
			tool:  "mcp__github__search_code",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rs := mcprules.New(tc.allow, tc.ask, tc.deny)

			got, matched := rs.Match(t.Context(), tc.tool, map[string]any{"ref": "main"})
			assert.Equal(t, tc.wantMatched, matched)
			assert.Equal(t, mcprules.Result{Decision: tc.wantDecision, Pattern: tc.wantPattern}, got)
		})
	}
}

// mustRule builds a conditional rule that the test knows is valid.
func mustRule(t *testing.T, tool string, conditions ...mcprules.Condition) mcprules.Rule {
	t.Helper()

	rule, err := mcprules.NewRule(tool, conditions...)
	require.NoError(t, err)

	return rule
}

// TestConditions checks each test of a [mcprules.Condition] against the
// values that a tool_input field can hold.
func TestConditions(t *testing.T) {
	t.Parallel()

	const tool = "mcp__git__git_push"

	matches := mcprules.Condition{Field: "v", Test: mcprules.TestMatches, Pattern: "(feat|fix)/.+"}
	isTrue := mcprules.Condition{Field: "v", Test: mcprules.TestTrue}
	absentOrTrue := mcprules.Condition{Field: "v", Test: mcprules.TestAbsentOrTrue}
	absentOrZero := mcprules.Condition{Field: "v", Test: mcprules.TestAbsentOrZero}
	absent := mcprules.Condition{Field: "v", Test: mcprules.TestAbsent}

	cases := map[string]struct {
		input     map[string]any
		condition mcprules.Condition
		want      bool
	}{
		"absent: absent field": {
			condition: absent,
			input:     map[string]any{"other": "x"},
			want:      true,
		},
		"absent: nil input": {
			condition: absent,
			want:      true,
		},
		"absent: null": {
			condition: absent,
			input:     map[string]any{"v": nil},
			want:      true,
		},
		"absent: false": {
			condition: absent,
			input:     map[string]any{"v": false},
		},
		"absent: true": {
			condition: absent,
			input:     map[string]any{"v": true},
		},
		"absent: empty string": {
			condition: absent,
			input:     map[string]any{"v": ""},
		},
		"absent: string with text": {
			condition: absent,
			input:     map[string]any{"v": "main"},
		},
		"absent: empty list": {
			condition: absent,
			input:     map[string]any{"v": []any{}},
		},
		"absent: the number zero": {
			condition: absent,
			input:     map[string]any{"v": float64(0)},
		},
		"absent: empty object": {
			condition: absent,
			input:     map[string]any{"v": map[string]any{}},
		},
		"matches: string that the pattern matches": {
			condition: matches,
			input:     map[string]any{"v": "feat/x"},
			want:      true,
		},
		"matches: string that the pattern rejects": {
			condition: matches,
			input:     map[string]any{"v": "main"},
		},
		"matches: pattern must match from the start": {
			condition: matches,
			input:     map[string]any{"v": "someone:feat/x"},
		},
		"matches: pattern must match to the end": {
			condition: mcprules.Condition{Field: "v", Test: mcprules.TestMatches, Pattern: "feat|fix"},
			input:     map[string]any{"v": "fix/y"},
		},
		"matches: alternation without a group matches each whole branch": {
			condition: mcprules.Condition{Field: "v", Test: mcprules.TestMatches, Pattern: "feat|fix"},
			input:     map[string]any{"v": "fix"},
			want:      true,
		},
		"matches: value with a second line": {
			condition: matches,
			input:     map[string]any{"v": "feat/x\nmain"},
		},
		"matches: pattern with its own anchors": {
			condition: mcprules.Condition{Field: "v", Test: mcprules.TestMatches, Pattern: "^(feat|fix)/.+$"},
			input:     map[string]any{"v": "fix/y"},
			want:      true,
		},
		"matches: alternation whose first branch is a prefix of the value": {
			condition: mcprules.Condition{Field: "v", Test: mcprules.TestMatches, Pattern: "feat|feature"},
			input:     map[string]any{"v": "feature"},
			want:      true,
		},
		"matches: lazy quantifier still matches the whole value": {
			condition: mcprules.Condition{Field: "v", Test: mcprules.TestMatches, Pattern: "feat/.+?"},
			input:     map[string]any{"v": "feat/xyz"},
			want:      true,
		},
		"matches: pattern that ends inside a quote": {
			condition: mcprules.Condition{Field: "v", Test: mcprules.TestMatches, Pattern: `\Qfeat/`},
			input:     map[string]any{"v": "feat/"},
			want:      true,
		},
		"matches: empty string that the pattern matches": {
			condition: mcprules.Condition{Field: "v", Test: mcprules.TestMatches, Pattern: "x?"},
			input:     map[string]any{"v": ""},
			want:      true,
		},
		"matches: quoted pattern must match to the end": {
			condition: mcprules.Condition{Field: "v", Test: mcprules.TestMatches, Pattern: `\Qfeat/`},
			input:     map[string]any{"v": "feat/x"},
		},
		"matches: empty string": {
			condition: matches,
			input:     map[string]any{"v": ""},
		},
		"matches: absent field": {
			condition: matches,
			input:     map[string]any{},
		},
		"matches: null field": {
			condition: matches,
			input:     map[string]any{"v": nil},
		},
		"matches: value that is not a string": {
			condition: matches,
			input:     map[string]any{"v": []any{"feat/x"}},
		},
		"matches: nil input": {
			condition: matches,
		},
		"true: true": {
			condition: isTrue,
			input:     map[string]any{"v": true},
			want:      true,
		},
		"true: false": {
			condition: isTrue,
			input:     map[string]any{"v": false},
		},
		"true: absent field": {
			condition: isTrue,
			input:     map[string]any{},
		},
		"true: null field": {
			condition: isTrue,
			input:     map[string]any{"v": nil},
		},
		"true: the string true": {
			condition: isTrue,
			input:     map[string]any{"v": "true"},
		},
		"true: the number one": {
			condition: isTrue,
			input:     map[string]any{"v": float64(1)},
		},
		"absentOrTrue: true": {
			condition: absentOrTrue,
			input:     map[string]any{"v": true},
			want:      true,
		},
		"absentOrTrue: absent field": {
			condition: absentOrTrue,
			input:     map[string]any{"other": false},
			want:      true,
		},
		"absentOrTrue: null field": {
			condition: absentOrTrue,
			input:     map[string]any{"v": nil},
			want:      true,
		},
		"absentOrTrue: nil input": {
			condition: absentOrTrue,
			want:      true,
		},
		"absentOrTrue: false": {
			condition: absentOrTrue,
			input:     map[string]any{"v": false},
		},
		"absentOrTrue: the string true": {
			condition: absentOrTrue,
			input:     map[string]any{"v": "true"},
		},
		"absentOrTrue: empty string": {
			condition: absentOrTrue,
			input:     map[string]any{"v": ""},
		},
		"absentOrZero: absent field": {
			condition: absentOrZero,
			input:     map[string]any{"other": true},
			want:      true,
		},
		"absentOrZero: null field": {
			condition: absentOrZero,
			input:     map[string]any{"v": nil},
			want:      true,
		},
		"absentOrZero: nil input": {
			condition: absentOrZero,
			want:      true,
		},
		"absentOrZero: false": {
			condition: absentOrZero,
			input:     map[string]any{"v": false},
			want:      true,
		},
		"absentOrZero: empty string": {
			condition: absentOrZero,
			input:     map[string]any{"v": ""},
			want:      true,
		},
		"absentOrZero: empty list": {
			condition: absentOrZero,
			input:     map[string]any{"v": []any{}},
			want:      true,
		},
		"absentOrZero: true": {
			condition: absentOrZero,
			input:     map[string]any{"v": true},
		},
		"absentOrZero: string with text": {
			condition: absentOrZero,
			input:     map[string]any{"v": "main"},
		},
		"absentOrZero: string of a space": {
			condition: absentOrZero,
			input:     map[string]any{"v": " "},
		},
		"absentOrZero: list with an item": {
			condition: absentOrZero,
			input:     map[string]any{"v": []any{"octocat"}},
		},
		"absentOrZero: list with an empty item": {
			condition: absentOrZero,
			input:     map[string]any{"v": []any{""}},
		},
		"absentOrZero: the number zero": {
			condition: absentOrZero,
			input:     map[string]any{"v": float64(0)},
		},
		"absentOrZero: empty object": {
			condition: absentOrZero,
			input:     map[string]any{"v": map[string]any{}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rs := mcprules.New(nil, nil, nil, mcprules.WithRules(mustRule(t, tool, tc.condition)))

			got, matched := rs.Match(t.Context(), tool, tc.input)
			require.True(t, matched, "a tool with a conditional rule always gets a decision")

			want := mcprules.DecisionAsk
			if tc.want {
				want = mcprules.DecisionAllow
			}

			assert.Equal(t, want, got.Decision)
			assert.Empty(t, got.Pattern)
			assert.NotEmpty(t, got.Conditions)
		})
	}
}

// TestBranchMatches checks the [mcprules.TestBranchMatches] test
// against the values of its field, the values of its repo field, and
// the answers of the [mcprules.BranchResolver].
func TestBranchMatches(t *testing.T) {
	t.Parallel()

	const tool = "mcp__git__git_push"

	errUnresolved := errors.New("unresolved")

	// branches maps a repository path to its current branch. A path
	// that the map does not hold resolves to an error.
	branches := map[string]string{
		"/feat":     "feat/x",
		"/main":     "main",
		"/unnamed":  "",
		"/prefixed": "someone/feat/x",
	}

	branch := mcprules.Condition{
		Field:     "ref",
		Test:      mcprules.TestBranchMatches,
		Pattern:   "(feat|fix)/.+",
		RepoField: "repo",
	}
	noTags := mcprules.Condition{Field: "tags", Test: mcprules.TestAbsentOrZero}

	cases := map[string]struct {
		input        map[string]any
		conditions   []mcprules.Condition
		wantResolved []string
		noResolver   bool
		want         bool
	}{
		"field names a branch that the pattern matches": {
			input: map[string]any{"repo": "/main", "ref": "fix/y"},
			want:  true,
		},
		"field names a branch that the pattern rejects": {
			input: map[string]any{"repo": "/feat", "ref": "main"},
		},
		"field must match from the start": {
			input: map[string]any{"repo": "/feat", "ref": "someone/feat/x"},
		},
		"field is absent and the current branch matches": {
			input:        map[string]any{"repo": "/feat"},
			wantResolved: []string{"/feat"},
			want:         true,
		},
		"field is null and the current branch matches": {
			input:        map[string]any{"repo": "/feat", "ref": nil},
			wantResolved: []string{"/feat"},
			want:         true,
		},
		"field is an empty string and the current branch matches": {
			input:        map[string]any{"repo": "/feat", "ref": ""},
			wantResolved: []string{"/feat"},
			want:         true,
		},
		"field is absent and the pattern rejects the current branch": {
			input:        map[string]any{"repo": "/main"},
			wantResolved: []string{"/main"},
		},
		"current branch must match from the start": {
			input:        map[string]any{"repo": "/prefixed"},
			wantResolved: []string{"/prefixed"},
		},
		"field is absent and the current branch does not resolve": {
			input:        map[string]any{"repo": "/detached"},
			wantResolved: []string{"/detached"},
		},
		"field is absent and the resolver returns no name": {
			input:        map[string]any{"repo": "/unnamed"},
			wantResolved: []string{"/unnamed"},
		},
		"field is absent and so is the repo field": {
			input: map[string]any{"remote": "origin"},
		},
		"field is absent and the repo field is empty": {
			input: map[string]any{"repo": ""},
		},
		"field is absent and the repo field is not a string": {
			input: map[string]any{"repo": []any{"/feat"}},
		},
		"field is not a string": {
			input: map[string]any{"repo": "/feat", "ref": float64(1)},
		},
		"nil input": {},
		"no resolver and the field names a matching branch": {
			input:      map[string]any{"repo": "/feat", "ref": "feat/x"},
			noResolver: true,
			want:       true,
		},
		"no resolver and the field is absent": {
			input:      map[string]any{"repo": "/feat"},
			noResolver: true,
		},
		"condition before it fails, so the resolver does not run": {
			conditions: []mcprules.Condition{noTags, branch},
			input:      map[string]any{"repo": "/feat", "tags": true},
		},
		"condition after it fails": {
			conditions:   []mcprules.Condition{branch, noTags},
			input:        map[string]any{"repo": "/feat", "tags": true},
			wantResolved: []string{"/feat"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var resolved []string

			opts := []mcprules.Option{}
			if !tc.noResolver {
				opts = append(opts, mcprules.WithBranchResolver(func(_ context.Context, repo string) (string, error) {
					resolved = append(resolved, repo)

					name, ok := branches[repo]
					if !ok {
						return "", errUnresolved
					}

					return name, nil
				}))
			}

			conditions := tc.conditions
			if conditions == nil {
				conditions = []mcprules.Condition{branch}
			}

			opts = append(opts, mcprules.WithRules(mustRule(t, tool, conditions...)))
			rs := mcprules.New(nil, nil, nil, opts...)

			got, matched := rs.Match(t.Context(), tool, tc.input)
			require.True(t, matched, "a tool with a conditional rule always gets a decision")

			want := mcprules.DecisionAsk
			if tc.want {
				want = mcprules.DecisionAllow
			}

			assert.Equal(t, want, got.Decision)
			assert.Equal(t, tc.wantResolved, resolved)

			if tc.conditions == nil {
				assert.Equal(
					t,
					"`ref`, or the current branch of `repo` when `ref` is empty, matches `(feat|fix)/.+`",
					got.Conditions,
				)
			}
		})
	}
}

// TestPullRequestRecorded checks the [mcprules.TestPullRequestRecorded]
// test against the values of its field, the values of the owner and
// repo fields, and the answers of the [mcprules.PullRequestLookup].
func TestPullRequestRecorded(t *testing.T) {
	t.Parallel()

	const tool = "mcp__github__update_pull_request"

	errUnread := errors.New("unread")

	// record holds the pull requests that the lookup reports. The
	// repository octo-org/broken fails every lookup.
	record := map[string]bool{"octo-org/widgets#12": true}

	recorded := mcprules.Condition{Field: "pullNumber", Test: mcprules.TestPullRequestRecorded}
	notDraft := mcprules.Condition{Field: "draft", Test: mcprules.TestAbsentOrTrue}

	cases := map[string]struct {
		input      map[string]any
		conditions []mcprules.Condition
		wantLookup []string
		noLookup   bool
		want       bool
	}{
		"recorded pull request": {
			input:      map[string]any{"owner": "octo-org", "repo": "widgets", "pullNumber": float64(12)},
			wantLookup: []string{"octo-org/widgets#12"},
			want:       true,
		},
		"another number": {
			input:      map[string]any{"owner": "octo-org", "repo": "widgets", "pullNumber": float64(13)},
			wantLookup: []string{"octo-org/widgets#13"},
		},
		"another repository": {
			input:      map[string]any{"owner": "octo-org", "repo": "gadgets", "pullNumber": float64(12)},
			wantLookup: []string{"octo-org/gadgets#12"},
		},
		"another owner": {
			input:      map[string]any{"owner": "someone", "repo": "widgets", "pullNumber": float64(12)},
			wantLookup: []string{"someone/widgets#12"},
		},
		"lookup returns an error": {
			input:      map[string]any{"owner": "octo-org", "repo": "broken", "pullNumber": float64(12)},
			wantLookup: []string{"octo-org/broken#12"},
		},
		"largest number": {
			input:      map[string]any{"owner": "octo-org", "repo": "widgets", "pullNumber": float64(2147483647)},
			wantLookup: []string{"octo-org/widgets#2147483647"},
		},
		"number past the largest": {
			input: map[string]any{"owner": "octo-org", "repo": "widgets", "pullNumber": float64(2147483648)},
		},
		"number is absent": {
			input: map[string]any{"owner": "octo-org", "repo": "widgets"},
		},
		"number is null": {
			input: map[string]any{"owner": "octo-org", "repo": "widgets", "pullNumber": nil},
		},
		"number is a string": {
			input: map[string]any{"owner": "octo-org", "repo": "widgets", "pullNumber": "12"},
		},
		"number is zero": {
			input: map[string]any{"owner": "octo-org", "repo": "widgets", "pullNumber": float64(0)},
		},
		"number is negative": {
			input: map[string]any{"owner": "octo-org", "repo": "widgets", "pullNumber": float64(-12)},
		},
		"number has a fraction": {
			input: map[string]any{"owner": "octo-org", "repo": "widgets", "pullNumber": 12.5},
		},
		"owner is absent": {
			input: map[string]any{"repo": "widgets", "pullNumber": float64(12)},
		},
		"owner is empty": {
			input: map[string]any{"owner": "", "repo": "widgets", "pullNumber": float64(12)},
		},
		"owner is not a string": {
			input: map[string]any{"owner": []any{"octo-org"}, "repo": "widgets", "pullNumber": float64(12)},
		},
		"repo is absent": {
			input: map[string]any{"owner": "octo-org", "pullNumber": float64(12)},
		},
		"repo is empty": {
			input: map[string]any{"owner": "octo-org", "repo": "", "pullNumber": float64(12)},
		},
		"nil input": {},
		"no lookup": {
			input:    map[string]any{"owner": "octo-org", "repo": "widgets", "pullNumber": float64(12)},
			noLookup: true,
		},
		"condition before it fails, so the lookup does not run": {
			conditions: []mcprules.Condition{notDraft, recorded},
			input: map[string]any{
				"owner":      "octo-org",
				"repo":       "widgets",
				"pullNumber": float64(12),
				"draft":      false,
			},
		},
		"condition after it fails": {
			conditions: []mcprules.Condition{recorded, notDraft},
			input: map[string]any{
				"owner":      "octo-org",
				"repo":       "widgets",
				"pullNumber": float64(12),
				"draft":      false,
			},
			wantLookup: []string{"octo-org/widgets#12"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var looked []string

			opts := []mcprules.Option{}
			if !tc.noLookup {
				opts = append(opts, mcprules.WithPullRequestLookup(
					func(_ context.Context, owner, repo string, number int) (bool, error) {
						key := fmt.Sprintf("%s/%s#%d", owner, repo, number)
						looked = append(looked, key)

						if repo == "broken" {
							return true, errUnread
						}

						return record[key], nil
					},
				))
			}

			conditions := tc.conditions
			if conditions == nil {
				conditions = []mcprules.Condition{recorded}
			}

			opts = append(opts, mcprules.WithRules(mustRule(t, tool, conditions...)))
			rs := mcprules.New(nil, nil, nil, opts...)

			got, matched := rs.Match(t.Context(), tool, tc.input)
			require.True(t, matched, "a tool with a conditional rule always gets a decision")

			want := mcprules.DecisionAsk
			if tc.want {
				want = mcprules.DecisionAllow
			}

			assert.Equal(t, want, got.Decision)
			assert.Equal(t, tc.wantLookup, looked)

			if tc.conditions == nil {
				assert.Equal(
					t,
					"`pullNumber` is a pull request of `owner`/`repo` that an agent opened from this host",
					got.Conditions,
				)
			}
		})
	}
}

// TestParsePullRequestLookup checks that [mcprules.Parse] hands its
// options to the ruleset, so that a parsed pull request condition
// reaches the lookup. It also checks that the lookup gets the number
// of a call as JSON decodes it.
func TestParsePullRequestLookup(t *testing.T) {
	t.Parallel()

	const (
		tool    = "mcp__github__update_pull_request_branch"
		payload = `{"conditional":[{"tool":"mcp__github__update_pull_request_branch","conditions":[
			{"field":"base","test":"absent"},
			{"field":"pullNumber","test":"pullRequestRecorded"}
		]}]}`
	)

	lookup := func(_ context.Context, owner, repo string, number int) (bool, error) {
		return owner == "octo-org" && repo == "widgets" && number == 12, nil
	}

	// input decodes args the way the hook decodes tool_input.
	input := func(args string) map[string]any {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(args), &m))

		return m
	}

	rs, err := mcprules.Parse(payload, mcprules.WithPullRequestLookup(lookup))
	require.NoError(t, err)

	got, matched := rs.Match(t.Context(), tool, input(`{"owner":"octo-org","repo":"widgets","pullNumber":12}`))
	require.True(t, matched)
	assert.Equal(t, mcprules.Result{
		Decision: mcprules.DecisionAllow,
		Conditions: "`base` is absent and " +
			"`pullNumber` is a pull request of `owner`/`repo` that an agent opened from this host",
	}, got)

	got, matched = rs.Match(t.Context(), tool, input(`{"owner":"octo-org","repo":"widgets","pullNumber":13}`))
	require.True(t, matched)
	assert.Equal(t, mcprules.DecisionAsk, got.Decision)

	got, matched = rs.Match(
		t.Context(), tool, input(`{"owner":"octo-org","repo":"widgets","pullNumber":12,"base":""}`),
	)
	require.True(t, matched)
	assert.Equal(t, mcprules.DecisionAsk, got.Decision)

	rs, err = mcprules.Parse(payload)
	require.NoError(t, err)

	got, matched = rs.Match(t.Context(), tool, input(`{"owner":"octo-org","repo":"widgets","pullNumber":12}`))
	require.True(t, matched)
	assert.Equal(t, mcprules.DecisionAsk, got.Decision)

	rs, err = mcprules.Parse("", mcprules.WithPullRequestLookup(lookup))
	require.NoError(t, err)
	assert.True(t, rs.Empty(), "a lookup alone decides no call")
}

// TestParseBranchResolver checks that [mcprules.Parse] hands its
// options to the ruleset, so that a parsed branch condition reaches the
// resolver.
func TestParseBranchResolver(t *testing.T) {
	t.Parallel()

	const payload = `{"conditional":[{"tool":"mcp__git__git_push","conditions":[
		{"field":"ref","test":"branchMatches","pattern":"feat/.+","repoField":"repo"}
	]}]}`

	resolve := func(_ context.Context, repo string) (string, error) {
		return "feat/" + repo, nil
	}

	rs, err := mcprules.Parse(payload, mcprules.WithBranchResolver(resolve))
	require.NoError(t, err)

	got, matched := rs.Match(t.Context(), "mcp__git__git_push", map[string]any{"repo": "x"})
	require.True(t, matched)
	assert.Equal(t, mcprules.DecisionAllow, got.Decision)

	rs, err = mcprules.Parse(payload)
	require.NoError(t, err)

	got, matched = rs.Match(t.Context(), "mcp__git__git_push", map[string]any{"repo": "x"})
	require.True(t, matched)
	assert.Equal(t, mcprules.DecisionAsk, got.Decision)

	rs, err = mcprules.Parse("", mcprules.WithBranchResolver(resolve))
	require.NoError(t, err)
	assert.True(t, rs.Empty(), "a resolver alone decides no call")
}

// TestRulesetMatchConditional checks how conditional rules combine with
// each other and with the pattern lists.
func TestRulesetMatchConditional(t *testing.T) {
	t.Parallel()

	const (
		push   = "mcp__git__git_push"
		create = "mcp__github__create_pull_request"
	)

	branch := mcprules.Condition{Field: "ref", Test: mcprules.TestMatches, Pattern: "(feat|fix)/.+"}
	noTags := mcprules.Condition{Field: "tags", Test: mcprules.TestAbsentOrZero}
	draft := mcprules.Condition{Field: "draft", Test: mcprules.TestTrue}
	keepDraft := mcprules.Condition{Field: "draft", Test: mcprules.TestAbsentOrTrue}

	const (
		pushConditions  = "`ref` matches `(feat|fix)/.+` and `tags` is absent, false, or empty"
		draftConditions = "`draft` is true"
	)

	cases := map[string]struct {
		input       map[string]any
		tool        string
		want        mcprules.Result
		allow       []string
		ask         []string
		deny        []string
		rules       []mcprules.Rule
		wantMatched bool
	}{
		"every condition holds: allow": {
			rules:       []mcprules.Rule{mustRule(t, push, branch, noTags)},
			tool:        push,
			input:       map[string]any{"repo": "/r", "ref": "feat/x", "tags": false},
			want:        mcprules.Result{Decision: mcprules.DecisionAllow, Conditions: pushConditions},
			wantMatched: true,
		},
		"first condition fails: ask": {
			rules:       []mcprules.Rule{mustRule(t, push, branch, noTags)},
			tool:        push,
			input:       map[string]any{"ref": "main"},
			want:        mcprules.Result{Decision: mcprules.DecisionAsk, Conditions: pushConditions},
			wantMatched: true,
		},
		"last condition fails: ask": {
			rules:       []mcprules.Rule{mustRule(t, push, branch, noTags)},
			tool:        push,
			input:       map[string]any{"ref": "feat/x", "tags": true},
			want:        mcprules.Result{Decision: mcprules.DecisionAsk, Conditions: pushConditions},
			wantMatched: true,
		},
		"nil input: ask": {
			rules:       []mcprules.Rule{mustRule(t, push, branch, noTags)},
			tool:        push,
			want:        mcprules.Result{Decision: mcprules.DecisionAsk, Conditions: pushConditions},
			wantMatched: true,
		},
		"deny pattern wins over a rule that holds": {
			deny:        []string{"mcp__git"},
			rules:       []mcprules.Rule{mustRule(t, push, branch, noTags)},
			tool:        push,
			input:       map[string]any{"ref": "feat/x"},
			want:        mcprules.Result{Decision: mcprules.DecisionDeny, Pattern: "mcp__git"},
			wantMatched: true,
		},
		"second rule of a tool holds: allow": {
			rules: []mcprules.Rule{
				mustRule(t, create, draft, branch),
				mustRule(t, create, keepDraft, noTags),
			},
			tool:  create,
			input: map[string]any{"ref": "main"},
			want: mcprules.Result{
				Decision:   mcprules.DecisionAllow,
				Conditions: "`draft` is absent or true and `tags` is absent, false, or empty",
			},
			wantMatched: true,
		},
		"no rule of a tool holds: ask names each rule": {
			rules: []mcprules.Rule{
				mustRule(t, create, draft, branch),
				mustRule(t, create, keepDraft, noTags),
			},
			tool:  create,
			input: map[string]any{"draft": false, "ref": "feat/x"},
			want: mcprules.Result{
				Decision: mcprules.DecisionAsk,
				Conditions: "`draft` is true and `ref` matches `(feat|fix)/.+`" +
					", or when `draft` is absent or true and `tags` is absent, false, or empty",
			},
			wantMatched: true,
		},
		"rule of another tool does not apply": {
			rules: []mcprules.Rule{mustRule(t, push, branch), mustRule(t, create, draft)},
			tool:  create,
			input: map[string]any{"ref": "feat/x"},
			want: mcprules.Result{
				Decision:   mcprules.DecisionAsk,
				Conditions: draftConditions,
			},
			wantMatched: true,
		},
		"rule that fails beats an allow pattern": {
			allow:       []string{"mcp__github"},
			rules:       []mcprules.Rule{mustRule(t, create, draft)},
			tool:        create,
			input:       map[string]any{},
			want:        mcprules.Result{Decision: mcprules.DecisionAsk, Conditions: draftConditions},
			wantMatched: true,
		},
		"rule that holds beats an ask pattern": {
			ask:         []string{create},
			rules:       []mcprules.Rule{mustRule(t, create, draft)},
			tool:        create,
			input:       map[string]any{"draft": true},
			want:        mcprules.Result{Decision: mcprules.DecisionAllow, Conditions: draftConditions},
			wantMatched: true,
		},
		"tool with no rule resolves through the ask list": {
			ask:         []string{"mcp__github__merge_pull_request"},
			allow:       []string{"mcp__github"},
			rules:       []mcprules.Rule{mustRule(t, create, draft)},
			tool:        "mcp__github__merge_pull_request",
			input:       map[string]any{"draft": true},
			want:        mcprules.Result{Decision: mcprules.DecisionAsk, Pattern: "mcp__github__merge_pull_request"},
			wantMatched: true,
		},
		"tool with no rule resolves through the allow list": {
			allow:       []string{"mcp__github"},
			rules:       []mcprules.Rule{mustRule(t, create, draft)},
			tool:        "mcp__github__search_code",
			want:        mcprules.Result{Decision: mcprules.DecisionAllow, Pattern: "mcp__github"},
			wantMatched: true,
		},
		"tool with no rule and no pattern is unmatched": {
			rules: []mcprules.Rule{mustRule(t, create, draft)},
			tool:  "mcp__github__create_pull_request_with_copilot",
			input: map[string]any{"draft": true},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rs := mcprules.New(tc.allow, tc.ask, tc.deny, mcprules.WithRules(tc.rules...))

			got, matched := rs.Match(t.Context(), tc.tool, tc.input)
			assert.Equal(t, tc.wantMatched, matched)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestZeroRule checks that a [mcprules.Rule] that did not come from
// [mcprules.NewRule] decides no call.
func TestZeroRule(t *testing.T) {
	t.Parallel()

	rs := mcprules.New(nil, nil, nil, mcprules.WithRules(mcprules.Rule{}))
	assert.True(t, rs.Empty())

	_, matched := rs.Match(t.Context(), "", nil)
	assert.False(t, matched)

	_, matched = rs.Match(t.Context(), "mcp__git__git_push", nil)
	assert.False(t, matched)
}

func TestRulesetNilSafe(t *testing.T) {
	t.Parallel()

	var rs *mcprules.Ruleset

	assert.True(t, rs.Empty())

	_, matched := rs.Match(t.Context(), "mcp__fetch__fetch", nil)
	assert.False(t, matched)
}

func TestMatchPattern(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		pattern string
		tool    string
		want    bool
	}{
		"exact name matches": {
			pattern: "mcp__slack__post_message",
			tool:    "mcp__slack__post_message",
			want:    true,
		},
		"exact name rejects a sibling tool": {
			pattern: "mcp__slack__post_message",
			tool:    "mcp__slack__post_reply",
		},
		"glob matches by prefix": {
			pattern: "mcp__slack__post_*",
			tool:    "mcp__slack__post_reply",
			want:    true,
		},
		"glob rejects another prefix": {
			pattern: "mcp__slack__post_*",
			tool:    "mcp__slack__list_channels",
		},
		"bare server matches its tools": {
			pattern: "mcp__slack",
			tool:    "mcp__slack__post_message",
			want:    true,
		},
		"bare server rejects a server that shares its prefix": {
			pattern: "mcp__slack",
			tool:    "mcp__slackbot__post_message",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, mcprules.MatchPattern(tc.pattern, tc.tool))
		})
	}
}
