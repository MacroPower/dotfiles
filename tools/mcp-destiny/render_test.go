package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestRenderActivitiesEmpty(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		rows   []ActivityRow
		offset int
		total  int
		kept   int
		listed int
		want   string
	}{
		"no activities": {
			want: "Bungie lists no activities that Hunter can play.",
		},
		"list leaves out every activity": {
			listed: 15,
			want: "The list leaves out all 15 activities that Bungie lists for Hunter, because the game hides " +
				"them, the definitions do not name them, or they are Story or Social activities.",
		},
		"list leaves out the one activity": {
			listed: 1,
			want: "The list leaves out the one activity that Bungie lists for Hunter, because the game hides it, " +
				"the definitions do not name it, or it is a Story or Social activity.",
		},
		"no matches": {
			kept:   3,
			listed: 5,
			want:   "No activities of Hunter match.",
		},
		"offset past the last activity": {
			offset: 4,
			total:  2,
			kept:   3,
			listed: 5,
			want:   "Offset 4 is past the last of 2 matching activities.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, renderActivities(tc.rows, tc.offset, tc.total, tc.kept, tc.listed, "Hunter"))
		})
	}
}

func TestRenderStats(t *testing.T) {
	t.Parallel()

	period, err := statPeriodOf("24h")
	require.NoError(t, err)

	to := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		summary statSummary
		want    []string
	}{
		"only currencies": {
			summary: statSummary{flows: []statFlow{{itemHash: itemGlimmer, currency: true, spent: 5, net: -5}}},
			want: []string{
				"Currencies and materials, currencies first:",
				"name | kind | item hash | net | gained | spent",
				"Glimmer | currency | 3159615086 | -5 | 0 | 5",
				"",
				"Weapon kills: none.",
			},
		},
		"only weapon kills": {
			summary: statSummary{weapons: []weaponKills{{instanceID: "1001", itemHash: 100, pvp: 3}}},
			want: []string{
				"Currencies and materials: no change.",
				"",
				"Weapon kills, most first:",
				"instance id | name | total | PvE | PvP",
				"1001 | Fatebringer | 3 | 0 | 3",
			},
		},
		"a net change of zero": {
			summary: statSummary{flows: []statFlow{{itemHash: 300, gained: 4, spent: 4}}},
			want: []string{
				"Enhancement Core | stack | 300 | 0 | 4 | 4",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tc.summary.from = period.start(to)
			tc.summary.to = to

			got := renderStats(fixtureDefs(), period, tc.summary)
			assert.Contains(t, got, "Period: Last 24 hours, 2026-11-11 12:00 UTC to 2026-11-12 12:00 UTC.\n")
			assert.Contains(t, got, strings.Join(tc.want, "\n"))
		})
	}
}
