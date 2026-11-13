package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EquipLoadout records a loadout equip as a step of the [stepMover].
func (m *stepMover) EquipLoadout(_ context.Context, action LoadoutAction) error {
	return m.step(fmt.Sprintf("equip loadout %d on %s", action.Index, action.CharacterID))
}

func TestSnapshotLoadouts(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)

	assert.Equal(t, []Loadout{
		{CharacterID: hunterID, Number: 2, InstanceIDs: []string{"1006", "1004"}},
		{CharacterID: hunterID, Number: 3, InstanceIDs: []string{"1001", "1004", "9999"}},
		{CharacterID: titanID, Number: 1, InstanceIDs: []string{"1006"}},
	}, s.Loadouts, "the loadouts follow the character id, then the number")
}

func TestFindLoadout(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err         error
		characterID string
		want        string
		number      int
		wantIDs     []string
	}{
		"saved loadout": {
			characterID: hunterID,
			number:      3,
			wantIDs:     []string{"1001", "1004", "9999"},
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
			assert.Equal(t, Loadout{CharacterID: tc.characterID, Number: tc.number, InstanceIDs: tc.wantIDs}, got)
		})
	}
}

func TestEquipLoadout(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		change       func(s *Snapshot)
		err          error
		fail         string
		characterID  string
		want         string
		wantStopped  string
		wantSteps    []string
		wantMissing  []string
		wantMoves    []string
		wantEquipped []string
		number       int
		wantChanged  bool
	}{
		"items that the character holds": {
			characterID:  hunterID,
			number:       2,
			wantSteps:    []string{"equip loadout 1 on c1"},
			wantChanged:  true,
			wantEquipped: []string{"1006", "1004"},
			want:         "Equipped loadout 2 on Hunter: Fatebringer (1006), Celestial Nighthawk (1004).",
		},
		"item in the vault": {
			characterID:  hunterID,
			number:       3,
			wantSteps:    []string{"from vault to c1", "equip loadout 2 on c1"},
			wantMoves:    []string{"1001 from vault"},
			wantMissing:  []string{"9999"},
			wantChanged:  true,
			wantEquipped: []string{"1001", "1004"},
			want: "- Moved Fatebringer (1001) from Vault to Hunter.\n" +
				"Equipped loadout 3 on Hunter: Fatebringer (1001), Celestial Nighthawk (1004). " +
				"The inventory does not hold these loadout items: 9999.",
		},
		"item on the character that is not transferable": {
			change: func(s *Snapshot) {
				s.byID["1004"].Transferable = false
			},
			characterID:  hunterID,
			number:       3,
			wantSteps:    []string{"from vault to c1", "equip loadout 2 on c1"},
			wantMoves:    []string{"1001 from vault"},
			wantMissing:  []string{"9999"},
			wantChanged:  true,
			wantEquipped: []string{"1001", "1004"},
			want: "- Moved Fatebringer (1001) from Vault to Hunter.\n" +
				"Equipped loadout 3 on Hunter: Fatebringer (1001), Celestial Nighthawk (1004). " +
				"The inventory does not hold these loadout items: 9999.",
		},
		"item equipped on another character": {
			characterID: titanID,
			number:      1,
			wantSteps: []string{
				"equip 1003 on c1", "to vault from c1", "from vault to c2", "equip loadout 0 on c2",
			},
			wantMoves:    []string{"1006 from c1, replaced by 1003"},
			wantChanged:  true,
			wantEquipped: []string{"1006"},
			want: "- Moved Fatebringer (1006) from Hunter to Titan. " +
				"Equipped Midnight Coup (1003) in its place on Hunter.\n" +
				"Equipped loadout 1 on Titan: Fatebringer (1006).",
		},
		"move fails": {
			characterID: titanID,
			number:      1,
			fail:        "from vault to c2",
			err:         errStep,
			wantSteps:   []string{"equip 1003 on c1", "to vault from c1"},
			wantStopped: "1006 from c1, replaced by 1003",
			wantChanged: true,
			want: "- Fatebringer (1006) did not reach Titan. It is at Vault. " +
				"Equipped Midnight Coup (1003) in its place on Hunter.\n" +
				"Loadout 1 is not equipped on Titan: moving Fatebringer (1006) to Titan: " +
				"transferring to Titan: step refused.",
		},
		"transfer to the vault fails": {
			characterID: titanID,
			number:      1,
			fail:        "to vault from c1",
			err:         errStep,
			wantSteps:   []string{"equip 1003 on c1"},
			wantStopped: "1006 from c1, replaced by 1003",
			wantChanged: true,
			want: "- Fatebringer (1006) did not reach Titan. It is at Hunter. " +
				"Equipped Midnight Coup (1003) in its place on Hunter.\n" +
				"Loadout 1 is not equipped on Titan: moving Fatebringer (1006) to Titan: " +
				"transferring to the vault: step refused.",
		},
		"move fails before a step": {
			characterID: titanID,
			number:      1,
			fail:        "equip 1003 on c1",
			err:         errStep,
			wantStopped: "1006 from c1",
			want: "- Fatebringer (1006) did not reach Titan. It is at Hunter (equipped).\n" +
				"Loadout 1 is not equipped on Titan: moving Fatebringer (1006) to Titan: " +
				"equipping Midnight Coup in its place: step refused.",
		},
		"equip fails": {
			characterID: hunterID,
			number:      2,
			fail:        "equip loadout 1 on c1",
			err:         errStep,
			want:        "Loadout 2 is not equipped on Hunter: equipping the loadout: step refused.",
		},
	}

	describe := func(move LoadoutMove) string {
		desc := move.Item.InstanceID + " from " + move.From
		if move.Replacement != nil {
			desc += ", replaced by " + move.Replacement.InstanceID
		}

		return desc
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			if tc.change != nil {
				tc.change(s)
			}

			mover := &stepMover{fail: tc.fail}

			l, err := s.FindLoadout(tc.characterID, tc.number)
			require.NoError(t, err)

			got, err := s.EquipLoadout(t.Context(), mover, l)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantSteps, mover.steps)
			assert.Equal(t, tc.wantMissing, got.Missing)
			assert.Equal(t, tc.wantChanged, got.Changed)
			assert.Equal(t, tc.err == nil, got.Equipped)
			assert.Equal(t, tc.want, renderLoadoutEquip(s, l, got, err))

			var moves []string

			for _, move := range got.Moves {
				moves = append(moves, describe(move))
			}

			assert.Equal(t, tc.wantMoves, moves)

			stopped := ""
			if got.Stopped != nil {
				stopped = describe(*got.Stopped)
			}

			assert.Equal(t, tc.wantStopped, stopped)

			for _, id := range tc.wantEquipped {
				item, ok := s.Find(id)
				require.True(t, ok)
				assert.True(t, item.Equipped, "%s is equipped", id)
				assert.Equal(t, tc.characterID, item.Owner, "%s is on the character", id)
			}
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

func TestEquipLoadoutFromPostmaster(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err          error
		fail         string
		want         string
		wantSteps    []string
		wantEquipped bool
	}{
		"loadout equipped": {
			wantSteps:    []string{"pull to c1", "equip loadout 3 on c1"},
			wantEquipped: true,
			want: "- Moved Fatebringer (1005) from the Postmaster to Hunter.\n" +
				"Equipped loadout 4 on Hunter: Fatebringer (1005).",
		},
		"equip fails after the pull": {
			fail:      "equip loadout 3 on c1",
			err:       errStep,
			wantSteps: []string{"pull to c1"},
			want: "- Moved Fatebringer (1005) from the Postmaster to Hunter.\n" +
				"Loadout 4 is not equipped on Hunter: equipping the loadout: step refused.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := fixtureSnapshot(t)
			mover := &stepMover{fail: tc.fail}
			l := Loadout{CharacterID: hunterID, Number: 4, InstanceIDs: []string{"1005"}}

			got, err := s.EquipLoadout(t.Context(), mover, l)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantSteps, mover.steps)
			assert.True(t, got.Changed, "the pull from the Postmaster changed the game")
			assert.Equal(t, tc.wantEquipped, got.Equipped)
			assert.Equal(t, tc.want, renderLoadoutEquip(s, l, got, err))

			item, ok := s.Find("1005")
			require.True(t, ok)
			assert.False(t, item.Postmaster)
			assert.Equal(t, hunterID, item.Owner)
			assert.Equal(t, tc.wantEquipped, item.Equipped)
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
		}},
	}

	assert.Equal(t, []Loadout{
		{CharacterID: hunterID, Number: 2, InstanceIDs: []string{"1001"}},
	}, loadoutsOf(&profile), "an item without an instance id holds nothing")
}

func TestEquipLoadoutWithoutItems(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)
	mover := &stepMover{}
	l := Loadout{CharacterID: hunterID, Number: 4, InstanceIDs: []string{"9999"}}

	got, err := s.EquipLoadout(t.Context(), mover, l)
	require.NoError(t, err)

	assert.Equal(t, []string{"equip loadout 3 on c1"}, mover.steps)
	assert.Equal(t, []string{"9999"}, got.Missing)
	assert.Empty(t, got.Items)
	assert.True(t, got.Changed)
	assert.True(t, got.Equipped)
	assert.Equal(t,
		"Equipped loadout 4 on Hunter. The inventory does not hold these loadout items: 9999.",
		renderLoadoutEquip(s, l, got, nil))
}
