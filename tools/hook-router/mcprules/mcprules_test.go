package mcprules_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/mcprules"
)

func TestParse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input     string
		wantEmpty bool
		err       bool
	}{
		"empty string yields empty ruleset": {
			input:     "",
			wantEmpty: true,
		},
		"all-empty object yields empty ruleset": {
			input:     `{"allow":[],"ask":[],"deny":[]}`,
			wantEmpty: true,
		},
		"valid object": {
			input: `{"allow":["mcp__fetch__fetch"],"ask":["mcp__spacelift__trigger_stack_run"],"deny":["mcp__spacelift__list_api_keys"]}`,
		},
		"unknown top-level field dropped": {
			input: `{"allow":["mcp__fetch__fetch"],"bogus":true}`,
		},
		"malformed JSON": {
			input: `{"allow":`,
			err:   true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rs, err := mcprules.Parse(tc.input)
			if tc.err {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantEmpty, rs.Empty())
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

			decision, pattern, matched := rs.Match(tc.tool)
			assert.Equal(t, tc.wantMatched, matched)
			assert.Equal(t, tc.wantDecision, decision)
			assert.Equal(t, tc.wantPattern, pattern)
		})
	}
}

func TestRulesetNilSafe(t *testing.T) {
	t.Parallel()

	var rs *mcprules.Ruleset

	assert.True(t, rs.Empty())

	_, _, matched := rs.Match("mcp__fetch__fetch")
	assert.False(t, matched)
}
