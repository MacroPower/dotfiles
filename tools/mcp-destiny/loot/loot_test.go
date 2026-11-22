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
