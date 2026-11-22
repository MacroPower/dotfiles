package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/activities"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/auth"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
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
			want: "- Fatebringer (1006) did not reach Titan. It is in the vault. " +
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
			want: "- Fatebringer (1006) did not reach Titan. It is on Hunter. " +
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
			want: "- Fatebringer (1006) did not reach Titan. It is equipped on Hunter.\n" +
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

func TestRenderTagsFavorites(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want      string
		favorites int
	}{
		"no favorites":  {want: "Favorites: 0 items"},
		"one favorite":  {favorites: 1, want: "Favorites: 1 item"},
		"two favorites": {favorites: 2, want: "Favorites: 2 items"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := &inventory.Snapshot{Items: []*inventory.Item{{Tag: "keep"}}}
			for range tc.favorites {
				s.Items = append(s.Items, &inventory.Item{Favorite: true})
			}

			got := renderTags(s)
			assert.Equal(t, tc.want, got[strings.LastIndex(got, "\n")+1:])
		})
	}
}

func TestRenderStats(t *testing.T) {
	t.Parallel()

	period, err := stats.PeriodOf("24h")
	require.NoError(t, err)

	to := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

	withheld := "The service withholds the definitions of dummy items, classified items, and items without a name, " +
		"so the server cannot name "
	later := "The game can hold an item that Bungie added in a later manifest version."

	tests := map[string]struct {
		// edit changes the fixture definitions before the render.
		edit    func(defs *definitions.Defs)
		summary stats.Summary
		// want holds the last lines of the output.
		want []string
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
				"",
				"Weapon kills: none.",
			},
		},
		"withheld stacks that the vendor items and the rewards name": {
			summary: stats.Summary{Flows: []stats.Flow{
				{ItemHash: 501, Gained: 2, Net: 2},
				{ItemHash: 600, Spent: 1, Net: -1},
			}},
			want: []string{
				"Legendary Shards | stack | 501 | +2 | 2 | 0",
				"Raid Gear | stack | 600 | -1 | 0 | 1",
				"",
				"Weapon kills: none.",
			},
		},
		"withheld stack that no table names": {
			summary: stats.Summary{Flows: []stats.Flow{{ItemHash: definitionstest.ItemWithheld, Gained: 1, Net: 1}}},
			want: []string{
				"Unknown item 78 | stack | 78 | +1 | 1 | 0",
				"",
				"Weapon kills: none.",
				"",
				withheld + "the item with the hash 78.",
			},
		},
		"withheld items and items that the manifest version lacks": {
			summary: stats.Summary{
				Flows: []stats.Flow{
					{ItemHash: 77, Currency: true, Gained: 2, Net: 2},
					{ItemHash: definitionstest.ItemWithheld, Gained: 1, Net: 1},
					{ItemHash: 999, Spent: 1, Net: -1},
					{ItemHash: 12345, Spent: 3, Net: -3},
				},
				Weapons: []stats.WeaponKills{
					{InstanceID: "1008", ItemHash: 103, PvE: 4},
					{InstanceID: "1009", ItemHash: 103, PvE: 2},
				},
			},
			want: []string{
				"Unknown item 77 | currency | 77 | +2 | 2 | 0",
				"Unknown item 78 | stack | 78 | +1 | 1 | 0",
				"Unknown item 999 | stack | 999 | -1 | 0 | 1",
				"Unknown item 12345 | stack | 12345 | -3 | 0 | 3",
				"",
				"Weapon kills, most first:",
				"instance id | name | total | PvE | PvP",
				"1008 | Unknown item 103 | 4 | 4 | 0",
				"1009 | Unknown item 103 | 2 | 2 | 0",
				"",
				withheld + "the items with the hashes 78 and 999. " +
					"The definitions of manifest version v1 lack the items with the hashes 77, 12345, and 103, " +
					"so the server cannot name them. " + later,
			},
		},
		"stack that a lookup at Bungie names": {
			edit: func(defs *definitions.Defs) {
				defs.Lookups = map[uint32]definitions.Lookup{
					definitionstest.ItemWithheld: {Name: "Wish-Keeper Snare Refit", Defined: true},
				}
			},
			summary: stats.Summary{Flows: []stats.Flow{{ItemHash: definitionstest.ItemWithheld, Gained: 1, Net: 1}}},
			want: []string{
				"Wish-Keeper Snare Refit | stack | 78 | +1 | 1 | 0",
				"",
				"Weapon kills: none.",
			},
		},
		"answers of Bungie without a name": {
			edit: func(defs *definitions.Defs) {
				defs.Lookups = map[uint32]definitions.Lookup{
					definitionstest.ItemWithheld: {Defined: true, Redacted: true},
					999:                          {Defined: true},
					12345:                        {},
				}
			},
			summary: stats.Summary{Flows: []stats.Flow{
				{ItemHash: definitionstest.ItemWithheld, Gained: 1, Net: 1},
				{ItemHash: 999, Spent: 1, Net: -1},
				{ItemHash: 12345, Spent: 3, Net: -3},
			}},
			want: []string{
				"Unknown item 78 | stack | 78 | +1 | 1 | 0",
				"Unknown item 999 | stack | 999 | -1 | 0 | 1",
				"Unknown item 12345 | stack | 12345 | -3 | 0 | 3",
				"",
				"Weapon kills: none.",
				"",
				"The definitions of manifest version v1 and the live manifest of Bungie both lack " +
					"the item with the hash 12345, so the server cannot name it. " +
					"Bungie classifies the item with the hash 78 and does not show a name yet, " +
					"so the server cannot name it. " +
					"Bungie defines the item with the hash 999 without a name.",
			},
		},
		"lookups at Bungie without an answer": {
			edit: func(defs *definitions.Defs) {
				down := &bungie.PlatformError{Code: 5, Status: "SystemDisabled", Message: "down"}

				// The error of a request names its URL, which differs for
				// each hash.
				timeout := func(hash uint32) error {
					return fmt.Errorf("performing request: %w", &url.Error{
						Op:  http.MethodGet,
						URL: "https://www.bungie.net/Platform" + bungietest.PathItemDefinition(hash),
						Err: context.DeadlineExceeded,
					})
				}

				defs.Lookups = map[uint32]definitions.Lookup{
					77:                           {Err: down},
					definitionstest.ItemWithheld: {Err: down},
					998:                          {Err: timeout(998)},
					999:                          {Err: timeout(999)},
					12345:                        {Skipped: true},
				}
			},
			summary: stats.Summary{Flows: []stats.Flow{
				{ItemHash: 77, Currency: true, Gained: 2, Net: 2},
				{ItemHash: definitionstest.ItemWithheld, Gained: 1, Net: 1},
				{ItemHash: 998, Spent: 2, Net: -2},
				{ItemHash: 999, Spent: 1, Net: -1},
				{ItemHash: 12345, Spent: 3, Net: -3},
			}},
			want: []string{
				"Unknown item 77 | currency | 77 | +2 | 2 | 0",
				"Unknown item 78 | stack | 78 | +1 | 1 | 0",
				"Unknown item 998 | stack | 998 | -2 | 0 | 2",
				"Unknown item 999 | stack | 999 | -1 | 0 | 1",
				"Unknown item 12345 | stack | 12345 | -3 | 0 | 3",
				"",
				"Weapon kills: none.",
				"",
				withheld + "the items with the hashes 78 and 999. " +
					"The definitions of manifest version v1 lack the items with the hashes 77, 998, and 12345, " +
					"so the server cannot name them. " + later + " " +
					"The lookup of the items with the hashes 77 and 78 at Bungie failed: bungie: SystemDisabled: down. " +
					"A later call looks them up again. " +
					"The lookup of the items with the hashes 998 and 999 at Bungie failed: " +
					"the lookups ran out of time. A later call looks them up again. " +
					"This call reached the limit of its lookups at Bungie before the item with the hash 12345, " +
					"so it did not look it up. A later call looks it up.",
			},
		},
		"index without withheld hashes": {
			edit: func(defs *definitions.Defs) {
				defs.Withheld = nil
				defs.WithheldListed = false
			},
			summary: stats.Summary{Flows: []stats.Flow{{ItemHash: definitionstest.ItemWithheld, Gained: 1, Net: 1}}},
			want: []string{
				"Unknown item 78 | stack | 78 | +1 | 1 | 0",
				"",
				"Weapon kills: none.",
				"",
				"The definitions of manifest version v1 lack the item with the hash 78, so the server cannot name it. " +
					"The service withholds the definitions of dummy items, classified items, and items without a name, " +
					"and the game can hold an item that Bungie added in a later manifest version. " +
					"The service publishes no list of the hashes that it withholds, " +
					"so the server cannot tell which cause applies.",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			defs := definitionstest.Defs()
			if tc.edit != nil {
				tc.edit(defs)
			}

			tc.summary.From = period.Start(to)
			tc.summary.To = to

			got := renderStats(defs, period, tc.summary, time.UTC)
			assert.Contains(t, got, "Period: Last 24 hours, 2026-11-11 12:00 UTC to 2026-11-12 12:00 UTC.\n")
			assert.True(t, strings.HasSuffix(got, "\n"+strings.Join(tc.want, "\n")), got)
		})
	}
}

// TestLookupReason pins that the lookups of two item hashes that failed
// for one cause get one reason. The error of each request names the path
// of its own hash, and the reason leaves that path out.
func TestLookupReason(t *testing.T) {
	t.Parallel()

	itemURL := func(hash uint32) string {
		return "https://www.bungie.net/Platform" + bungietest.PathItemDefinition(hash)
	}

	tests := map[string]struct {
		// err returns the error of the lookup of hash.
		err  func(hash uint32) error
		want string
	}{
		"platform error": {
			err: func(uint32) error {
				return &bungie.PlatformError{Code: 5, Status: "SystemDisabled", Message: "down"}
			},
			want: "bungie: SystemDisabled: down",
		},
		"platform message with a period": {
			err: func(uint32) error {
				return &bungie.PlatformError{
					Code:    5,
					Status:  "SystemDisabled",
					Message: "This system is temporarily disabled for maintenance.",
				}
			},
			want: "bungie: SystemDisabled: This system is temporarily disabled for maintenance",
		},
		"request out of time": {
			err: func(hash uint32) error {
				return fmt.Errorf("performing request: %w", &url.Error{
					Op:  http.MethodGet,
					URL: itemURL(hash),
					Err: context.DeadlineExceeded,
				})
			},
			want: "the lookups ran out of time",
		},
		"body out of time": {
			err: func(hash uint32) error {
				return fmt.Errorf("reading %s: %w", bungietest.PathItemDefinition(hash), context.DeadlineExceeded)
			},
			want: "the lookups ran out of time",
		},
		"body read failed": {
			err: func(hash uint32) error {
				return fmt.Errorf("reading %s: %w", bungietest.PathItemDefinition(hash), io.ErrUnexpectedEOF)
			},
			want: "reading the item definition: unexpected EOF",
		},
		"response that does not decode": {
			err: func(hash uint32) error {
				return fmt.Errorf(
					"%w: decoding %s: %w",
					bungie.ErrPlatform, bungietest.PathItemDefinition(hash), io.ErrUnexpectedEOF,
				)
			},
			want: "bungie: decoding the item definition: unexpected EOF",
		},
		"refused connection": {
			err: func(hash uint32) error {
				return fmt.Errorf("performing request: %w", &url.Error{
					Op:  http.MethodGet,
					URL: itemURL(hash),
					Err: errors.New("dial tcp 127.0.0.1:443: connect: connection refused"),
				})
			},
			want: "dial tcp 127.0.0.1:443: connect: connection refused",
		},
		"no platform response": {
			err: func(hash uint32) error {
				return &bungie.StatusError{Path: bungietest.PathItemDefinition(hash), Status: http.StatusBadGateway}
			},
			want: "Bungie answered with the HTTP status 502 and no platform response",
		},
		"sign-in error": {
			err: func(uint32) error {
				return auth.ErrNotSignedIn
			},
			want: auth.ErrNotSignedIn.Error(),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, lookupReason(4148227246, tc.err(4148227246)))
			assert.Equal(t, tc.want, lookupReason(4148227247, tc.err(4148227247)))
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

func TestPatternLabel(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		pattern *inventory.Pattern
		want    string
	}{
		"no pattern": {},
		"one objective": {
			pattern: &inventory.Pattern{Objectives: []inventory.Objective{{Progress: 3, Completion: 5}}},
			want:    "3 of 5",
		},
		"several objectives": {
			pattern: &inventory.Pattern{Objectives: []inventory.Objective{
				{Progress: 3, Completion: 5},
				{Progress: 0, Completion: 1},
			}},
			want: "3 of 5, 0 of 1",
		},
		"incomplete pattern without an objective": {
			pattern: &inventory.Pattern{},
			want:    "incomplete",
		},
		"complete pattern": {
			pattern: &inventory.Pattern{
				Objectives: []inventory.Objective{{Progress: 5, Completion: 5}},
				Complete:   true,
			},
			want: "complete",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, patternLabel(tc.pattern))
		})
	}
}

func TestActivationCost(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		item inventory.Item
		want string
	}{
		"weapon outside the raids": {
			item: inventory.Item{SourceKnown: true},
			want: "1 harmonizer",
		},
		"raid weapon": {
			item: inventory.Item{Raid: true, SourceKnown: true},
			want: "1 harmonizer + 15 Spoils",
		},
		"weapon without source tags": {
			want: "1 harmonizer + 15 Spoils if a raid weapon",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, activationCost(&tc.item))
		})
	}
}

func TestMoveFailure(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		moveErr    error
		instanceID string
		want       string
	}{
		"item in the vault": {
			instanceID: "1001",
			moveErr:    fmt.Errorf("transferring to Hunter: %w", bungietest.ErrStep),
			want:       "Fatebringer (1001) did not move: transferring to Hunter: step refused. It is in the vault.",
		},
		"item equipped on a character": {
			instanceID: "1006",
			moveErr:    fmt.Errorf("transferring to the vault: %w", bungietest.ErrStep),
			want: "Fatebringer (1006) did not move: transferring to the vault: step refused. " +
				"It is equipped on Hunter.",
		},
		"error that ends with a period": {
			instanceID: "1001",
			moveErr:    errors.New("transferring to Hunter: bungie: DestinyItemNotFound: gone."),
			want:       "Fatebringer (1001) did not move: transferring to Hunter: bungie: DestinyItemNotFound: gone. It is in the vault.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := inventorytest.Snapshot(t)

			item, ok := s.Find(tc.instanceID)
			require.True(t, ok)

			assert.Equal(t, tc.want, moveFailure(s, item, tc.moveErr))
		})
	}
}

func TestLocationSentence(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want string
		item inventory.Item
	}{
		"vault": {
			item: inventory.Item{Owner: inventory.OwnerVault},
			want: "It is in the vault.",
		},
		"account-wide bucket": {
			item: inventory.Item{Owner: inventory.OwnerAccount},
			want: "It is in an account-wide bucket.",
		},
		"character inventory": {
			item: inventory.Item{Owner: bungietest.HunterID},
			want: "It is on Hunter.",
		},
		"equipped": {
			item: inventory.Item{Owner: bungietest.TitanID, Equipped: true},
			want: "It is equipped on Titan.",
		},
		"postmaster": {
			item: inventory.Item{Owner: bungietest.HunterID, Postmaster: true},
			want: "It is at the Postmaster for Hunter.",
		},
		"unknown character": {
			item: inventory.Item{Owner: "c9"},
			want: "It is on Character c9.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, locationSentence(inventorytest.Snapshot(t), &tc.item))
		})
	}
}

func TestSignedInSentences(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		zone   *time.Location
		want   []string
		tokens auth.Tokens
	}{
		"sign-in with a refresh expiry": {
			tokens: auth.Tokens{BungieID: "42", RefreshExpires: bungietest.Now.Add(24 * time.Hour)},
			zone:   time.UTC,
			want: []string{
				"Signed in as Bungie.net membership 42.",
				"The sign-in lasts until 2026-11-13 12:00 UTC.",
			},
		},
		"refresh expiry in another zone": {
			tokens: auth.Tokens{BungieID: "42", RefreshExpires: bungietest.Now.Add(24 * time.Hour)},
			zone:   time.FixedZone("XST", -5*60*60),
			want: []string{
				"Signed in as Bungie.net membership 42.",
				"The sign-in lasts until 2026-11-13 07:00 XST.",
			},
		},
		"sign-in without a refresh expiry": {
			tokens: auth.Tokens{BungieID: "42"},
			zone:   time.UTC,
			want:   []string{"Signed in as Bungie.net membership 42."},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, signedInSentences(tc.tokens, tc.zone))
		})
	}
}
