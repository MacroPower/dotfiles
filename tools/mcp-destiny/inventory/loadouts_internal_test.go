package inventory

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
)

// fixtureLoadoutPlugs returns the plugs that the third loadout of the
// fixture Hunter saved for the vault hand cannon (1001).
func fixtureLoadoutPlugs() map[string][]Plug {
	return map[string][]Plug{"1001": {
		{Name: "Explosive Payload", Type: "Trait", Hash: 900},
		{Name: "Firefly", Type: "Trait", Description: definitionstest.Defs().Items[901].Description, Hash: 901},
	}}
}

func TestSnapshotLoadouts(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)

	assert.Equal(t, []Loadout{
		{
			CharacterID: bungietest.HunterID,
			Number:      2,
			InstanceIDs: []string{"1006", "1004"},
			NameHash:    1,
			IconHash:    2,
			ColorHash:   3,
		},
		{
			CharacterID: bungietest.HunterID,
			Number:      3,
			InstanceIDs: []string{"1001", "1004", "9999"},
			Plugs:       fixtureLoadoutPlugs(),
			NameHash:    4,
			IconHash:    5,
			ColorHash:   6,
		},
		{
			CharacterID: bungietest.TitanID,
			Number:      1,
			InstanceIDs: []string{"1006"},
			NameHash:    7,
			IconHash:    8,
			ColorHash:   9,
		},
	}, s.Loadouts, "the loadouts follow the character id, then the number")
}

func TestFindLoadout(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err         error
		characterID string
		want        string
		wantLoadout Loadout
		number      int
	}{
		"saved loadout": {
			characterID: bungietest.HunterID,
			number:      3,
			wantLoadout: Loadout{
				CharacterID: bungietest.HunterID,
				Number:      3,
				InstanceIDs: []string{"1001", "1004", "9999"},
				Plugs:       fixtureLoadoutPlugs(),
				NameHash:    4,
				IconHash:    5,
				ColorHash:   6,
			},
		},
		"loadout that holds nothing": {
			characterID: bungietest.HunterID,
			number:      1,
			err:         ErrUnknownLoadout,
			want: "unknown loadout: Hunter has no saved loadout with the number 1. " +
				"Its saved loadouts have the numbers 2, 3",
		},
		"character without loadouts": {
			characterID: "c3",
			number:      1,
			err:         ErrUnknownLoadout,
			want:        "unknown loadout: Warlock has no saved loadouts",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			s.Characters = append(s.Characters, Character{ID: "c3", Class: "Warlock"})

			got, err := s.FindLoadout(tc.characterID, tc.number)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.EqualError(t, err, tc.want)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantLoadout, got)
		})
	}
}

func TestEquipLoadoutMarksSlots(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		wantLocations map[string]string
		wantSteps     []string
		wantMoves     []string
		loadout       Loadout
	}{
		"item from the vault": {
			loadout: Loadout{
				CharacterID: bungietest.HunterID,
				Number:      3,
				InstanceIDs: []string{"1001", "1004", "9999"},
			},
			wantSteps: []string{"from vault to c1", "equip loadout 2 on c1"},
			wantMoves: []string{"1001"},
			wantLocations: map[string]string{
				"1001": "Hunter (equipped)",
				"1006": "Hunter",
			},
		},
		"item that the character holds": {
			loadout:   Loadout{CharacterID: bungietest.HunterID, Number: 4, InstanceIDs: []string{"1003", "1004"}},
			wantSteps: []string{"equip loadout 3 on c1"},
			wantLocations: map[string]string{
				"1003": "Hunter (equipped)",
				"1006": "Hunter",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			mover := &bungietest.StepMover{}

			got, err := s.EquipLoadout(t.Context(), mover, tc.loadout)
			require.NoError(t, err)

			assert.Equal(t, tc.wantSteps, mover.Steps)

			var moves []string

			for _, move := range got.Moves {
				moves = append(moves, move.Item.InstanceID)
			}

			assert.Equal(t, tc.wantMoves, moves)

			for id, want := range tc.wantLocations {
				item, ok := s.Find(id)
				require.True(t, ok)
				assert.Equal(t, want, s.LocationLabel(item), "the loadout item takes the slot of %s", id)
			}
		})
	}
}

func TestEquipLoadoutChecksMovesFirst(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		change        func(s *Snapshot)
		want          string
		wantLocations map[string]string
		ids           []string
	}{
		"item that is not transferable": {
			change: func(s *Snapshot) {
				s.byID["1006"].Transferable = false
			},
			ids:           []string{"1006"},
			want:          "cannot move: Fatebringer is not transferable",
			wantLocations: map[string]string{"1006": "Hunter (equipped)"},
		},
		"equipped item without a replacement": {
			change: func(s *Snapshot) {
				s.byID["1003"].Owner = OwnerVault
			},
			ids: []string{"1002", "1006"},
			want: "cannot move: Fatebringer is equipped on Hunter, " +
				"which holds no other item to equip in that slot",
			wantLocations: map[string]string{"1002": "Vault", "1006": "Hunter (equipped)"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			tc.change(s)

			mover := &bungietest.StepMover{}
			l := Loadout{CharacterID: bungietest.TitanID, Number: 2, InstanceIDs: tc.ids}

			got, err := s.EquipLoadout(t.Context(), mover, l)
			require.ErrorIs(t, err, ErrMove)
			require.EqualError(t, err, tc.want)
			assert.Empty(t, mover.Steps)
			assert.False(t, got.Changed)
			assert.Nil(t, got.Stopped)

			for id, want := range tc.wantLocations {
				item, ok := s.Find(id)
				require.True(t, ok)
				assert.Equal(t, want, s.LocationLabel(item), "%s did not move", id)
			}
		})
	}
}

func TestLoadoutsOfDropsEmptyItems(t *testing.T) {
	t.Parallel()

	var profile bungie.Profile

	profile.CharacterLoadouts.Data = map[string]bungie.ProfileLoadouts{
		bungietest.HunterID: {Loadouts: []bungie.ProfileLoadout{
			{Items: []bungie.ProfileLoadoutItem{{}}},
			{Items: []bungie.ProfileLoadoutItem{{InstanceID: "1001"}, {}}},
			{Items: []bungie.ProfileLoadoutItem{{InstanceID: emptyLoadoutItem}, {InstanceID: emptyLoadoutItem}}},
			{},
		}},
		bungietest.TitanID: {Loadouts: []bungie.ProfileLoadout{
			{Items: []bungie.ProfileLoadoutItem{{InstanceID: emptyLoadoutItem}, {InstanceID: "1006"}}},
		}},
	}

	assert.Equal(t, []Loadout{
		{CharacterID: bungietest.HunterID, Number: 2, InstanceIDs: []string{"1001"}},
		{CharacterID: bungietest.TitanID, Number: 1, InstanceIDs: []string{"1006"}},
	}, loadoutsOf(&profile, definitionstest.Defs()), "an item without an instance id or with the id 0 holds nothing")

	assert.Equal(t, map[string]int{bungietest.HunterID: 4, bungietest.TitanID: 1}, loadoutSlotsOf(&profile),
		"the slot count includes the empty slots")
}

func TestLoadoutsOfPlugs(t *testing.T) {
	t.Parallel()

	defs := definitionstest.Defs()
	defs.Items[920] = definitions.ItemDef{Name: "Empty Mod Socket", TypeName: "General Armor Mod"}
	defs.Items[921] = definitions.ItemDef{Name: "Recovery Mod", TypeName: "General Armor Mod"}
	defs.Items[922] = definitions.ItemDef{Name: "Radiant Orbs", TypeName: "Artifact Perk"}
	defs.Items[923] = definitions.ItemDef{Name: "Empty Artifact Mod"}
	defs.Items[924] = definitions.ItemDef{Name: "Empty Memento Socket", TypeName: "Memento"}
	defs.Items[925] = definitions.ItemDef{Name: "Untyped Perk"}
	defs.Items[926] = definitions.ItemDef{Name: "Kill Tracker", ItemType: bungie.ItemTypePlug}
	defs.Items[927] = definitions.ItemDef{Name: "Default Ornament", ItemType: bungie.ItemTypePlug}
	defs.Items[928] = definitions.ItemDef{
		Name:     "Iron Fellowship Vest",
		TypeName: "Chest Armor",
		ItemType: bungie.ItemTypeArmor,
	}
	defs.Items[929] = definitions.ItemDef{Name: "Thunderlord", TypeName: "Machine Gun", ItemType: bungie.ItemTypeWeapon}
	defs.Items[930] = definitions.ItemDef{
		Name:     "Temporal Tracker",
		TypeName: "Weapon Mod",
		ItemType: bungie.ItemTypePlug,
	}

	tests := map[string]struct {
		want        []Plug
		plugs       []uint32
		wantEmpty   int
		wantUnknown int
	}{
		"perks in socket order": {
			plugs: []uint32{922, 921},
			want: []Plug{
				{Name: "Radiant Orbs", Type: "Artifact Perk", Hash: 922},
				{Name: "Recovery Mod", Type: "General Armor Mod", Hash: 921},
			},
		},
		"sockets that the loadout does not hold": {
			plugs: []uint32{bungie.UnsetPlugHash, 921, bungie.UnsetPlugHash},
			want:  []Plug{{Name: "Recovery Mod", Type: "General Armor Mod", Hash: 921}},
		},
		"cosmetic plug and placeholder": {
			plugs:     []uint32{902, 920, 921},
			want:      []Plug{{Name: "Recovery Mod", Type: "General Armor Mod", Hash: 921}},
			wantEmpty: 1,
		},
		"plug that the definitions lack": {
			plugs:       []uint32{12345, 921},
			want:        []Plug{{Name: "Recovery Mod", Type: "General Armor Mod", Hash: 921}},
			wantUnknown: 1,
		},
		"plugs that the definitions do not describe": {
			plugs:       []uint32{925, 12345, bungie.UnsetPlugHash, 923},
			wantEmpty:   1,
			wantUnknown: 2,
		},
		"empty sockets without a plug to keep": {
			plugs:     []uint32{bungie.UnsetPlugHash, 923, 920, 923},
			wantEmpty: 3,
		},
		"cosmetic placeholder": {
			plugs: []uint32{924, 922},
			want:  []Plug{{Name: "Radiant Orbs", Type: "Artifact Perk", Hash: 922}},
		},
		"no plug to keep": {
			plugs: []uint32{bungie.UnsetPlugHash, 902, 924},
		},
		"untyped tracker and default ornament": {
			plugs: []uint32{926, 927},
		},
		"gear that gives the item its appearance": {
			plugs: []uint32{928, 929},
		},
		"cosmetic plugs beside perks": {
			plugs: []uint32{926, 921, 928, 930, 927},
			want: []Plug{
				{Name: "Recovery Mod", Type: "General Armor Mod", Hash: 921},
				{Name: "Temporal Tracker", Type: "Weapon Mod", Hash: 930},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var profile bungie.Profile

			profile.CharacterLoadouts.Data = map[string]bungie.ProfileLoadouts{
				bungietest.HunterID: {Loadouts: []bungie.ProfileLoadout{{Items: []bungie.ProfileLoadoutItem{
					{InstanceID: "1004", PlugHashes: tc.plugs},
					{InstanceID: "1006", PlugHashes: []uint32{bungie.UnsetPlugHash}},
				}}}},
			}

			var (
				want        map[string][]Plug
				wantEmpty   map[string]int
				wantUnknown map[string]int
			)

			if tc.want != nil {
				want = map[string][]Plug{"1004": tc.want}
			}

			if tc.wantEmpty > 0 {
				wantEmpty = map[string]int{"1004": tc.wantEmpty}
			}

			if tc.wantUnknown > 0 {
				wantUnknown = map[string]int{"1004": tc.wantUnknown}
			}

			got := loadoutsOf(&profile, defs)
			require.Len(t, got, 1)
			assert.Equal(t, []string{"1004", "1006"}, got[0].InstanceIDs)
			assert.Equal(t, want, got[0].Plugs, "an item without a plug to keep has no entry")
			assert.Equal(t, wantEmpty, got[0].EmptySockets, "an item without an empty socket has no entry")
			assert.Equal(t, wantUnknown, got[0].UnknownPlugs, "an item without an unknown plug has no entry")
			assert.Equal(t, want != nil || wantEmpty != nil || wantUnknown != nil, got[0].HoldsPlugs("1004"))
			assert.False(t, got[0].HoldsPlugs("1006"), "a socket that the loadout does not hold does not count")
			assert.False(t, got[0].PlugsPending)
		})
	}
}

func TestLoadoutSlot(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err         error
		characterID string
		want        string
		wantLoadout Loadout
		number      int
		wantSaved   bool
	}{
		"saved loadout": {
			characterID: bungietest.HunterID,
			number:      2,
			wantSaved:   true,
			wantLoadout: Loadout{
				CharacterID: bungietest.HunterID,
				Number:      2,
				InstanceIDs: []string{"1006", "1004"},
				NameHash:    1,
				IconHash:    2,
				ColorHash:   3,
			},
		},
		"empty slot": {
			characterID: bungietest.HunterID,
			number:      1,
			wantLoadout: Loadout{CharacterID: bungietest.HunterID, Number: 1},
		},
		"last slot": {
			characterID: bungietest.HunterID,
			number:      3,
			wantSaved:   true,
			wantLoadout: Loadout{
				CharacterID: bungietest.HunterID,
				Number:      3,
				InstanceIDs: []string{"1001", "1004", "9999"},
				Plugs:       fixtureLoadoutPlugs(),
				NameHash:    4,
				IconHash:    5,
				ColorHash:   6,
			},
		},
		"number after the last slot": {
			characterID: bungietest.HunterID,
			number:      4,
			err:         ErrNoLoadoutSlot,
			want: "no loadout slot: Hunter has no loadout slot with the number 4. " +
				"Its 3 loadout slots have the numbers 1 to 3",
		},
		"number zero": {
			characterID: bungietest.HunterID,
			number:      0,
			err:         ErrNoLoadoutSlot,
			want: "no loadout slot: Hunter has no loadout slot with the number 0. " +
				"Its 3 loadout slots have the numbers 1 to 3",
		},
		"character with fewer slots": {
			characterID: bungietest.TitanID,
			number:      2,
			err:         ErrNoLoadoutSlot,
			want: "no loadout slot: Titan has no loadout slot with the number 2. " +
				"Its only loadout slot has the number 1",
		},
		"character without slots": {
			characterID: "c3",
			number:      1,
			err:         ErrNoLoadoutSlot,
			want:        "no loadout slot: Warlock has no loadout slots",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			s.Characters = append(s.Characters, Character{ID: "c3", Class: "Warlock"})

			assert.Equal(t, map[string]int{bungietest.HunterID: 3, bungietest.TitanID: 1}, s.LoadoutSlots)

			got, saved, err := s.LoadoutSlot(tc.characterID, tc.number)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.EqualError(t, err, tc.want)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantSaved, saved)
			assert.Equal(t, tc.wantLoadout, got)
		})
	}
}

func TestLoadoutGear(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		change      func(s *Snapshot)
		characterID string
		want        []string
	}{
		"weapons and armor": {
			characterID: bungietest.HunterID,
			want:        []string{"1006", "1004"},
		},
		"subclass comes first": {
			change:      func(s *Snapshot) { s.byID["1004"].Bucket = bungie.BucketSubclass },
			characterID: bungietest.HunterID,
			want:        []string{"1004", "1006"},
		},
		"item in a bucket that a loadout does not hold": {
			change:      func(s *Snapshot) { s.byID["1004"].Bucket = bungie.BucketConsumables },
			characterID: bungietest.HunterID,
			want:        []string{"1006"},
		},
		"character without equipped gear": {
			characterID: bungietest.TitanID,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			if tc.change != nil {
				tc.change(s)
			}

			var got []string

			for _, item := range s.LoadoutGear(tc.characterID) {
				got = append(got, item.InstanceID)
			}

			assert.Equal(t, tc.want, got)
		})
	}
}

// loadoutKeys returns one line per loadout, with the character, the number,
// the identifiers, and the instance ids, in the order of loadouts. The
// names of the saved plugs of an item follow its id in brackets. A line
// ends with "pending" for a loadout with [Loadout.PlugsPending].
func loadoutKeys(loadouts []Loadout) []string {
	out := make([]string, 0, len(loadouts))
	for _, l := range loadouts {
		ids := make([]string, 0, len(l.InstanceIDs))
		for _, id := range l.InstanceIDs {
			if plugs := l.Plugs[id]; len(plugs) > 0 {
				id += "[" + strings.Join(plugNames(plugs), "+") + "]"
			}

			ids = append(ids, id)
		}

		key := fmt.Sprintf("%s %d %d/%d/%d %s",
			l.CharacterID, l.Number, l.NameHash, l.IconHash, l.ColorHash, strings.Join(ids, ","))
		if l.PlugsPending {
			key += " pending"
		}

		out = append(out, key)
	}

	return out
}

func TestRecordLoadoutSave(t *testing.T) {
	t.Parallel()

	ids := bungie.LoadoutIdentifiers{NameHash: 10, IconHash: 20, ColorHash: 30}

	tests := map[string]struct {
		change      func(s *Snapshot)
		characterID string
		want        []string
		number      int
	}{
		"empty slot before the saved loadouts": {
			characterID: bungietest.HunterID,
			number:      1,
			want: []string{
				"c1 1 10/20/30 1006,1004 pending",
				"c1 2 1/2/3 1006,1004",
				"c1 3 4/5/6 1001[Explosive Payload+Firefly],1004,9999",
				"c2 1 7/8/9 1006",
			},
		},
		"empty slot between the loadouts of two characters": {
			change:      func(s *Snapshot) { s.RecordLoadoutClear(bungietest.HunterID, 3) },
			characterID: bungietest.HunterID,
			number:      3,
			want: []string{
				"c1 2 1/2/3 1006,1004",
				"c1 3 10/20/30 1006,1004 pending",
				"c2 1 7/8/9 1006",
			},
		},
		"saved loadout": {
			characterID: bungietest.HunterID,
			number:      3,
			want: []string{
				"c1 2 1/2/3 1006,1004",
				"c1 3 10/20/30 1006,1004 pending",
				"c2 1 7/8/9 1006",
			},
		},
		"character without equipped gear": {
			characterID: bungietest.TitanID,
			number:      1,
			want: []string{
				"c1 2 1/2/3 1006,1004",
				"c1 3 4/5/6 1001[Explosive Payload+Firefly],1004,9999",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			if tc.change != nil {
				tc.change(s)
			}

			s.RecordLoadoutSave(tc.characterID, tc.number, ids)

			assert.Equal(t, tc.want, loadoutKeys(s.Loadouts))
		})
	}
}

func TestRecordLoadoutClear(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		characterID string
		want        []string
		number      int
	}{
		"saved loadout": {
			characterID: bungietest.HunterID,
			number:      2,
			want: []string{
				"c1 3 4/5/6 1001[Explosive Payload+Firefly],1004,9999",
				"c2 1 7/8/9 1006",
			},
		},
		"empty slot": {
			characterID: bungietest.HunterID,
			number:      1,
			want: []string{
				"c1 2 1/2/3 1006,1004",
				"c1 3 4/5/6 1001[Explosive Payload+Firefly],1004,9999",
				"c2 1 7/8/9 1006",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			s.RecordLoadoutClear(tc.characterID, tc.number)

			assert.Equal(t, tc.want, loadoutKeys(s.Loadouts))
		})
	}
}
