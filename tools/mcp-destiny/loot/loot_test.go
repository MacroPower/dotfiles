package loot_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/loot"
)

func TestDrops(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		name string
		want []string
	}{
		"raid weapon": {
			name: "Fatebringer",
			want: []string{"Vault of Glass raid"},
		},
		// The collectible text of the weapon names Ketchcrash and
		// Expeditions. The Vendors tab of the sheet of Sax marks the
		// version that Xûr sells as not craftable. The Destiny
		// definitions hold only the craftable version of the weapon.
		"weapon that Xur sells": {
			name: "Brigand's Law",
			want: []string{"Xûr"},
		},
		"weapon with two places": {
			name: "Aberrant Action",
			want: []string{"Encore mission", "Xûr"},
		},
		// Onslaught drops a version of Succession that the pattern
		// does not craft.
		"weapon with a version that does not advance the pattern": {
			name: "Succession",
			want: []string{"Deep Stone Crypt raid"},
		},
		"weapon whose sources disagree": {
			name: "Palmyra-B",
			want: []string{"Banshee-44"},
		},
		"name in another letter case and without its diacritic": {
			name: "fang of ir yut",
			want: []string{"Crota's End raid"},
		},
		"name with a curly apostrophe": {
			name: "Zaouli\u2019s Bane",
			want: []string{"King's Fall raid"},
		},
		"exotic weapon": {
			name: "Osteo Striga",
		},
		"weapon that is not craftable": {
			name: "Midnight Coup",
		},
	}

	table, err := loot.Load()
	require.NoError(t, err)

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, table.Drops(tc.name))
		})
	}
}

func TestPlace(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		activity string
		want     string
	}{
		"raid with a difficulty": {
			activity: "Vault of Glass: Master",
			want:     "Vault of Glass raid",
		},
		"raid without a difficulty": {
			activity: "Deep Stone Crypt",
			want:     "Deep Stone Crypt raid",
		},
		"dungeon": {
			activity: "Duality: Standard",
			want:     "Duality dungeon",
		},
		"difficulty in parentheses": {
			activity: "Starcrossed (Legend)",
			want:     "Starcrossed mission",
		},
		"mission whose name holds a colon": {
			activity: "Operation: Seraph's Shield: Customize",
			want:     "Operation: Seraph's Shield mission",
		},
		// The Destiny definitions give the Avalon mission another name.
		"mission with another name than its place": {
			activity: "//node.ovrd.AVALON//: Customize",
			want:     "Avalon mission",
		},
		"activity that is not a raid, a dungeon, or a mission": {
			activity: "Dares of Eternity: Legend",
			want:     "Dares of Eternity",
		},
		"name in another letter case": {
			activity: "VOW OF THE DISCIPLE: master",
			want:     "Vow of the Disciple raid",
		},
		"name with a curly apostrophe": {
			activity: "King\u2019s Fall: Standard",
			want:     "King's Fall raid",
		},
		// The Pantheon brings back raid bosses but drops none of the
		// weapons of the Encore mission.
		"name that holds the name of a place after its start": {
			activity: "Featured Encore: Warpriest: The Pantheon",
		},
		"name that continues the name of a place without a separator": {
			activity: "Encores",
		},
		"strike": {
			activity: "Nightfall: Grandmaster",
		},
		"destination": {
			activity: "Neomuna",
		},
		"vendor": {
			activity: "X\u00fbr",
		},
	}

	table, err := loot.Load()
	require.NoError(t, err)

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := table.Place(tc.activity)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.want != "", ok)
		})
	}
}
