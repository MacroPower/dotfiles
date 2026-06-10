package postimpl_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/postimpl"
)

// testCatalog mirrors the canonical post-impl skills declared in
// home/claude.nix (same fixture as the main-package handler tests).
func testCatalog() *postimpl.Catalog {
	return postimpl.New([]postimpl.Skill{
		{Label: "/review-implementation", Description: "Review code changes against the plan."},
		{Label: "/simplify", Description: "Review and simplify the implemented code."},
		{Label: "/humanize", Description: "Clean up AI writing patterns in any prose/docs that changed."},
		{Label: "/commit", Description: "Wrap up the cycle by creating a git commit."},
	})
}

func TestParsePostImplSkills(t *testing.T) {
	t.Parallel()

	type check func(t *testing.T, cat *postimpl.Catalog)

	cases := map[string]struct {
		in    string
		err   bool
		check check
	}{
		"empty string yields empty catalog": {
			in: "",
			check: func(t *testing.T, cat *postimpl.Catalog) {
				t.Helper()
				assert.True(t, cat.Empty())
				assert.False(t, cat.HasLabel("/review-implementation"))
			},
		},
		"entry round-trips": {
			in: `[{"label":"commit","description":"Create a git commit."}]`,
			check: func(t *testing.T, cat *postimpl.Catalog) {
				t.Helper()
				assert.True(t, cat.HasLabel("commit"))
				// BuildAskReason renders the single bullet.
				reason := cat.BuildAskReason("/p.md", "abc123")
				assert.Contains(t, reason, "commit: Create a git commit.")
			},
		},
		"malformed JSON returns error": {
			in:  `[{"label":`,
			err: true,
		},
		"duplicate labels are not deduped": {
			in: `[{"label":"commit","description":"first"},{"label":"commit","description":"second"}]`,
			check: func(t *testing.T, cat *postimpl.Catalog) {
				t.Helper()
				assert.True(t, cat.HasLabel("commit"))

				reason := cat.BuildAskReason("/p.md", "abc123")
				// Both bullets render -- catalog trusts the Nix list as source of truth.
				assert.Contains(t, reason, "commit: first")
				assert.Contains(t, reason, "commit: second")
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cat, err := postimpl.Parse(tc.in)
			if tc.err {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cat)
			tc.check(t, cat)
		})
	}
}

// TestBuildAskReason_EmptyCatalog documents the degraded-mode
// fallback: with no skills, Stop still renders a (bullet-less) block
// message rather than panicking. Production should never hit this
// path (mainErr logs a warning when the catalog is empty) but the
// invariant that handlers can call BuildAskReason without a nil-guard
// is enforced here.
func TestBuildAskReason_EmptyCatalog(t *testing.T) {
	t.Parallel()

	cat := postimpl.New(nil)
	reason := cat.BuildAskReason("/p.md", "abc123")

	assert.Contains(t, reason, "AskUserQuestion")
	assert.Contains(t, reason, "/p.md")
	assert.Contains(t, reason, "abc123")
	assert.Contains(t, reason, "If you are not done")
	assert.NotContains(t, reason, "  - ") // zero bullets rendered
}

// TestBuildAskReason_NotDoneBranchPresent enforces the unified-message
// contract: the populated-catalog rendering must include all three
// branches so Claude can ask a clarifying question, keep working, or
// confirm done (post-impl AUQ).
func TestBuildAskReason_NotDoneBranchPresent(t *testing.T) {
	t.Parallel()

	reason := testCatalog().BuildAskReason("/p.md", "abc123")

	assert.Contains(t, reason, "completed the implementation")
	assert.Contains(t, reason, "If you are not done")
	assert.Contains(t, reason, "If you have a question for the user")
	assert.Contains(t, reason, "/review-implementation")
	assert.Contains(t, reason, "/p.md")
	assert.Contains(t, reason, "abc123")
}
