package loot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lastUpdate is the last update of the game with public patch notes.
const lastUpdate = "9.7.0.6"

// TestEmbeddedData pins the provenance of the loot data. The data passes
// the checks of the loader, so each drop names a source. Each weapon also
// names the last update of the game as the update of its last check. Each
// place that names a raid, a dungeon, or a mission has an activity.
func TestEmbeddedData(t *testing.T) {
	t.Parallel()

	_, err := load()
	require.NoError(t, err)

	var f file
	require.NoError(t, json.Unmarshal(embedded, &f))

	assert.NotEmpty(t, f.Patch)
	assert.NotEmpty(t, f.Weapons)

	for key, src := range f.Sources {
		assert.NotEmpty(t, src.Title, key)
		assert.NotEmpty(t, src.URL, key)
		assert.NotEmpty(t, src.Date, key)
		assert.NotEmpty(t, src.Patch, key)
	}

	for _, w := range f.Weapons {
		assert.Equal(t, lastUpdate, w.Checked, w.Name)

		for _, d := range w.Drops {
			for _, kind := range []string{" raid", " dungeon", " mission"} {
				if strings.HasSuffix(d.From, kind) {
					assert.Contains(t, f.Activities, d.From, "the activity of the place")
				}
			}
		}
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	// doc returns loot data with the given weapons.
	doc := func(weapons ...string) string {
		return `{
			"patch": "9.7.0.2",
			"sources": {"sax": {"title": "Sax", "url": "https://example.com", "date": "2026-11-22", "patch": "9.7.0.2"}},
			"weapons": [` + strings.Join(weapons, ", ") + `]
		}`
	}

	// withActivities returns loot data with the given weapons and the
	// activities of the given JSON object.
	withActivities := func(activities string, weapons ...string) string {
		return strings.Replace(doc(weapons...), `"weapons"`, `"activities": `+activities+`, "weapons"`, 1)
	}

	// weapon returns one weapon with the given name and drops.
	weapon := func(name, drops string) string {
		return `{"name": "` + name + `", "drops": [` + drops + `], "checked": "9.7.0.6"}`
	}

	const raid = `{"from": "Vault of Glass raid", "sources": ["sax"]}`

	tests := map[string]struct {
		want       map[string][]string
		data       string
		err        string
		activities []activity
	}{
		"valid": {
			data: doc(
				weapon("Fatebringer", raid),
				weapon(
					"Aberrant Action",
					`{"from": "Encore mission", "sources": ["sax"]}, {"from": "Xûr", "sources": ["sax"]}`,
				),
			),
			want: map[string][]string{
				"fatebringer":     {"Vault of Glass raid"},
				"aberrant action": {"Encore mission", "Xûr"},
			},
		},
		"activities ordered by place": {
			data: withActivities(
				`{"Vault of Glass raid": "Vault of Glass", "Encore mission": "Encore"}`,
				weapon("Fatebringer", raid),
				weapon("Aberrant Action", `{"from": "Encore mission", "sources": ["sax"]}`),
			),
			want: map[string][]string{
				"fatebringer":     {"Vault of Glass raid"},
				"aberrant action": {"Encore mission"},
			},
			activities: []activity{
				{place: "Encore mission", name: "encore"},
				{place: "Vault of Glass raid", name: "vault of glass"},
			},
		},
		"activity of a place that no drop names": {
			data: withActivities(`{"Encore mission": "Encore"}`, weapon("Fatebringer", raid)),
			err:  `loot data: the activity of "Encore mission" belongs to a place that no drop names`,
		},
		"activity without a name": {
			data: withActivities(`{"Vault of Glass raid": ""}`, weapon("Fatebringer", raid)),
			err:  `loot data: the activity of "Vault of Glass raid" has no name`,
		},
		"two places with one activity name in another letter case": {
			data: withActivities(
				`{"Vault of Glass raid": "Vault of Glass", "Encore mission": "VAULT OF GLASS"}`,
				weapon("Fatebringer", raid),
				weapon("Aberrant Action", `{"from": "Encore mission", "sources": ["sax"]}`),
			),
			err: `loot data: the activities of "Encore mission" and "Vault of Glass raid" share the name "Vault of Glass"`,
		},
		"weapon with a note": {
			data: doc(
				`{"name": "Palmyra-B", "note": "The sources disagree.", "drops": [` + raid + `], "checked": "9.7.0.6"}`,
			),
			want: map[string][]string{"palmyra-b": {"Vault of Glass raid"}},
		},
		"not JSON": {
			data: "{",
			err:  "loot data: unexpected EOF",
		},
		"unknown field": {
			data: strings.Replace(doc(weapon("Fatebringer", raid)), `"patch"`, `"version"`, 1),
			err:  `loot data: json: unknown field "version"`,
		},
		"weapon without a name": {
			data: doc(weapon("", raid)),
			err:  `loot data: weapon 0 has the name "", which is empty or taken`,
		},
		"weapon twice in another letter case and with another apostrophe": {
			data: doc(weapon("Zaouli's Bane", raid), weapon("ZAOULI\u2019S BANE", raid)),
			err:  "loot data: weapon 1 has the name \"ZAOULI\u2019S BANE\", which is empty or taken",
		},
		"weapon without the update of its last check": {
			data: doc(`{"name": "Fatebringer", "drops": [` + raid + `]}`),
			err:  "loot data: Fatebringer: the weapon names no game update of its last check",
		},
		"weapon without a drop": {
			data: doc(weapon("Fatebringer", "")),
			err:  "loot data: Fatebringer: the weapon has no drop",
		},
		"drop without a place": {
			data: doc(weapon("Fatebringer", `{"from": "", "sources": ["sax"]}`)),
			err:  `loot data: Fatebringer: the drop from "" is empty or repeats`,
		},
		"place twice": {
			data: doc(weapon("Fatebringer", raid+", "+raid)),
			err:  `loot data: Fatebringer: the drop from "Vault of Glass raid" is empty or repeats`,
		},
		"drop without a source": {
			data: doc(weapon("Fatebringer", `{"from": "Vault of Glass raid", "sources": []}`)),
			err:  `loot data: Fatebringer: the drop from "Vault of Glass raid" names no source`,
		},
		"drop with an unknown source": {
			data: doc(weapon("Fatebringer", `{"from": "Vault of Glass raid", "sources": ["forum"]}`)),
			err: `loot data: Fatebringer: the drop from "Vault of Glass raid" names "forum", ` +
				`which is not a source of the file`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := parse([]byte(tc.data))
			if tc.err != "" {
				require.ErrorIs(t, err, ErrData)
				require.EqualError(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got.drops)
			assert.Equal(t, tc.activities, got.activities)
		})
	}
}

// TestPlaceLongestName pins the place of an activity whose name continues
// the activity names of two places. The place with the longer activity
// name wins, in both orders of the places.
func TestPlaceLongestName(t *testing.T) {
	t.Parallel()

	// data returns loot data with one weapon from the Encore mission and
	// one weapon from the Vault of Glass raid. The data holds the
	// activities of the given JSON object.
	data := func(activities string) []byte {
		return []byte(`{
			"patch": "9.7.0.2",
			"sources": {"sax": {"title": "Sax", "url": "https://example.com", "date": "2026-11-22", "patch": "9.7.0.2"}},
			"activities": ` + activities + `,
			"weapons": [
				{"name": "Fatebringer", "drops": [{"from": "Vault of Glass raid", "sources": ["sax"]}], "checked": "9.7.0.6"},
				{"name": "Aberrant Action", "drops": [{"from": "Encore mission", "sources": ["sax"]}], "checked": "9.7.0.6"}
			]
		}`)
	}

	const (
		// longerFirst gives the longer activity name to the place that
		// comes first, and longerLast gives it to the place that comes
		// last.
		longerFirst = `{"Encore mission": "Vault: Encore", "Vault of Glass raid": "Vault"}`
		longerLast  = `{"Encore mission": "Vault", "Vault of Glass raid": "Vault: Encore"}`
	)

	tests := map[string]struct {
		activities string
		activity   string
		want       string
	}{
		"longer name on the first place": {
			activities: longerFirst,
			activity:   "Vault: Encore: Master",
			want:       "Encore mission",
		},
		"longer name on the last place": {
			activities: longerLast,
			activity:   "Vault: Encore: Master",
			want:       "Vault of Glass raid",
		},
		"activity that equals the longer name": {
			activities: longerLast,
			activity:   "Vault: Encore",
			want:       "Vault of Glass raid",
		},
		"activity that continues only the shorter name": {
			activities: longerFirst,
			activity:   "Vault: Master",
			want:       "Vault of Glass raid",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			table, err := parse(data(tc.activities))
			require.NoError(t, err)

			got, ok := table.Place(tc.activity)
			assert.True(t, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
