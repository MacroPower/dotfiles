package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventorytest"
)

// Plugs of the roll tests that the fixture definitions do not hold. The
// enhanced Rampage has a name of its own. The enhanced Kill Clip of the
// fixture plugs shares the name of its base trait.
var (
	plugFirefly         = inventory.Plug{Name: "Firefly", Type: "Trait", Hash: 901}
	plugRampageEnhanced = inventory.Plug{Name: "Rampage Enhanced", Type: "Enhanced Trait", Hash: 909}
)

// rolledPerks returns the perks of a copy that holds Explosive Payload
// with Rampage beside it in one socket, and Firefly alone in another.
func rolledPerks() []inventory.Perk {
	return []inventory.Perk{
		{
			Plug:  inventorytest.PlugExplosivePayload,
			Plugs: []inventory.Plug{inventorytest.PlugExplosivePayload, inventorytest.PlugRampage},
		},
		{Plug: plugFirefly, Plugs: []inventory.Plug{plugFirefly}, Socket: 1},
	}
}

// rollDefs returns definitions that name the plugs of the pick cases.
// Plugs 904 and 905 are the base trait and the enhanced trait of one
// name. The enhanced Rampage, plug 909, has a name of its own. The
// definitions do not hold plug 999.
func rollDefs() *definitions.Defs {
	return &definitions.Defs{Items: map[uint32]definitions.ItemDef{
		900: {Name: "Explosive Payload"},
		901: {Name: "Firefly"},
		903: {Name: "Rampage"},
		904: {Name: "Kill Clip"},
		905: {Name: "Kill Clip"},
		906: {Name: "Chill Clip"},
		909: {Name: "Rampage Enhanced"},
	}}
}

// listed returns the source of a loaded sheet that gives item 100 a row
// with the given tier and perk columns. As in a decoded sheet, a row
// without columns has no entry in the column map. The source has no
// stance map. Its definitions are empty, so its verdicts name no pick.
func listed(tier string, columns ...[]uint32) RatingSource {
	sheet := &Sheet{
		Rows:    map[uint32]SheetRow{100: {Tier: tier}},
		Columns: map[uint32][][]uint32{},
	}

	if len(columns) > 0 {
		sheet.Columns[100] = columns
	}

	return RatingSource{Sheet: sheet, Defs: &definitions.Defs{}}
}

// hits returns the verdict of a column that hit with the named perks,
// none of which the user likes.
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
		err    error
		want   *Roll
		source RatingSource
		perks  []inventory.Perk
	}{
		"source with a reason and no sheet returns the reason": {
			source: RatingSource{Err: errNoSheet},
			perks:  rolledPerks(),
			err:    errNoSheet,
		},
		"weapon that the sheet does not list has no roll": {
			source: RatingSource{
				Sheet: &Sheet{
					Rows:    map[uint32]SheetRow{101: {Tier: "B"}},
					Columns: map[uint32][][]uint32{101: {{900}}},
				},
				Defs: &definitions.Defs{},
			},
			perks: rolledPerks(),
		},
		"row without columns keeps its tier": {
			source: listed("A"),
			perks:  rolledPerks(),
			want:   &Roll{Row: SheetRow{Tier: "A"}, Overall: "A"},
		},
		"row without columns on a copy without perks keeps its tier": {
			source: listed("A"),
			want:   &Roll{Row: SheetRow{Tier: "A"}, Overall: "A"},
		},
		"row without a tier and without columns has no tier": {
			source: listed(""),
			perks:  rolledPerks(),
			want:   &Roll{},
		},
		"copy without perks is unknown": {
			source: listed("S", []uint32{900}, []uint32{901}),
			want:   &Roll{Row: SheetRow{Tier: "S"}, Unknown: true},
		},
		"row without a tier on a copy without perks is unknown": {
			source: listed("", []uint32{900}),
			want:   &Roll{Unknown: true},
		},
		"no miss keeps the tier": {
			source: listed("S", []uint32{900}, []uint32{901}),
			perks:  rolledPerks(),
			want: &Roll{
				Row:     SheetRow{Tier: "S"},
				Overall: "S",
				Columns: []ColumnVerdict{hits("Explosive Payload"), hits("Firefly")},
			},
		},
		"one miss moves the tier down one step": {
			source: listed("S", []uint32{900}, []uint32{904}),
			perks:  rolledPerks(),
			want: &Roll{
				Row:     SheetRow{Tier: "S"},
				Overall: "A",
				Columns: []ColumnVerdict{hits("Explosive Payload"), {}},
			},
		},
		"two misses move the tier down two steps": {
			source: listed("S", []uint32{904}, []uint32{905}),
			perks:  rolledPerks(),
			want: &Roll{
				Row:     SheetRow{Tier: "S"},
				Overall: "B",
				Columns: []ColumnVerdict{{}, {}},
			},
		},
		"two misses from E stop at F": {
			source: listed("E", []uint32{904}, []uint32{905}),
			perks:  rolledPerks(),
			want: &Roll{
				Row:     SheetRow{Tier: "E"},
				Overall: "F",
				Columns: []ColumnVerdict{{}, {}},
			},
		},
		"two misses from F stay at F": {
			source: listed("F", []uint32{904}, []uint32{905}),
			perks:  rolledPerks(),
			want: &Roll{
				Row:     SheetRow{Tier: "F"},
				Overall: "F",
				Columns: []ColumnVerdict{{}, {}},
			},
		},
		"row without a tier that has columns keeps its verdicts": {
			source: listed("", []uint32{900}, []uint32{904}),
			perks:  rolledPerks(),
			want:   &Roll{Columns: []ColumnVerdict{hits("Explosive Payload"), {}}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := rate(tc.source, &inventory.Item{Hash: 100, Perks: tc.perks})

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
		perks   []inventory.Perk
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
			perks: []inventory.Perk{
				{Plug: inventorytest.PlugExplosivePayload, Plugs: []inventory.Plug{inventorytest.PlugRampage}},
			},
			columns: [][]uint32{{900}},
			want:    []ColumnVerdict{hits("Explosive Payload")},
		},
		"socket without a plug list counts its active plug": {
			perks:   []inventory.Perk{{Plug: plugFirefly}},
			columns: [][]uint32{{901}},
			want:    []ColumnVerdict{hits("Firefly")},
		},
		"no socket holds a pick": {
			perks:   rolledPerks(),
			columns: [][]uint32{{904, 905}},
			want:    []ColumnVerdict{{}},
		},
		"two sockets hold picks and the first one names its picks": {
			perks: []inventory.Perk{
				{
					Plug:  inventorytest.PlugExplosivePayload,
					Plugs: []inventory.Plug{inventorytest.PlugExplosivePayload, inventorytest.PlugRampage},
				},
				{Plug: plugFirefly, Plugs: []inventory.Plug{plugFirefly, inventorytest.PlugKillClip}, Socket: 1},
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
			perks: []inventory.Perk{
				{
					Plug: inventorytest.PlugExplosivePayload,
					Plugs: []inventory.Plug{
						inventorytest.PlugExplosivePayload,
						inventorytest.PlugKillClip,
						inventorytest.PlugKillClipEnhanced,
					},
				},
			},
			columns: [][]uint32{{904, 905}},
			want:    []ColumnVerdict{hits("Kill Clip")},
		},
		"enhanced plug is a pick and its base plug is not": {
			perks: []inventory.Perk{
				{
					Plug:  inventorytest.PlugKillClip,
					Plugs: []inventory.Plug{inventorytest.PlugKillClip, inventorytest.PlugKillClipEnhanced},
				},
			},
			columns: [][]uint32{{905}},
			want:    []ColumnVerdict{hits("Kill Clip")},
		},
		"active plug comes first when the plug list has it after another pick": {
			perks: []inventory.Perk{
				{
					Plug:  inventorytest.PlugRampage,
					Plugs: []inventory.Plug{inventorytest.PlugExplosivePayload, inventorytest.PlugRampage},
				},
			},
			columns: [][]uint32{{900, 903}},
			want:    []ColumnVerdict{hits("Rampage", "Explosive Payload")},
		},
		"plugs whose names differ stay apart": {
			perks: []inventory.Perk{
				{
					Plug:  inventorytest.PlugRampage,
					Plugs: []inventory.Plug{inventorytest.PlugRampage, plugRampageEnhanced},
				},
			},
			columns: [][]uint32{{903, 909}},
			want:    []ColumnVerdict{hits("Rampage", "Rampage Enhanced")},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := rate(listed("S", tc.columns...), &inventory.Item{Hash: 100, Perks: tc.perks})

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

	item := &inventory.Item{Hash: 100, Perks: rolledPerks()}

	sources := map[string]RatingSource{
		modePvE: listed("S", []uint32{900}, []uint32{901}),
		modePvP: listed("C", []uint32{900}, []uint32{904}),
	}

	want := map[string]string{modePvE: "S", modePvP: "D"}

	for _, mode := range perkModes {
		got := rate(sources[mode], item)

		require.NotNilf(t, got.Roll, "mode %s", mode)
		assert.Equalf(t, want[mode], got.Roll.Overall, "mode %s", mode)
	}
}

func TestStancesByMode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want  map[string]map[string]string
		prefs []dvm.PerkPreference
	}{
		"no preference gives each mode an empty map": {
			want: map[string]map[string]string{modePvE: {}, modePvP: {}},
		},
		// The set_perk_preferences tool writes a removal as a removed
		// row with the stance up.
		"removed row has no stance": {
			prefs: []dvm.PerkPreference{{Mode: modePvE, Perk: "Firefly", Stance: stanceUp, Removed: true}},
			want:  map[string]map[string]string{modePvE: {}, modePvP: {}},
		},
		"each row lands only in the map of its mode": {
			prefs: []dvm.PerkPreference{
				{Mode: modePvE, Perk: "Firefly", Stance: stanceUp},
				{Mode: modePvE, Perk: "Kill Clip", Stance: stanceDown},
				{Mode: modePvP, Perk: "Rampage", Stance: stanceDown},
				{Mode: modePvP, Perk: "Kill Clip", Stance: stanceUp},
			},
			want: map[string]map[string]string{
				modePvE: {"Firefly": stanceUp, "Kill Clip": stanceDown},
				modePvP: {"Rampage": stanceDown, "Kill Clip": stanceUp},
			},
		},
		"row of another mode lands in neither map": {
			prefs: []dvm.PerkPreference{
				{Mode: "gambit", Perk: "Firefly", Stance: stanceUp},
				{Perk: "Rampage", Stance: stanceDown},
			},
			want: map[string]map[string]string{modePvE: {}, modePvP: {}},
		},
		"perk name is the key as the row has it": {
			prefs: []dvm.PerkPreference{{Mode: modePvE, Perk: " rampage Enhanced", Stance: stanceUp}},
			want:  map[string]map[string]string{modePvE: {" rampage Enhanced": stanceUp}, modePvP: {}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, stancesByMode(tc.prefs))
		})
	}
}

// TestRateStances rates a copy against one sheet row with the tier S and
// the given columns. The source carries the given stances.
func TestRateStances(t *testing.T) {
	t.Parallel()

	liked := func(name string) Hit { return Hit{Name: name, Liked: true} }

	tests := map[string]struct {
		stances map[string]string
		// overall is the tier of the copy.
		overall string
		perks   []inventory.Perk
		columns [][]uint32
		want    []ColumnVerdict
	}{
		"disliked only pick misses and moves the tier down one step": {
			stances: map[string]string{"Explosive Payload": stanceDown},
			perks:   rolledPerks(),
			columns: [][]uint32{{900}, {901}},
			want:    []ColumnVerdict{{}, hits("Firefly")},
			overall: "A",
		},
		"disliked pick leaves the other pick of its socket": {
			stances: map[string]string{"Rampage": stanceDown},
			perks:   rolledPerks(),
			columns: [][]uint32{{900, 903}},
			want:    []ColumnVerdict{hits("Explosive Payload")},
			overall: "S",
		},
		"liked perk earns the column beside a disliked only pick": {
			stances: map[string]string{"Explosive Payload": stanceDown, "Rampage": stanceUp},
			perks:   rolledPerks(),
			columns: [][]uint32{{900}},
			want:    []ColumnVerdict{{Hits: []Hit{liked("Rampage")}}},
			overall: "S",
		},
		// The copy rolled no pick of the second column, and no plug set
		// names the socket of the column.
		"liked perk misses a column that rolled no pick when the definitions hold no plug sets": {
			stances: map[string]string{"Firefly": stanceUp},
			perks:   rolledPerks(),
			columns: [][]uint32{{900}, {904}},
			want:    []ColumnVerdict{hits("Explosive Payload"), {}},
			overall: "A",
		},
		"disliked only pick of the first socket misses before a later socket with a pick": {
			stances: map[string]string{"Explosive Payload": stanceDown},
			perks: []inventory.Perk{
				{
					Plug:  inventorytest.PlugExplosivePayload,
					Plugs: []inventory.Plug{inventorytest.PlugExplosivePayload, inventorytest.PlugRampage},
				},
				{Plug: plugFirefly, Plugs: []inventory.Plug{plugFirefly, inventorytest.PlugKillClip}, Socket: 1},
			},
			columns: [][]uint32{{900, 904}},
			want:    []ColumnVerdict{{}},
			overall: "A",
		},
		"liked pick carries the mark beside another pick": {
			stances: map[string]string{"Rampage": stanceUp},
			perks:   rolledPerks(),
			columns: [][]uint32{{900, 903}},
			want:    []ColumnVerdict{{Hits: []Hit{{Name: "Explosive Payload"}, liked("Rampage")}}},
			overall: "S",
		},
		"liked perk that is not a pick comes before a later pick": {
			stances: map[string]string{"Explosive Payload": stanceUp},
			perks:   rolledPerks(),
			columns: [][]uint32{{903}},
			want:    []ColumnVerdict{{Hits: []Hit{liked("Explosive Payload"), {Name: "Rampage"}}}},
			overall: "S",
		},
		"liked base name counts a plug with the Enhanced suffix": {
			stances: map[string]string{"Rampage": stanceUp},
			perks: []inventory.Perk{
				{
					Plug:  inventorytest.PlugExplosivePayload,
					Plugs: []inventory.Plug{inventorytest.PlugExplosivePayload, plugRampageEnhanced},
				},
			},
			columns: [][]uint32{{900}},
			want:    []ColumnVerdict{{Hits: []Hit{{Name: "Explosive Payload"}, liked("Rampage Enhanced")}}},
			overall: "S",
		},
		"disliked base name drops a pick with the Enhanced suffix": {
			stances: map[string]string{"Rampage": stanceDown},
			perks:   []inventory.Perk{{Plug: plugRampageEnhanced, Plugs: []inventory.Plug{plugRampageEnhanced}}},
			columns: [][]uint32{{909}},
			want:    []ColumnVerdict{{}},
			overall: "A",
		},
		"liked base plug and enhanced plug of one name hit one time": {
			stances: map[string]string{"Kill Clip": stanceUp},
			perks: []inventory.Perk{
				{
					Plug: inventorytest.PlugExplosivePayload,
					Plugs: []inventory.Plug{
						inventorytest.PlugExplosivePayload,
						inventorytest.PlugKillClip,
						inventorytest.PlugKillClipEnhanced,
					},
				},
			},
			columns: [][]uint32{{900}},
			want:    []ColumnVerdict{{Hits: []Hit{{Name: "Explosive Payload"}, liked("Kill Clip")}}},
			overall: "S",
		},
		"stance that is neither up nor down changes nothing": {
			stances: map[string]string{"Explosive Payload": "sideways", "Rampage": ""},
			perks:   rolledPerks(),
			columns: [][]uint32{{900}},
			want:    []ColumnVerdict{hits("Explosive Payload")},
			overall: "S",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := listed("S", tc.columns...)
			source.Stances = tc.stances

			got := rate(source, &inventory.Item{Hash: 100, Perks: tc.perks})

			require.NoError(t, got.Err)
			require.NotNil(t, got.Roll)
			assert.Equal(t, tc.want, got.Roll.Columns)
			assert.Equal(t, tc.overall, got.Roll.Overall)
		})
	}
}

// TestRateStancesOfOneMode pins that a preference counts only in its own
// game mode. The user dislikes the only pick of the column for PvP.
func TestRateStancesOfOneMode(t *testing.T) {
	t.Parallel()

	item := &inventory.Item{Hash: 100, Perks: rolledPerks()}

	stances := stancesByMode([]dvm.PerkPreference{
		{Mode: modePvP, Perk: "Explosive Payload", Stance: stanceDown},
	})

	want := map[string][]ColumnVerdict{
		modePvE: {hits("Explosive Payload")},
		modePvP: {{}},
	}

	for _, mode := range perkModes {
		source := listed("S", []uint32{900})
		source.Stances = stances[mode]

		got := rate(source, item)

		require.NotNilf(t, got.Roll, "mode %s", mode)
		assert.Equalf(t, want[mode], got.Roll.Columns, "mode %s", mode)
	}
}

// TestRatePlugSets rates a copy against one sheet row with the tier S
// and the given columns. The definitions of the source give the weapon
// the given plug set hash for each socket index, and they hold the given
// plug sets.
func TestRatePlugSets(t *testing.T) {
	t.Parallel()

	likesFirefly := map[string]string{"Firefly": stanceUp}
	liked := ColumnVerdict{Hits: []Hit{{Name: "Firefly", Liked: true}}}

	tests := map[string]struct {
		stances  map[string]string
		plugSets map[uint32][]uint32
		// overall is the tier of the copy.
		overall string
		perks   []inventory.Perk
		sockets []uint32
		columns [][]uint32
		want    []ColumnVerdict
	}{
		"liked perk in the plug set socket earns the column and keeps the tier": {
			stances:  likesFirefly,
			perks:    rolledPerks(),
			sockets:  []uint32{0, 40},
			plugSets: map[uint32][]uint32{40: {901, 904, 905}},
			columns:  [][]uint32{{900}, {904, 905}},
			want:     []ColumnVerdict{hits("Explosive Payload"), liked},
			overall:  "S",
		},
		"column without a like stays a miss and moves the tier down one step": {
			perks:    rolledPerks(),
			sockets:  []uint32{0, 40},
			plugSets: map[uint32][]uint32{40: {901, 904, 905}},
			columns:  [][]uint32{{900}, {904, 905}},
			want:     []ColumnVerdict{hits("Explosive Payload"), {}},
			overall:  "A",
		},
		"liked perk that is not active earns the column": {
			stances: likesFirefly,
			perks: []inventory.Perk{
				{
					Plug:  inventorytest.PlugExplosivePayload,
					Plugs: []inventory.Plug{inventorytest.PlugExplosivePayload, inventorytest.PlugRampage},
				},
				{
					Plug:   inventorytest.PlugKillClip,
					Plugs:  []inventory.Plug{inventorytest.PlugKillClip, plugFirefly},
					Socket: 1,
				},
			},
			sockets:  []uint32{0, 40},
			plugSets: map[uint32][]uint32{40: {906}},
			columns:  [][]uint32{{906}},
			want:     []ColumnVerdict{liked},
			overall:  "S",
		},
		"perk without a stance beside the liked perk is no hit": {
			stances: likesFirefly,
			perks: []inventory.Perk{
				{
					Plug:  inventorytest.PlugExplosivePayload,
					Plugs: []inventory.Plug{inventorytest.PlugExplosivePayload, inventorytest.PlugRampage},
				},
				{Plug: plugFirefly, Plugs: []inventory.Plug{plugFirefly, inventorytest.PlugKillClip}, Socket: 1},
			},
			sockets:  []uint32{0, 40},
			plugSets: map[uint32][]uint32{40: {906}},
			columns:  [][]uint32{{906}},
			want:     []ColumnVerdict{liked},
			overall:  "S",
		},
		// The first socket rolled the pick, so it is the socket of the
		// column. The plug set of the second socket also lists the
		// pick, and the rating does not read it.
		"socket that rolled a disliked pick keeps the column from the plug set socket": {
			stances:  map[string]string{"Explosive Payload": stanceDown, "Firefly": stanceUp},
			perks:    rolledPerks(),
			sockets:  []uint32{0, 40},
			plugSets: map[uint32][]uint32{40: {900}},
			columns:  [][]uint32{{900}},
			want:     []ColumnVerdict{{}},
			overall:  "A",
		},
		// The liked Firefly sits in the second socket.
		"first socket whose plug set lists the pick is the socket of the column": {
			stances:  likesFirefly,
			perks:    rolledPerks(),
			sockets:  []uint32{40, 41},
			plugSets: map[uint32][]uint32{40: {904}, 41: {904}},
			columns:  [][]uint32{{904}},
			want:     []ColumnVerdict{{}},
			overall:  "A",
		},
		"one liked perk earns two columns that resolve to one plug set socket": {
			stances:  likesFirefly,
			perks:    rolledPerks(),
			sockets:  []uint32{0, 40},
			plugSets: map[uint32][]uint32{40: {904, 906}},
			columns:  [][]uint32{{904}, {906}},
			want:     []ColumnVerdict{liked, liked},
			overall:  "S",
		},
		// The web client has the same case, "leaves a column unresolved
		// when the pick is nowhere on the weapon".
		"pick that no plug set lists stays a miss": {
			stances:  likesFirefly,
			perks:    rolledPerks(),
			sockets:  []uint32{40, 41},
			plugSets: map[uint32][]uint32{40: {900, 903}, 41: {901, 906}},
			columns:  [][]uint32{{904}},
			want:     []ColumnVerdict{{}},
			overall:  "A",
		},
		// The perk list holds the sockets 0 and 1, and the plug set of
		// socket 2 lists the pick.
		"plug set socket that the perk list leaves out is a miss": {
			stances:  likesFirefly,
			perks:    rolledPerks(),
			sockets:  []uint32{0, 0, 40},
			plugSets: map[uint32][]uint32{40: {904}},
			columns:  [][]uint32{{904}},
			want:     []ColumnVerdict{{}},
			overall:  "A",
		},
		// The plug sets of the sockets 0 and 1 list the pick. The perk
		// list leaves out socket 0, and the liked Firefly sits in
		// socket 1. The rating stops at socket 0.
		"later plug set socket does not stand in for one that the perk list leaves out": {
			stances: likesFirefly,
			perks: []inventory.Perk{
				{Plug: plugFirefly, Plugs: []inventory.Plug{plugFirefly}, Socket: 1},
				{
					Plug:   inventorytest.PlugExplosivePayload,
					Plugs:  []inventory.Plug{inventorytest.PlugExplosivePayload, inventorytest.PlugRampage},
					Socket: 2,
				},
			},
			sockets:  []uint32{40, 41},
			plugSets: map[uint32][]uint32{40: {904}, 41: {904}},
			columns:  [][]uint32{{904}},
			want:     []ColumnVerdict{{}},
			overall:  "A",
		},
		// The perk list leaves out socket 0, so the entry of socket 1
		// is its first entry.
		"plug set socket is the perk with its socket index": {
			stances: likesFirefly,
			perks: []inventory.Perk{
				{Plug: plugFirefly, Plugs: []inventory.Plug{plugFirefly}, Socket: 1},
				{
					Plug:   inventorytest.PlugExplosivePayload,
					Plugs:  []inventory.Plug{inventorytest.PlugExplosivePayload, inventorytest.PlugRampage},
					Socket: 2,
				},
			},
			sockets:  []uint32{0, 40},
			plugSets: map[uint32][]uint32{40: {904}},
			columns:  [][]uint32{{904}},
			want:     []ColumnVerdict{liked},
			overall:  "S",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := listed("S", tc.columns...)
			source.Stances = tc.stances
			source.Defs = &definitions.Defs{Sockets: map[uint32][]uint32{100: tc.sockets}, PlugSets: tc.plugSets}

			got := rate(source, &inventory.Item{Hash: 100, Perks: tc.perks})

			require.NoError(t, got.Err)
			require.NotNil(t, got.Roll)
			assert.Equal(t, tc.want, got.Roll.Columns)
			assert.Equal(t, tc.overall, got.Roll.Overall)
		})
	}
}

// TestRatePlugSetsOfOneMode pins that a liked perk earns a column that
// rolled no pick only in its own game mode. The user likes Firefly for
// PvE, and the plug set of the socket of Firefly lists the pick of the
// second column. The web client has the same case, "counts a perk pushed
// up as a hit in that mode only".
func TestRatePlugSetsOfOneMode(t *testing.T) {
	t.Parallel()

	item := &inventory.Item{Hash: 100, Perks: rolledPerks()}

	stances := stancesByMode([]dvm.PerkPreference{
		{Mode: modePvE, Perk: "Firefly", Stance: stanceUp},
	})

	want := map[string][]ColumnVerdict{
		modePvE: {hits("Explosive Payload"), {Hits: []Hit{{Name: "Firefly", Liked: true}}}},
		modePvP: {hits("Explosive Payload"), {}},
	}

	for _, mode := range perkModes {
		source := listed("S", []uint32{900}, []uint32{904})
		source.Stances = stances[mode]
		source.Defs = &definitions.Defs{
			Sockets:  map[uint32][]uint32{100: {0, 40}},
			PlugSets: map[uint32][]uint32{40: {904}},
		}

		got := rate(source, item)

		require.NotNilf(t, got.Roll, "mode %s", mode)
		assert.Equalf(t, want[mode], got.Roll.Columns, "mode %s", mode)
	}
}

func TestPicksOf(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stances map[string]string
		column  []uint32
		want    []string
	}{
		"picks keep the order of the column hashes": {
			column: []uint32{903, 900, 904},
			want:   []string{"Rampage", "Explosive Payload", "Kill Clip"},
		},
		"base plug and enhanced plug of one name give one entry": {
			column: []uint32{904, 900, 905},
			want:   []string{"Kill Clip", "Explosive Payload"},
		},
		"plug with the Enhanced suffix is an entry of its own": {
			column: []uint32{903, 909},
			want:   []string{"Rampage", "Rampage Enhanced"},
		},
		"hash without a definition drops out between two picks": {
			column: []uint32{900, 999, 903},
			want:   []string{"Explosive Payload", "Rampage"},
		},
		"disliked pick drops out": {
			stances: map[string]string{"Rampage": stanceDown},
			column:  []uint32{900, 903, 904},
			want:    []string{"Explosive Payload", "Kill Clip"},
		},
		"disliked base name drops a pick with the Enhanced suffix": {
			stances: map[string]string{"Rampage": stanceDown},
			column:  []uint32{909, 900},
			want:    []string{"Explosive Payload"},
		},
		"liked pick stays in the list one time": {
			stances: map[string]string{"Kill Clip": stanceUp},
			column:  []uint32{904, 905, 900},
			want:    []string{"Kill Clip", "Explosive Payload"},
		},
		"liked perk that the column does not pick stays out": {
			stances: map[string]string{"Firefly": stanceUp},
			column:  []uint32{900},
			want:    []string{"Explosive Payload"},
		},
		"stance that is neither up nor down changes nothing": {
			stances: map[string]string{"Rampage": "sideways"},
			column:  []uint32{903},
			want:    []string{"Rampage"},
		},
		"column whose picks are all disliked gives no pick": {
			stances: map[string]string{"Rampage": stanceDown, "Kill Clip": stanceDown},
			column:  []uint32{903, 904, 905, 909},
		},
		"column whose hashes all lack a definition gives no pick": {
			column: []uint32{998, 999},
		},
		"column without hashes gives no pick": {},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, picksOf(rollDefs(), tc.column, tc.stances))
		})
	}
}

// TestPicksOfOneMode pins that a dislike drops a pick only in its own
// game mode. The user dislikes Rampage for PvP.
func TestPicksOfOneMode(t *testing.T) {
	t.Parallel()

	stances := stancesByMode([]dvm.PerkPreference{
		{Mode: modePvP, Perk: "Rampage", Stance: stanceDown},
	})

	want := map[string][]string{
		modePvE: {"Explosive Payload", "Rampage"},
		modePvP: {"Explosive Payload"},
	}

	for _, mode := range perkModes {
		got := picksOf(rollDefs(), []uint32{900, 903}, stances[mode])

		assert.Equalf(t, want[mode], got, "mode %s", mode)
	}
}

// TestRatePicks rates a copy against one sheet row with the tier S and
// the given columns. The source carries the given stances and the
// definitions of [rollDefs].
func TestRatePicks(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stances map[string]string
		perks   []inventory.Perk
		columns [][]uint32
		want    []ColumnVerdict
	}{
		"hit column carries its picks": {
			perks:   rolledPerks(),
			columns: [][]uint32{{900, 903}},
			want: []ColumnVerdict{{
				Hits:  []Hit{{Name: "Explosive Payload"}, {Name: "Rampage"}},
				Picks: []string{"Explosive Payload", "Rampage"},
			}},
		},
		"missed column carries its picks when no socket holds one": {
			perks:   rolledPerks(),
			columns: [][]uint32{{904, 905, 906}},
			want:    []ColumnVerdict{{Picks: []string{"Kill Clip", "Chill Clip"}}},
		},
		// The plug list of the socket holds Firefly alone, so the copy
		// cannot hold Chill Clip in the column.
		"pick that no socket can hold is named beside a pick that rolled": {
			perks:   rolledPerks(),
			columns: [][]uint32{{901, 906}},
			want: []ColumnVerdict{{
				Hits:  []Hit{{Name: "Firefly"}},
				Picks: []string{"Firefly", "Chill Clip"},
			}},
		},
		"each column carries its own picks": {
			perks:   rolledPerks(),
			columns: [][]uint32{{900}, {904}},
			want: []ColumnVerdict{
				{Hits: []Hit{{Name: "Explosive Payload"}}, Picks: []string{"Explosive Payload"}},
				{Picks: []string{"Kill Clip"}},
			},
		},
		// The liked Firefly sits in a socket without a pick of the
		// second column, and the definitions hold no plug sets. Firefly
		// therefore neither earns the column nor joins its picks.
		"liked perk outside the column does not join the picks": {
			stances: map[string]string{"Firefly": stanceUp},
			perks:   rolledPerks(),
			columns: [][]uint32{{900}, {904}},
			want: []ColumnVerdict{
				{Hits: []Hit{{Name: "Explosive Payload"}}, Picks: []string{"Explosive Payload"}},
				{Picks: []string{"Kill Clip"}},
			},
		},
		"liked perk that earns the column beside a pick does not join the picks": {
			stances: map[string]string{"Explosive Payload": stanceUp},
			perks:   rolledPerks(),
			columns: [][]uint32{{903}},
			want: []ColumnVerdict{{
				Hits:  []Hit{{Name: "Explosive Payload", Liked: true}, {Name: "Rampage"}},
				Picks: []string{"Rampage"},
			}},
		},
		"disliked only pick is neither a hit nor a pick": {
			stances: map[string]string{"Explosive Payload": stanceDown},
			perks:   rolledPerks(),
			columns: [][]uint32{{900}, {901}},
			want: []ColumnVerdict{
				{},
				{Hits: []Hit{{Name: "Firefly"}}, Picks: []string{"Firefly"}},
			},
		},
		"disliked base name drops a pick with the Enhanced suffix": {
			stances: map[string]string{"Rampage": stanceDown},
			perks:   []inventory.Perk{{Plug: plugRampageEnhanced, Plugs: []inventory.Plug{plugRampageEnhanced}}},
			columns: [][]uint32{{909}},
			want:    []ColumnVerdict{{}},
		},
		"row without columns has no column verdict": {
			perks: rolledPerks(),
		},
		"copy without perks has no column verdict": {
			columns: [][]uint32{{900, 903}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := listed("S", tc.columns...)
			source.Stances = tc.stances
			source.Defs = rollDefs()

			got := rate(source, &inventory.Item{Hash: 100, Perks: tc.perks})

			require.NoError(t, got.Err)
			require.NotNil(t, got.Roll)
			assert.Equal(t, tc.want, got.Roll.Columns)
		})
	}
}

func TestRatedAtLeast(t *testing.T) {
	t.Parallel()

	// rolled returns the rating of a roll with the given tier.
	rolled := func(tier string) Rating {
		return Rating{Roll: &Roll{Row: SheetRow{Tier: "S"}, Overall: tier}}
	}

	tests := map[string]struct {
		rating Rating
		lowest string
		want   bool
	}{
		"tier S passes the minimum A": {
			rating: rolled("S"),
			lowest: "A",
			want:   true,
		},
		"tier A passes the minimum A": {
			rating: rolled("A"),
			lowest: "A",
			want:   true,
		},
		"tier B does not pass the minimum A": {
			rating: rolled("B"),
			lowest: "A",
		},
		"tier S passes the minimum F": {
			rating: rolled("S"),
			lowest: "F",
			want:   true,
		},
		"tier F passes the minimum F": {
			rating: rolled("F"),
			lowest: "F",
			want:   true,
		},
		"tier F does not pass the minimum E": {
			rating: rolled("F"),
			lowest: "E",
		},
		"row without a tier does not pass the minimum F": {
			rating: Rating{Roll: &Roll{}},
			lowest: "F",
		},
		"unknown roll does not pass the minimum F": {
			rating: Rating{Roll: &Roll{Row: SheetRow{Tier: "S"}, Unknown: true}},
			lowest: "F",
		},
		"weapon off the sheet does not pass the minimum F": {
			rating: Rating{},
			lowest: "F",
		},
		"mode without ratings does not pass the minimum F": {
			rating: Rating{Err: ErrNoRating},
			lowest: "F",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, ratedAtLeast(tc.rating, tc.lowest))
		})
	}
}
