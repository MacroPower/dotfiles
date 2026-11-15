package clarity

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
)

func TestOutdated(t *testing.T) {
	t.Parallel()

	day := func(month time.Month, d int) time.Time {
		return time.Date(2026, month, d, 0, 0, 0, 0, time.UTC)
	}

	update := Patch{Version: "9.7.0", Date: day(time.June, 9)}
	firstHotfix := Patch{Version: "9.7.0.1", Date: day(time.June, 16)}
	secondHotfix := Patch{Version: "9.7.0.2", Date: day(time.June, 23)}

	tests := map[string]struct {
		desc Description
		want Change
		ok   bool
	}{
		"a perk that the update changed after the upload": {
			desc: Description{Name: "Focused Fury", Uploaded: day(time.January, 17)},
			want: Change{Name: "Focused Fury", Patch: update},
			ok:   true,
		},
		"an upload in the last second before the day of the notes": {
			desc: Description{Name: "Focused Fury", Uploaded: day(time.June, 9).Add(-time.Second)},
			want: Change{Name: "Focused Fury", Patch: update},
			ok:   true,
		},
		"an upload on the day of the notes": {
			desc: Description{Name: "Focused Fury", Uploaded: day(time.June, 9)},
		},
		"an upload after the update": {
			desc: Description{Name: "Focused Fury", Uploaded: day(time.July, 9)},
		},
		"a perk that no patch changed": {
			desc: Description{Name: "Kill Clip", Uploaded: day(time.January, 17)},
		},
		"a perk of an exotic item that the update changed": {
			desc: Description{Name: "Sun Blast", Item: "Sunshot", Uploaded: day(time.January, 17)},
			want: Change{Name: "Sunshot", Patch: update},
			ok:   true,
		},
		"an exotic perk that Clarity files without its item": {
			desc: Description{Name: "The Fundamentals", Uploaded: day(time.January, 17)},
			want: Change{Name: "The Fundamentals", Patch: update},
			ok:   true,
		},
		"the last of two patches that changed the item": {
			desc: Description{Name: "Truth Catalyst", Item: "Truth", Uploaded: day(time.January, 17)},
			want: Change{Name: "Truth", Patch: secondHotfix},
			ok:   true,
		},
		"an upload between two patches that changed the item": {
			desc: Description{Name: "Truth Catalyst", Item: "Truth", Uploaded: day(time.June, 12)},
			want: Change{Name: "Truth", Patch: secondHotfix},
			ok:   true,
		},
		"an upload between the update and the hotfix that changed the perk": {
			desc: Description{Name: "Heat Rises", Uploaded: day(time.June, 12)},
			want: Change{Name: "Heat Rises", Patch: firstHotfix},
			ok:   true,
		},
		"an upload after the last patch that changed the perk": {
			desc: Description{Name: "Heat Rises", Uploaded: day(time.June, 20)},
		},
		"a name in another letter case and without its accent": {
			desc: Description{Name: "Horn of Flight", Item: "MATAIODOXIA", Uploaded: day(time.January, 17)},
			want: Change{Name: "Mataiodox\U000000EDa", Patch: update},
			ok:   true,
		},
		"an item with the name of a changed perk": {
			// The update changed the aspect Bastion, and it did not
			// change the exotic fusion rifle of that name.
			desc: Description{Name: "Bastion Catalyst", Item: "Bastion", Uploaded: day(time.January, 17)},
		},
		"a perk with the name of a changed item": {
			desc: Description{Name: "Sunshot", Uploaded: day(time.January, 17)},
		},
		"a description without names": {
			desc: Description{Uploaded: day(time.January, 17)},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := tc.desc.Outdated()
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestFinalPatches pins the form of the vendored list. [Description.Outdated]
// reads the patches in the order of their dates and looks each name up
// after a fold.
func TestFinalPatches(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, finalPatches)

	var last time.Time

	for _, notes := range finalPatches {
		version := notes.patch.Version

		assert.NotEmpty(t, version)
		assert.True(t, notes.patch.Date.After(last), "patch %s follows the patch before it", version)
		assert.Equal(t, time.UTC, notes.patch.Date.Location(), "patch %s has a UTC date", version)

		last = notes.patch.Date

		for kind, names := range map[string][]string{"perk": notes.perks, "item": notes.items} {
			assert.NotEmpty(t, names, "patch %s names a %s", version, kind)

			seen := map[string]bool{}

			for _, name := range names {
				folded := fold.Name(name)

				assert.NotEmpty(t, folded, "patch %s holds a %s without a name", version, kind)
				assert.Equal(t, strings.TrimSpace(name), name, "patch %s, %s %q", version, kind, name)
				assert.False(t, seen[folded], "patch %s names the %s %q twice", version, kind, name)

				seen[folded] = true
			}
		}
	}

	assert.Len(t, finalIndex, len(finalPatches))
}
