package main

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/activities"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventorytest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/stats"
)

func TestEquipLoadout(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		change       func(t *testing.T, s *inventory.Snapshot)
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
			characterID:  bungietest.HunterID,
			number:       2,
			wantSteps:    []string{"equip loadout 1 on c1"},
			wantChanged:  true,
			wantEquipped: []string{"1006", "1004"},
			want:         "Equipped loadout 2 on Hunter: Fatebringer (1006), Celestial Nighthawk (1004).",
		},
		"item in the vault": {
			characterID:  bungietest.HunterID,
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
			change: func(t *testing.T, s *inventory.Snapshot) {
				t.Helper()

				item, ok := s.Find("1004")
				require.True(t, ok)

				item.Transferable = false
			},
			characterID:  bungietest.HunterID,
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
			characterID: bungietest.TitanID,
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
			characterID: bungietest.TitanID,
			number:      1,
			fail:        "from vault to c2",
			err:         bungietest.ErrStep,
			wantSteps:   []string{"equip 1003 on c1", "to vault from c1"},
			wantStopped: "1006 from c1, replaced by 1003",
			wantChanged: true,
			want: "- Fatebringer (1006) did not reach Titan. It is at Vault. " +
				"Equipped Midnight Coup (1003) in its place on Hunter.\n" +
				"Loadout 1 is not equipped on Titan: moving Fatebringer (1006) to Titan: " +
				"transferring to Titan: step refused.",
		},
		"transfer to the vault fails": {
			characterID: bungietest.TitanID,
			number:      1,
			fail:        "to vault from c1",
			err:         bungietest.ErrStep,
			wantSteps:   []string{"equip 1003 on c1"},
			wantStopped: "1006 from c1, replaced by 1003",
			wantChanged: true,
			want: "- Fatebringer (1006) did not reach Titan. It is at Hunter. " +
				"Equipped Midnight Coup (1003) in its place on Hunter.\n" +
				"Loadout 1 is not equipped on Titan: moving Fatebringer (1006) to Titan: " +
				"transferring to the vault: step refused.",
		},
		"move fails before a step": {
			characterID: bungietest.TitanID,
			number:      1,
			fail:        "equip 1003 on c1",
			err:         bungietest.ErrStep,
			wantStopped: "1006 from c1",
			want: "- Fatebringer (1006) did not reach Titan. It is at Hunter (equipped).\n" +
				"Loadout 1 is not equipped on Titan: moving Fatebringer (1006) to Titan: " +
				"equipping Midnight Coup in its place: step refused.",
		},
		"equip fails": {
			characterID: bungietest.HunterID,
			number:      2,
			fail:        "equip loadout 1 on c1",
			err:         bungietest.ErrStep,
			want:        "Loadout 2 is not equipped on Hunter: equipping the loadout: step refused.",
		},
	}

	describe := func(move inventory.LoadoutMove) string {
		desc := move.Item.InstanceID + " from " + move.From
		if move.Replacement != nil {
			desc += ", replaced by " + move.Replacement.InstanceID
		}

		return desc
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := inventorytest.Snapshot(t)
			if tc.change != nil {
				tc.change(t, s)
			}

			mover := &bungietest.StepMover{Fail: tc.fail}

			l, err := s.FindLoadout(tc.characterID, tc.number)
			require.NoError(t, err)

			got, err := s.EquipLoadout(t.Context(), mover, l)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantSteps, mover.Steps)
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
			err:       bungietest.ErrStep,
			wantSteps: []string{"pull to c1"},
			want: "- Moved Fatebringer (1005) from the Postmaster to Hunter.\n" +
				"Loadout 4 is not equipped on Hunter: equipping the loadout: step refused.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := inventorytest.Snapshot(t)
			mover := &bungietest.StepMover{Fail: tc.fail}
			l := inventory.Loadout{CharacterID: bungietest.HunterID, Number: 4, InstanceIDs: []string{"1005"}}

			got, err := s.EquipLoadout(t.Context(), mover, l)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantSteps, mover.Steps)
			assert.True(t, got.Changed, "the pull from the Postmaster changed the game")
			assert.Equal(t, tc.wantEquipped, got.Equipped)
			assert.Equal(t, tc.want, renderLoadoutEquip(s, l, got, err))

			item, ok := s.Find("1005")
			require.True(t, ok)
			assert.False(t, item.Postmaster)
			assert.Equal(t, bungietest.HunterID, item.Owner)
			assert.Equal(t, tc.wantEquipped, item.Equipped)
		})
	}
}

func TestEquipLoadoutWithoutItems(t *testing.T) {
	t.Parallel()

	s := inventorytest.Snapshot(t)
	mover := &bungietest.StepMover{}
	l := inventory.Loadout{CharacterID: bungietest.HunterID, Number: 4, InstanceIDs: []string{"9999"}}

	got, err := s.EquipLoadout(t.Context(), mover, l)
	require.NoError(t, err)

	assert.Equal(t, []string{"equip loadout 3 on c1"}, mover.Steps)
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
		rows   []activities.Row
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

	period, err := stats.PeriodOf("24h")
	require.NoError(t, err)

	to := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

	tests := map[string]struct {
		summary stats.Summary
		want    []string
	}{
		"only currencies": {
			summary: stats.Summary{
				Flows: []stats.Flow{{ItemHash: bungietest.ItemGlimmer, Currency: true, Spent: 5, Net: -5}},
			},
			want: []string{
				"Currencies and materials, currencies first:",
				"name | kind | item hash | net | gained | spent",
				"Glimmer | currency | 3159615086 | -5 | 0 | 5",
				"",
				"Weapon kills: none.",
			},
		},
		"only weapon kills": {
			summary: stats.Summary{Weapons: []stats.WeaponKills{{InstanceID: "1001", ItemHash: 100, PvP: 3}}},
			want: []string{
				"Currencies and materials: no change.",
				"",
				"Weapon kills, most first:",
				"instance id | name | total | PvE | PvP",
				"1001 | Fatebringer | 3 | 0 | 3",
			},
		},
		"a net change of zero": {
			summary: stats.Summary{Flows: []stats.Flow{{ItemHash: 300, Gained: 4, Spent: 4}}},
			want: []string{
				"Enhancement Core | stack | 300 | 0 | 4 | 4",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tc.summary.From = period.Start(to)
			tc.summary.To = to

			got := renderStats(definitionstest.Defs(), period, tc.summary)
			assert.Contains(t, got, "Period: Last 24 hours, 2026-11-11 12:00 UTC to 2026-11-12 12:00 UTC.\n")
			assert.Contains(t, got, strings.Join(tc.want, "\n"))
		})
	}
}

func TestUnsupportedRefusal(t *testing.T) {
	t.Parallel()

	evaluates := "The server evaluates is:weapon, is:armor, is:equipment, is:equippable, is:equipped, " +
		"is:invault, is:postmaster, is:inpostmaster, is:locked, is:unlocked, is:titan, is:hunter, is:warlock, " +
		"is:favorite, is:autorifle, is:handcannon, is:pulserifle, is:scoutrifle, is:fusionrifle, " +
		"is:sniperrifle, is:shotgun, is:machinegun, is:lmg, is:rocketlauncher, is:sidearm, is:sword, " +
		"is:grenadelauncher, is:heavygrenadelauncher, is:specialgrenadelauncher, is:tracerifle, " +
		"is:linearfusionrifle, is:lfr, is:submachine, is:smg, is:bow, is:glaive, is:transmat, is:weaponmod, " +
		"is:armormod, is:reptoken, is:primary, is:special, is:heavy, is:dupe, is:incurrentchar, " +
		"is:currentclass, is:common, is:uncommon, is:rare, is:legendary, is:exotic, is:white, is:green, " +
		"is:blue, is:purple, is:yellow, is:kinetic, is:arc, is:solar, is:void, is:stasis, is:strand, " +
		"is:light, is:dark, is:engrams, is:lostitems, is:messages, is:specialorders, is:kineticslot, " +
		"is:energy, is:power, is:helmet, is:gauntlets, is:chest, is:leg, is:classitem, is:subclass, " +
		"is:seasonalartifacts, is:ghost, is:emblems, is:ships, is:vehicle, is:emotes, is:finishers, " +
		"is:accessories, is:consumables, is:modifications, is:haspower, is:haslight, is:autotagged, tag:, " +
		"dupe:item, count:, id:, hash:, name:, exactname:, power:, light:, rate:, rating:, ratebase:, " +
		"ratingbase:, misses:, setbonus:, verdict:, pvprate:, pvprating:, pvpratebase:, pvpratingbase:, " +
		"pvpmisses:, pvpsetbonus:, and pvpverdict:. " +
		"Each is: word also works as not:<word>."

	tests := map[string]struct {
		terms []string
		want  string
	}{
		"one term": {
			terms: []string{"perk:outlaw"},
			want: "unsupported query: the server does not evaluate perk:outlaw, " +
				"so it previews nothing. The web client might still accept these terms. " + evaluates,
		},
		"two terms": {
			terms: []string{"is:masterwork", "perk:outlaw"},
			want: "unsupported query: the server does not evaluate is:masterwork, perk:outlaw, " +
				"so it previews nothing. The web client might still accept these terms. " + evaluates,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, unsupportedRefusal(tc.terms))
		})
	}
}

func TestUnratedRefusal(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		reason string
		want   string
	}{
		"PvP": {
			reason: "PvP: rating unavailable: the service publishes no PvP sheet",
			want: "the query reads a game mode without ratings. " +
				"PvP: rating unavailable: the service publishes no PvP sheet",
		},
		"PvE": {
			reason: "PvE: rating unavailable: the service publishes no Aegis sheet",
			want: "the query reads a game mode without ratings. " +
				"PvE: rating unavailable: the service publishes no Aegis sheet",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, unratedRefusal(tc.reason))
		})
	}
}

func TestListWithAnd(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		words []string
		want  string
	}{
		"none":  {want: ""},
		"one":   {words: []string{"a"}, want: "a"},
		"two":   {words: []string{"a", "b"}, want: "a and b"},
		"three": {words: []string{"a", "b", "c"}, want: "a, b, and c"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, listWithAnd(tc.words))
		})
	}
}
