package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Plugs of the roll tests that the fixture definitions do not hold. The
// enhanced Rampage has a name of its own. The enhanced Kill Clip of the
// fixture plugs shares the name of its base trait.
var (
	plugFirefly         = Plug{Name: "Firefly", Type: "Trait", Hash: 901}
	plugRampageEnhanced = Plug{Name: "Rampage Enhanced", Type: "Enhanced Trait", Hash: 909}
)

// rolledPerks returns the perks of a copy that holds Explosive Payload
// with Rampage beside it in one socket, and Firefly alone in another.
func rolledPerks() []Perk {
	return []Perk{
		{Plug: plugExplosivePayload, plugs: []Plug{plugExplosivePayload, plugRampage}},
		{Plug: plugFirefly, plugs: []Plug{plugFirefly}, Socket: 1},
	}
}

// listed returns the state of a loaded sheet that gives item 100 a row
// with the given tier and perk columns. As in a decoded sheet, a row
// without columns has no entry in the column map.
func listed(tier string, columns ...[]uint32) SheetState {
	sheet := &Sheet{
		Rows:    map[uint32]SheetRow{100: {Tier: tier}},
		Columns: map[uint32][][]uint32{},
	}

	if len(columns) > 0 {
		sheet.Columns[100] = columns
	}

	return SheetState{Sheet: sheet}
}

// hits returns the verdict of a column that hit with the named perks.
func hits(names ...string) ColumnVerdict {
	var verdict ColumnVerdict

	for _, name := range names {
		verdict.Hits = append(verdict.Hits, Hit{Name: name})
	}

	return verdict
}

func TestRate(t *testing.T) {
	t.Parallel()

	errNoSheet := fmt.Errorf("%w: the service publishes no Aegis sheet", ErrNoRating)

	tests := map[string]struct {
		// err is the reason that the rating carries.
		err   error
		want  *Roll
		state SheetState
		perks []Perk
	}{
		"state with a reason and no sheet returns the reason": {
			state: SheetState{Err: errNoSheet},
			perks: rolledPerks(),
			err:   errNoSheet,
		},
		"weapon that the sheet does not list has no roll": {
			state: SheetState{Sheet: &Sheet{
				Rows:    map[uint32]SheetRow{101: {Tier: "B"}},
				Columns: map[uint32][][]uint32{101: {{900}}},
			}},
			perks: rolledPerks(),
		},
		"row without columns keeps its tier": {
			state: listed("A"),
			perks: rolledPerks(),
			want:  &Roll{Row: SheetRow{Tier: "A"}, Overall: "A"},
		},
		"row without columns on a copy without perks keeps its tier": {
			state: listed("A"),
			want:  &Roll{Row: SheetRow{Tier: "A"}, Overall: "A"},
		},
		"row without a tier and without columns has no tier": {
			state: listed(""),
			perks: rolledPerks(),
			want:  &Roll{},
		},
		"copy without perks is unknown": {
			state: listed("S", []uint32{900}, []uint32{901}),
			want:  &Roll{Row: SheetRow{Tier: "S"}, Unknown: true},
		},
		"row without a tier on a copy without perks is unknown": {
			state: listed("", []uint32{900}),
			want:  &Roll{Unknown: true},
		},
		"no miss keeps the tier": {
			state: listed("S", []uint32{900}, []uint32{901}),
			perks: rolledPerks(),
			want: &Roll{
				Row:     SheetRow{Tier: "S"},
				Overall: "S",
				Columns: []ColumnVerdict{hits("Explosive Payload"), hits("Firefly")},
			},
		},
		"one miss moves the tier down one step": {
			state: listed("S", []uint32{900}, []uint32{904}),
			perks: rolledPerks(),
			want: &Roll{
				Row:     SheetRow{Tier: "S"},
				Overall: "A",
				Columns: []ColumnVerdict{hits("Explosive Payload"), {}},
			},
		},
		"two misses move the tier down two steps": {
			state: listed("S", []uint32{904}, []uint32{905}),
			perks: rolledPerks(),
			want: &Roll{
				Row:     SheetRow{Tier: "S"},
				Overall: "B",
				Columns: []ColumnVerdict{{}, {}},
			},
		},
		"two misses from E stop at F": {
			state: listed("E", []uint32{904}, []uint32{905}),
			perks: rolledPerks(),
			want: &Roll{
				Row:     SheetRow{Tier: "E"},
				Overall: "F",
				Columns: []ColumnVerdict{{}, {}},
			},
		},
		"two misses from F stay at F": {
			state: listed("F", []uint32{904}, []uint32{905}),
			perks: rolledPerks(),
			want: &Roll{
				Row:     SheetRow{Tier: "F"},
				Overall: "F",
				Columns: []ColumnVerdict{{}, {}},
			},
		},
		"row without a tier that has columns keeps its verdicts": {
			state: listed("", []uint32{900}, []uint32{904}),
			perks: rolledPerks(),
			want:  &Roll{Columns: []ColumnVerdict{hits("Explosive Payload"), {}}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := rate(tc.state, &Item{Hash: 100, Perks: tc.perks})

			if tc.err != nil {
				require.ErrorIs(t, got.Err, tc.err)
			} else {
				require.NoError(t, got.Err)
			}

			assert.Equal(t, tc.want, got.Roll)
		})
	}
}

func TestRateColumns(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		perks   []Perk
		columns [][]uint32
		want    []ColumnVerdict
	}{
		"active plug is a pick": {
			perks:   rolledPerks(),
			columns: [][]uint32{{900}},
			want:    []ColumnVerdict{hits("Explosive Payload")},
		},
		"plug that is not active is the only pick": {
			perks:   rolledPerks(),
			columns: [][]uint32{{903}},
			want:    []ColumnVerdict{hits("Rampage")},
		},
		"active plug is a pick that the plug list leaves out": {
			perks:   []Perk{{Plug: plugExplosivePayload, plugs: []Plug{plugRampage}}},
			columns: [][]uint32{{900}},
			want:    []ColumnVerdict{hits("Explosive Payload")},
		},
		"socket without a plug list counts its active plug": {
			perks:   []Perk{{Plug: plugFirefly}},
			columns: [][]uint32{{901}},
			want:    []ColumnVerdict{hits("Firefly")},
		},
		"no socket holds a pick": {
			perks:   rolledPerks(),
			columns: [][]uint32{{904, 905}},
			want:    []ColumnVerdict{{}},
		},
		"two sockets hold picks and the first one names its picks": {
			perks: []Perk{
				{Plug: plugExplosivePayload, plugs: []Plug{plugExplosivePayload, plugRampage}},
				{Plug: plugFirefly, plugs: []Plug{plugFirefly, plugKillClip}, Socket: 1},
			},
			columns: [][]uint32{{901, 904, 903}},
			want:    []ColumnVerdict{hits("Rampage")},
		},
		"two columns resolve to one socket": {
			perks:   rolledPerks(),
			columns: [][]uint32{{900}, {903}},
			want:    []ColumnVerdict{hits("Explosive Payload"), hits("Rampage")},
		},
		"base plug and enhanced plug of one name are both picks": {
			perks: []Perk{
				{Plug: plugExplosivePayload, plugs: []Plug{plugExplosivePayload, plugKillClip, plugKillClipEnhanced}},
			},
			columns: [][]uint32{{904, 905}},
			want:    []ColumnVerdict{hits("Kill Clip")},
		},
		"enhanced plug is a pick and its base plug is not": {
			perks:   []Perk{{Plug: plugKillClip, plugs: []Plug{plugKillClip, plugKillClipEnhanced}}},
			columns: [][]uint32{{905}},
			want:    []ColumnVerdict{hits("Kill Clip")},
		},
		"active plug comes first when the plug list has it after another pick": {
			perks:   []Perk{{Plug: plugRampage, plugs: []Plug{plugExplosivePayload, plugRampage}}},
			columns: [][]uint32{{900, 903}},
			want:    []ColumnVerdict{hits("Rampage", "Explosive Payload")},
		},
		"plugs whose names differ stay apart": {
			perks:   []Perk{{Plug: plugRampage, plugs: []Plug{plugRampage, plugRampageEnhanced}}},
			columns: [][]uint32{{903, 909}},
			want:    []ColumnVerdict{hits("Rampage", "Rampage Enhanced")},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := rate(listed("S", tc.columns...), &Item{Hash: 100, Perks: tc.perks})

			require.NoError(t, got.Err)
			require.NotNil(t, got.Roll)
			assert.Equal(t, tc.want, got.Roll.Columns)
		})
	}
}

// TestRateModes rates one copy against the sheet of each game mode. Each
// sheet has its own tier and its own columns for the weapon.
func TestRateModes(t *testing.T) {
	t.Parallel()

	item := &Item{Hash: 100, Perks: rolledPerks()}

	states := map[string]SheetState{
		modePvE: listed("S", []uint32{900}, []uint32{901}),
		modePvP: listed("C", []uint32{900}, []uint32{904}),
	}

	want := map[string]string{modePvE: "S", modePvP: "D"}

	for _, mode := range perkModes {
		got := rate(states[mode], item)

		require.NotNilf(t, got.Roll, "mode %s", mode)
		assert.Equalf(t, want[mode], got.Roll.Overall, "mode %s", mode)
	}
}
