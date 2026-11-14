package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
)

// EquipLoadout records a loadout equip as a step of the [stepMover].
func (m *stepMover) EquipLoadout(_ context.Context, action LoadoutAction) error {
	return m.step(fmt.Sprintf("equip loadout %d on %s", action.Index, action.CharacterID))
}

func TestSnapshotLoadouts(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)

	assert.Equal(t, []Loadout{
		{
			CharacterID: hunterID,
			Number:      2,
			InstanceIDs: []string{"1006", "1004"},
			NameHash:    1,
			IconHash:    2,
			ColorHash:   3,
		},
		{
			CharacterID: hunterID,
			Number:      3,
			InstanceIDs: []string{"1001", "1004", "9999"},
			NameHash:    4,
			IconHash:    5,
			ColorHash:   6,
		},
		{
			CharacterID: titanID,
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
			characterID: hunterID,
			number:      3,
			wantLoadout: Loadout{
				CharacterID: hunterID,
				Number:      3,
				InstanceIDs: []string{"1001", "1004", "9999"},
				NameHash:    4,
				IconHash:    5,
				ColorHash:   6,
			},
		},
		"loadout that holds nothing": {
			characterID: hunterID,
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
			loadout:   Loadout{CharacterID: hunterID, Number: 3, InstanceIDs: []string{"1001", "1004", "9999"}},
			wantSteps: []string{"from vault to c1", "equip loadout 2 on c1"},
			wantMoves: []string{"1001"},
			wantLocations: map[string]string{
				"1001": "Hunter (equipped)",
				"1006": "Hunter",
			},
		},
		"item that the character holds": {
			loadout:   Loadout{CharacterID: hunterID, Number: 4, InstanceIDs: []string{"1003", "1004"}},
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
			mover := &stepMover{}

			got, err := s.EquipLoadout(t.Context(), mover, tc.loadout)
			require.NoError(t, err)

			assert.Equal(t, tc.wantSteps, mover.steps)

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
				s.byID["1003"].Owner = ownerVault
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

			mover := &stepMover{}
			l := Loadout{CharacterID: titanID, Number: 2, InstanceIDs: tc.ids}

			got, err := s.EquipLoadout(t.Context(), mover, l)
			require.ErrorIs(t, err, ErrMove)
			require.EqualError(t, err, tc.want)
			assert.Empty(t, mover.steps)
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

	var profile Profile

	profile.CharacterLoadouts.Data = map[string]ProfileLoadouts{
		hunterID: {Loadouts: []ProfileLoadout{
			{Items: []ProfileLoadoutItem{{}}},
			{Items: []ProfileLoadoutItem{{InstanceID: "1001"}, {}}},
			{Items: []ProfileLoadoutItem{{InstanceID: emptyLoadoutItem}, {InstanceID: emptyLoadoutItem}}},
			{},
		}},
		titanID: {Loadouts: []ProfileLoadout{
			{Items: []ProfileLoadoutItem{{InstanceID: emptyLoadoutItem}, {InstanceID: "1006"}}},
		}},
	}

	assert.Equal(t, []Loadout{
		{CharacterID: hunterID, Number: 2, InstanceIDs: []string{"1001"}},
		{CharacterID: titanID, Number: 1, InstanceIDs: []string{"1006"}},
	}, loadoutsOf(&profile), "an item without an instance id or with the id 0 holds nothing")

	assert.Equal(t, map[string]int{hunterID: 4, titanID: 1}, loadoutSlotsOf(&profile),
		"the slot count includes the empty slots")
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
			characterID: hunterID,
			number:      2,
			wantSaved:   true,
			wantLoadout: Loadout{
				CharacterID: hunterID,
				Number:      2,
				InstanceIDs: []string{"1006", "1004"},
				NameHash:    1,
				IconHash:    2,
				ColorHash:   3,
			},
		},
		"empty slot": {
			characterID: hunterID,
			number:      1,
			wantLoadout: Loadout{CharacterID: hunterID, Number: 1},
		},
		"last slot": {
			characterID: hunterID,
			number:      3,
			wantSaved:   true,
			wantLoadout: Loadout{
				CharacterID: hunterID,
				Number:      3,
				InstanceIDs: []string{"1001", "1004", "9999"},
				NameHash:    4,
				IconHash:    5,
				ColorHash:   6,
			},
		},
		"number after the last slot": {
			characterID: hunterID,
			number:      4,
			err:         ErrNoLoadoutSlot,
			want: "no loadout slot: Hunter has no loadout slot with the number 4. " +
				"Its 3 loadout slots have the numbers 1 to 3",
		},
		"number zero": {
			characterID: hunterID,
			number:      0,
			err:         ErrNoLoadoutSlot,
			want: "no loadout slot: Hunter has no loadout slot with the number 0. " +
				"Its 3 loadout slots have the numbers 1 to 3",
		},
		"character with fewer slots": {
			characterID: titanID,
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

			assert.Equal(t, map[string]int{hunterID: 3, titanID: 1}, s.LoadoutSlots)

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
			characterID: hunterID,
			want:        []string{"1006", "1004"},
		},
		"subclass comes first": {
			change:      func(s *Snapshot) { s.byID["1004"].Bucket = bungie.BucketSubclass },
			characterID: hunterID,
			want:        []string{"1004", "1006"},
		},
		"item in a bucket that a loadout does not hold": {
			change:      func(s *Snapshot) { s.byID["1004"].Bucket = bungie.BucketConsumables },
			characterID: hunterID,
			want:        []string{"1006"},
		},
		"character without equipped gear": {
			characterID: titanID,
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
// the identifiers, and the instance ids, in the order of loadouts.
func loadoutKeys(loadouts []Loadout) []string {
	out := make([]string, 0, len(loadouts))
	for _, l := range loadouts {
		out = append(out, fmt.Sprintf("%s %d %d/%d/%d %s",
			l.CharacterID, l.Number, l.NameHash, l.IconHash, l.ColorHash, strings.Join(l.InstanceIDs, ",")))
	}

	return out
}

func TestRecordLoadoutSave(t *testing.T) {
	t.Parallel()

	ids := LoadoutIdentifiers{NameHash: 10, IconHash: 20, ColorHash: 30}

	tests := map[string]struct {
		change      func(s *Snapshot)
		characterID string
		want        []string
		number      int
	}{
		"empty slot before the saved loadouts": {
			characterID: hunterID,
			number:      1,
			want: []string{
				"c1 1 10/20/30 1006,1004",
				"c1 2 1/2/3 1006,1004",
				"c1 3 4/5/6 1001,1004,9999",
				"c2 1 7/8/9 1006",
			},
		},
		"empty slot between the loadouts of two characters": {
			change:      func(s *Snapshot) { s.recordLoadoutClear(hunterID, 3) },
			characterID: hunterID,
			number:      3,
			want: []string{
				"c1 2 1/2/3 1006,1004",
				"c1 3 10/20/30 1006,1004",
				"c2 1 7/8/9 1006",
			},
		},
		"saved loadout": {
			characterID: hunterID,
			number:      3,
			want: []string{
				"c1 2 1/2/3 1006,1004",
				"c1 3 10/20/30 1006,1004",
				"c2 1 7/8/9 1006",
			},
		},
		"character without equipped gear": {
			characterID: titanID,
			number:      1,
			want: []string{
				"c1 2 1/2/3 1006,1004",
				"c1 3 4/5/6 1001,1004,9999",
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

			s.recordLoadoutSave(tc.characterID, tc.number, ids)

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
			characterID: hunterID,
			number:      2,
			want: []string{
				"c1 3 4/5/6 1001,1004,9999",
				"c2 1 7/8/9 1006",
			},
		},
		"empty slot": {
			characterID: hunterID,
			number:      1,
			want: []string{
				"c1 2 1/2/3 1006,1004",
				"c1 3 4/5/6 1001,1004,9999",
				"c2 1 7/8/9 1006",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			s.recordLoadoutClear(tc.characterID, tc.number)

			assert.Equal(t, tc.want, loadoutKeys(s.Loadouts))
		})
	}
}
