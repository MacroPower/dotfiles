package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/clarity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/damage"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvmtest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpfake"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/rolls"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/search"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/stats"
)

const (
	pathStats    = "/api/stats"
	pathTags     = "/api/tags"
	pathNotes    = "/api/notes"
	pathPrefs    = "/api/perk-preferences"
	pathAutotag  = "/api/autotag"
	pathTransfer = "/Destiny2/Actions/Items/TransferItem/"
	pathEquip    = "/Destiny2/Actions/Items/EquipItem/"
	pathLock     = "/Destiny2/Actions/Items/SetLockState/"
	pathInsert   = "/Destiny2/Actions/Items/InsertSocketPlugFree/"
	pathLoadout  = "/Destiny2/Actions/Loadouts/EquipLoadout/"
	pathSnapshot = "/Destiny2/Actions/Loadouts/SnapshotLoadout/"
	pathClear    = "/Destiny2/Actions/Loadouts/ClearLoadout/"
)

const loadoutsIntro = "Loadouts saved in the game. A loadout number is the position of the loadout " +
	"on the in-game loadout screen, counted from 1. The server cannot show the icon or the color " +
	"of a loadout, because the game defines both only as images. " +
	"Pass the character and the loadout number to equip_loadout."

// killClipPlugSet gives socket 1 of Fatebringer (100) a plug set that
// lists Firefly and the two Kill Clip plugs. Fatebringer has no plug set
// for socket 0. Socket 1 of instance 1001 holds Firefly, which the fake
// perk preferences like for PvE.
func killClipPlugSet(d *definitions.Defs) {
	d.Sockets = map[uint32][]uint32{100: {0, 40}}
	d.PlugSets = map[uint32][]uint32{40: {901, 904, 905}}
}

func TestServerListsTools(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	serverSide, clientSide := mcp.NewInMemoryTransports()

	_, err := newServer(f.h).Connect(t.Context(), serverSide, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)

	session, err := client.Connect(t.Context(), clientSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, session.Close()) })

	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)

	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}

	assert.ElementsMatch(t, []string{
		toolStatus, toolLogin, toolCompleteLogin, toolListCharacters, toolListItems, toolGetItem,
		toolCompareDuplicates, toolMoveItems,
		toolListTags, toolSetTag, toolSetFavorite, toolListNotes, toolSetNote, toolListPerkPreferences,
		toolSetPerkPreferences, toolListAutotagRules, toolSaveAutotagRule, toolPreviewAutotagRule,
		toolRemoveAutotagRule, toolRunAutotagRules,
		toolMoveStack, toolStageJunk, toolSetLock, toolSetPerk, toolSetAutotagEnabled, toolListActivityHistory,
		toolListLoadouts, toolEquipLoadout, toolSaveLoadout, toolClearLoadout, toolGetStats, toolListVendors,
		toolListActivities,
	}, names)

	destructive := true

	for _, tool := range tools.Tools {
		switch tool.Name {
		case toolListActivities:
			assert.Equal(t, &mcp.ToolAnnotations{ReadOnlyHint: true}, tool.Annotations)
		case toolListVendors:
			assert.Equal(t, &mcp.ToolAnnotations{ReadOnlyHint: true}, tool.Annotations)
			assert.Contains(t, tool.Description, "No group lists the sub-vendors without a name that hold part "+
				"of the Eververse stock, so the list leaves them out")
			assert.Contains(t, tool.Description, "the Eververse sub-vendors among them")
			assert.NotContains(t, tool.Description, "Destiny Item Manager")

		case toolPreviewAutotagRule:
			assert.Equal(t, &mcp.ToolAnnotations{ReadOnlyHint: true}, tool.Annotations)
			assert.Contains(t, tool.Description, "is:weapon, is:armor")
			assert.Contains(t, tool.Description, "so a match does not mean that a saved rule takes the item")

		case toolGetStats:
			require.NotNil(t, tool.Annotations)
			assert.True(t, tool.Annotations.ReadOnlyHint)

		case toolSaveAutotagRule:
			assert.Contains(t, tool.Description, "A rule needs a tag_id or vault=true")
			assert.Contains(
				t,
				tool.Description,
				"This tool refuses a query that the search parser of the web client rejects",
			)
			assert.Contains(t, tool.Description, "It also refuses a filter value that the web client rejects")
			assert.NotContains(t, tool.Description, "It does not check filter names or filter values")

		case toolGetItem:
			assert.Contains(t, tool.Description, "a pick can be a perk that this version cannot roll")
			assert.Contains(t, tool.Description, "A line that starts with 'Clarity:' then opens the tested description")
			assert.Contains(t, tool.Description, "'"+clarity.EnhancedMark+"' precedes each value")
			assert.Contains(t, tool.Description, "A line that starts with 'Might predate the final patch'")
			assert.Contains(t, tool.Description, "the time to aim down sights in seconds")
			assert.Contains(
				t,
				tool.Description,
				"The values are therefore community measurements and not numbers from Bungie.",
			)
			assert.Contains(t, tool.Description, "without a stat bonus, such as Snapshot Sights, change them as well.")
			assert.Contains(
				t,
				tool.Description,
				"A weapon also shows its time to kill a Guardian and its damage per second.",
			)
			assert.Contains(t, tool.Description, "The numbers come from community measurements, "+
				"as D2Foundry's Oracle Engine and the Clarity database collect them, "+
				"and from the damage values that Bungie's patch notes state. The Bungie API publishes none of them.")
			assert.Contains(t, tool.Description, "They match Update "+damage.Patch()+" of the game")
			assert.Contains(t, tool.Description, "It does not have the damage of the Support Frame of auto rifles, "+
				"the Spread Shot frame of hand cannons, the Aggressive Burst frame of submachine guns, "+
				"the Balanced Heat Weapon and Dynamic Heat Weapon frames, "+
				"or the Micro-Missile Frame of pulse rifles and sidearms.")
			assert.Contains(
				t,
				tool.Description,
				"A last line names each active perk whose effect the numbers leave out",
			)

		case toolCompareDuplicates:
			assert.Contains(t, tool.Description, "so two rolls compare in seconds and meters")
			assert.Contains(t, tool.Description, "The values are the community measurements that get_item describes")
			assert.Contains(t, tool.Description, "without a stat bonus, such as Snapshot Sights.")

		case toolSaveLoadout:
			assert.Equal(t, &mcp.ToolAnnotations{DestructiveHint: &destructive, IdempotentHint: true}, tool.Annotations)
			assert.Contains(
				t,
				tool.Description,
				"The Bungie API has no request that builds a loadout from a list of items",
			)
			assert.Contains(
				t,
				tool.Description,
				"First equip the gear with move_items and equip=true, then call this tool.",
			)
			assert.Contains(t, tool.Description, "Without name, a saved loadout keeps its name")

		case toolClearLoadout:
			assert.Equal(t, &mcp.ToolAnnotations{DestructiveHint: &destructive, IdempotentHint: true}, tool.Annotations)
			assert.Contains(t, tool.Description, "The tool refuses a slot that holds no loadout.")

		case toolRunAutotagRules:
			// A claim cannot be undone, so DestructiveHint keeps its
			// default of true.
			assert.Equal(t, &mcp.ToolAnnotations{}, tool.Annotations)
			assert.Contains(t, tool.Description, "A claim happens at most once.")
			assert.Contains(t, tool.Description, "list_autotag_rules with matches=new shows what the tool would do.")
			assert.Contains(t, tool.Description,
				"For each rule with vault=true, the tool then moves each item that the web client moves to the vault.")
			assert.NotContains(t, tool.Description, "an enabled rule moves items to the vault")

		case toolSetAutotagEnabled:
			assert.Contains(t, tool.Description, "run_autotag_rules runs the rules when a caller invokes it.")

		case toolStageJunk:
			assert.Equal(t, &mcp.ToolAnnotations{DestructiveHint: new(bool)}, tool.Annotations)
			assert.Contains(t, tool.Description, "The Bungie API has no request that dismantles an item")
			assert.Contains(t, tool.Description, "so that the user can dismantle them in the game")
		}
	}

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      toolGetItem,
		Arguments: map[string]any{"instance_id": "1001"},
	})
	require.NoError(t, err)

	got, isError := text(t, result, nil)
	assert.False(t, isError)
	assert.Contains(t, got, "Fatebringer")
}

func TestStatusAndLogin(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleStatus, EmptyInput{})
	assert.False(t, isError)
	assert.Contains(t, got, "Signed in: yes")
	assert.Contains(t, got, "Sign-in expires: 2026-11-13 12:00 UTC")

	got, isError = call(t, f.h.handleLogin, EmptyInput{})
	assert.False(t, isError)
	assert.Contains(t, got, "https://www.bungie.net/en/OAuth/Authorize?client_id=54520")

	saved, err := f.h.auth.Saved()
	require.NoError(t, err)

	saved.RefreshExpires = bungietest.Now.Add(-time.Hour)
	require.NoError(t, f.h.auth.Store().Save(saved))

	got, isError = call(t, f.h.handleStatus, EmptyInput{})
	assert.False(t, isError)
	assert.Contains(t, got, "Signed in: no")
	assert.Contains(t, got, "expired on 2026-11-12 11:00 UTC")

	saved.RefreshToken = ""
	saved.RefreshExpires = time.Time{}
	require.NoError(t, f.h.auth.Store().Save(saved))

	got, isError = call(t, f.h.handleStatus, EmptyInput{})
	assert.False(t, isError)
	assert.Contains(t, got, "Signed in: yes")
	assert.Contains(t, got, "Sign-in expires with the access token")

	require.NoError(t, f.h.auth.Logout())

	got, isError = call(t, f.h.handleStatus, EmptyInput{})
	assert.False(t, isError)
	assert.Contains(t, got, "Signed in: no")

	got, isError = call(t, f.h.handleListItems, ListItemsInput{})
	assert.True(t, isError)
	assert.Equal(t, "not signed in. Call the login tool to sign in.", got)
}

// TestTimesInZone checks that each tool prints its times in the zone of
// the handler. The token file and the Bungie fixtures give every time in
// UTC. The handler zone has the offset of EST but an abbreviation that no
// zone of the tz database uses. When a tool prints in the zone of the host
// instead, the test fails on every host, including one that runs in EST.
func TestTimesInZone(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		call func(t *testing.T, f *fakes) (string, bool)
		want []string
	}{
		"status": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				return call(t, f.h.handleStatus, EmptyInput{})
			},
			want: []string{
				"Access token expires: 2026-11-12 08:00 XST\n",
				"Sign-in expires: 2026-11-13 07:00 XST",
			},
		},
		"expired sign-in": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				saved, err := f.h.auth.Saved()
				require.NoError(t, err)

				saved.RefreshExpires = bungietest.Now.Add(-time.Hour)
				require.NoError(t, f.h.auth.Store().Save(saved))

				return call(t, f.h.handleStatus, EmptyInput{})
			},
			want: []string{"expired on 2026-11-12 06:00 XST"},
		},
		"complete login": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				return call(t, f.h.handleCompleteLogin,
					CompleteLoginInput{RedirectURL: "https://example.test/auth/callback?code=abc"})
			},
			want: []string{"The sign-in lasts until 2027-02-10 07:00 XST."},
		},
		"characters": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				return call(t, f.h.handleListCharacters, EmptyInput{})
			},
			want: []string{
				"- Hunter | id c1 | power 2000 | last played 2026-11-10 07:00 XST | ",
				"- Titan | id c2 | power 1990 | last played 2026-11-01 07:00 XST | ",
			},
		},
		"activity history": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				return call(t, f.h.handleListActivityHistory, ListActivityHistoryInput{})
			},
			want: []string{
				"\n2026-11-11 16:00 XST | Hunter | Vault of Glass: Master | ",
				"\n2026-11-09 03:00 XST | Hunter | Activity 3333 | ",
			},
		},
		"vendor list": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				return call(t, f.h.handleListVendors, ListVendorsInput{})
			},
			want: []string{
				"\nBanshee-44 | Gunsmith | 672118013 | 8 | 2026-11-13 12:00 XST | \n",
				"\nTess Everis | Eververse Trading Company | 3361454721 | 1 | 2026-11-17 12:00 XST | ",
			},
		},
		"one vendor": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				return call(t, f.h.handleListVendors, ListVendorsInput{Vendor: "Banshee-44"})
			},
			want: []string{"\nNext refresh: 2026-11-13 12:00 XST\n"},
		},
		"every vendor": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				return call(t, f.h.handleListVendors, ListVendorsInput{All: true})
			},
			want: []string{" | 2026-11-13 12:00 XST | "},
		},
		"stats": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				f.service.Data[pathStats] = statsFixture(t)

				return call(t, f.h.handleGetStats, GetStatsInput{})
			},
			want: []string{
				"Period: Last 24 hours, 2026-11-11 07:00 XST to 2026-11-12 07:00 XST.\n",
				"\nNewest recorded change: 2026-11-12 06:00 XST.\n",
			},
		},
		"stats of all time": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				f.service.Data[pathStats] = statsFixture(t)

				return call(t, f.h.handleGetStats, GetStatsInput{Period: "all"})
			},
			want: []string{"Period: All time, up to 2026-11-12 07:00 XST.\n"},
		},
		"stats without changes": {
			call: func(t *testing.T, f *fakes) (string, bool) {
				t.Helper()

				f.service.Data[pathStats] = dvmtest.StatPage(t, false)

				return call(t, f.h.handleGetStats, GetStatsInput{Period: "30d"})
			},
			want: []string{"\nNewest recorded change since 2026-10-13 04:00 XST: none.\n"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.h.zone = time.FixedZone("XST", -5*60*60)

			got, isError := tc.call(t, f)
			require.False(t, isError, got)

			for _, want := range tc.want {
				assert.Contains(t, got, want)
			}

			assert.NotContains(t, got, "UTC")
			assert.NotContains(t, got, "EST")
		})
	}
}

func TestListCharacters(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	assert.False(t, isError)
	assert.Equal(t, strings.Join([]string{
		"Characters, most recently played first:",
		"- Hunter | id c1 | power 2000 | last played 2026-11-10 12:00 UTC | " +
			"2 equipped, 1 in inventory, 1 at the Postmaster",
		"  Slots used: Kinetic Weapons 2/10, Power Weapons 0/10, Helmet 1/10, Engrams 0/10, Postmaster 1/21",
		"- Titan | id c2 | power 1990 | last played 2026-11-01 12:00 UTC | " +
			"0 equipped, 0 in inventory, 0 at the Postmaster",
		"  Slots used: Kinetic Weapons 0/10, Power Weapons 0/10, Helmet 0/10, Engrams 0/10, Postmaster 0/21",
		"",
		"Vault: 2/700 slots used",
		"Account-wide slots used: Consumables 1/50",
		"",
		"Currencies:",
		"- Glimmer: 250000",
		"- Bright Dust: 1500",
		"- Unknown item 77: 3",
	}, "\n"), got)
}

func TestRenderCharactersWithoutDefinitions(t *testing.T) {
	t.Parallel()

	var profile bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(bungietest.ProfileJSON), &profile))

	profile.ProfileCurrencies.Data.Items = nil
	defs := definitionstest.Defs()
	defs.Buckets = nil

	assert.Equal(t, strings.Join([]string{
		"Characters, most recently played first:",
		"- Hunter | id c1 | power 2000 | last played 2026-11-10 12:00 UTC | " +
			"2 equipped, 1 in inventory, 1 at the Postmaster",
		"  Slots used: Postmaster 1",
		"- Titan | id c2 | power 1990 | last played 2026-11-01 12:00 UTC | " +
			"0 equipped, 0 in inventory, 0 at the Postmaster",
		"  Slots used: Postmaster 0",
		"",
		"Vault: 2 slots used",
		"",
		"Currencies: none",
	}, "\n"), renderCharacters(inventory.NewSnapshot(&profile, defs, bungietest.Now), time.UTC))
}

func TestListItems(t *testing.T) {
	t.Parallel()

	const (
		// The Aegis sheet lists Fatebringer (100) with the tier S and
		// two perk columns. It lists Gjallarhorn (101) with the tier A
		// and Midnight Coup (102) without a tier, both without columns.
		// The PvP sheet lists only Fatebringer, with the tier C and one
		// column.
		//
		// Instance 1001 holds Explosive Payload (900) with Rampage (903)
		// beside it in one socket, so it hits the first column of the
		// Aegis sheet. No socket of the copy holds Kill Clip (904), so
		// it misses the second. The fake perk preferences dislike
		// Rampage for PvP, so the copy misses the column of the PvP
		// sheet. The profile lists perks for no other weapon.
		aegisSheet = `{"rolls": [` +
			`{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900], [904]]}, ` +
			`{"category": "Exotic Weapons", "tier": "A", "hashes": [101], "slots": []}, ` +
			`{"category": "HCs", "hashes": [102], "slots": []}]}`
		pvpSheet = `{"rolls": [{"category": "Hand Cannon", "tier": "C", "hashes": [100], "slots": [[903]]}]}`

		noAegis = "PvE: rating unavailable: the service publishes no Aegis sheet"
		noPvP   = "PvP: rating unavailable: the service publishes no PvP sheet"

		ratedHeader = "instance id | name | type | tier | power | stat total | location | tag | flags | " +
			"pve rating | pvp rating"
		noRating = "unavailable | unavailable"

		// The rows of the two copies that reach a PvE minimum, in a
		// table without rating columns. Instance 1001 has the PvE roll
		// tier A and the PvP roll tier D. Instance 1002 has the PvE roll
		// tier A, and the PvP sheet does not list it.
		plainHeader = "instance id | name | type | tier | power | stat total | location | tag | flags"
		row1001     = "1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 |  | Vault | keep | " +
			"favorite, locked, masterwork, note"
		row1002 = "1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 |  | Vault |  | "

		noAegisMinimum = "min_pve_rating: rating unavailable: the service publishes no Aegis sheet"
		noPvPMinimum   = "min_pvp_rating: rating unavailable: the service publishes no PvP sheet"

		// The reason of a call that cannot read the definitions.
		noDefs = "rating unavailable: loading definitions: definitions: the service index lists no core artifact"
	)

	// aegisOnly makes the service publish only the Aegis sheet.
	aegisOnly := func(f *fakes) {
		f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
		f.service.Data["/artifacts/rolls.a.json"] = aegisSheet
	}

	// bothSheets makes the service publish the Aegis sheet and the PvP
	// sheet.
	bothSheets := func(f *fakes) {
		aegisOnly(f)

		f.service.ArtifactFiles["pvprolls"] = []string{"pvprolls.a.json"}
		f.service.Data["/artifacts/pvprolls.a.json"] = pvpSheet
	}

	// rated returns the rows of the unfiltered listing with the rating
	// columns. Cells holds the two rating cells of each weapon row, in
	// row order. The rating cells of the helmet are empty.
	rated := func(cells ...string) []string {
		return []string{
			"1004 | Celestial Nighthawk | Helmet | Exotic | 2000 | 55 | Hunter (equipped) |  |  |  | ",
			"1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 |  | Vault | keep | " +
				"favorite, locked, masterwork, note | " + cells[0],
			"1005 | Fatebringer | Kinetic Hand Cannon | Legendary | 1970 |  | Hunter (postmaster) |  |  | " + cells[1],
			"1006 | Fatebringer | Kinetic Hand Cannon | Legendary | 1980 |  | Hunter (equipped) |  |  | " + cells[2],
			"1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 |  | Vault |  |  | " + cells[3],
			"1003 | Midnight Coup | Kinetic Hand Cannon | Legendary | 2005 |  | Hunter |  |  | " + cells[4],
		}
	}

	// pveA is the output of a call that keeps exactly the copies with a
	// PvE roll tier of A or better, without rating columns.
	pveA := strings.Join([]string{"Items 1-2 of 2.", "", plainHeader, row1001, row1002}, "\n")

	// pvpD is the output of a call that keeps only instance 1001,
	// without rating columns.
	pvpD := strings.Join([]string{"Items 1-1 of 1.", "", plainHeader, row1001}, "\n")

	tests := map[string]struct {
		// prepare changes the fakes before the call.
		prepare func(*fakes)
		// want is the output. "{service}" stands for the base URL of the
		// service.
		want string
		in   ListItemsInput
		// noDefs makes the definition read of the ratings not succeed.
		// The case runs list_items one time to load the snapshot and
		// then drops the loaded definitions. The call under test reuses
		// the snapshot, and its definition read asks the service, whose
		// index lists no core artifact.
		noDefs  bool
		isError bool
	}{
		"vault with perks": {
			in: ListItemsInput{Location: "vault", IncludePerks: true},
			want: strings.Join([]string{
				"Items 1-2 of 2.",
				"",
				"instance id | name | type | tier | power | stat total | location | tag | flags | perks",
				"1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 |  | Vault | keep | " +
					"favorite, locked, masterwork, note | Explosive Payload, Firefly",
				"1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 |  | Vault |  |  | ",
			}, "\n"),
		},
		"paging": {
			in: ListItemsInput{Sort: inventory.SortPower, Limit: 2, Offset: 2},
			want: strings.Join([]string{
				"Items 3-4 of 6. Pass offset=4 for the next page.",
				"",
				"instance id | name | type | tier | power | stat total | location | tag | flags",
				"1004 | Celestial Nighthawk | Helmet | Exotic | 2000 | 55 | Hunter (equipped) |  | ",
				"1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 |  | Vault |  | ",
			}, "\n"),
		},
		"stack": {
			in: ListItemsInput{Kind: inventory.KindOther},
			want: strings.Join([]string{
				"Items 1-1 of 1.",
				"",
				"instance id | name | type | tier | power | stat total | location | tag | flags",
				"stack of 42 (item hash 300) | Enhancement Core | Material | Legendary |  |  | Account |  | ",
			}, "\n"),
		},
		"minimum stat total": {
			in: ListItemsInput{MinTotal: 55},
			want: strings.Join([]string{
				"Items 1-1 of 1.",
				"",
				"instance id | name | type | tier | power | stat total | location | tag | flags",
				"1004 | Celestial Nighthawk | Helmet | Exotic | 2000 | 55 | Hunter (equipped) |  | ",
			}, "\n"),
		},
		"sort by a named stat": {
			in: ListItemsInput{Stat: "HEALTH", Sort: inventory.SortStat, Limit: 2},
			want: strings.Join([]string{
				"Items 1-2 of 6. Pass offset=2 for the next page.",
				"",
				"instance id | name | type | tier | power | stat total | health | location | tag | flags",
				"1004 | Celestial Nighthawk | Helmet | Exotic | 2000 | 55 | 30 | Hunter (equipped) |  | ",
				"1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 |  |  | Vault | keep | " +
					"favorite, locked, masterwork, note",
			}, "\n"),
		},
		"minimum value of a named stat": {
			in:   ListItemsInput{Stat: "health", MinStat: 31},
			want: "No items match.",
		},
		"unknown stat": {
			in:      ListItemsInput{Stat: "mobility"},
			want:    `invalid filter: no item in the inventory has a stat named "mobility", so pass one of Grenade, Health, Range`,
			isError: true,
		},
		"no match": {
			in:   ListItemsInput{Query: "thorn"},
			want: "No items match.",
		},
		"query with digits for letters": {
			in:   ListItemsInput{Query: "9jallarh0rn"},
			want: strings.Join([]string{"Items 1-1 of 1.", "", plainHeader, row1002}, "\n"),
		},
		"stat with a digit for a letter": {
			in: ListItemsInput{Stat: "H3alth", MinStat: 30},
			want: strings.Join([]string{
				"Items 1-1 of 1.",
				"",
				"instance id | name | type | tier | power | stat total | h3alth | location | tag | flags",
				"1004 | Celestial Nighthawk | Helmet | Exotic | 2000 | 55 | 30 | Hunter (equipped) |  | ",
			}, "\n"),
		},
		"offset past the end": {
			in:   ListItemsInput{Offset: 50},
			want: "Offset 6 is past the last of 6 matching items.",
		},
		"invalid filter": {
			in:      ListItemsInput{Kind: "ghost"},
			want:    `invalid filter: kind "ghost" is not weapon, armor, gear, other, or all`,
			isError: true,
		},
		"ratings of both sheets": {
			prepare: bothSheets,
			in:      ListItemsInput{IncludeRatings: true},
			want: strings.Join(slices.Concat([]string{
				"Items 1-6 of 6.",
				"",
				ratedHeader,
			}, rated(
				"A (sheet S) | D (sheet C)",
				"roll unknown (sheet S) | roll unknown (sheet C)",
				"roll unknown (sheet S) | roll unknown (sheet C)",
				"A | not listed",
				"no tier | not listed",
			)), "\n"),
		},
		"ratings between the flags and the perks": {
			prepare: bothSheets,
			in:      ListItemsInput{Stat: "health", IncludePerks: true, IncludeRatings: true},
			want: strings.Join([]string{
				"Items 1-6 of 6.",
				"",
				"instance id | name | type | tier | power | stat total | health | location | tag | flags | " +
					"pve rating | pvp rating | perks",
				"1004 | Celestial Nighthawk | Helmet | Exotic | 2000 | 55 | 30 | Hunter (equipped) |  |  |  |  | ",
				"1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 |  |  | Vault | keep | " +
					"favorite, locked, masterwork, note | A (sheet S) | D (sheet C) | Explosive Payload, Firefly",
				"1005 | Fatebringer | Kinetic Hand Cannon | Legendary | 1970 |  |  | Hunter (postmaster) |  |  | " +
					"roll unknown (sheet S) | roll unknown (sheet C) | ",
				"1006 | Fatebringer | Kinetic Hand Cannon | Legendary | 1980 |  |  | Hunter (equipped) |  |  | " +
					"roll unknown (sheet S) | roll unknown (sheet C) | ",
				"1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 |  |  | Vault |  |  | A | not listed | ",
				"1003 | Midnight Coup | Kinetic Hand Cannon | Legendary | 2005 |  |  | Hunter |  |  | " +
					"no tier | not listed | ",
			}, "\n"),
		},
		"ratings of the one sheet that the service publishes": {
			prepare: aegisOnly,
			in:      ListItemsInput{IncludeRatings: true},
			want: strings.Join(slices.Concat([]string{
				"Items 1-6 of 6.",
				noPvP,
				"",
				ratedHeader,
			}, rated(
				"A (sheet S) | unavailable",
				"roll unknown (sheet S) | unavailable",
				"roll unknown (sheet S) | unavailable",
				"A | unavailable",
				"no tier | unavailable",
			)), "\n"),
		},
		"ratings without a sheet": {
			in: ListItemsInput{IncludeRatings: true},
			want: strings.Join(slices.Concat([]string{
				"Items 1-6 of 6.",
				noAegis,
				noPvP,
				"",
				ratedHeader,
			}, rated(noRating, noRating, noRating, noRating, noRating)), "\n"),
		},
		"rating reasons follow the paging hint": {
			in: ListItemsInput{Limit: 2, IncludeRatings: true},
			want: strings.Join(slices.Concat([]string{
				"Items 1-2 of 6. Pass offset=2 for the next page.",
				noAegis,
				noPvP,
				"",
				ratedHeader,
			}, rated(noRating, noRating, noRating, noRating, noRating)[:2]), "\n"),
		},
		"stack has empty rating cells": {
			prepare: bothSheets,
			in:      ListItemsInput{Kind: inventory.KindOther, IncludeRatings: true},
			want: strings.Join([]string{
				"Items 1-1 of 1.",
				"",
				ratedHeader,
				"stack of 42 (item hash 300) | Enhancement Core | Material | Legendary |  |  | Account |  |  |  | ",
			}, "\n"),
		},
		"armor has empty rating cells also without a sheet": {
			in: ListItemsInput{Kind: inventory.KindArmor, IncludeRatings: true},
			want: strings.Join([]string{
				"Items 1-1 of 1.",
				noAegis,
				noPvP,
				"",
				ratedHeader,
				"1004 | Celestial Nighthawk | Helmet | Exotic | 2000 | 55 | Hunter (equipped) |  |  |  | ",
			}, "\n"),
		},
		// The index lists a file that the service does not serve. A mode
		// whose sheet did not load never reads "not listed".
		"ratings of a sheet that does not load": {
			prepare: func(f *fakes) {
				f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.x.json"}}
			},
			in: ListItemsInput{IncludeRatings: true},
			want: strings.Join(slices.Concat([]string{
				"Items 1-6 of 6.",
				"PvE: rating unavailable: fetching artifact rolls.x.json: " +
					"service: {service}/artifacts/rolls.x.json returned status 404",
				noPvP,
				"",
				ratedHeader,
			}, rated(noRating, noRating, noRating, noRating, noRating)), "\n"),
		},
		// The mode with a sheet reports the perk preferences, and the
		// mode without a sheet keeps the reason of the sheet.
		"ratings without the perk preferences": {
			prepare: func(f *fakes) {
				aegisOnly(f)
				delete(f.service.Data, pathPrefs)
			},
			in: ListItemsInput{IncludeRatings: true},
			want: strings.Join(slices.Concat([]string{
				"Items 1-6 of 6.",
				"PvE: rating unavailable: reading perk preferences: " +
					"service: GET /api/perk-preferences?since=0 returned status 404: Not found",
				noPvP,
				"",
				ratedHeader,
			}, rated(noRating, noRating, noRating, noRating, noRating)), "\n"),
		},
		// The call still returns the table, and no cell reads "not
		// listed".
		"ratings without the definitions": {
			prepare: bothSheets,
			in:      ListItemsInput{IncludeRatings: true},
			want: strings.Join(slices.Concat([]string{
				"Items 1-6 of 6.",
				"PvE: " + noDefs,
				"PvP: " + noDefs,
				"",
				ratedHeader,
			}, rated(noRating, noRating, noRating, noRating, noRating)), "\n"),
			noDefs: true,
		},
		// The mode without a sheet keeps the reason of the sheet.
		"ratings without the definitions and without the PvP sheet": {
			prepare: aegisOnly,
			in:      ListItemsInput{IncludeRatings: true},
			want: strings.Join(slices.Concat([]string{
				"Items 1-6 of 6.",
				"PvE: " + noDefs,
				noPvP,
				"",
				ratedHeader,
			}, rated(noRating, noRating, noRating, noRating, noRating)), "\n"),
			noDefs: true,
		},
		"no match with ratings": {
			in:   ListItemsInput{Query: "thorn", IncludeRatings: true},
			want: "No items match.",
		},
		"offset past the end with ratings": {
			in:   ListItemsInput{Offset: 50, IncludeRatings: true},
			want: "Offset 6 is past the last of 6 matching items.",
		},
		"invalid filter with ratings": {
			in:      ListItemsInput{Kind: "ghost", IncludeRatings: true},
			want:    `invalid filter: kind "ghost" is not weapon, armor, gear, other, or all`,
			isError: true,
		},
		"minimum pve rating": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "A"},
			want:    pveA,
		},
		"minimum pve rating in lower case": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "a"},
			want:    pveA,
		},
		"minimum pve rating that no roll reaches": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "S"},
			want:    "No items match.",
		},
		// Instance 1003 sits on a row without a tier, the profile lists
		// no perks for 1005 and 1006, and the helmet is not a weapon.
		"lowest minimum keeps only the rolls with a tier": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "F"},
			want:    pveA,
		},
		"minimum pvp rating": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvPRating: "D"},
			want:    pvpD,
		},
		// The disliked Rampage costs instance 1001 the one PvP column.
		"minimum pvp rating above a roll with a disliked pick": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvPRating: "C"},
			want:    "No items match.",
		},
		"minimum pvp rating without a perk preference": {
			prepare: func(f *fakes) {
				bothSheets(f)

				f.service.Data[pathPrefs] = `{"preferences": [], "now": 10}`
			},
			in:   ListItemsInput{MinPvPRating: "C"},
			want: pvpD,
		},
		"minimums of both modes": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "A", MinPvPRating: "D"},
			want:    pvpD,
		},
		// Instance 1001 rolled no pick of the second column of the Aegis
		// sheet. The plug set of the socket with the liked Firefly lists
		// Kill Clip, the pick of that column. The copy therefore hits
		// the column for PvE and keeps the tier of the weapon.
		"liked perk raises the rating of a copy that rolled no pick of a column": {
			prepare: func(f *fakes) {
				bothSheets(f)
				killClipPlugSet(f.defs)
			},
			in: ListItemsInput{IncludeRatings: true},
			want: strings.Join(slices.Concat([]string{
				"Items 1-6 of 6.",
				"",
				ratedHeader,
			}, rated(
				"S | D (sheet C)",
				"roll unknown (sheet S) | roll unknown (sheet C)",
				"roll unknown (sheet S) | roll unknown (sheet C)",
				"A | not listed",
				"no tier | not listed",
			)), "\n"),
		},
		// No roll reaches the PvE tier S without the plug sets.
		"minimum keeps a copy whose rating a liked perk raises": {
			prepare: func(f *fakes) {
				bothSheets(f)
				killClipPlugSet(f.defs)
			},
			in:   ListItemsInput{MinPvERating: "S"},
			want: pvpD,
		},
		"minimum with rating columns": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "A", IncludeRatings: true},
			want: strings.Join([]string{
				"Items 1-2 of 2.",
				"",
				ratedHeader,
				row1001 + " | A (sheet S) | D (sheet C)",
				row1002 + " | A | not listed",
			}, "\n"),
		},
		"minimum on the first page": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "A", Limit: 1},
			want: strings.Join([]string{
				"Items 1-1 of 2. Pass offset=1 for the next page.",
				"",
				plainHeader,
				row1001,
			}, "\n"),
		},
		"minimum on the second page": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "A", Limit: 1, Offset: 1},
			want:    strings.Join([]string{"Items 2-2 of 2.", "", plainHeader, row1002}, "\n"),
		},
		"minimum with an offset past the end": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "A", Offset: 5},
			want:    "Offset 2 is past the last of 2 matching items.",
		},
		// The Aegis sheet also lists the hash of the helmet, with the
		// tier S and without columns.
		"minimum never keeps armor that the sheet lists": {
			prepare: func(f *fakes) {
				bothSheets(f)

				f.service.Data["/artifacts/rolls.a.json"] = `{"rolls": [` +
					`{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900], [904]]}, ` +
					`{"category": "Exotic Weapons", "tier": "A", "hashes": [101], "slots": []}, ` +
					`{"category": "HCs", "hashes": [102], "slots": []}, ` +
					`{"category": "Exotic Weapons", "tier": "S", "hashes": [200], "slots": []}]}`
			},
			in:   ListItemsInput{MinPvERating: "S"},
			want: "No items match.",
		},
		"minimum of the one mode with a sheet": {
			prepare: aegisOnly,
			in:      ListItemsInput{MinPvERating: "A"},
			want:    pveA,
		},
		"minimum of the one mode with a sheet, with rating columns": {
			prepare: aegisOnly,
			in:      ListItemsInput{MinPvERating: "A", IncludeRatings: true},
			want: strings.Join([]string{
				"Items 1-2 of 2.",
				noPvP,
				"",
				ratedHeader,
				row1001 + " | A (sheet S) | unavailable",
				row1002 + " | A | unavailable",
			}, "\n"),
		},
		"invalid minimum pve rating": {
			in:      ListItemsInput{MinPvERating: "z"},
			want:    `invalid input: min_pve_rating "z" is not S, A, B, C, D, E, or F`,
			isError: true,
		},
		"invalid minimum pvp rating": {
			in:      ListItemsInput{MinPvPRating: "z"},
			want:    `invalid input: min_pvp_rating "z" is not S, A, B, C, D, E, or F`,
			isError: true,
		},
		"minimum that starts with a tier letter": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "ab"},
			want:    `invalid input: min_pve_rating "ab" is not S, A, B, C, D, E, or F`,
			isError: true,
		},
		"two invalid minimums name the pve input": {
			in:      ListItemsInput{MinPvERating: "z", MinPvPRating: "z"},
			want:    `invalid input: min_pve_rating "z" is not S, A, B, C, D, E, or F`,
			isError: true,
		},
		"invalid minimum wins against a mode without ratings": {
			in:      ListItemsInput{MinPvERating: "A", MinPvPRating: "z"},
			want:    `invalid input: min_pvp_rating "z" is not S, A, B, C, D, E, or F`,
			isError: true,
		},
		"minimum pve rating without a sheet": {
			in:      ListItemsInput{MinPvERating: "A"},
			want:    noAegisMinimum,
			isError: true,
		},
		"minimum pvp rating without a sheet": {
			in:      ListItemsInput{MinPvPRating: "A"},
			want:    noPvPMinimum,
			isError: true,
		},
		"two minimums without a sheet name the pve input": {
			in:      ListItemsInput{MinPvERating: "A", MinPvPRating: "A"},
			want:    noAegisMinimum,
			isError: true,
		},
		"two minimums with only the Aegis sheet name the pvp input": {
			prepare: aegisOnly,
			in:      ListItemsInput{MinPvERating: "A", MinPvPRating: "A"},
			want:    noPvPMinimum,
			isError: true,
		},
		// The index lists a file that the service does not serve.
		"minimum of a sheet that does not load": {
			prepare: func(f *fakes) {
				f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.x.json"}}
			},
			in: ListItemsInput{MinPvERating: "A"},
			want: "min_pve_rating: rating unavailable: fetching artifact rolls.x.json: " +
				"service: {service}/artifacts/rolls.x.json returned status 404",
			isError: true,
		},
		"minimum without the perk preferences": {
			prepare: func(f *fakes) {
				aegisOnly(f)
				delete(f.service.Data, pathPrefs)
			},
			in: ListItemsInput{MinPvERating: "A"},
			want: "min_pve_rating: rating unavailable: reading perk preferences: " +
				"service: GET /api/perk-preferences?since=0 returned status 404: Not found",
			isError: true,
		},
		"minimum without the definitions": {
			prepare: bothSheets,
			in:      ListItemsInput{MinPvERating: "A"},
			want:    "min_pve_rating: " + noDefs,
			noDefs:  true,
			isError: true,
		},
		// A mode without ratings never reads as an empty result.
		"minimum without a sheet and without a match": {
			in:      ListItemsInput{Query: "thorn", MinPvERating: "A"},
			want:    noAegisMinimum,
			isError: true,
		},
		"invalid filter with a minimum": {
			in:      ListItemsInput{Kind: "ghost", MinPvERating: "A"},
			want:    `invalid filter: kind "ghost" is not weapon, armor, gear, other, or all`,
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			if tc.prepare != nil {
				tc.prepare(f)
			}

			if tc.noDefs {
				_, isError := call(t, f.h.handleListItems, ListItemsInput{})
				require.False(t, isError)

				f.dropDefs()
			}

			got, isError := call(t, f.h.handleListItems, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, strings.ReplaceAll(tc.want, "{service}", f.h.service.BaseURL()), got)
		})
	}
}

// withoutPowerFakes returns fakes whose Hunter holds the Sparrow
// Cryptomnesia and has the subclass Arcstrider equipped. The primary stat
// of the Sparrow is its Speed of 190, and the primary stat of the subclass
// reads -10. Neither item has a power. The vault holds Fatebringer, whose
// primary stat is an Attack of 1950.
func withoutPowerFakes(t *testing.T) *fakes {
	t.Helper()

	f := newFakes(t)
	f.defs.Items[400] = definitions.ItemDef{
		Name:       "Cryptomnesia",
		TypeName:   "Vehicle",
		Tier:       "Exotic",
		Bucket:     bungie.BucketVehicle,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}
	f.defs.Items[401] = definitions.ItemDef{
		Name:            "Arcstrider",
		TypeName:        "Hunter Subclass",
		Tier:            "Common",
		Bucket:          bungie.BucketSubclass,
		ClassType:       1,
		Equippable:      true,
		NonTransferable: true,
	}

	f.bungie.ProfileBody = `{"ErrorCode": 1, "Response": {
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"profileInventory": {"data": {"items": [
			{"itemHash": 100, "itemInstanceId": "2001", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802}
		]}},
		"characterInventories": {"data": {"c1": {"items": [
			{"itemHash": 400, "itemInstanceId": "2003", "lockable": true, "quantity": 1, "location": 1, "bucketHash": 2025709351}
		]}}},
		"characterEquipment": {"data": {"c1": {"items": [
			{"itemHash": 401, "itemInstanceId": "2002", "quantity": 1, "location": 1, "bucketHash": 3284755031, "transferStatus": 1}
		]}}},
		"itemComponents": {"instances": {"data": {
			"2001": {"primaryStat": {"statHash": 1480404414, "value": 1950}},
			"2002": {"primaryStat": {"statHash": 1935470627, "value": -10}, "isEquipped": true},
			"2003": {"primaryStat": {"statHash": 1501155019, "value": 190}}
		}}}
	}}`

	return f
}

// TestListItemsWithoutPower pins the power of the items that have none.
// Their power cell is empty, the power sort puts them after the items
// with power, and they pass no power bound.
func TestListItemsWithoutPower(t *testing.T) {
	t.Parallel()

	const (
		header   = "instance id | name | type | tier | power | stat total | location | tag | flags"
		weapon   = "2001 | Fatebringer | Kinetic Hand Cannon | Legendary | 1950 |  | Vault |  | "
		subclass = "2002 | Arcstrider | Hunter Subclass | Common |  |  | Hunter (equipped) |  | not transferable"
		vehicle  = "2003 | Cryptomnesia | Vehicle | Exotic |  |  | Hunter |  | "
	)

	tests := map[string]struct {
		in   ListItemsInput
		want []string
	}{
		"other items of the Hunter": {
			in:   ListItemsInput{Kind: inventory.KindOther, Location: "hunter"},
			want: []string{"Items 1-2 of 2.", "", header, subclass, vehicle},
		},
		"other items by power": {
			in:   ListItemsInput{Kind: inventory.KindOther, Sort: inventory.SortPower},
			want: []string{"Items 1-2 of 2.", "", header, subclass, vehicle},
		},
		"every item by power": {
			in:   ListItemsInput{Kind: inventory.KindAll, Sort: inventory.SortPower},
			want: []string{"Items 1-3 of 3.", "", header, weapon, subclass, vehicle},
		},
		"maximum power": {
			in:   ListItemsInput{Kind: inventory.KindAll, MaxPower: 2000},
			want: []string{"Items 1-1 of 1.", "", header, weapon},
		},
		"minimum power": {
			in:   ListItemsInput{Kind: inventory.KindAll, MinPower: 1},
			want: []string{"Items 1-1 of 1.", "", header, weapon},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, isError := call(t, withoutPowerFakes(t).h.handleListItems, tc.in)
			assert.False(t, isError)
			assert.Equal(t, strings.Join(tc.want, "\n"), got)
		})
	}
}

// TestGetItemWithoutPower pins that get_item leaves out the power line of
// an item that has no power.
func TestGetItemWithoutPower(t *testing.T) {
	t.Parallel()

	got, isError := call(t, withoutPowerFakes(t).h.handleGetItem, GetItemInput{InstanceID: "2003"})
	assert.False(t, isError)
	assert.Contains(t, got, "Cryptomnesia\nInstance id: 2003\n")
	assert.NotContains(t, got, "Power:")
}

// TestListItemsRatingRequests pins the requests that the rating columns
// of list_items cost. In a call without a minimum roll rating, the
// include_ratings input alone decides them. A call without it sends
// none, and a call with it sends them also for an empty result. The
// filter runs first, so an invalid filter sends none.
func TestListItemsRatingRequests(t *testing.T) {
	t.Parallel()

	const sheet = `{"rolls": [{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900]]}]}`

	sheets := []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"}

	// step is one call on the fakes of a case. The counts and the paths
	// cover every request since the start of the case.
	type step struct {
		// fetched lists the artifact paths that the service received.
		fetched []string
		in      ListItemsInput
		// configs counts the requests for /api/config. The first tool
		// call reads the Bungie API key from it, and each sheet load
		// reads the artifact index from it.
		configs int
		// prefs counts the requests for the perk preferences.
		prefs   int
		isError bool
	}

	tests := map[string]struct {
		steps []step
	}{
		"only a call with ratings reads the rating inputs": {
			steps: []step{
				{in: ListItemsInput{}, configs: 1},
				{in: ListItemsInput{IncludeRatings: true}, configs: 2, prefs: 1, fetched: sheets},
			},
		},
		"invalid filter reads no rating input": {
			steps: []step{
				{in: ListItemsInput{Kind: "ghost", IncludeRatings: true}, configs: 1, isError: true},
			},
		},
		"empty result reads the rating inputs": {
			steps: []step{
				{in: ListItemsInput{Query: "thorn", IncludeRatings: true}, configs: 2, prefs: 1, fetched: sheets},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}}
			f.service.Data["/artifacts/rolls.a.json"] = sheet
			f.service.Data["/artifacts/pvprolls.a.json"] = sheet

			for i, step := range tc.steps {
				_, isError := call(t, f.h.handleListItems, step.in)
				assert.Equalf(t, step.isError, isError, "call %d", i+1)

				var fetched []string

				for _, req := range f.service.All() {
					if strings.HasPrefix(req.Path, "/artifacts/") {
						fetched = append(fetched, req.Path)
					}
				}

				assert.Equalf(t, step.fetched, fetched, "call %d", i+1)
				assert.Lenf(t, f.service.Calls(http.MethodGet, "/api/config"), step.configs, "call %d", i+1)
				assert.Lenf(t, f.service.Calls(http.MethodGet, pathPrefs), step.prefs, "call %d", i+1)
			}
		})
	}
}

// TestListItemsMinimumRequests pins the requests that a minimum roll
// rating of list_items costs. A value that is not a tier letter fails
// the call before the first request. A call with valid minimums reads
// the perk preferences one time, with and without the rating columns.
// The filter runs first, so an invalid filter does not read them.
func TestListItemsMinimumRequests(t *testing.T) {
	t.Parallel()

	const sheet = `{"rolls": [{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900]]}]}`

	tests := map[string]struct {
		in ListItemsInput
		// prefs counts the requests for the perk preferences.
		prefs int
		// quiet marks a call that reaches neither the fake service nor
		// the fake Bungie platform.
		quiet   bool
		isError bool
	}{
		"invalid minimum sends no request": {
			in:      ListItemsInput{MinPvERating: "z"},
			quiet:   true,
			isError: true,
		},
		"two minimums read the perk preferences one time": {
			in:    ListItemsInput{MinPvERating: "A", MinPvPRating: "D"},
			prefs: 1,
		},
		"minimum with rating columns reads the perk preferences one time": {
			in:    ListItemsInput{MinPvERating: "A", IncludeRatings: true},
			prefs: 1,
		},
		"invalid filter does not read the perk preferences": {
			in:      ListItemsInput{Kind: "ghost", MinPvERating: "A"},
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}}
			f.service.Data["/artifacts/rolls.a.json"] = sheet
			f.service.Data["/artifacts/pvprolls.a.json"] = sheet

			_, isError := call(t, f.h.handleListItems, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Len(t, f.service.Calls(http.MethodGet, pathPrefs), tc.prefs)

			if tc.quiet {
				assert.Empty(t, f.service.All())
				assert.Empty(t, f.bungie.All())
			}
		})
	}
}

func TestRenderItemsStatTotal(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		item *inventory.Item
		want string
	}{
		"armor with a total of 0": {
			item: &inventory.Item{
				Name:  "Zero Helmet",
				Kind:  inventory.KindArmor,
				Stats: []inventory.Stat{{Name: "Grenade", Value: 0}, {Name: "Health", Value: 0}},
			},
			want: "0",
		},
		"armor without stats": {
			item: &inventory.Item{Name: "Bare Helmet", Kind: inventory.KindArmor},
			want: "",
		},
		"weapon with stats": {
			item: &inventory.Item{
				Name:  "Ace of Spades",
				Kind:  inventory.KindWeapon,
				Stats: []inventory.Stat{{Name: "Range", Value: 60}},
			},
			want: "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tc.item.InstanceID = "21"
			tc.item.Owner = inventory.OwnerVault
			tc.item.Transferable = true

			got := renderItems(&inventory.Snapshot{}, []*inventory.Item{tc.item}, 0, 1, "", false, nil)
			assert.Equal(t, strings.Join([]string{
				"Items 1-1 of 1.",
				"",
				"instance id | name | type | tier | power | stat total | location | tag | flags",
				"21 | " + tc.item.Name + " |  |  |  | " + tc.want + " | Vault |  | ",
			}, "\n"), got)
		})
	}
}

func TestSnapshotCache(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	for range 2 {
		_, isError := call(t, f.h.handleListItems, ListItemsInput{})
		require.False(t, isError)
	}

	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1, "the second call reuses the profile")
	assert.Len(t, f.service.Calls(http.MethodGet, pathTags), 2, "every call reads the tags")

	_, isError := call(t, f.h.handleListItems, ListItemsInput{Refresh: true})
	require.False(t, isError)
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 2)

	saved, err := f.h.auth.Saved()
	require.NoError(t, err)
	assert.Equal(t, bungie.Membership{ID: "4611", Type: 3}, saved.Membership)
	assert.Len(t, f.bungie.Calls(http.MethodGet, "/User/GetMembershipsForCurrentUser/"), 1)
}

func TestGetItem(t *testing.T) {
	t.Parallel()

	// The lines that end the output of a fixture weapon. No fixture
	// weapon holds a frame. A hand cannon with perks ends with noFrame,
	// and a hand cannon for which the profile lists no perks ends with
	// noPerks. The damage data has no rocket launchers, so a rocket
	// launcher without perks ends with noPerksRocket.
	const (
		noPlugs = "The profile lists no perks for this copy, so the server cannot tell its frame."
		noFrame = "Times and falloff: unknown. The community data has no curves for the frame of this weapon.\n\n" +
			"Time to kill and damage per second: unknown. The profile lists no frame among the perks of this copy."
		noPerks = "Times and falloff: unknown. " + noPlugs + "\n\n" +
			"Time to kill and damage per second: unknown. " + noPlugs
		noPerksRocket = "Times and falloff: unknown. " + noPlugs + "\n\n" +
			"Time to kill and damage per second: unknown. The community data has no damage for this weapon type."
	)

	// fatebringer returns the output for instance 1001 with the given
	// lines of the roll ratings block. The fixture database of Clarity
	// describes Explosive Payload and not Firefly, and the fixture
	// definitions give only Firefly a text of Bungie.
	fatebringer := func(ratings ...string) string {
		return strings.Join(slices.Concat([]string{
			"Fatebringer",
			"Instance id: 1001",
			"Item hash: 100",
			"Type: Kinetic Hand Cannon",
			"Tier: Legendary",
			"Slot: Kinetic Weapons",
			"Power: 2010",
			"Location: Vault",
			"Tag: keep",
			"Flags: favorite, locked, masterwork, note",
			"Note: god roll",
			"",
			"Perks:",
			"- Explosive Payload (Trait), can swap to Rampage",
			"  Clarity: Each bullet deals half of its damage as an explosion.",
			"  \U00002022 The explosion ignores damage falloff.",
			"- Firefly (Trait)",
			"  Precision final blows cause targets to explode.",
			"  Explosions deal Solar damage.",
			clarityCreditLine,
			"",
			"Roll ratings:",
		}, ratings, []string{
			"",
			"Stats:",
			"- Range: 55",
			"",
			noFrame,
		}), "\n")
	}

	const (
		noAegis = "- PvE: rating unavailable: the service publishes no Aegis sheet"
		noPvP   = "- PvP: rating unavailable: the service publishes no PvP sheet"

		// noPrefsPvE is the PvE line of a call that cannot read the perk
		// preferences.
		noPrefsPvE = "- PvE: rating unavailable: reading perk preferences: " +
			"service: GET /api/perk-preferences?since=0 returned status 404: Not found"

		// The lines of a call that cannot read the definitions.
		noDefsPvE = "- PvE: rating unavailable: loading definitions: " +
			"definitions: the service index lists no core artifact"
		noDefsPvP = "- PvP: rating unavailable: loading definitions: " +
			"definitions: the service index lists no core artifact"

		// Fatebringer is item 100, Gjallarhorn is item 101, and Midnight
		// Coup is item 102. The row of an exotic has no perk columns.
		//
		// Instance 1001 holds Explosive Payload (900) with Rampage (903)
		// beside it in one socket, and Firefly (901) in another. Its
		// socket with the shader (902) is not a perk socket, and no
		// socket of the copy holds Kill Clip (904).
		//
		// The fake perk preferences like Firefly for PvE and dislike
		// Rampage for PvP. Each preference changes a line only when a
		// column of its game mode lists its perk. The cases use allHit
		// as a PvE sheet and rampageOnly as a PvP sheet to show the
		// change.
		//
		// The fixture definitions name every plug of the sheets, so each
		// column line also names the picks of its column. Plugs 904 and
		// 905 are the base trait and the enhanced trait of Kill Clip.
		tiered      = `{"rolls": [{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900, 903], [904]]}]}`
		allHit      = `{"rolls": [{"category": "HCs", "tier": "B", "hashes": [100], "slots": [[900], [901]]}]}`
		rampageOnly = `{"rolls": [{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[903]]}]}`
		untiered    = `{"rolls": [{"category": "HCs", "hashes": [100], "slots": [[904], [900]]}]}`
		shaderOnly  = `{"rolls": [{"category": "HCs", "tier": "E", "hashes": [100], "slots": [[902], [902]]}]}`
		columnless  = `{"rolls": [{"category": "HCs", "tier": "A", "hashes": [100], "slots": []}]}`
		elsewhere   = `{"rolls": [{"category": "Exotic Weapons", "tier": "B", "hashes": [101], "slots": []}]}`

		// The row of ranked has a rank and a note. A blank line parts
		// the two lines of the note. The row of unranked has neither.
		ranked = `{"rolls": [{"category": "HCs", "tier": "S", "rank": 3, "ranked": 62, "hashes": [100], ` +
			`"slots": [[900], [904]], "notes": "Add clear and boss damage.\n\nPairs with Firefly."}]}`
		unranked = `{"rolls": [{"category": "Hand Cannon", "tier": "C", "hashes": [100], "slots": [[900], [904]]}]}`

		// Both sheets pick Explosive Payload and Rampage for the first
		// column and the two Kill Clip plugs for the second.
		pickedAegis = `{"rolls": [{"category": "HCs", "tier": "S", "rank": 3, "ranked": 62, "hashes": [100], ` +
			`"slots": [[900, 903], [904, 905]]}]}`
		pickedPvP = `{"rolls": [{"category": "Hand Cannon", "tier": "C", "hashes": [100], ` +
			`"slots": [[900, 903], [904, 905]]}]}`

		// Both sheets pick Explosive Payload for the first column and
		// the two Kill Clip plugs for the second.
		clipAegis = `{"rolls": [{"category": "HCs", "tier": "S", "rank": 3, "ranked": 62, "hashes": [100], ` +
			`"slots": [[900], [904, 905]]}]}`
		clipPvP = `{"rolls": [{"category": "Hand Cannon", "tier": "C", "hashes": [100], ` +
			`"slots": [[900], [904, 905]]}]}`

		// likesPayloadForPvP holds the fake perk preferences and a PvP
		// like of Explosive Payload.
		likesPayloadForPvP = `{"preferences": [
			{"mode": "pve", "perk": "Firefly", "stance": "up", "removed": false, "updatedAt": 5},
			{"mode": "pvp", "perk": "Rampage", "stance": "down", "removed": false, "updatedAt": 5},
			{"mode": "pvp", "perk": "Explosive Payload", "stance": "up", "removed": false, "updatedAt": 5}
		], "now": 10}`
	)

	// midnightCoup returns the output for instance 1003 with the given
	// lines of the roll ratings block. The profile lists no perks for
	// the copy.
	midnightCoup := func(ratings ...string) string {
		return strings.Join(slices.Concat([]string{
			"Midnight Coup",
			"Instance id: 1003",
			"Item hash: 102",
			"Type: Kinetic Hand Cannon",
			"Tier: Legendary",
			"Slot: Kinetic Weapons",
			"Power: 2005",
			"Location: Hunter",
			"",
			"Roll ratings:",
		}, ratings, []string{"", noPerks}), "\n")
	}

	tests := map[string]struct {
		// files holds the file list of each artifact in the service
		// index.
		files map[string][]string
		// bodies holds the body of each sheet file that the service
		// serves.
		bodies map[string]string
		// defs changes the fixture definitions before the call.
		defs func(*definitions.Defs)
		// prefsBody replaces the perk preferences that the service
		// serves.
		prefsBody string
		id        string
		// want is the output. "{service}" stands for the base URL of the
		// service.
		want string
		// fetched lists the artifact paths that the call requests.
		fetched []string
		// configs counts the requests for /api/config. The first tool
		// call reads the Bungie API key from it, and each sheet load
		// reads the artifact index from it.
		configs int
		// prefs counts the requests for the perk preferences.
		prefs int
		// noPrefs makes the service answer the request for the perk
		// preferences with the status 404.
		noPrefs bool
		// noDefs makes the definition read of the rating not succeed.
		// The case runs get_item one time to load the snapshot and then
		// drops the loaded definitions. The call under test reuses the
		// snapshot, and its definition read asks the service, whose
		// index lists no core artifact. The counts cover both calls.
		noDefs  bool
		isError bool
	}{
		// The call reads the perk preferences also when no mode has a
		// sheet.
		"service publishes neither sheet": {
			id:      "1001",
			want:    fatebringer(noAegis, noPvP),
			configs: 2,
			prefs:   1,
		},
		"liked perk carries the mark in its game mode only": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    allHit,
				"pvprolls.a.json": allHit,
			},
			id: "1001",
			want: fatebringer(
				"- PvE: B for this roll, B on the Aegis sheet (in HCs), 2 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: hit with Firefly (liked). The sheet picks Firefly",
				"- PvP: B for this roll, B on the PvP sheet (in HCs), 2 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: hit with Firefly. The sheet picks Firefly",
			),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		// The PvP line names no pick, because the user dislikes the only
		// pick of its column.
		"disliked only pick misses in its game mode only": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    rampageOnly,
				"pvprolls.a.json": rampageOnly,
			},
			id: "1001",
			want: fatebringer(
				"- PvE: S for this roll, S on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Rampage. The sheet picks Rampage",
				"- PvP: A for this roll, S on the PvP sheet (in HCs), 0 of 1 perk columns hit",
				"  Column 1: miss",
			),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		// The mode without a sheet keeps the reason of the sheet.
		"unreadable perk preferences replace the rating of the loaded sheet": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			bodies:  map[string]string{"rolls.a.json": tiered},
			id:      "1001",
			want:    fatebringer(noPrefsPvE, noPvP),
			fetched: []string{"/artifacts/rolls.a.json"},
			configs: 2,
			prefs:   1,
			noPrefs: true,
		},
		// The reason does not depend on the item, so the line does not
		// say that the sheet leaves out the weapon.
		"unreadable perk preferences give a weapon that the sheet does not list the same reason": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			bodies:  map[string]string{"rolls.a.json": tiered},
			id:      "1003",
			want:    midnightCoup(noPrefsPvE, noPvP),
			fetched: []string{"/artifacts/rolls.a.json"},
			configs: 2,
			prefs:   1,
			noPrefs: true,
		},
		"roll with a hit and a miss, and sheet that does not list the weapon": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    tiered,
				"pvprolls.a.json": elsewhere,
			},
			id: "1001",
			want: fatebringer(
				"- PvE: A for this roll, S on the Aegis sheet (in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload, Rampage. The sheet picks Explosive Payload, Rampage",
				"  Column 2: miss. The sheet picks Kill Clip",
				"- PvP: not on the PvP sheet",
			),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		"roll that hits every column and row without a tier": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    untiered,
				"pvprolls.a.json": allHit,
			},
			id: "1001",
			want: fatebringer(
				"- PvE: no tier on the Aegis sheet (in HCs), 1 of 2 perk columns hit",
				"  Column 1: miss. The sheet picks Kill Clip",
				"  Column 2: hit with Explosive Payload. The sheet picks Explosive Payload",
				"- PvP: B for this roll, B on the PvP sheet (in HCs), 2 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: hit with Firefly. The sheet picks Firefly",
			),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		// The copy holds plug 902 only in a socket that the perk list
		// leaves out, so the rating does not read that socket. The line
		// still names the plug, because the picks come from the sheet.
		"column that lists only a plug outside the perk list misses": {
			files:  map[string][]string{"rolls": {"rolls.a.json"}},
			bodies: map[string]string{"rolls.a.json": shaderOnly},
			id:     "1001",
			want: fatebringer(
				"- PvE: F for this roll, E on the Aegis sheet (in HCs), 0 of 2 perk columns hit",
				"  Column 1: miss. The sheet picks Default Shader",
				"  Column 2: miss. The sheet picks Default Shader",
				noPvP,
			),
			fetched: []string{"/artifacts/rolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		"row without columns and sheet that does not list the weapon": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    columnless,
				"pvprolls.a.json": elsewhere,
			},
			id: "1001",
			want: fatebringer(
				"- PvE: A on the Aegis sheet (in HCs), which judges no perk columns",
				"- PvP: not on the PvP sheet",
			),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		"sheet that does not list the weapon and sheet that the index does not list": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			bodies:  map[string]string{"rolls.a.json": elsewhere},
			id:      "1001",
			want:    fatebringer("- PvE: not on the Aegis sheet", noPvP),
			fetched: []string{"/artifacts/rolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		"listed sheet file returns 404": {
			files:  map[string][]string{"rolls": {"rolls.x.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{"pvprolls.a.json": allHit},
			id:     "1001",
			want: fatebringer(
				"- PvE: rating unavailable: fetching artifact rolls.x.json: "+
					"service: {service}/artifacts/rolls.x.json returned status 404",
				"- PvP: B for this roll, B on the PvP sheet (in HCs), 2 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: hit with Firefly. The sheet picks Firefly",
			),
			fetched: []string{"/artifacts/rolls.x.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		"sheet file does not decode": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.x.json"}},
			bodies: map[string]string{
				"rolls.a.json":    tiered,
				"pvprolls.x.json": `{"rolls": []}`,
			},
			id: "1001",
			want: fatebringer(
				"- PvE: A for this roll, S on the Aegis sheet (in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload, Rampage. The sheet picks Explosive Payload, Rampage",
				"  Column 2: miss. The sheet picks Kill Clip",
				"- PvP: rating unavailable: decoding artifact pvprolls.x.json: the roll sheet lists no weapon",
			),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.x.json"},
			configs: 2,
			prefs:   1,
		},
		"copy without perks has an unknown roll": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    tiered,
				"pvprolls.a.json": untiered,
			},
			id: "1005",
			want: strings.Join([]string{
				"Fatebringer",
				"Instance id: 1005",
				"Item hash: 100",
				"Type: Kinetic Hand Cannon",
				"Tier: Legendary",
				"Slot: Kinetic Weapons",
				"Power: 1970",
				"Location: Hunter (postmaster)",
				"",
				"Roll ratings:",
				"- PvE: S on the Aegis sheet (in HCs), roll unknown: the profile lists no perks for this copy",
				"- PvP: no tier on the PvP sheet (in HCs), roll unknown: the profile lists no perks for this copy",
				"",
				noPerks,
			}, "\n"),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		// The note of the PvE row comes before the PvP line.
		"row with a rank and a note, and row with neither": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    ranked,
				"pvprolls.a.json": unranked,
			},
			id: "1001",
			want: fatebringer(
				"- PvE: A for this roll, S on the Aegis sheet (rank 3 of 62 in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: miss. The sheet picks Kill Clip",
				"  Sheet note: Add clear and boss damage.",
				"  Pairs with Firefly.",
				"- PvP: D for this roll, C on the PvP sheet (in Hand Cannon), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: miss. The sheet picks Kill Clip",
			),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		"copy without perks shows the rank and the note of its row": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    ranked,
				"pvprolls.a.json": unranked,
			},
			id: "1005",
			want: strings.Join([]string{
				"Fatebringer",
				"Instance id: 1005",
				"Item hash: 100",
				"Type: Kinetic Hand Cannon",
				"Tier: Legendary",
				"Slot: Kinetic Weapons",
				"Power: 1970",
				"Location: Hunter (postmaster)",
				"",
				"Roll ratings:",
				"- PvE: S on the Aegis sheet (rank 3 of 62 in HCs), " +
					"roll unknown: the profile lists no perks for this copy",
				"  Sheet note: Add clear and boss damage.",
				"  Pairs with Firefly.",
				"- PvP: C on the PvP sheet (in Hand Cannon), roll unknown: the profile lists no perks for this copy",
				"",
				noPerks,
			}, "\n"),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		"exotic without perks keeps the tier of a row without columns": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    elsewhere,
				"pvprolls.a.json": allHit,
			},
			id: "1002",
			want: strings.Join([]string{
				"Gjallarhorn",
				"Instance id: 1002",
				"Item hash: 101",
				"Type: Solar Rocket Launcher",
				"Tier: Exotic",
				"Slot: Power Weapons",
				"Power: 1990",
				"Location: Vault",
				"",
				"Roll ratings:",
				"- PvE: B on the Aegis sheet (in Exotic Weapons), which judges no perk columns",
				"- PvP: not on the PvP sheet",
				"",
				noPerksRocket,
			}, "\n"),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		"weapon that neither sheet lists": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    tiered,
				"pvprolls.a.json": elsewhere,
			},
			id:      "1003",
			want:    midnightCoup("- PvE: not on the Aegis sheet", "- PvP: not on the PvP sheet"),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		// Rampage is a hit and a pick for PvE. The user dislikes it for
		// PvP, so the PvP lines name it as neither. The base trait and
		// the enhanced trait of Kill Clip give one pick.
		"each column line names the picks of its column": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    pickedAegis,
				"pvprolls.a.json": pickedPvP,
			},
			id: "1001",
			want: fatebringer(
				"- PvE: A for this roll, S on the Aegis sheet (rank 3 of 62 in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload, Rampage. The sheet picks Explosive Payload, Rampage",
				"  Column 2: miss. The sheet picks Kill Clip",
				"- PvP: D for this roll, C on the PvP sheet (in Hand Cannon), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: miss. The sheet picks Kill Clip",
			),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		// The user dislikes Rampage, the only pick of the PvP column,
		// so the line names no pick. The liked Explosive Payload shares
		// the socket of Rampage and earns the column.
		"hit line whose picks are all disliked names only the liked hit": {
			files:     map[string][]string{"pvprolls": {"pvprolls.a.json"}},
			bodies:    map[string]string{"pvprolls.a.json": rampageOnly},
			prefsBody: likesPayloadForPvP,
			id:        "1001",
			want: fatebringer(
				noAegis,
				"- PvP: S for this roll, S on the PvP sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Explosive Payload (liked)",
			),
			fetched: []string{"/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		// The copy rolled no pick of the second column. The plug set of
		// the socket with Firefly lists the picks of that column, so
		// that socket is the socket of the column. The user likes
		// Firefly only for PvE.
		"liked perk earns a column that rolled no pick in its game mode only": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    clipAegis,
				"pvprolls.a.json": clipPvP,
			},
			defs: killClipPlugSet,
			id:   "1001",
			want: fatebringer(
				"- PvE: S for this roll, S on the Aegis sheet (rank 3 of 62 in HCs), 2 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: hit with Firefly (liked). The sheet picks Kill Clip",
				"- PvP: D for this roll, C on the PvP sheet (in Hand Cannon), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: miss. The sheet picks Kill Clip",
			),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		// The definitions hold no plug sets, so no socket of the copy
		// is the socket of the second column.
		"liked perk earns no column that rolled no pick without the plug sets": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    clipAegis,
				"pvprolls.a.json": clipPvP,
			},
			id: "1001",
			want: fatebringer(
				"- PvE: A for this roll, S on the Aegis sheet (rank 3 of 62 in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: miss. The sheet picks Kill Clip",
				"- PvP: D for this roll, C on the PvP sheet (in Hand Cannon), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
				"  Column 2: miss. The sheet picks Kill Clip",
			),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 2,
			prefs:   1,
		},
		// The result is not an error result, and the perks and the
		// stats still print.
		"unreadable definitions replace the rating of each loaded sheet": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    tiered,
				"pvprolls.a.json": allHit,
			},
			id:      "1001",
			want:    fatebringer(noDefsPvE, noDefsPvP),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 4,
			prefs:   2,
			noDefs:  true,
		},
		"unreadable definitions leave the reason of a mode without a sheet": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			bodies:  map[string]string{"rolls.a.json": tiered},
			id:      "1001",
			want:    fatebringer(noDefsPvE, noPvP),
			fetched: []string{"/artifacts/rolls.a.json"},
			configs: 4,
			prefs:   2,
			noDefs:  true,
		},
		// The reason does not depend on the item, so no line says that
		// the sheet leaves out the weapon.
		"unreadable definitions give a weapon that the sheets do not list the same reason": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{
				"rolls.a.json":    tiered,
				"pvprolls.a.json": elsewhere,
			},
			id:      "1003",
			want:    midnightCoup(noDefsPvE, noDefsPvP),
			fetched: []string{"/artifacts/rolls.a.json", "/artifacts/pvprolls.a.json"},
			configs: 4,
			prefs:   2,
			noDefs:  true,
		},
		// The call reads the definitions also when no mode has a sheet.
		// The definition read of the second call is its second request
		// for the service index.
		"unreadable definitions leave the reasons of two modes without a sheet": {
			id:      "1001",
			want:    fatebringer(noAegis, noPvP),
			configs: 4,
			prefs:   2,
			noDefs:  true,
		},
		// The server reads the perk preferences before the definitions,
		// and the reason of the earlier read stays.
		"unreadable perk preferences win against unreadable definitions": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			bodies:  map[string]string{"rolls.a.json": tiered},
			id:      "1001",
			want:    fatebringer(noPrefsPvE, noPvP),
			fetched: []string{"/artifacts/rolls.a.json"},
			configs: 4,
			prefs:   2,
			noPrefs: true,
			noDefs:  true,
		},
		"armor has no ratings and reads no sheet and no perk preferences": {
			files:  map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}},
			bodies: map[string]string{"rolls.a.json": tiered, "pvprolls.a.json": allHit},
			id:     "1004",
			want: strings.Join([]string{
				"Celestial Nighthawk",
				"Instance id: 1004",
				"Item hash: 200",
				"Type: Helmet",
				"Tier: Exotic",
				"Slot: Helmet",
				"Class: Hunter",
				"Power: 2000",
				"Location: Hunter (equipped)",
				"",
				"Stats:",
				"- Grenade: 25",
				"- Health: 30",
			}, "\n"),
			configs: 1,
		},
		"unknown instance id": {
			id:      "9",
			want:    "invalid input: no item in the inventory has the instance id 9",
			configs: 1,
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.ArtifactFiles = tc.files

			for file, body := range tc.bodies {
				f.service.Data["/artifacts/"+file] = body
			}

			if tc.prefsBody != "" {
				f.service.Data[pathPrefs] = tc.prefsBody
			}

			if tc.noPrefs {
				delete(f.service.Data, pathPrefs)
			}

			if tc.defs != nil {
				tc.defs(f.defs)
			}

			if tc.noDefs {
				_, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: tc.id})
				require.False(t, isError)

				f.dropDefs()
			}

			got, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: tc.id})
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, strings.ReplaceAll(tc.want, "{service}", f.h.service.BaseURL()), got)

			var fetched []string

			for _, req := range f.service.All() {
				if strings.HasPrefix(req.Path, "/artifacts/") {
					fetched = append(fetched, req.Path)
				}
			}

			assert.Equal(t, tc.fetched, fetched)
			assert.Len(t, f.service.Calls(http.MethodGet, "/api/config"), tc.configs)
			assert.Len(t, f.service.Calls(http.MethodGet, pathPrefs), tc.prefs)
		})
	}
}

// TestGetItemRatesOnEachCall pins that get_item rates the copy from the
// cached snapshot on every call. A rating that the server kept from the
// first call names Explosive Payload first after the perk change.
func TestGetItemRatesOnEachCall(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
	f.service.Data["/artifacts/rolls.a.json"] = `{"rolls": [` +
		`{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[903, 900]]}]}`

	got, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
	require.False(t, isError)
	assert.Contains(t, got, "- PvE: S for this roll, S on the Aegis sheet (in HCs), 1 of 1 perk columns hit\n"+
		"  Column 1: hit with Explosive Payload, Rampage. The sheet picks Rampage, Explosive Payload\n")

	_, isError = call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: "Rampage"})
	require.False(t, isError)

	got, isError = call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
	require.False(t, isError)
	assert.Contains(t, got, "- PvE: S for this roll, S on the Aegis sheet (in HCs), 1 of 1 perk columns hit\n"+
		"  Column 1: hit with Rampage, Explosive Payload. The sheet picks Rampage, Explosive Payload\n")
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1, "both ratings read one profile")
}

// TestGetItemReadsPerkPreferencesOnEachCall pins that the server keeps
// no copy of the perk preferences. A preference that the user changes in
// the web client counts in the next call.
func TestGetItemReadsPerkPreferencesOnEachCall(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
	f.service.Data["/artifacts/rolls.a.json"] = `{"rolls": [` +
		`{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[901]]}]}`

	got, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
	require.False(t, isError)
	assert.Contains(t, got, "- PvE: S for this roll, S on the Aegis sheet (in HCs), 1 of 1 perk columns hit\n"+
		"  Column 1: hit with Firefly (liked). The sheet picks Firefly\n")

	f.service.Data[pathPrefs] = `{"preferences": [
		{"mode": "pve", "perk": "Firefly", "stance": "down", "removed": false, "updatedAt": 6}
	], "now": 11}`

	got, isError = call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
	require.False(t, isError)
	assert.Contains(t, got, "- PvE: A for this roll, S on the Aegis sheet (in HCs), 0 of 1 perk columns hit\n"+
		"  Column 1: miss\n")
	assert.Len(t, f.service.Calls(http.MethodGet, pathPrefs), 2)
}

// TestRatingToolsReadDefinitionsBeforeProfile pins the definition read
// of a call that fetches the profile. The read comes before the fetch,
// and the call returns the error of the read as an error result. Such a
// call therefore rates no roll. A call that reuses the fetched profile
// gives the reason of the definitions in place of a rating.
func TestRatingToolsReadDefinitionsBeforeProfile(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		run func(t *testing.T, h *handler) (string, bool)
	}{
		"get_item": {
			run: func(t *testing.T, h *handler) (string, bool) {
				t.Helper()

				return call(t, h.handleGetItem, GetItemInput{InstanceID: "1001"})
			},
		},
		"list_items with include_ratings": {
			run: func(t *testing.T, h *handler) (string, bool) {
				t.Helper()

				return call(t, h.handleListItems, ListItemsInput{IncludeRatings: true})
			},
		},
		"compare_duplicates": {
			run: func(t *testing.T, h *handler) (string, bool) {
				t.Helper()

				return call(t, h.handleCompareDuplicates, CompareDuplicatesInput{})
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
			f.service.Data["/artifacts/rolls.a.json"] = `{"rolls": [` +
				`{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900]]}]}`
			f.dropDefs()

			got, isError := tc.run(t, f.h)
			assert.True(t, isError)
			assert.Equal(t, "loading definitions: definitions: the service index lists no core artifact", got)
			assert.Empty(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), "the call fetches no profile")
			assert.Empty(t, f.service.Calls(http.MethodGet, pathPrefs), "the call rates no roll")
		})
	}
}

func TestRatingSources(t *testing.T) {
	t.Parallel()

	const (
		sheet = `{"rolls": [{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900]]}]}`

		noPvP      = "rating unavailable: the service publishes no PvP sheet"
		unreadable = "rating unavailable: reading perk preferences: " +
			"service: GET /api/perk-preferences?since=0 returned status 404: Not found"
		undefined = "rating unavailable: loading definitions: " +
			"definitions: the service index lists no core artifact"
	)

	tests := map[string]struct {
		// files holds the file list of each artifact in the service
		// index.
		files map[string][]string
		// stances holds the stance map of each mode whose source has a
		// sheet.
		stances map[string]map[string]string
		// reasons holds the reason of each mode whose source has no
		// sheet.
		reasons map[string]string
		// causes holds the error that the reason of a mode wraps beside
		// [rolls.ErrNoRating].
		causes map[string]error
		// noPrefs makes the service answer the request for the perk
		// preferences with the status 404.
		noPrefs bool
		// noDefs drops the loaded definitions. The definition read then
		// asks the service, whose index lists no core artifact.
		noDefs bool
	}{
		// Both artifacts list one file, so each mode loads the same
		// sheet.
		"each mode with a sheet carries the stances of the mode": {
			files: map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"rolls.a.json"}},
			stances: map[string]map[string]string{
				rolls.ModePvE: {"Firefly": rolls.StanceUp},
				rolls.ModePvP: {"Rampage": rolls.StanceDown},
			},
		},
		"mode without a sheet carries the reason of the sheet": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			stances: map[string]map[string]string{rolls.ModePvE: {"Firefly": rolls.StanceUp}},
			reasons: map[string]string{rolls.ModePvP: noPvP},
		},
		"unreadable perk preferences are the reason of each mode with a sheet": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			reasons: map[string]string{rolls.ModePvE: unreadable, rolls.ModePvP: noPvP},
			causes:  map[string]error{rolls.ModePvE: dvm.ErrService},
			noPrefs: true,
		},
		"unreadable definitions are the reason of each mode with a sheet": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			reasons: map[string]string{rolls.ModePvE: undefined, rolls.ModePvP: noPvP},
			causes:  map[string]error{rolls.ModePvE: definitions.ErrDefs},
			noDefs:  true,
		},
		"unreadable perk preferences win against unreadable definitions": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"rolls.a.json"}},
			reasons: map[string]string{rolls.ModePvE: unreadable, rolls.ModePvP: unreadable},
			causes:  map[string]error{rolls.ModePvE: dvm.ErrService, rolls.ModePvP: dvm.ErrService},
			noPrefs: true,
			noDefs:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.ArtifactFiles = tc.files
			f.service.Data["/artifacts/rolls.a.json"] = sheet

			if tc.noPrefs {
				delete(f.service.Data, pathPrefs)
			}

			defs := f.defs

			if tc.noDefs {
				f.dropDefs()
			}

			got := f.h.ratingSources(t.Context())
			require.Len(t, got, len(rolls.Modes))

			for _, mode := range rolls.Modes {
				source := got[mode]

				reason, ok := tc.reasons[mode]
				if !ok {
					require.NoErrorf(t, source.Err, "mode %s", mode)
					assert.NotNilf(t, source.Sheet, "mode %s", mode)
					assert.Equalf(t, tc.stances[mode], source.Stances, "mode %s", mode)
					assert.Samef(t, defs, source.Defs, "mode %s", mode)

					continue
				}

				require.ErrorIsf(t, source.Err, rolls.ErrNoRating, "mode %s", mode)
				require.EqualErrorf(t, source.Err, reason, "mode %s", mode)
				assert.Nilf(t, source.Sheet, "mode %s", mode)
				assert.Nilf(t, source.Stances, "mode %s", mode)
				assert.Nilf(t, source.Defs, "mode %s", mode)

				cause := tc.causes[mode]
				if cause != nil {
					require.ErrorIsf(t, source.Err, cause, "mode %s", mode)
				}
			}
		})
	}
}

func TestWriteRating(t *testing.T) {
	t.Parallel()

	// hit returns the verdict of a column that hit with the named
	// perks, none of which the user likes.
	hit := func(names ...string) rolls.ColumnVerdict {
		var verdict rolls.ColumnVerdict

		for _, name := range names {
			verdict.Hits = append(verdict.Hits, rolls.Hit{Name: name})
		}

		return verdict
	}

	// The note holds a blank line at its start and between its two
	// lines, and spaces around its second line.
	const note = "\nAdd clear and boss damage.\n\n  Pairs with Firefly.  \n"

	tests := map[string]struct {
		mode   string
		rating rolls.Rating
		want   []string
	}{
		"reason": {
			mode:   rolls.ModePvE,
			rating: rolls.Rating{Err: fmt.Errorf("%w: the service publishes no Aegis sheet", rolls.ErrNoRating)},
			want:   []string{"- PvE: rating unavailable: the service publishes no Aegis sheet"},
		},
		"weapon that the sheet does not list": {
			mode: rolls.ModePvE,
			want: []string{"- PvE: not on the Aegis sheet"},
		},
		"weapon that the PvP sheet does not list": {
			mode: rolls.ModePvP,
			want: []string{"- PvP: not on the PvP sheet"},
		},
		"row with a tier and no columns": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{
				Roll: &rolls.Roll{Row: rolls.SheetRow{Tier: "A", Category: "Exotic Weapons"}, Overall: "A"},
			},
			want: []string{"- PvE: A on the Aegis sheet (in Exotic Weapons), which judges no perk columns"},
		},
		"row without a tier and without columns": {
			mode:   rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{Row: rolls.SheetRow{Category: "Exotic Weapons"}}},
			want:   []string{"- PvE: no tier on the Aegis sheet (in Exotic Weapons), which judges no perk columns"},
		},
		"unknown roll on a row with a tier": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "S", Category: "HCs", Rank: 3, Ranked: 62},
				Unknown: true,
			}},
			want: []string{
				"- PvE: S on the Aegis sheet (rank 3 of 62 in HCs), " +
					"roll unknown: the profile lists no perks for this copy",
			},
		},
		"unknown roll on a row without a tier": {
			mode:   rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{Row: rolls.SheetRow{Category: "HCs"}, Unknown: true}},
			want: []string{
				"- PvE: no tier on the Aegis sheet (in HCs), roll unknown: the profile lists no perks for this copy",
			},
		},
		"roll with a tier and no miss prints the tier twice": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "S", Category: "HCs"},
				Overall: "S",
				Columns: []rolls.ColumnVerdict{hit("Explosive Payload"), hit("Firefly")},
			}},
			want: []string{
				"- PvE: S for this roll, S on the Aegis sheet (in HCs), 2 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload",
				"  Column 2: hit with Firefly",
			},
		},
		"roll with a tier and one miss": {
			mode: rolls.ModePvP,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "S", Category: "Hand Cannon"},
				Overall: "A",
				Columns: []rolls.ColumnVerdict{{}, hit("Firefly")},
			}},
			want: []string{
				"- PvP: A for this roll, S on the PvP sheet (in Hand Cannon), 1 of 2 perk columns hit",
				"  Column 1: miss",
				"  Column 2: hit with Firefly",
			},
		},
		"roll without a tier has no tier for the roll": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Category: "HCs", Rank: 3, Ranked: 62},
				Columns: []rolls.ColumnVerdict{hit("Explosive Payload"), {}},
			}},
			want: []string{
				"- PvE: no tier on the Aegis sheet (rank 3 of 62 in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload",
				"  Column 2: miss",
			},
		},
		"row with one column keeps the plural": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "HCs"},
				Overall: "C",
				Columns: []rolls.ColumnVerdict{hit("Firefly")},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly",
			},
		},
		"hit with two names": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "D", Category: "HCs"},
				Overall: "E",
				Columns: []rolls.ColumnVerdict{hit("Explosive Payload", "Rampage"), {}},
			}},
			want: []string{
				"- PvE: E for this roll, D on the Aegis sheet (in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload, Rampage",
				"  Column 2: miss",
			},
		},
		"hit with one liked name": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "HCs"},
				Overall: "C",
				Columns: []rolls.ColumnVerdict{{Hits: []rolls.Hit{{Name: "Firefly", Liked: true}}}},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly (liked)",
			},
		},
		"hit with one liked name and one name without the mark": {
			mode: rolls.ModePvP,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "Hand Cannon"},
				Overall: "C",
				Columns: []rolls.ColumnVerdict{
					{Hits: []rolls.Hit{{Name: "Explosive Payload"}, {Name: "Firefly", Liked: true}}},
				},
			}},
			want: []string{
				"- PvP: C for this roll, C on the PvP sheet (in Hand Cannon), 1 of 1 perk columns hit",
				"  Column 1: hit with Explosive Payload, Firefly (liked)",
			},
		},
		"hit with picks names the picks": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "HCs"},
				Overall: "C",
				Columns: []rolls.ColumnVerdict{
					{Hits: []rolls.Hit{{Name: "Explosive Payload"}}, Picks: []string{"Explosive Payload"}},
				},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
			},
		},
		"miss with picks names the picks": {
			mode: rolls.ModePvP,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "Hand Cannon"},
				Overall: "D",
				Columns: []rolls.ColumnVerdict{{Picks: []string{"Kill Clip"}}},
			}},
			want: []string{
				"- PvP: D for this roll, C on the PvP sheet (in Hand Cannon), 0 of 1 perk columns hit",
				"  Column 1: miss. The sheet picks Kill Clip",
			},
		},
		"liked hit carries the mark before the picks": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "HCs"},
				Overall: "C",
				Columns: []rolls.ColumnVerdict{{
					Hits:  []rolls.Hit{{Name: "Explosive Payload"}, {Name: "Firefly", Liked: true}},
					Picks: []string{"Explosive Payload", "Rampage"},
				}},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Explosive Payload, Firefly (liked). The sheet picks Explosive Payload, Rampage",
			},
		},
		"hit without picks beside a hit with picks has no second sentence": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "HCs"},
				Overall: "C",
				Columns: []rolls.ColumnVerdict{
					{Hits: []rolls.Hit{{Name: "Firefly", Liked: true}}},
					{Hits: []rolls.Hit{{Name: "Rampage"}}, Picks: []string{"Rampage"}},
				},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 2 of 2 perk columns hit",
				"  Column 1: hit with Firefly (liked)",
				"  Column 2: hit with Rampage. The sheet picks Rampage",
			},
		},
		"miss without picks beside a miss with picks has no second sentence": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "HCs"},
				Overall: "E",
				Columns: []rolls.ColumnVerdict{{}, {Picks: []string{"Kill Clip"}}},
			}},
			want: []string{
				"- PvE: E for this roll, C on the Aegis sheet (in HCs), 0 of 2 perk columns hit",
				"  Column 1: miss",
				"  Column 2: miss. The sheet picks Kill Clip",
			},
		},
		"two picks print with a comma and a space between them": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "HCs"},
				Overall: "D",
				Columns: []rolls.ColumnVerdict{{Picks: []string{"Explosive Payload", "Rampage"}}},
			}},
			want: []string{
				"- PvE: D for this roll, C on the Aegis sheet (in HCs), 0 of 1 perk columns hit",
				"  Column 1: miss. The sheet picks Explosive Payload, Rampage",
			},
		},
		"note follows a column line with picks": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "S", Category: "HCs", Notes: "Add clear and boss damage."},
				Overall: "A",
				Columns: []rolls.ColumnVerdict{{Picks: []string{"Kill Clip"}}},
			}},
			want: []string{
				"- PvE: A for this roll, S on the Aegis sheet (in HCs), 0 of 1 perk columns hit",
				"  Column 1: miss. The sheet picks Kill Clip",
				"  Sheet note: Add clear and boss damage.",
			},
		},
		// Rank 1 is the lowest rank that prints.
		"rated roll on a row with the rank 1": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "S", Category: "HCs", Rank: 1, Ranked: 62},
				Overall: "S",
				Columns: []rolls.ColumnVerdict{hit("Firefly")},
			}},
			want: []string{
				"- PvE: S for this roll, S on the Aegis sheet (rank 1 of 62 in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly",
			},
		},
		// The category size prints only beside a rank.
		"rated roll on a row without a rank": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "S", Category: "HCs", Ranked: 62},
				Overall: "S",
				Columns: []rolls.ColumnVerdict{hit("Firefly")},
			}},
			want: []string{
				"- PvE: S for this roll, S on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly",
			},
		},
		"note follows the last column line": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "S", Category: "HCs", Notes: "Add clear and boss damage."},
				Overall: "A",
				Columns: []rolls.ColumnVerdict{hit("Explosive Payload"), {}},
			}},
			want: []string{
				"- PvE: A for this roll, S on the Aegis sheet (in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload",
				"  Column 2: miss",
				"  Sheet note: Add clear and boss damage.",
			},
		},
		"note with blank lines and spaces around a line prints its two lines": {
			mode: rolls.ModePvP,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "Hand Cannon", Notes: note},
				Overall: "C",
				Columns: []rolls.ColumnVerdict{hit("Firefly")},
			}},
			want: []string{
				"- PvP: C for this roll, C on the PvP sheet (in Hand Cannon), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly",
				"  Sheet note: Add clear and boss damage.",
				"  Pairs with Firefly.",
			},
		},
		"note follows a line that judges no perk columns": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "A", Category: "Exotic Weapons", Notes: "Boss damage."},
				Overall: "A",
			}},
			want: []string{
				"- PvE: A on the Aegis sheet (in Exotic Weapons), which judges no perk columns",
				"  Sheet note: Boss damage.",
			},
		},
		"note follows a roll unknown line": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "S", Category: "HCs", Notes: note},
				Unknown: true,
			}},
			want: []string{
				"- PvE: S on the Aegis sheet (in HCs), roll unknown: the profile lists no perks for this copy",
				"  Sheet note: Add clear and boss damage.",
				"  Pairs with Firefly.",
			},
		},
		"note follows the column lines of a row without a tier": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Category: "HCs", Notes: "Add clear and boss damage."},
				Columns: []rolls.ColumnVerdict{{}},
			}},
			want: []string{
				"- PvE: no tier on the Aegis sheet (in HCs), 0 of 1 perk columns hit",
				"  Column 1: miss",
				"  Sheet note: Add clear and boss damage.",
			},
		},
		"row without a note prints no note line": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "B", Category: "HCs", Rank: 3, Ranked: 62},
				Overall: "C",
				Columns: []rolls.ColumnVerdict{hit("Firefly"), {}},
			}},
			want: []string{
				"- PvE: C for this roll, B on the Aegis sheet (rank 3 of 62 in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Firefly",
				"  Column 2: miss",
			},
		},
		"note of blank lines and spaces prints no line": {
			mode: rolls.ModePvE,
			rating: rolls.Rating{Roll: &rolls.Roll{
				Row:     rolls.SheetRow{Tier: "C", Category: "HCs", Notes: "\n  \n\t\n\n"},
				Overall: "C",
				Columns: []rolls.ColumnVerdict{hit("Firefly")},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var b strings.Builder

			writeRating(&b, tc.mode, tc.rating)

			assert.Equal(t, strings.Join(tc.want, "\n")+"\n", b.String())
		})
	}
}

func TestRatingLabel(t *testing.T) {
	t.Parallel()

	hit := rolls.ColumnVerdict{Hits: []rolls.Hit{{Name: "Firefly"}}}

	tests := map[string]struct {
		want   string
		rating rolls.Rating
	}{
		"game mode without ratings": {
			rating: rolls.Rating{Err: fmt.Errorf("%w: the service publishes no Aegis sheet", rolls.ErrNoRating)},
			want:   "unavailable",
		},
		"weapon that the sheet does not list": {
			want: "not listed",
		},
		"roll with a missed column on a row without a tier": {
			rating: rolls.Rating{Roll: &rolls.Roll{Columns: []rolls.ColumnVerdict{hit, {}}}},
			want:   "no tier",
		},
		"unknown roll on a row without a tier": {
			rating: rolls.Rating{Roll: &rolls.Roll{Unknown: true}},
			want:   "no tier",
		},
		"unknown roll on a row with a tier": {
			rating: rolls.Rating{Roll: &rolls.Roll{Row: rolls.SheetRow{Tier: "S"}, Unknown: true}},
			want:   "roll unknown (sheet S)",
		},
		"roll that keeps the tier of the row": {
			rating: rolls.Rating{
				Roll: &rolls.Roll{Row: rolls.SheetRow{Tier: "S"}, Overall: "S", Columns: []rolls.ColumnVerdict{hit}},
			},
			want: "S",
		},
		"roll that lost a tier": {
			rating: rolls.Rating{
				Roll: &rolls.Roll{
					Row:     rolls.SheetRow{Tier: "S"},
					Overall: "A",
					Columns: []rolls.ColumnVerdict{hit, {}},
				},
			},
			want: "A (sheet S)",
		},
		"roll that lost the last tier": {
			rating: rolls.Rating{
				Roll: &rolls.Roll{Row: rolls.SheetRow{Tier: "E"}, Overall: "F", Columns: []rolls.ColumnVerdict{{}, {}}},
			},
			want: "F (sheet E)",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, ratingLabel(tc.rating))
		})
	}
}

func TestRatingCell(t *testing.T) {
	t.Parallel()

	// The sheet lists item 7 with the tier A and without perk columns.
	listed := rolls.RatingSource{Sheet: &rolls.Sheet{
		Rows:    map[uint32]rolls.SheetRow{7: {Tier: "A"}},
		Columns: map[uint32][][]uint32{},
	}}
	noSheet := rolls.RatingSource{Err: fmt.Errorf("%w: the service publishes no Aegis sheet", rolls.ErrNoRating)}

	tests := map[string]struct {
		item   *inventory.Item
		want   string
		source rolls.RatingSource
	}{
		"armor that the sheet lists with a tier": {
			source: listed,
			item:   &inventory.Item{Hash: 7, Kind: inventory.KindArmor},
			want:   "",
		},
		"armor in a game mode without ratings": {
			source: noSheet,
			item:   &inventory.Item{Hash: 7, Kind: inventory.KindArmor},
			want:   "",
		},
		"weapon that the sheet lists with a tier": {
			source: listed,
			item:   &inventory.Item{Hash: 7, Kind: inventory.KindWeapon},
			want:   "A",
		},
		"weapon in a game mode without ratings": {
			source: noSheet,
			item:   &inventory.Item{Hash: 7, Kind: inventory.KindWeapon},
			want:   "unavailable",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, ratingCell(tc.source, tc.item))
		})
	}
}

func TestRatingReasons(t *testing.T) {
	t.Parallel()

	var (
		loaded  = rolls.RatingSource{Sheet: &rolls.Sheet{}}
		noAegis = rolls.RatingSource{Err: fmt.Errorf("%w: the service publishes no Aegis sheet", rolls.ErrNoRating)}
		noPvP   = rolls.RatingSource{Err: fmt.Errorf("%w: the service publishes no PvP sheet", rolls.ErrNoRating)}
		noPrefs = rolls.RatingSource{
			Err: fmt.Errorf("%w: reading perk preferences: %w", rolls.ErrNoRating, dvm.ErrService),
		}
	)

	tests := map[string]struct {
		sources map[string]rolls.RatingSource
		want    []string
	}{
		"every game mode has ratings": {
			sources: map[string]rolls.RatingSource{rolls.ModePvE: loaded, rolls.ModePvP: loaded},
		},
		"only PvE has no ratings": {
			sources: map[string]rolls.RatingSource{rolls.ModePvE: noAegis, rolls.ModePvP: loaded},
			want:    []string{"PvE: rating unavailable: the service publishes no Aegis sheet"},
		},
		"only PvP has no ratings": {
			sources: map[string]rolls.RatingSource{rolls.ModePvE: loaded, rolls.ModePvP: noPvP},
			want:    []string{"PvP: rating unavailable: the service publishes no PvP sheet"},
		},
		"PvE line comes first": {
			sources: map[string]rolls.RatingSource{rolls.ModePvP: noPvP, rolls.ModePvE: noAegis},
			want: []string{
				"PvE: rating unavailable: the service publishes no Aegis sheet",
				"PvP: rating unavailable: the service publishes no PvP sheet",
			},
		},
		// The loop compares each line of every case with the line of
		// get_item. This case adds a reason that wraps a cause.
		"line of a mode is the get_item line without the list marker": {
			sources: map[string]rolls.RatingSource{rolls.ModePvE: noPrefs, rolls.ModePvP: noPvP},
			want: []string{
				"PvE: rating unavailable: reading perk preferences: service",
				"PvP: rating unavailable: the service publishes no PvP sheet",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := ratingReasons(tc.sources)
			assert.Equal(t, tc.want, got)

			// get_item prints the reason of each mode as a list item.
			var b strings.Builder

			for _, mode := range rolls.Modes {
				err := tc.sources[mode].Err
				if err != nil {
					writeRating(&b, mode, rolls.Rating{Err: err})
				}
			}

			var lines strings.Builder
			for _, line := range got {
				lines.WriteString("- " + line + "\n")
			}

			assert.Equal(t, b.String(), lines.String())
		})
	}
}

func TestCompareDuplicates(t *testing.T) {
	t.Parallel()

	const (
		// Each sheet lists Fatebringer (100) with one perk column. The
		// Aegis sheet gives the weapon the tier S, and the PvP sheet
		// gives it the tier C.
		//
		// Instance 1001 holds Explosive Payload (900) with Rampage (903)
		// beside it in one socket, so it hits the column of the Aegis
		// sheet. The fake perk preferences dislike Rampage for PvP, so
		// the copy misses the column of the PvP sheet. The profile lists
		// no perks for instances 1005 and 1006.
		aegisSheet = `{"rolls": [{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900]]}]}`
		pvpSheet   = `{"rolls": [{"category": "Hand Cannon", "tier": "C", "hashes": [100], "slots": [[903]]}]}`

		noAegis = "PvE: rating unavailable: the service publishes no Aegis sheet"
		noPvP   = "PvP: rating unavailable: the service publishes no PvP sheet"

		header = "instance id | name | type | power | location | tag | flags | pve rating | pvp rating | " +
			"reload | ready | stow | aim | hip falloff | aim falloff | perks"
		noRating = "unavailable | unavailable"

		// The lines after the title of a group. No fixture weapon holds a
		// frame, so each copy leaves its six measure cells empty, as
		// noCells shows. A copy with perks gives the reason noFrame, and a
		// copy for which the profile lists no perks gives noPerks.
		noFrame = "Times and falloff: unknown. The community data has no curves for the frame of this weapon."
		noPerks = "Times and falloff: unknown. " +
			"The profile lists no perks for this copy, so the server cannot tell its frame."
		noCells = " |  |  |  |  |  |  | "

		// The rating cells of a copy without perks.
		unknownBoth  = "roll unknown (sheet S) | roll unknown (sheet C)"
		unknownAegis = "roll unknown (sheet S) | unavailable"
	)

	// aegisOnly makes the service publish only the Aegis sheet.
	aegisOnly := func(f *fakes) {
		f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
		f.service.Data["/artifacts/rolls.a.json"] = aegisSheet
	}

	// bothSheets makes the service publish the Aegis sheet and the PvP
	// sheet.
	bothSheets := func(f *fakes) {
		aegisOnly(f)

		f.service.ArtifactFiles["pvprolls"] = []string{"pvprolls.a.json"}
		f.service.Data["/artifacts/pvprolls.a.json"] = pvpSheet
	}

	// fatebringer returns the table of the Fatebringer copies in the
	// fixture profile. Cells holds the two rating cells of each copy, in
	// row order.
	fatebringer := func(cells ...string) []string {
		return []string{
			"Fatebringer: 3 copies",
			noFrame,
			noPerks,
			"",
			header,
			"1001 | Fatebringer | Kinetic Hand Cannon | 2010 | Vault | keep | favorite, locked, masterwork, note | " +
				cells[0] + noCells + "Explosive Payload (can swap to Rampage); Firefly",
			"1005 | Fatebringer | Kinetic Hand Cannon | 1970 | Hunter (postmaster) |  |  | " + cells[1] + noCells,
			"1006 | Fatebringer | Kinetic Hand Cannon | 1980 | Hunter (equipped) |  |  | " + cells[2] + noCells,
		}
	}

	// unrated is the table without a sheet, and rated is the table with
	// the two sheets. ratedAegis is the table with only the Aegis sheet.
	unrated := fatebringer(noRating, noRating, noRating)
	rated := fatebringer("S | D (sheet C)", unknownBoth, unknownBoth)
	ratedAegis := fatebringer("S | unavailable", unknownAegis, unknownAegis)

	// oneGroup is the result of a call that selects Fatebringer without
	// a sheet.
	oneGroup := strings.Join(slices.Concat([]string{noAegis, noPvP, ""}, unrated), "\n")

	// everyDuplicate is the result of a call that selects no weapon,
	// without a sheet.
	everyDuplicate := strings.Join(slices.Concat([]string{
		"Weapons 1-1 of 1 that have more than one copy.",
		noAegis,
		noPvP,
		"",
	}, unrated), "\n")

	// oneCopy is the result of a call that selects Midnight Coup without
	// a sheet.
	oneCopy := strings.Join([]string{
		noAegis,
		noPvP,
		"",
		"Midnight Coup: 1 copy",
		noPerks,
		"",
		header,
		"1003 | Midnight Coup | Kinetic Hand Cannon | 2005 | Hunter |  |  | " + noRating + noCells,
	}, "\n")

	tests := map[string]struct {
		// prepare changes the fakes before the call.
		prepare func(*fakes)
		// edit changes the cached snapshot before the call.
		edit func(*inventory.Snapshot)
		// want is the output. "{service}" stands for the base URL of the
		// service.
		want string
		in   CompareDuplicatesInput
		// noDefs makes the definition read of the ratings not succeed.
		// The case runs compare_duplicates one time to load the snapshot
		// and then drops the loaded definitions. The call under test
		// reuses the snapshot, and its definition read asks the service,
		// whose index lists no core artifact.
		noDefs  bool
		isError bool
	}{
		"name": {
			in:   CompareDuplicatesInput{Name: "fatebringer"},
			want: oneGroup,
		},
		"instance id": {
			in:   CompareDuplicatesInput{InstanceID: "1006"},
			want: oneGroup,
		},
		"one copy": {
			in:   CompareDuplicatesInput{Name: "coup"},
			want: oneCopy,
		},
		"instance id of a weapon whose name differs only in case": {
			edit: func(s *inventory.Snapshot) {
				coup, _ := s.Find("1003")
				twin := *coup
				twin.InstanceID = "1007"
				twin.Name = "MIDNIGHT COUP"
				s.Add(&twin)
			},
			in:   CompareDuplicatesInput{InstanceID: "1003"},
			want: oneCopy,
		},
		"every weapon with more than one copy": {
			want: everyDuplicate,
		},
		"blank name": {
			in:   CompareDuplicatesInput{Name: "   "},
			want: everyDuplicate,
		},
		"no weapon with more than one copy": {
			edit: func(s *inventory.Snapshot) {
				s.Items = slices.DeleteFunc(s.Items, func(item *inventory.Item) bool {
					return item.InstanceID == "1005" || item.InstanceID == "1006"
				})
			},
			want: "No weapon has more than one copy.",
		},
		"offset past the end": {
			in:   CompareDuplicatesInput{Offset: 5},
			want: "Offset 1 is past the last of 1 weapons that have more than one copy.",
		},
		"name and instance id": {
			in:      CompareDuplicatesInput{Name: "fatebringer", InstanceID: "1001"},
			want:    "invalid input: pass name or instance_id, not both",
			isError: true,
		},
		"armor": {
			in:      CompareDuplicatesInput{InstanceID: "1004"},
			want:    "invalid input: 1004 (Celestial Nighthawk) is not a weapon",
			isError: true,
		},
		"unknown instance": {
			in:      CompareDuplicatesInput{InstanceID: "9"},
			want:    "invalid input: no item in the inventory has the instance id 9",
			isError: true,
		},
		"unknown name": {
			in:      CompareDuplicatesInput{Name: "hand"},
			want:    `unknown weapon: no weapon in the inventory has a name that contains "hand"`,
			isError: true,
		},
		// A result whose game modes all have ratings starts with the
		// title of the group.
		"name with both sheets": {
			prepare: bothSheets,
			in:      CompareDuplicatesInput{Name: "fatebringer"},
			want:    strings.Join(rated, "\n"),
		},
		"instance id with both sheets": {
			prepare: bothSheets,
			in:      CompareDuplicatesInput{InstanceID: "1006"},
			want:    strings.Join(rated, "\n"),
		},
		"every weapon with more than one copy, with both sheets": {
			prepare: bothSheets,
			want: strings.Join(slices.Concat([]string{
				"Weapons 1-1 of 1 that have more than one copy.",
				"",
			}, rated), "\n"),
		},
		"name with only the Aegis sheet": {
			prepare: aegisOnly,
			in:      CompareDuplicatesInput{Name: "fatebringer"},
			want:    strings.Join(slices.Concat([]string{noPvP, ""}, ratedAegis), "\n"),
		},
		"every weapon with more than one copy, with only the Aegis sheet": {
			prepare: aegisOnly,
			want: strings.Join(slices.Concat([]string{
				"Weapons 1-1 of 1 that have more than one copy.",
				noPvP,
				"",
			}, ratedAegis), "\n"),
		},
		// The group holds the Adept copy under the name of the base
		// weapon. Each sheet lists only the item hash of the base weapon,
		// so the Adept copy does not take the rating of that row. The
		// service holds no tag and no note for the new instance.
		"Adept copy beside copies of the base weapon": {
			prepare: bothSheets,
			edit: func(s *inventory.Snapshot) {
				base, _ := s.Find("1001")
				adept := *base
				adept.InstanceID = "1007"
				adept.Name = "Fatebringer (Adept)"
				adept.Hash = 110
				s.Add(&adept)
			},
			in: CompareDuplicatesInput{Name: "fatebringer"},
			want: strings.Join(slices.Concat(
				[]string{"Fatebringer: 4 copies", noFrame, noPerks, "", header},
				// The rows of the three copies of the base weapon.
				rated[5:],
				[]string{
					"1007 | Fatebringer (Adept) | Kinetic Hand Cannon | 2010 | Vault |  | locked, masterwork | " +
						"not listed | not listed" + noCells + "Explosive Payload (can swap to Rampage); Firefly",
				},
			), "\n"),
		},
		// The Aegis sheet gains a second column, which picks Kill Clip
		// (904). Instance 1001 rolled no pick of that column, so it
		// reads "A (sheet S)" without the plug sets. The plug set of
		// the socket with the liked Firefly lists Kill Clip, so the
		// copy hits the column for PvE.
		"liked perk raises the rating of a copy that rolled no pick of a column": {
			prepare: func(f *fakes) {
				bothSheets(f)
				killClipPlugSet(f.defs)

				f.service.Data["/artifacts/rolls.a.json"] = `{"rolls": [` +
					`{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900], [904]]}]}`
			},
			in:   CompareDuplicatesInput{Name: "fatebringer"},
			want: strings.Join(rated, "\n"),
		},
		// The index lists a file that the service does not serve. A mode
		// whose sheet did not load never reads "not listed".
		"name with a sheet that does not load": {
			prepare: func(f *fakes) {
				f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.x.json"}}
			},
			in: CompareDuplicatesInput{Name: "fatebringer"},
			want: strings.Join(slices.Concat([]string{
				"PvE: rating unavailable: fetching artifact rolls.x.json: " +
					"service: {service}/artifacts/rolls.x.json returned status 404",
				noPvP,
				"",
			}, unrated), "\n"),
		},
		// The mode with a sheet reports the perk preferences, and the
		// mode without a sheet keeps the reason of the sheet. The call
		// still returns the table.
		"name without the perk preferences": {
			prepare: func(f *fakes) {
				aegisOnly(f)
				delete(f.service.Data, pathPrefs)
			},
			in: CompareDuplicatesInput{Name: "fatebringer"},
			want: strings.Join(slices.Concat([]string{
				"PvE: rating unavailable: reading perk preferences: " +
					"service: GET /api/perk-preferences?since=0 returned status 404: Not found",
				noPvP,
				"",
			}, unrated), "\n"),
		},
		// The mode with a sheet reports the definitions, and the mode
		// without a sheet keeps the reason of the sheet. The call still
		// returns the table.
		"name without the definitions": {
			prepare: aegisOnly,
			in:      CompareDuplicatesInput{Name: "fatebringer"},
			want: strings.Join(slices.Concat([]string{
				"PvE: rating unavailable: loading definitions: " +
					"definitions: the service index lists no core artifact",
				noPvP,
				"",
			}, unrated), "\n"),
			noDefs: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			if tc.prepare != nil {
				tc.prepare(f)
			}

			if tc.edit != nil {
				s, err := f.h.loadSnapshot(t.Context(), false)
				require.NoError(t, err)
				tc.edit(s)
			}

			if tc.noDefs {
				_, isError := call(t, f.h.handleCompareDuplicates, CompareDuplicatesInput{})
				require.False(t, isError)

				f.dropDefs()
			}

			got, isError := call(t, f.h.handleCompareDuplicates, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, strings.ReplaceAll(tc.want, "{service}", f.h.service.BaseURL()), got)
		})
	}
}

// twoDuplicates returns fakes whose cached snapshot holds two weapons
// with more than one copy. The fixture profile holds the copies of
// Fatebringer, and instance 1007 is a second Midnight Coup in the vault.
func twoDuplicates(t *testing.T) *fakes {
	t.Helper()

	f := newFakes(t)

	s, err := f.h.loadSnapshot(t.Context(), false)
	require.NoError(t, err)

	coup, ok := s.Find("1003")
	require.True(t, ok)

	twin := *coup
	twin.InstanceID = "1007"
	twin.Owner = inventory.OwnerVault
	s.Add(&twin)

	return f
}

func TestCompareDuplicatesPaging(t *testing.T) {
	t.Parallel()

	f := twoDuplicates(t)

	got, isError := call(t, f.h.handleCompareDuplicates, CompareDuplicatesInput{Limit: 1, Offset: 1})
	assert.False(t, isError)
	assert.Equal(t, strings.Join([]string{
		"Weapons 2-2 of 2 that have more than one copy.",
		"PvE: rating unavailable: the service publishes no Aegis sheet",
		"PvP: rating unavailable: the service publishes no PvP sheet",
		"",
		"Midnight Coup: 2 copies",
		"Times and falloff: unknown. The profile lists no perks for this copy, so the server cannot tell its frame.",
		"",
		"instance id | name | type | power | location | tag | flags | pve rating | pvp rating | " +
			"reload | ready | stow | aim | hip falloff | aim falloff | perks",
		"1003 | Midnight Coup | Kinetic Hand Cannon | 2005 | Hunter |  |  | unavailable | unavailable |  |  |  |  |  |  | ",
		"1007 | Midnight Coup | Kinetic Hand Cannon | 2005 | Vault |  |  | unavailable | unavailable |  |  |  |  |  |  | ",
	}, "\n"), got)

	got, isError = call(t, f.h.handleCompareDuplicates, CompareDuplicatesInput{Limit: 1})
	assert.False(t, isError)
	assert.True(t, strings.HasPrefix(got, strings.Join([]string{
		"Weapons 1-1 of 2 that have more than one copy. Pass offset=1 for the next page.",
		"PvE: rating unavailable: the service publishes no Aegis sheet",
		"PvP: rating unavailable: the service publishes no PvP sheet",
		"",
		"Fatebringer: 3 copies",
	}, "\n")))
}

// TestCompareDuplicatesPageRatings pins what the ratings cost on a page
// with more than one table. The page gives the reason of each game mode
// without ratings one time, and the call reads the perk preferences one
// time.
func TestCompareDuplicatesPageRatings(t *testing.T) {
	t.Parallel()

	f := twoDuplicates(t)

	got, isError := call(t, f.h.handleCompareDuplicates, CompareDuplicatesInput{})
	assert.False(t, isError)

	header, _, _ := strings.Cut(got, "\n")
	assert.Equal(t, "Weapons 1-2 of 2 that have more than one copy.", header)
	assert.Contains(t, got, "\n\nFatebringer: 3 copies\n")
	assert.Contains(t, got, "\n\nMidnight Coup: 2 copies\n")

	assert.Equal(t, 1, strings.Count(got, "PvE: rating unavailable"))
	assert.Equal(t, 1, strings.Count(got, "PvP: rating unavailable"))
	assert.Len(t, f.service.Calls(http.MethodGet, pathPrefs), 1)
}

func TestCompareDuplicatesLimit(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want  string
		limit int
	}{
		"default": {
			want: "Weapons 1-10 of 61 that have more than one copy. Pass offset=10 for the next page.",
		},
		"maximum": {
			limit: 1000,
			want:  "Weapons 1-50 of 61 that have more than one copy. Pass offset=50 for the next page.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			s, err := f.h.loadSnapshot(t.Context(), false)
			require.NoError(t, err)

			coup, ok := s.Find("1003")
			require.True(t, ok)

			// Sixty more weapons with two copies each join Fatebringer.
			for i := range 60 {
				for j := range 2 {
					twin := *coup
					twin.InstanceID = strconv.Itoa(2000 + 2*i + j)
					twin.Name = fmt.Sprintf("Replica %02d", i)
					s.Add(&twin)
				}
			}

			got, isError := call(t, f.h.handleCompareDuplicates, CompareDuplicatesInput{Limit: tc.limit})
			assert.False(t, isError)

			header, _, _ := strings.Cut(got, "\n")
			assert.Equal(t, tc.want, header)
		})
	}
}

func TestPaginate(t *testing.T) {
	t.Parallel()

	ten := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	tests := map[string]struct {
		items      []int
		want       []int
		limit      int
		offset     int
		wantOffset int
		wantLimit  int
	}{
		"zero limit takes the fallback": {
			items:     ten,
			want:      []int{0, 1, 2},
			wantLimit: 3,
		},
		"negative limit takes the fallback": {
			items:     ten,
			limit:     -1,
			want:      []int{0, 1, 2},
			wantLimit: 3,
		},
		"limit below the ceiling": {
			items:     ten,
			limit:     4,
			want:      []int{0, 1, 2, 3},
			wantLimit: 4,
		},
		"ceiling caps the limit": {
			items:     ten,
			limit:     1000,
			want:      []int{0, 1, 2, 3, 4},
			wantLimit: 5,
		},
		"offset inside the items": {
			items:      ten,
			limit:      2,
			offset:     4,
			want:       []int{4, 5},
			wantOffset: 4,
			wantLimit:  2,
		},
		"last page holds fewer items than the limit": {
			items:      ten,
			limit:      4,
			offset:     8,
			want:       []int{8, 9},
			wantOffset: 8,
			wantLimit:  4,
		},
		"negative offset starts at the first item": {
			items:     ten,
			limit:     2,
			offset:    -3,
			want:      []int{0, 1},
			wantLimit: 2,
		},
		"offset at the end": {
			items:      ten,
			limit:      2,
			offset:     10,
			want:       []int{},
			wantOffset: 10,
			wantLimit:  2,
		},
		"offset past the end": {
			items:      ten,
			limit:      2,
			offset:     25,
			want:       []int{},
			wantOffset: 10,
			wantLimit:  2,
		},
		"no items": {
			items:     []int{},
			want:      []int{},
			wantLimit: 3,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, offset, limit := paginate(tc.items, tc.limit, tc.offset, 3, 5)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantOffset, offset)
			assert.Equal(t, tc.wantLimit, limit)
		})
	}
}

func TestListActivityHistory(t *testing.T) {
	t.Parallel()

	const header = "started | character | activity | duration | result | kills | deaths | instance id"

	tests := map[string]struct {
		in      ListActivityHistoryInput
		data    map[string]string
		queries map[string][]string
		want    string
		isError bool
	}{
		"every character": {
			queries: map[string][]string{
				bungietest.PathHunterHistory: {"count=25&page=0"},
				bungietest.PathTitanHistory:  {"count=25&page=0"},
			},
			want: strings.Join([]string{
				"The 5 newest runs of the account.",
				"",
				header,
				"2026-11-11 21:00 UTC | Hunter | Vault of Glass: Master | 1h2m3s | completed | 84 | 2 | 9001",
				"2026-11-11 18:00 UTC | Titan | Control | 10m0s | incomplete | 12 | 9 | 9004",
				"2026-11-10 19:30 UTC | Hunter | Vanguard Ops | 21m30s | completed | 150 | 0 | 9002",
				"2026-11-09 08:00 UTC | Hunter | Activity 3333 | 45s | incomplete | 0 | 0 | 9003",
				"2026-11-01 12:00 UTC | Titan | Vanguard Ops | 30m0s | completed | 60 | 1 | 9005",
			}, "\n"),
		},
		"limit": {
			in: ListActivityHistoryInput{Limit: 2},
			queries: map[string][]string{
				bungietest.PathHunterHistory: {"count=2&page=0"},
				bungietest.PathTitanHistory:  {"count=2&page=0"},
			},
			want: strings.Join([]string{
				"The 2 newest runs of the account. Pass a larger limit, up to 250, for older runs.",
				"",
				header,
				"2026-11-11 21:00 UTC | Hunter | Vault of Glass: Master | 1h2m3s | completed | 84 | 2 | 9001",
				"2026-11-11 18:00 UTC | Titan | Control | 10m0s | incomplete | 12 | 9 | 9004",
			}, "\n"),
		},
		"class name": {
			in: ListActivityHistoryInput{Character: "TITAN", Limit: 1000},
			queries: map[string][]string{
				bungietest.PathTitanHistory: {"count=250&page=0"},
			},
			want: strings.Join([]string{
				"The 2 newest runs of Titan.",
				"",
				header,
				"2026-11-11 18:00 UTC | Titan | Control | 10m0s | incomplete | 12 | 9 | 9004",
				"2026-11-01 12:00 UTC | Titan | Vanguard Ops | 30m0s | completed | 60 | 1 | 9005",
			}, "\n"),
		},
		"one run": {
			in: ListActivityHistoryInput{Character: bungietest.HunterID, Limit: 1},
			queries: map[string][]string{
				bungietest.PathHunterHistory: {"count=1&page=0"},
			},
			want: strings.Join([]string{
				"The newest run of Hunter. Pass a larger limit, up to 250, for older runs.",
				"",
				header,
				"2026-11-11 21:00 UTC | Hunter | Vault of Glass: Master | 1h2m3s | completed | 84 | 2 | 9001",
			}, "\n"),
		},
		"no runs": {
			data: map[string]string{
				bungietest.PathHunterHistory: `{"ErrorCode": 1, "Response": {}}`,
				bungietest.PathTitanHistory:  `{"ErrorCode": 1, "Response": {}}`,
			},
			queries: map[string][]string{
				bungietest.PathHunterHistory: {"count=25&page=0"},
				bungietest.PathTitanHistory:  {"count=25&page=0"},
			},
			want: "The activity history of the account holds no runs.",
		},
		"vault": {
			in:      ListActivityHistoryInput{Character: "vault"},
			want:    "unknown location: the vault is not a character, so pass a character id or a class name",
			isError: true,
		},
		"unknown character": {
			in:      ListActivityHistoryInput{Character: "tower"},
			want:    `unknown location: "tower" is not a character id or a class name`,
			isError: true,
		},
		"Bungie refuses one character": {
			data: map[string]string{
				bungietest.PathTitanHistory: `{"ErrorCode": 1665, "ErrorStatus": "DestinyPrivacyRestriction", "Message": "Private."}`,
			},
			queries: map[string][]string{
				bungietest.PathHunterHistory: {"count=25&page=0"},
				bungietest.PathTitanHistory:  {"count=25&page=0"},
			},
			want:    "fetching the activity history: character c2: bungie: DestinyPrivacyRestriction: Private.",
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			maps.Copy(f.bungie.Data, tc.data)

			got, isError := call(t, f.h.handleListActivityHistory, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)

			for _, path := range []string{bungietest.PathHunterHistory, bungietest.PathTitanHistory} {
				var queries []string
				for _, req := range f.bungie.Calls(http.MethodGet, path) {
					queries = append(queries, req.Query)
				}

				assert.Equal(t, tc.queries[path], queries, path)
			}
		})
	}
}

func TestActivityName(t *testing.T) {
	t.Parallel()

	defs := definitionstest.Defs()

	tests := map[string]struct {
		activity bungie.HistoryActivity
		want     string
	}{
		"reference id": {
			activity: bungie.HistoryActivity{ReferenceID: 3022541210},
			want:     "Vault of Glass: Master",
		},
		"reference id first": {
			activity: bungie.HistoryActivity{ReferenceID: 4444, DirectorActivityHash: 5555},
			want:     "Control",
		},
		"director hash": {
			activity: bungie.HistoryActivity{ReferenceID: 1111, DirectorActivityHash: 2222},
			want:     "Vanguard Ops",
		},
		"definition without a name": {
			activity: bungie.HistoryActivity{ReferenceID: 3334, DirectorActivityHash: 2222},
			want:     "Vanguard Ops",
		},
		"no definition": {
			activity: bungie.HistoryActivity{ReferenceID: 3333, DirectorActivityHash: 3334},
			want:     "Activity 3333",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, activityName(defs, tc.activity))
		})
	}
}

func TestListVendors(t *testing.T) {
	t.Parallel()

	banshee := strings.Join([]string{
		"Banshee-44",
		"Subtitle: Gunsmith",
		"Vendor hash: 672118013",
		"Character: Hunter",
		"Next refresh: 2026-11-13 17:00 UTC",
		"Sales: 8",
		"",
		"Weapons:",
		"- Midnight Coup | 1000 Glimmer | unavailable: Requires Gunsmith rank 10.; Already Purchased on Account",
		"- Fatebringer | 50000 Glimmer, 10 Legendary Shards | available",
		"",
		"Materials:",
		"- Ammo Synthesis x5 | 2 Legendary Shards | available",
		"- Enhancement Core | no cost | available",
		"",
		"Category 2:",
		"- Ammo Synthesis | 500 Glimmer | available",
		"",
		"Uncategorized:",
		"- Fatebringer | 5 Glimmer | available",
		"- Enhancement Core x3 | 4 Item 77 | unavailable",
		"- Legendary Shards x2 | 100 Glimmer | available",
	}, "\n")

	const (
		header     = "name | subtitle | hash | sales | next refresh | flags"
		bansheeRow = "Banshee-44 | Gunsmith | 672118013 | 8 | 2026-11-13 17:00 UTC | "
		tessRow    = "Tess Everis | Eververse Trading Company | 3361454721 | 1 | 2026-11-17 17:00 UTC | "
		hint       = " Pass vendor with a hash or a name to list the sales of one vendor."
	)

	grouped := strings.Join([]string{
		"The 2 vendors of Hunter that Bungie groups for its Companion app and that have a sale. " +
			"Pass all=true to list all 6 vendors that Bungie returns." + hint,
		"",
		header,
		bansheeRow,
		tessRow,
	}, "\n")

	all := strings.Join([]string{
		"The 6 vendors of Hunter." + hint,
		"",
		header,
		bansheeRow,
		tessRow,
		"Vendor 1111 |  | 1111 | 0 | 2026-11-13 17:00 UTC | ",
		"Vendor 2168194999 |  | 2168194999 | 1 | unknown | ",
		"Xûr | Agent of the Nine | 2190858386 | 0 | 2026-11-14 17:00 UTC | disabled",
		"Xûr | Strange Gear Offers | 3751514131 | 0 | never | ",
	}, "\n")

	tests := map[string]struct {
		in      ListVendorsInput
		data    map[string]string
		paths   []string
		want    string
		isError bool
	}{
		"vendor list": {
			paths: []string{bungietest.PathHunterVendors},
			want:  grouped,
		},
		"blank vendor": {
			in:    ListVendorsInput{Vendor: "  "},
			paths: []string{bungietest.PathHunterVendors},
			want:  grouped,
		},
		"every vendor": {
			in:    ListVendorsInput{All: true},
			paths: []string{bungietest.PathHunterVendors},
			want:  all,
		},
		"every vendor with a vendor": {
			in:    ListVendorsInput{Vendor: "Banshee-44", All: true},
			paths: []string{bungietest.PathHunterVendors},
			want:  banshee,
		},
		"no grouped vendor with a sale": {
			data: map[string]string{bungietest.PathHunterVendors: `{"ErrorCode": 1, "Response": {
				"vendors": {"data": {
					"1111": {"vendorHash": 1111, "enabled": true, "nextRefreshDate": "2026-11-13T17:00:00Z"},
					"672118013": {"vendorHash": 672118013, "enabled": true, "nextRefreshDate": "2026-11-13T17:00:00Z"}
				}},
				"vendorGroups": {"data": {"groups": [{"vendorGroupHash": 3, "vendorHashes": [1111]}]}}
			}}`},
			paths: []string{bungietest.PathHunterVendors},
			want: "No vendor of Hunter that Bungie groups for its Companion app has a sale. " +
				"Pass all=true to list all 2 vendors that Bungie returns." + hint,
		},
		"one vendor outside the groups": {
			data: map[string]string{bungietest.PathHunterVendors: `{"ErrorCode": 1, "Response": {
				"vendors": {"data": {
					"1111": {"vendorHash": 1111, "enabled": true, "nextRefreshDate": "2026-11-13T17:00:00Z"}
				}}
			}}`},
			paths: []string{bungietest.PathHunterVendors},
			want: "No vendor of Hunter that Bungie groups for its Companion app has a sale. " +
				"Pass all=true to list the one vendor that Bungie returns." + hint,
		},
		"vendor by exact name": {
			in:    ListVendorsInput{Vendor: "Banshee-44"},
			paths: []string{bungietest.PathHunterVendors},
			want:  banshee,
		},
		"vendor by part of its name": {
			in:    ListVendorsInput{Vendor: " banshee "},
			paths: []string{bungietest.PathHunterVendors},
			want:  banshee,
		},
		"vendor by its name with digits for letters": {
			in:    ListVendorsInput{Vendor: "Ban5h33-44"},
			paths: []string{bungietest.PathHunterVendors},
			want:  banshee,
		},
		"vendor by hash": {
			in:    ListVendorsInput{Vendor: "3361454721"},
			paths: []string{bungietest.PathHunterVendors},
			want: strings.Join([]string{
				"Tess Everis",
				"Subtitle: Eververse Trading Company",
				"Vendor hash: 3361454721",
				"Character: Hunter",
				"Next refresh: 2026-11-17 17:00 UTC",
				"Sales: 1",
				"",
				"Featured:",
				"- Eververse Engram | 500 Bright Dust | available",
			}, "\n"),
		},
		"vendor without a definition": {
			in:    ListVendorsInput{Vendor: "2168194999"},
			paths: []string{bungietest.PathHunterVendors},
			want: strings.Join([]string{
				"Vendor 2168194999",
				"Vendor hash: 2168194999",
				"Character: Hunter",
				"Next refresh: unknown",
				"Sales: 1",
				"",
				"Category 0:",
				"- Ornament of the Hunt | 700 Bright Dust | available",
			}, "\n"),
		},
		"disabled vendor": {
			in:    ListVendorsInput{Vendor: "2190858386"},
			paths: []string{bungietest.PathHunterVendors},
			want: strings.Join([]string{
				"Xûr",
				"Subtitle: Agent of the Nine",
				"Vendor hash: 2190858386",
				"Character: Hunter",
				"Next refresh: 2026-11-14 17:00 UTC",
				"Flags: disabled",
				"Sales: 0",
			}, "\n"),
		},
		"vendors that share a name": {
			in:      ListVendorsInput{Vendor: "Xûr"},
			paths:   []string{bungietest.PathHunterVendors},
			want:    `unknown vendor: "Xûr" matches 2 vendors, so pass a vendor hash, such as Xûr (2190858386), Xûr (3751514131)`,
			isError: true,
		},
		"vendors that share a name without its accent": {
			in:      ListVendorsInput{Vendor: "xur"},
			paths:   []string{bungietest.PathHunterVendors},
			want:    `unknown vendor: "xur" matches 2 vendors, so pass a vendor hash, such as Xûr (2190858386), Xûr (3751514131)`,
			isError: true,
		},
		"ambiguous part of a name": {
			in:    ListVendorsInput{Vendor: "vendor"},
			paths: []string{bungietest.PathHunterVendors},
			want: `unknown vendor: "vendor" matches 2 vendors, so pass a vendor hash, ` +
				`such as Vendor 1111 (1111), Vendor 2168194999 (2168194999)`,
			isError: true,
		},
		"unknown vendor name": {
			in:      ListVendorsInput{Vendor: "Zavala"},
			paths:   []string{bungietest.PathHunterVendors},
			want:    `unknown vendor: no vendor of Hunter has a name that contains "Zavala"`,
			isError: true,
		},
		"unknown vendor hash": {
			in:      ListVendorsInput{Vendor: "42"},
			paths:   []string{bungietest.PathHunterVendors},
			want:    "unknown vendor: no vendor of Hunter has the hash 42",
			isError: true,
		},
		"character by class name": {
			in:    ListVendorsInput{Character: "TITAN"},
			paths: []string{bungietest.PathTitanVendors},
			want: strings.Join([]string{
				"The vendor of Titan that Bungie groups for its Companion app and that has a sale." + hint,
				"",
				header,
				"Banshee-44 | Gunsmith | 672118013 | 1 | 2026-11-13 17:00 UTC | ",
			}, "\n"),
		},
		"every vendor of a character": {
			in:    ListVendorsInput{Character: "titan", All: true},
			paths: []string{bungietest.PathTitanVendors},
			want: strings.Join([]string{
				"The vendor of Titan." + hint,
				"",
				header,
				"Banshee-44 | Gunsmith | 672118013 | 1 | 2026-11-13 17:00 UTC | ",
			}, "\n"),
		},
		"vendor of a character by id": {
			in:    ListVendorsInput{Character: bungietest.TitanID, Vendor: "banshee-44"},
			paths: []string{bungietest.PathTitanVendors},
			want: strings.Join([]string{
				"Banshee-44",
				"Subtitle: Gunsmith",
				"Vendor hash: 672118013",
				"Character: Titan",
				"Next refresh: 2026-11-13 17:00 UTC",
				"Sales: 1",
				"",
				"Weapons:",
				"- Midnight Coup | 1000 Glimmer | available",
			}, "\n"),
		},
		"no vendors": {
			data:  map[string]string{bungietest.PathHunterVendors: `{"ErrorCode": 1, "Response": {}}`},
			paths: []string{bungietest.PathHunterVendors},
			want:  "Bungie lists no vendors for Hunter.",
		},
		"every vendor without vendors": {
			in:    ListVendorsInput{All: true},
			data:  map[string]string{bungietest.PathHunterVendors: `{"ErrorCode": 1, "Response": {}}`},
			paths: []string{bungietest.PathHunterVendors},
			want:  "Bungie lists no vendors for Hunter.",
		},
		"vault": {
			in:      ListVendorsInput{Character: "vault"},
			want:    "unknown location: the vault is not a character, so pass a character id or a class name",
			isError: true,
		},
		"Bungie refuses": {
			data: map[string]string{
				bungietest.PathHunterVendors: `{"ErrorCode": 1627, "ErrorStatus": "DestinyVendorNotFound", "Message": "Not found."}`,
			},
			paths:   []string{bungietest.PathHunterVendors},
			want:    "fetching the vendors: bungie: DestinyVendorNotFound: Not found.",
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			maps.Copy(f.bungie.Data, tc.data)

			got, isError := call(t, f.h.handleListVendors, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)

			var paths []string

			for _, path := range []string{bungietest.PathHunterVendors, bungietest.PathTitanVendors} {
				for _, req := range f.bungie.Calls(http.MethodGet, path) {
					paths = append(paths, req.Path)
					assert.Regexp(t, `^components=400,401,402&_=\d+$`, req.Query)
				}
			}

			assert.Equal(t, tc.paths, paths)
		})
	}
}

func TestListVendorsRefetches(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	for range 2 {
		got, isError := call(t, f.h.handleListVendors, ListVendorsInput{})
		require.False(t, isError, got)
	}

	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathHunterVendors), 2)
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1)
}

func TestListActivities(t *testing.T) {
	t.Parallel()

	const (
		header = "activity | type | location | power | bonus drops | focus | bonus loot | flags | hash"
		vog    = "Vault of Glass: Master | Raid | Venus | 2010 | 3 | Deepsight Weapon | Raid Gear x2 |  | 3022541210"
		ruin   = "Warlord's Ruin | Dungeon | Earth |  | 1 |  |  |  | 4001"
		gm     = "Nightfall: Grandmaster | Strike |  | 2020 |  | Adept Weapon |  |  | 4003"
		ops    = "Vanguard Ops | Strike |  |  |  |  | Enhancement Prism x2 |  | 2222"
		coil   = "The Coil | Exotic Mission | Neptune |  |  |  |  | featured | 4002"
		oracle = "Battleground: Oracle | Strike |  | 1980 |  |  |  |  | 4009"
		ctrl   = "Control | The Crucible |  |  |  |  |  |  | 4444"
	)

	intro := func(first, last, total int, owner string) string {
		return fmt.Sprintf("Activities %d-%d of %d for %s, with the rewards that each one still offers this week.",
			first, last, total, owner)
	}

	hunter := strings.Join([]string{intro(1, 7, 7, "Hunter"), "", header, vog, ruin, gm, ops, coil, oracle, ctrl}, "\n")

	// The Hunter gets one named activity more than a page holds at most.
	manyEntries := make([]string, 0, maxItemLimit+1)
	manyPage := []string{intro(1, 200, 201, "Hunter") + " Pass offset=200 for the next page.", "", header}

	for i := range uint32(maxItemLimit + 1) {
		manyEntries = append(manyEntries, fmt.Sprintf(`{"activityHash": %d, "isVisible": true}`, 5000+i))

		if i < maxItemLimit {
			manyPage = append(manyPage, fmt.Sprintf("Activity %03d |  |  |  |  |  |  |  | %d", i, 5000+i))
		}
	}

	manyDefs := func(d *definitions.Defs) {
		for i := range uint32(maxItemLimit + 1) {
			d.Activities[5000+i] = definitions.ActivityDef{Name: fmt.Sprintf("Activity %03d", i)}
		}
	}

	tests := map[string]struct {
		in         ListActivitiesInput
		defs       func(*definitions.Defs)
		activities string
		want       string
		fetches    int
		isError    bool
	}{
		"most recently played character": {
			fetches: 1,
			want:    hunter,
		},
		"character id": {
			in:      ListActivitiesInput{Character: bungietest.HunterID},
			fetches: 1,
			want:    hunter,
		},
		"class name": {
			in:      ListActivitiesInput{Character: "TITAN"},
			fetches: 1,
			want: strings.Join([]string{
				intro(1, 1, 1, "Titan"),
				"",
				header,
				"Control | The Crucible |  | 1990 |  |  |  |  | 4444",
			}, "\n"),
		},
		"rewards": {
			in:      ListActivitiesInput{Rewards: true},
			fetches: 1,
			want:    strings.Join([]string{intro(1, 4, 4, "Hunter"), "", header, vog, ruin, gm, ops}, "\n"),
		},
		"query matches a bonus loot name": {
			in:      ListActivitiesInput{Query: " RAID gear "},
			fetches: 1,
			want:    strings.Join([]string{intro(1, 1, 1, "Hunter"), "", header, vog}, "\n"),
		},
		"query matches a focused reward": {
			in:      ListActivitiesInput{Query: "adept"},
			fetches: 1,
			want:    strings.Join([]string{intro(1, 1, 1, "Hunter"), "", header, gm}, "\n"),
		},
		"query matches a location": {
			in:      ListActivitiesInput{Query: "earth"},
			fetches: 1,
			want:    strings.Join([]string{intro(1, 1, 1, "Hunter"), "", header, ruin}, "\n"),
		},
		"query matches a type": {
			in:      ListActivitiesInput{Query: "strike"},
			fetches: 1,
			want:    strings.Join([]string{intro(1, 3, 3, "Hunter"), "", header, gm, ops, oracle}, "\n"),
		},
		"query matches a name": {
			in:      ListActivitiesInput{Query: "coil"},
			fetches: 1,
			want:    strings.Join([]string{intro(1, 1, 1, "Hunter"), "", header, coil}, "\n"),
		},
		"query matches a name with digits for letters": {
			in:      ListActivitiesInput{Query: "c01l"},
			fetches: 1,
			want:    strings.Join([]string{intro(1, 1, 1, "Hunter"), "", header, coil}, "\n"),
		},
		"query and rewards": {
			in:      ListActivitiesInput{Query: "strike", Rewards: true},
			fetches: 1,
			want:    strings.Join([]string{intro(1, 2, 2, "Hunter"), "", header, gm, ops}, "\n"),
		},
		"query matches nothing": {
			in:      ListActivitiesInput{Query: "gambit"},
			fetches: 1,
			want:    "No activities of Hunter match.",
		},
		"limit and offset": {
			in:      ListActivitiesInput{Limit: 2, Offset: 1},
			fetches: 1,
			want: strings.Join([]string{
				intro(2, 3, 7, "Hunter") + " Pass offset=3 for the next page.",
				"",
				header,
				ruin,
				gm,
			}, "\n"),
		},
		"last page": {
			in:      ListActivitiesInput{Limit: 1000, Offset: 5},
			fetches: 1,
			want:    strings.Join([]string{intro(6, 7, 7, "Hunter"), "", header, oracle, ctrl}, "\n"),
		},
		"limit above the maximum": {
			in:   ListActivitiesInput{Limit: 1000},
			defs: manyDefs,
			activities: `{"ErrorCode": 1, "Response": {"characterActivities": {"data": {"` + bungietest.HunterID +
				`": {"availableActivities": [` + strings.Join(manyEntries, ", ") + `]}}}}}`,
			fetches: 1,
			want:    strings.Join(manyPage, "\n"),
		},
		"offset past the last activity": {
			in:      ListActivitiesInput{Offset: 7},
			fetches: 1,
			want:    "Offset 7 is past the last of 7 matching activities.",
		},
		"no activities": {
			activities: `{"ErrorCode": 1, "Response": {}}`,
			fetches:    1,
			want:       "Bungie lists no activities that Hunter can play.",
		},
		"definitions without activities": {
			defs:    func(d *definitions.Defs) { d.Activities = map[uint32]definitions.ActivityDef{} },
			fetches: 1,
			want: "The list leaves out all 13 activities that Bungie lists for Hunter, because the game hides " +
				"them, the definitions do not name them, or they are Story or Social activities.",
		},
		"Bungie refuses": {
			activities: `{"ErrorCode": 1665, "ErrorStatus": "DestinyPrivacyRestriction", "Message": "Private."}`,
			fetches:    1,
			want:       "fetching the activities: bungie: DestinyPrivacyRestriction: Private.",
			isError:    true,
		},
		"vault": {
			in:      ListActivitiesInput{Character: "vault"},
			want:    "unknown location: the vault is not a character, so pass a character id or a class name",
			isError: true,
		},
		"unknown character": {
			in:      ListActivitiesInput{Character: "tower"},
			want:    `unknown location: "tower" is not a character id or a class name`,
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.bungie.ActivitiesBody = tc.activities

			if tc.defs != nil {
				tc.defs(f.defs)
			}

			got, isError := call(t, f.h.handleListActivities, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)

			// The first request fetches the inventory snapshot, and every
			// later one fetches only the activities.
			calls := f.bungie.Calls(http.MethodGet, bungietest.PathProfile)
			require.Len(t, calls, 1+tc.fetches)

			query, err := url.ParseQuery(calls[0].Query)
			require.NoError(t, err)
			assert.NotContains(t, strings.Split(query.Get("components"), ","), "204")

			for _, req := range calls[1:] {
				assert.Regexp(t, `^components=204&_=\d+$`, req.Query)
			}
		})
	}
}

func TestListActivitiesRefetches(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	for range 2 {
		got, isError := call(t, f.h.handleListActivities, ListActivitiesInput{})
		require.False(t, isError, got)
	}

	// The snapshot serves both calls, and each call fetches the activities.
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 3)
}

func TestRefreshOnUnauthorized(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.bungie.StaleAccess = "access-1"
	f.service.StaleSession = "session-1"

	got, isError := call(t, f.h.handleListItems, ListItemsInput{Location: "vault"})
	require.False(t, isError, got)
	assert.Contains(t, got, "Fatebringer")

	refreshes := f.service.Calls(http.MethodPost, "/api/auth/refresh")
	require.Len(t, refreshes, 1, "one refresh serves Bungie and the service")
	assert.Equal(t, map[string]any{"refreshToken": "refresh-1"}, refreshes[0].Body)

	saved, err := f.h.auth.Saved()
	require.NoError(t, err)
	assert.Equal(t, "access-2", saved.AccessToken)
	assert.Equal(t, "session-2", saved.Session)
}

func TestBungieValue(t *testing.T) {
	t.Parallel()

	errRead := errors.New("read broke")

	tests := map[string]struct {
		// fail maps the access token of a run to the error of that run.
		fail     map[string]error
		err      error
		want     string
		wantRuns []string
	}{
		"first run returns its value": {
			want:     "access-1",
			wantRuns: []string{"access-1"},
		},
		"rejected token runs again with the refreshed token": {
			fail:     map[string]error{"access-1": bungie.ErrUnauthorized},
			want:     "access-2",
			wantRuns: []string{"access-1", "access-2"},
		},
		"other error ends the read": {
			fail:     map[string]error{"access-1": errRead},
			err:      errRead,
			wantRuns: []string{"access-1"},
		},
		"second rejection ends the read": {
			fail:     map[string]error{"access-1": bungie.ErrUnauthorized, "access-2": bungie.ErrUnauthorized},
			err:      bungie.ErrUnauthorized,
			wantRuns: []string{"access-1", "access-2"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			var runs []string

			got, err := bungieValue(t.Context(), f.h, func(creds bungie.Credentials) (string, error) {
				runs = append(runs, creds.AccessToken)

				return creds.AccessToken, tc.fail[creds.AccessToken]
			})
			assert.Equal(t, tc.wantRuns, runs)

			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMoveItems(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleMoveItems, MoveItemsInput{
		InstanceIDs: []string{"1003", "1006"},
		Target:      "titan",
	})
	assert.False(t, isError, "one item moved")
	assert.Equal(t, "- Moved Midnight Coup (1003). It is on Titan.\n"+
		"- Fatebringer (1006) did not move: cannot move: Fatebringer is equipped on Hunter, "+
		"which holds no other item to equip in that slot. It is equipped on Hunter.", got)

	transfers := f.bungie.Calls(http.MethodPost, pathTransfer)
	require.Len(t, transfers, 2)
	assert.Equal(t, map[string]any{
		"itemReferenceHash": float64(102),
		"stackSize":         float64(1),
		"transferToVault":   true,
		"itemId":            "1003",
		"characterId":       bungietest.HunterID,
		"membershipType":    float64(3),
	}, transfers[0].Body)
	assert.Equal(t, false, transfers[1].Body["transferToVault"])
	assert.Equal(t, bungietest.TitanID, transfers[1].Body["characterId"])

	// The snapshot carries the move, so the next read does not ask Bungie
	// for a profile that might predate it.
	got, isError = call(t, f.h.handleListItems, ListItemsInput{Location: "titan"})
	assert.False(t, isError)
	assert.Contains(t, got, "1003 | Midnight Coup")
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1)
}

func TestMoveItemsEquip(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleMoveItems, MoveItemsInput{
		InstanceIDs: []string{"1001"},
		Target:      bungietest.HunterID,
		Equip:       true,
	})
	assert.False(t, isError)
	assert.Equal(t, "- Equipped Fatebringer (1001). It is equipped on Hunter.", got)
	assert.Len(t, f.bungie.Calls(http.MethodPost, pathTransfer), 1)
	assert.Len(t, f.bungie.Calls(http.MethodPost, pathEquip), 1)
}

func TestMoveItemsReplacesEquipped(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleMoveItems, MoveItemsInput{
		InstanceIDs: []string{"1006"},
		Target:      "vault",
	})
	assert.False(t, isError)
	assert.Equal(t,
		"- Moved Fatebringer (1006). It is in the vault. Equipped Midnight Coup (1003) in its place.", got)

	equips := f.bungie.Calls(http.MethodPost, pathEquip)
	require.Len(t, equips, 1)
	assert.Equal(t, map[string]any{
		"itemId":         "1003",
		"characterId":    bungietest.HunterID,
		"membershipType": float64(3),
	}, equips[0].Body)
	assert.Len(t, f.bungie.Calls(http.MethodPost, pathTransfer), 1)
}

func TestMoveStackTool(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleMoveStack, MoveStackInput{ItemHash: 300, Target: "Vault", Amount: 12})
	assert.False(t, isError)
	assert.Equal(t, "Moved 12 of Enhancement Core to the vault.", got)

	transfers := f.bungie.Calls(http.MethodPost, pathTransfer)
	require.Len(t, transfers, 1)
	assert.Equal(t, map[string]any{
		"itemReferenceHash": float64(300),
		"stackSize":         float64(12),
		"transferToVault":   true,
		"itemId":            "0",
		"characterId":       bungietest.HunterID,
		"membershipType":    float64(3),
	}, transfers[0].Body)

	got, isError = call(t, f.h.handleListItems, ListItemsInput{Kind: inventory.KindOther, Location: "vault"})
	assert.False(t, isError)
	assert.Contains(t, got, "stack of 12 (item hash 300) | Enhancement Core | Material | Legendary |  |  | Vault")

	tests := map[string]struct {
		want string
		in   MoveStackInput
	}{
		"unknown target": {
			in:   MoveStackInput{ItemHash: 300, Target: "hunter"},
			want: `invalid input: target "hunter" is not vault or inventory`,
		},
		"too many": {
			in:   MoveStackInput{ItemHash: 300, Target: "vault", Amount: 99},
			want: "cannot move: the inventory holds only 42 of Enhancement Core",
		},
		"nothing to move": {
			in:   MoveStackInput{ItemHash: 300, Target: "inventory"},
			want: "cannot move: the vault holds no stack with the item hash 300",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			got, isError := call(t, f.h.handleMoveStack, tc.in)
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)
			assert.Empty(t, f.bungie.Calls(http.MethodPost, pathTransfer))
		})
	}
}

func TestSetAutotagEnabled(t *testing.T) {
	t.Parallel()

	const on = "Autotagging is on. The rules apply to items acquired from now on. " +
		"The web client runs them while it is open, and run_autotag_rules runs them when a caller invokes it."

	tests := map[string]struct {
		wantBody map[string]any
		// profile is the body of the profile requests. When it is empty,
		// the requests get bungietest.ProfileJSON.
		profile string
		want    string
		enabled bool
	}{
		"on stores the newest instance id as the mark": {
			enabled:  true,
			wantBody: map[string]any{"kind": "enable", "mark": "00000000000000001006"},
			want:     on,
		},
		"an emote with the highest id": {
			enabled: true,
			profile: newItemsProfile(t, []string{
				`{"itemHash": 400, "itemInstanceId": "1010", "quantity": 1, "location": 1, "bucketHash": 1107761855}`,
			}, nil),
			wantBody: map[string]any{"kind": "enable", "mark": "00000000000000001006"},
			want:     on,
		},
		"off": {
			wantBody: map[string]any{"kind": "disable"},
			want:     "Autotagging is off.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.bungie.ProfileBody = tc.profile
			f.defs.Items[400] = definitions.ItemDef{
				Name:       "Victory Dance",
				TypeName:   "Emote",
				Bucket:     bungie.BucketEmotes,
				ClassType:  bungie.ClassAny,
				Equippable: true,
			}

			// A cached profile must not decide the mark.
			_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
			require.False(t, isError)

			got, isError := call(t, f.h.handleSetAutotagEnabled, SetAutotagEnabledInput{Enabled: tc.enabled})
			assert.False(t, isError)
			assert.Equal(t, tc.want, got)

			writes := f.service.Calls(http.MethodPost, "/api/autotag/state")
			require.Len(t, writes, 1)
			assert.Equal(t, tc.wantBody, writes[0].Body)

			wantProfiles := 1
			if tc.enabled {
				wantProfiles = 2
			}

			assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), wantProfiles)
		})
	}
}

func TestMoveItemsErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		bungieError string
		want        string
		in          MoveItemsInput
	}{
		"bungie refuses": {
			in: MoveItemsInput{InstanceIDs: []string{"1001"}, Target: "hunter"},
			bungieError: `{"ErrorCode": 1642, "ErrorStatus": "DestinyNoRoomInDestination",
				"Message": "There are no item slots available to transfer this item."}`,
			want: "- Fatebringer (1001) did not move: transferring to Hunter: bungie: DestinyNoRoomInDestination: " +
				"There are no item slots available to transfer this item. It is in the vault.",
		},
		"unknown instance rejects the batch": {
			in:   MoveItemsInput{InstanceIDs: []string{"1001", "9"}, Target: "vault"},
			want: "invalid input: no item in the inventory has the instance id 9",
		},
		"unknown target": {
			in:   MoveItemsInput{InstanceIDs: []string{"1001"}, Target: "warlock"},
			want: `unknown location: "warlock" is not "vault", a character id, or a class name`,
		},
		"no ids": {
			in:   MoveItemsInput{Target: "vault"},
			want: "invalid input: pass between 1 and 200 instance ids",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.bungie.ActionError = tc.bungieError

			got, isError := call(t, f.h.handleMoveItems, tc.in)
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLogMoveFailure(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		moveErr error
		item    *inventory.Item
		want    map[string]any
	}{
		"transfer refused": {
			item:    &inventory.Item{Name: "Fatebringer", InstanceID: "1001"},
			moveErr: fmt.Errorf("transferring to Hunter: %w", bungietest.ErrStep),
			want: map[string]any{
				"level":       "WARN",
				"msg":         "move failed",
				"instance_id": "1001",
				"error":       "transferring to Hunter: step refused",
			},
		},
		"pull refused": {
			item:    &inventory.Item{Name: "Thorn", InstanceID: "1008"},
			moveErr: fmt.Errorf("pulling from the Postmaster: %w", bungietest.ErrStep),
			want: map[string]any{
				"level":       "WARN",
				"msg":         "move failed",
				"instance_id": "1008",
				"error":       "pulling from the Postmaster: step refused",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			h := &handler{log: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
				ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
					if len(groups) == 0 && a.Key == slog.TimeKey {
						return slog.Attr{}
					}

					return a
				},
			}))}

			h.logMoveFailure(t.Context(), tc.item, tc.moveErr)

			var got map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMoveItemsSnapshotAge(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	require.False(t, isError)

	later := bungietest.Now.Add(10 * time.Second)
	f.h.now = func() time.Time { return later }
	f.bungie.ActionError = `{"ErrorCode": 1623, "ErrorStatus": "DestinyItemNotFound", "Message": "gone"}`

	_, isError = call(t, f.h.handleMoveItems, MoveItemsInput{InstanceIDs: []string{"1001"}, Target: "hunter"})
	require.True(t, isError)
	assert.Equal(t, bungietest.Now, f.h.snapshot.Fetched, "a call that moved nothing keeps the age of the snapshot")

	f.bungie.ActionError = ""

	_, isError = call(t, f.h.handleMoveItems, MoveItemsInput{InstanceIDs: []string{"1001"}, Target: "hunter"})
	require.False(t, isError)
	assert.Equal(t, later, f.h.snapshot.Fetched, "a move restarts the age of the snapshot")
}

// Texts of the stage_junk results that the tests share.
const (
	noJunkClosing = "If you moved other items out of the slot for this run, move them back with move_items."
	lockLine      = "The game does not let the player dismantle a locked item. set_lock unlocks it."
	nothingStaged = "No junk is in the slot, so the user has nothing to dismantle. " +
		"Call stage_junk again to stage the junk that waits."
	fixtureRefusal = "cannot move: Kinetic Weapons on Hunter holds unequipped gear that is not tagged junk: " +
		"Midnight Coup (1003). stage_junk moved nothing. Move that gear to the vault with move_items, " +
		"with target vault and instance_ids 1003. Then call stage_junk again. After stage_junk reports " +
		"that no junk is left, move that gear back with move_items and target c1."
	goneError = `{"ErrorCode": 1623, "ErrorStatus": "DestinyItemNotFound", "Message": "gone"}`
)

// dismantleLine returns the last line of a stage_junk call that staged
// junk. Shown is the number of unequipped items that the game shows in
// the slot. It counts the staged junk and the items that move_items
// cannot move.
func dismantleLine(shown int) string {
	return fmt.Sprintf("The items in the list under the first line are tagged junk, "+
		"and the user dismantles them in the game. "+
		"The user keeps every other unequipped item of the slot. "+
		"Do not dismantle when the number of unequipped items that the game shows in the slot is higher than %d, "+
		"because stage_junk did not check an item that reached the slot after the last inventory fetch. "+
		"Call stage_junk again after the user dismantled them. "+
		"A call in the next 30 seconds might reuse this inventory copy and list the same items.", shown)
}

func skippedLine(skipped int) string {
	return fmt.Sprintf("Junk of this slot that stage_junk skips: %d. "+
		"Each one is equipped, at the Postmaster, not transferable, or for another class.", skipped)
}

// junkTags returns a tag body of the fake service that tags each instance
// in ids junk and tags no other instance.
func junkTags(t *testing.T, ids ...string) string {
	t.Helper()

	rows := make([]dvm.ItemTag, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, dvm.ItemTag{InstanceID: id, TagID: dvm.TagJunk, UpdatedAt: 5})
	}

	return tagRows(t, rows...)
}

// kineticWeapon returns a weapon of the kinetic slot that every class can
// use, for a test to add to the snapshot.
func kineticWeapon(id, name, owner string) *inventory.Item {
	hash := uint32(100)
	if name == "Midnight Coup" {
		hash = 102
	}

	return &inventory.Item{
		InstanceID:   id,
		Name:         name,
		TypeName:     "Hand Cannon",
		Slot:         "Kinetic Weapons",
		Owner:        owner,
		Kind:         inventory.KindWeapon,
		ClassType:    bungie.ClassAny,
		Quantity:     1,
		Hash:         hash,
		Bucket:       bungie.BucketKinetic,
		Transferable: true,
		Equippable:   true,
		Lockable:     true,
	}
}

// transferSteps describes each transfer request as the instance id and
// the direction, such as "1001 to c1" or "1007 to the vault from c2".
func transferSteps(reqs []httpfake.Request) []string {
	var steps []string

	for _, req := range reqs {
		step := fmt.Sprintf("%v to %v", req.Body["itemId"], req.Body["characterId"])
		if req.Body["transferToVault"] == true {
			step = fmt.Sprintf("%v to the vault from %v", req.Body["itemId"], req.Body["characterId"])
		}

		steps = append(steps, step)
	}

	return steps
}

// bungiePosts returns the Bungie actions that a fake received.
func bungiePosts(f *fakes) []httpfake.Request {
	return slices.DeleteFunc(f.bungie.All(), func(req httpfake.Request) bool { return req.Method != http.MethodPost })
}

// serviceWrites returns the path of each POST that the fake service
// received, in the order of arrival.
func serviceWrites(f *fakes) []string {
	var paths []string

	for _, req := range f.service.All() {
		if req.Method == http.MethodPost {
			paths = append(paths, req.Path)
		}
	}

	return paths
}

func TestStageJunk(t *testing.T) {
	t.Parallel()

	refuseEvery := func(map[string]any) bool { return true }
	moveFailure := func(name, id, target string) string {
		return fmt.Sprintf("- %s (%s) did not move: transferring to %s: bungie: DestinyItemNotFound: gone. "+
			"It is in the vault.", name, id, target)
	}
	kinetic := StageJunkInput{Slot: "kinetic"}

	// unknownStack returns a stack without an instance id in the kinetic
	// slot of owner, which move_items cannot move.
	unknownStack := func(owner string) *inventory.Item {
		return &inventory.Item{
			Name:         "Unknown item 373750294",
			Owner:        owner,
			Kind:         inventory.KindOther,
			ClassType:    bungie.ClassAny,
			Quantity:     1,
			Hash:         373750294,
			Bucket:       bungie.BucketKinetic,
			Transferable: true,
		}
	}

	// pinnedWeapon returns a hand cannon of owner that is not
	// transferable, which move_items cannot move.
	pinnedWeapon := func(owner string) *inventory.Item {
		item := kineticWeapon("1007", "Fatebringer", owner)
		item.Transferable = false

		return item
	}

	tests := map[string]struct {
		refuse    func(body map[string]any) bool
		in        StageJunkInput
		want      string
		junk      []string
		add       []*inventory.Item
		transfers []string
		capacity  int
		isError   bool
		moved     bool
	}{
		"no junk": {
			in:   kinetic,
			want: "No junk is left for Kinetic Weapons on Hunter.\n" + noJunkClosing,
		},
		"no junk with skipped junk": {
			in:   kinetic,
			junk: []string{"1006", "1005"},
			want: "No junk is left for Kinetic Weapons on Hunter.\n" + skippedLine(2) + "\n" + noJunkClosing,
		},
		"refusal with waiting junk": {
			in:      kinetic,
			junk:    []string{"1001"},
			want:    fixtureRefusal,
			isError: true,
		},
		"refusal with junk in the slot": {
			in:      kinetic,
			junk:    []string{"1007"},
			add:     []*inventory.Item{kineticWeapon("1007", "Fatebringer", bungietest.HunterID)},
			want:    fixtureRefusal,
			isError: true,
		},
		"refusal lists every keeper": {
			in:   kinetic,
			junk: []string{"1001"},
			add:  []*inventory.Item{kineticWeapon("1007", "Fatebringer", bungietest.HunterID)},
			want: "cannot move: Kinetic Weapons on Hunter holds unequipped gear that is not tagged junk: " +
				"Fatebringer (1007), Midnight Coup (1003). stage_junk moved nothing. Move that gear to the vault " +
				"with move_items, with target vault and instance_ids 1007, 1003. Then call stage_junk again. " +
				"After stage_junk reports that no junk is left, move that gear back with move_items and target c1.",
			isError: true,
		},
		"refusal leaves out a stack without an instance id": {
			in:      kinetic,
			junk:    []string{"1001"},
			add:     []*inventory.Item{unknownStack(bungietest.HunterID)},
			want:    fixtureRefusal,
			isError: true,
		},
		"refusal leaves out an item that is not transferable": {
			in:      kinetic,
			junk:    []string{"1001"},
			add:     []*inventory.Item{pinnedWeapon(bungietest.HunterID)},
			want:    fixtureRefusal,
			isError: true,
		},
		"refusal when items that move_items cannot move fill the slot": {
			in:   StageJunkInput{Slot: "kinetic", Character: "titan"},
			junk: []string{"1001"},
			add: []*inventory.Item{
				unknownStack(bungietest.TitanID),
				pinnedWeapon(bungietest.TitanID),
			},
			capacity: 2,
			want: "cannot move: Kinetic Weapons on Titan has no free place for junk. " +
				"Unequipped items that are not tagged junk and that move_items cannot move fill the slot: " +
				"Fatebringer (1007), Unknown item 373750294 (stack of 1, item hash 373750294). " +
				"The user keeps them, so stage_junk cannot stage the junk that waits. stage_junk moved nothing.",
			isError: true,
		},
		"refusal when one item that move_items cannot move fills the slot": {
			in:       StageJunkInput{Slot: "kinetic", Character: "titan"},
			junk:     []string{"1001"},
			add:      []*inventory.Item{unknownStack(bungietest.TitanID)},
			capacity: 1,
			want: "cannot move: Kinetic Weapons on Titan has no free place for junk. " +
				"An unequipped item that is not tagged junk and that move_items cannot move fills the slot: " +
				"Unknown item 373750294 (stack of 1, item hash 373750294). " +
				"The user keeps it, so stage_junk cannot stage the junk that waits. stage_junk moved nothing.",
			isError: true,
		},
		"stage beside a stack without an instance id": {
			in:   kinetic,
			junk: []string{"1003"},
			add:  []*inventory.Item{unknownStack(bungietest.HunterID)},
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 1\n" +
				"- Midnight Coup (1003)\n" +
				"The slot also holds an unequipped item that is not tagged junk and that move_items cannot move, " +
				"so stage_junk leaves it in place: Unknown item 373750294 (stack of 1, item hash 373750294). " +
				"The user keeps it.\n" +
				"Junk that waits for the next call: 0\n" +
				dismantleLine(2),
		},
		"stage beside items that move_items cannot move": {
			in:   kinetic,
			junk: []string{"1001", "1003"},
			add:  []*inventory.Item{unknownStack(bungietest.HunterID), pinnedWeapon(bungietest.HunterID)},
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 2\n" +
				"- Fatebringer (1001), locked\n" +
				"- Midnight Coup (1003)\n" +
				lockLine + "\n" +
				"The slot also holds unequipped items that are not tagged junk and that move_items cannot move, " +
				"so stage_junk leaves them in place: Fatebringer (1007), " +
				"Unknown item 373750294 (stack of 1, item hash 373750294). The user keeps them.\n" +
				"Junk that waits for the next call: 0\n" +
				dismantleLine(4),
			transfers: []string{"1001 to c1"},
			moved:     true,
		},
		"stage from the vault": {
			in:   kinetic,
			junk: []string{"1001", "1003"},
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 2\n" +
				"- Fatebringer (1001), locked\n" +
				"- Midnight Coup (1003)\n" +
				"The game does not let the player dismantle a locked item. set_lock unlocks it.\n" +
				"Junk that waits for the next call: 0\n" +
				"The items in the list under the first line are tagged junk, and the user dismantles them in " +
				"the game. The user keeps every other unequipped item of the slot. Do not dismantle " +
				"when the number of unequipped items that the game shows in the slot is higher than 2, because " +
				"stage_junk did not check an item that reached the slot after the last inventory fetch. Call " +
				"stage_junk again after the user dismantled them. A call in the next 30 seconds might reuse " +
				"this inventory copy and list the same items.",
			transfers: []string{"1001 to c1"},
			moved:     true,
		},
		"stage from another character": {
			in:   kinetic,
			junk: []string{"1003", "1007"},
			add:  []*inventory.Item{kineticWeapon("1007", "Fatebringer", bungietest.TitanID)},
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 2\n" +
				"- Fatebringer (1007)\n" +
				"- Midnight Coup (1003)\n" +
				"Junk that waits for the next call: 0\n" +
				dismantleLine(2),
			transfers: []string{"1007 to the vault from c2", "1007 to c1"},
			moved:     true,
		},
		"batch stops at the room": {
			in:   kinetic,
			junk: []string{"1001", "1003", "1007", "1008"},
			add: []*inventory.Item{
				kineticWeapon("1007", "Midnight Coup", inventory.OwnerVault),
				kineticWeapon("1008", "Fatebringer", inventory.OwnerVault),
			},
			capacity: 4,
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 3\n" +
				"- Fatebringer (1001), locked\n" +
				"- Fatebringer (1008)\n" +
				"- Midnight Coup (1003)\n" +
				lockLine + "\n" +
				"Junk that waits for the next call: 1\n" +
				dismantleLine(3),
			transfers: []string{"1001 to c1", "1008 to c1"},
			moved:     true,
		},
		"no room": {
			in:       kinetic,
			junk:     []string{"1001", "1003"},
			capacity: 2,
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 1\n" +
				"- Midnight Coup (1003)\n" +
				"Junk that waits for the next call: 1\n" +
				dismantleLine(1),
		},
		"nothing waits": {
			in:   kinetic,
			junk: []string{"1003"},
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 1\n" +
				"- Midnight Coup (1003)\n" +
				"Junk that waits for the next call: 0\n" +
				dismantleLine(1),
		},
		"one move does not succeed": {
			in:     kinetic,
			junk:   []string{"1001", "1003", "1007"},
			add:    []*inventory.Item{kineticWeapon("1007", "Midnight Coup", inventory.OwnerVault)},
			refuse: func(body map[string]any) bool { return body["itemId"] == "1001" },
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 2\n" +
				"- Midnight Coup (1003)\n" +
				"- Midnight Coup (1007)\n" +
				moveFailure("Fatebringer", "1001", "Hunter") + "\n" +
				"Junk that waits for the next call: 1\n" +
				dismantleLine(2),
			transfers: []string{"1001 to c1", "1007 to c1"},
			moved:     true,
		},
		"second transfer does not succeed": {
			in:     kinetic,
			junk:   []string{"1003", "1007"},
			add:    []*inventory.Item{kineticWeapon("1007", "Fatebringer", bungietest.TitanID)},
			refuse: func(body map[string]any) bool { return body["transferToVault"] == false },
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 1\n" +
				"- Midnight Coup (1003)\n" +
				moveFailure("Fatebringer", "1007", "Hunter") + "\n" +
				"Junk that waits for the next call: 1\n" +
				dismantleLine(1),
			transfers: []string{"1007 to the vault from c2", "1007 to c1"},
			isError:   true,
			moved:     true,
		},
		"every move fails with junk in the slot": {
			in:     kinetic,
			junk:   []string{"1001", "1003"},
			refuse: refuseEvery,
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 1\n" +
				"- Midnight Coup (1003)\n" +
				moveFailure("Fatebringer", "1001", "Hunter") + "\n" +
				"Junk that waits for the next call: 1\n" +
				dismantleLine(1),
			transfers: []string{"1001 to c1"},
			isError:   true,
		},
		"every move fails with an empty slot": {
			in:     StageJunkInput{Slot: "kinetic", Character: "titan"},
			junk:   []string{"1001"},
			refuse: refuseEvery,
			want: "Unequipped junk in Kinetic Weapons on Titan after this call: 0\n" +
				moveFailure("Fatebringer", "1001", "Titan") + "\n" +
				"Junk that waits for the next call: 1\n" +
				nothingStaged,
			transfers: []string{"1001 to c2"},
			isError:   true,
		},
		"item count": {
			in:   kinetic,
			junk: []string{"1001", "1003", "1007"},
			add:  []*inventory.Item{kineticWeapon("1007", "Midnight Coup", inventory.OwnerVault)},
			want: "Unequipped junk in Kinetic Weapons on Hunter after this call: 3\n" +
				"- Fatebringer (1001), locked\n" +
				"- Midnight Coup (1003)\n" +
				"- Midnight Coup (1007)\n" +
				lockLine + "\n" +
				"Junk that waits for the next call: 0\n" +
				dismantleLine(3),
			transfers: []string{"1001 to c1", "1007 to c1"},
			moved:     true,
		},
		"character input": {
			in:   StageJunkInput{Slot: "kinetic", Character: "titan"},
			junk: []string{"1001"},
			want: "Unequipped junk in Kinetic Weapons on Titan after this call: 1\n" +
				"- Fatebringer (1001), locked\n" +
				lockLine + "\n" +
				"Junk that waits for the next call: 0\n" +
				dismantleLine(1),
			transfers: []string{"1001 to c2"},
			moved:     true,
		},
		"unknown character": {
			in:      StageJunkInput{Slot: "kinetic", Character: "warlock"},
			junk:    []string{"1001", "1003"},
			want:    `unknown location: "warlock" is not a character id or a class name`,
			isError: true,
		},
		"character error wins over slot error": {
			in:      StageJunkInput{Slot: "boots", Character: "warlock"},
			junk:    []string{"1001", "1003"},
			want:    `unknown location: "warlock" is not a character id or a class name`,
			isError: true,
		},
		"slot error": {
			in:   StageJunkInput{Slot: "boots"},
			junk: []string{"1001", "1003"},
			want: `invalid input: slot "boots" matches no weapon or armor slot, ` +
				"so pass one of Kinetic Weapons, Power Weapons, Helmet",
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.Data[pathTags] = junkTags(t, tc.junk...)

			s, err := f.h.loadSnapshot(t.Context(), false)
			require.NoError(t, err)

			for _, item := range tc.add {
				s.Add(item)
			}

			if tc.capacity > 0 {
				i := slices.IndexFunc(
					s.Buckets,
					func(b inventory.Bucket) bool { return b.Hash == bungie.BucketKinetic },
				)
				require.GreaterOrEqual(t, i, 0)

				s.Buckets[i].Capacity = tc.capacity
			}

			if tc.refuse != nil {
				f.bungie.ActionError = goneError
				f.bungie.ActionErrorIf = tc.refuse
			}

			later := bungietest.Now.Add(10 * time.Second)
			f.h.now = func() time.Time { return later }

			got, isError := call(t, f.h.handleStageJunk, tc.in)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.isError, isError)

			transfers := f.bungie.Calls(http.MethodPost, pathTransfer)
			assert.Equal(t, tc.transfers, transferSteps(transfers))
			assert.Len(t, bungiePosts(f), len(transfers), "the call sends no equip and no Postmaster request")

			wantFetched := bungietest.Now
			if tc.moved {
				wantFetched = later
			}

			assert.Equal(t, wantFetched, f.h.snapshot.Fetched)
		})
	}
}

func TestStageJunkLines(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line   string
		junk   []string
		absent bool
	}{
		"lock line":       {junk: []string{"1001", "1003"}, line: lockLine},
		"no lock line":    {junk: []string{"1003"}, line: lockLine, absent: true},
		"skipped line":    {junk: []string{"1003", "1005", "1006"}, line: skippedLine(2)},
		"no skipped line": {junk: []string{"1003"}, line: "stage_junk skips", absent: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.Data[pathTags] = junkTags(t, tc.junk...)

			got, isError := call(t, f.h.handleStageJunk, StageJunkInput{Slot: "kinetic"})
			assert.False(t, isError)

			if tc.absent {
				assert.NotContains(t, got, tc.line)
			} else {
				assert.Contains(t, got, tc.line)
			}
		})
	}
}

func TestStageJunkSnapshotAge(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	require.False(t, isError)

	later := bungietest.Now.Add(10 * time.Second)
	f.h.now = func() time.Time { return later }
	kinetic := StageJunkInput{Slot: "kinetic"}

	_, isError = call(t, f.h.handleStageJunk, kinetic)
	require.False(t, isError)
	assert.Equal(t, bungietest.Now, f.h.snapshot.Fetched, "a call without junk keeps the age of the snapshot")

	f.service.Data[pathTags] = junkTags(t, "1001")

	_, isError = call(t, f.h.handleStageJunk, kinetic)
	require.True(t, isError)
	assert.Equal(t, bungietest.Now, f.h.snapshot.Fetched, "a refusal keeps the age of the snapshot")

	f.service.Data[pathTags] = junkTags(t, "1001", "1003")
	f.bungie.ActionError = goneError

	_, isError = call(t, f.h.handleStageJunk, kinetic)
	require.True(t, isError)
	assert.Equal(t, bungietest.Now, f.h.snapshot.Fetched, "a call that moved nothing keeps the age of the snapshot")

	f.bungie.ActionError = ""

	_, isError = call(t, f.h.handleStageJunk, kinetic)
	require.False(t, isError)
	assert.Equal(t, later, f.h.snapshot.Fetched, "a move restarts the age of the snapshot")
}

func TestStageJunkReadsTags(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.service.Data[pathTags] = junkTags(t, "1001")

	got, isError := call(t, f.h.handleStageJunk, StageJunkInput{Slot: "kinetic"})
	assert.True(t, isError)
	assert.Equal(t, fixtureRefusal, got)

	f.service.Data[pathTags] = junkTags(t, "1001", "1003")

	got, isError = call(t, f.h.handleStageJunk, StageJunkInput{Slot: "kinetic"})
	assert.False(t, isError)
	assert.True(t, strings.HasPrefix(got, "Unequipped junk in Kinetic Weapons on Hunter after this call: 2\n"), got)

	assert.Len(t, f.service.Calls(http.MethodGet, pathTags), 2, "every call reads the tags")
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1, "the second call reuses the profile")
}

func TestStageJunkLoadError(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.service.Data[pathTags] = ""

	got, isError := call(t, f.h.handleStageJunk, StageJunkInput{Slot: "kinetic"})
	assert.True(t, isError)
	assert.True(t, strings.HasPrefix(got, "reading tags and notes:"), got)
	assert.Empty(t, bungiePosts(f))
}

func TestSetLockTool(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleSetLock, SetLockInput{
		InstanceIDs: []string{"1002", "1003"},
		Locked:      true,
	})
	assert.False(t, isError)
	assert.Equal(t, "- Locked Gjallarhorn (1002).\n- Locked Midnight Coup (1003).", got)

	locks := f.bungie.Calls(http.MethodPost, pathLock)
	require.Len(t, locks, 2)
	assert.Equal(t, map[string]any{
		"state":          true,
		"itemId":         "1002",
		"characterId":    bungietest.HunterID,
		"membershipType": float64(3),
	}, locks[0].Body)
	assert.Equal(t, "1003", locks[1].Body["itemId"])

	got, isError = call(t, f.h.handleSetLock, SetLockInput{InstanceIDs: []string{"1001"}})
	assert.False(t, isError)
	assert.Equal(t, "- Unlocked Fatebringer (1001).", got)

	locks = f.bungie.Calls(http.MethodPost, pathLock)
	require.Len(t, locks, 3)
	assert.Equal(t, false, locks[2].Body["state"])

	// The snapshot carries the lock changes, so the next read does not ask
	// Bungie for a profile that might predate them.
	got, isError = call(t, f.h.handleListItems, ListItemsInput{})
	assert.False(t, isError)
	assert.Contains(t, got, "1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 |  | Vault |  | locked\n")
	assert.Contains(t, got, "1003 | Midnight Coup | Kinetic Hand Cannon | Legendary | 2005 |  | Hunter |  | locked")
	assert.Contains(t, got, "1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 |  | Vault | keep | "+
		"favorite, masterwork, note\n")
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1)
}

func TestSetLockToolErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		bungieError string
		want        string
		in          SetLockInput
	}{
		"bungie refuses": {
			in:          SetLockInput{InstanceIDs: []string{"1002"}, Locked: true},
			bungieError: `{"ErrorCode": 1623, "ErrorStatus": "DestinyItemNotFound", "Message": "gone"}`,
			want: "- Gjallarhorn (1002) did not lock: setting the lock state: " +
				"bungie: DestinyItemNotFound: gone.",
		},
		"unknown instance rejects the batch": {
			in:   SetLockInput{InstanceIDs: []string{"1002", "9"}, Locked: true},
			want: "invalid input: no item in the inventory has the instance id 9",
		},
		"no ids": {
			in:   SetLockInput{Locked: true},
			want: "invalid input: pass between 1 and 200 instance ids",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.bungie.ActionError = tc.bungieError

			got, isError := call(t, f.h.handleSetLock, tc.in)
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)

			item, ok := f.h.snapshot.Find("1002")
			require.True(t, ok)
			assert.False(t, item.Locked)
		})
	}
}

func TestSetLockToolSnapshotAge(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	require.False(t, isError)

	later := bungietest.Now.Add(10 * time.Second)
	f.h.now = func() time.Time { return later }

	stuck, ok := f.h.snapshot.Find("1003")
	require.True(t, ok)

	stuck.Lockable = false
	refusal := "- Midnight Coup (1003) did not lock: cannot change the lock: Midnight Coup cannot be locked."

	got, isError := call(t, f.h.handleSetLock, SetLockInput{InstanceIDs: []string{"1003"}, Locked: true})
	assert.True(t, isError)
	assert.Equal(t, refusal, got)
	assert.Empty(t, f.bungie.Calls(http.MethodPost, pathLock))
	assert.Equal(t, bungietest.Now, f.h.snapshot.Fetched, "a call that changed nothing keeps the age of the snapshot")

	got, isError = call(t, f.h.handleSetLock, SetLockInput{InstanceIDs: []string{"1003", "1002"}, Locked: true})
	assert.False(t, isError, "one item locked")
	assert.Equal(t, refusal+"\n- Locked Gjallarhorn (1002).", got)
	assert.Equal(t, later, f.h.snapshot.Fetched, "a lock change restarts the age of the snapshot")
}

func TestSetPerkTool(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	require.False(t, isError)

	got, isError := call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: " rampage "})
	assert.False(t, isError)
	assert.Equal(t, "Replaced Explosive Payload with Rampage on Fatebringer (1001). "+
		"Its stats keep their old values until the next inventory fetch.", got)

	inserts := f.bungie.Calls(http.MethodPost, pathInsert)
	require.Len(t, inserts, 1)
	assert.Equal(t, map[string]any{
		"plug": map[string]any{
			"socketIndex":     float64(0),
			"socketArrayType": float64(0),
			"plugItemHash":    float64(903),
		},
		"itemId":         "1001",
		"characterId":    bungietest.HunterID,
		"membershipType": float64(3),
	}, inserts[0].Body)

	// The snapshot carries the new perk, so the next read does not ask
	// Bungie for a profile that might predate it.
	got, isError = call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
	assert.False(t, isError)
	assert.Contains(t, got, "Perks:\n- Rampage (Trait), can swap to Explosive Payload\n- Firefly (Trait)\n")
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1)

	got, isError = call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: "Rampage"})
	assert.False(t, isError)
	assert.Equal(t, "Rampage is already active on Fatebringer (1001).", got)
	assert.Len(t, f.bungie.Calls(http.MethodPost, pathInsert), 1, "an active perk sends no request")
}

func TestSetPerkToolSnapshotAge(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	require.False(t, isError)

	later := bungietest.Now.Add(10 * time.Second)
	f.h.now = func() time.Time { return later }

	_, isError = call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: "Explosive Payload"})
	require.False(t, isError)
	assert.Equal(t, bungietest.Now, f.h.snapshot.Fetched, "a perk that is already active keeps the age of the snapshot")

	_, isError = call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: "Kill Clip"})
	require.True(t, isError)
	assert.Equal(t, bungietest.Now, f.h.snapshot.Fetched, "a perk that no socket offers keeps the age of the snapshot")
	assert.Empty(t, f.bungie.Calls(http.MethodPost, pathInsert))

	f.bungie.ActionError = `{"ErrorCode": 1634, "ErrorStatus": "DestinyCharacterNotInTower", "Message": "not in a social space"}`

	_, isError = call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: "Rampage"})
	require.True(t, isError)
	assert.Equal(t, bungietest.Now, f.h.snapshot.Fetched, "a swap that Bungie refuses keeps the age of the snapshot")
	assert.Len(t, f.bungie.Calls(http.MethodPost, pathInsert), 1)

	f.bungie.ActionError = ""

	_, isError = call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: "Rampage"})
	require.False(t, isError)
	assert.Equal(t, later, f.h.snapshot.Fetched, "a perk change restarts the age of the snapshot")
}

func TestSetPerkToolErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		bungieError string
		want        string
		in          SetPerkInput
	}{
		"bungie refuses": {
			in:          SetPerkInput{InstanceID: "1001", Perk: "Rampage"},
			bungieError: `{"ErrorCode": 1634, "ErrorStatus": "DestinyCharacterNotInTower", "Message": "not in a social space"}`,
			want:        "inserting Rampage: bungie: DestinyCharacterNotInTower: not in a social space",
		},
		"perk that no socket can swap to": {
			in: SetPerkInput{InstanceID: "1001", Perk: "Kill Clip"},
			want: `cannot change the perk: no socket of Fatebringer can swap to "Kill Clip". ` +
				"Sockets that can swap: Explosive Payload (can swap to Rampage)",
		},
		"replace names no active perk": {
			in: SetPerkInput{InstanceID: "1001", Perk: "Rampage", Replace: " Kill Clip "},
			want: `cannot change the perk: Fatebringer has no active perk named "Kill Clip". ` +
				"Its perks are Explosive Payload, Firefly",
		},
		"unknown instance": {
			in:   SetPerkInput{InstanceID: "9", Perk: "Rampage"},
			want: "invalid input: no item in the inventory has the instance id 9",
		},
		"no perk name": {
			in:   SetPerkInput{InstanceID: "1001", Perk: " "},
			want: "invalid input: a perk change needs a perk name",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.bungie.ActionError = tc.bungieError

			got, isError := call(t, f.h.handleSetPerk, tc.in)
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)

			if f.h.snapshot == nil {
				return
			}

			item, ok := f.h.snapshot.Find("1001")
			require.True(t, ok)
			assert.Equal(t, "Explosive Payload", item.Perks[0].Name)
		})
	}
}

func TestListLoadouts(t *testing.T) {
	t.Parallel()

	// hunter returns the section of the Hunter with the heading lines of
	// its loadouts 2 and 3.
	hunter := func(second, third string) string {
		return strings.Join([]string{
			"Hunter (id c1):",
			"loadout | instance id | name | slot | location",
			second,
			"2 | 1006 | Fatebringer | Kinetic Weapons | Hunter (equipped)",
			"2 | 1004 | Celestial Nighthawk | Helmet | Hunter (equipped)",
			third,
			"3 | 1001 | Fatebringer | Kinetic Weapons | Vault",
			"3 | 1004 | Celestial Nighthawk | Helmet | Hunter (equipped)",
			"3 | 9999 | not in the inventory |  | ",
		}, "\n")
	}

	// titan returns the section of the Titan with the heading line of its
	// loadout 1.
	titan := func(first string) string {
		return strings.Join([]string{
			"Titan (id c2):",
			"loadout | instance id | name | slot | location",
			first,
			"1 | 1006 | Fatebringer | Kinetic Weapons | Hunter (equipped)",
		}, "\n")
	}

	named := hunter("Loadout 2: Raid Night", "Loadout 3: Trials") + "\n\n" + titan("Loadout 1: Crucible")
	numbered := hunter("Loadout 2", "Loadout 3") + "\n\n" + titan("Loadout 1")
	unavailable := loadoutsIntro + "\nLoadout names unavailable: "

	tests := map[string]struct {
		edit     func(f *fakes)
		want     string
		in       ListLoadoutsInput
		hunterOn bool
		isError  bool
	}{
		"every character": {
			want: loadoutsIntro + "\n\n" + named,
		},
		"one character": {
			in:   ListLoadoutsInput{Character: "titan"},
			want: loadoutsIntro + "\n\n" + titan("Loadout 1: Crucible"),
		},
		"character without loadouts": {
			in:       ListLoadoutsInput{Character: bungietest.TitanID},
			hunterOn: true,
			want:     loadoutsIntro + "\n\nTitan (id c2): no saved loadouts",
		},
		"name hash that the table lacks": {
			edit: func(f *fakes) {
				f.bungie.Data[bungietest.PathLoadoutNames] = `{"1": {"hash": 1, "name": "Raid Night"}}`
			},
			want: loadoutsIntro + "\n\n" + hunter("Loadout 2: Raid Night", "Loadout 3: unnamed") +
				"\n\n" + titan("Loadout 1: unnamed"),
		},
		"redacted name without text": {
			edit: func(f *fakes) {
				f.bungie.Data[bungietest.PathLoadoutNames] = `{"7": {"hash": 7, "name": "", "redacted": true}}`
			},
			in:   ListLoadoutsInput{Character: "titan"},
			want: loadoutsIntro + "\n\n" + titan("Loadout 1: unnamed"),
		},
		"manifest does not load": {
			edit: func(f *fakes) { delete(f.bungie.Data, bungietest.PathManifest) },
			want: unavailable + "reading the Bungie manifest: bungie: ParameterParseFailure: no route\n\n" +
				numbered,
		},
		"manifest lists no name table": {
			edit: func(f *fakes) {
				f.bungie.Data[bungietest.PathManifest] = `{"ErrorCode": 1, "Response": {"jsonWorldComponentContentPaths": {
					"fr": {"DestinyLoadoutNameDefinition": "/fr.json"}}}}`
			},
			want: unavailable + "the Bungie manifest lists no table of loadout names\n\n" + numbered,
		},
		"name table does not download": {
			edit: func(f *fakes) { delete(f.bungie.Data, bungietest.PathLoadoutNames) },
			want: unavailable + "fetching " + bungietest.PathLoadoutNames + ": bungie: content file returned status 404\n\n" +
				numbered,
		},
		"name table does not decode": {
			edit: func(f *fakes) { f.bungie.Data[bungietest.PathLoadoutNames] = `{"1": {"hash": 1,` },
			in:   ListLoadoutsInput{Character: "titan"},
			want: unavailable + "decoding " + bungietest.PathLoadoutNames +
				": reading the loadout name table: unexpected EOF\n\n" + titan("Loadout 1"),
		},
		"vault": {
			in:      ListLoadoutsInput{Character: "vault"},
			want:    "unknown location: the vault is not a character, so pass a character id or a class name",
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			if tc.edit != nil {
				tc.edit(f)
			}

			if tc.hunterOn {
				_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
				require.False(t, isError)

				f.h.snapshot.Loadouts = f.h.snapshot.LoadoutsOf(bungietest.HunterID)
			}

			got, isError := call(t, f.h.handleListLoadouts, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)

			// The list shows only names, so it downloads neither the icon
			// table nor the color table.
			assert.Empty(t, f.bungie.Calls(http.MethodGet, bungietest.PathLoadoutIcons))
			assert.Empty(t, f.bungie.Calls(http.MethodGet, bungietest.PathLoadoutColors))
		})
	}
}

func TestEquipLoadoutTool(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleEquipLoadout, EquipLoadoutInput{Character: "titan", Loadout: 1})
	assert.False(t, isError)
	assert.Equal(t, "- Moved Fatebringer (1006) from Hunter to Titan. "+
		"Equipped Midnight Coup (1003) in its place on Hunter.\n"+
		"Equipped loadout 1 on Titan: Fatebringer (1006).", got)

	equips := f.bungie.Calls(http.MethodPost, pathEquip)
	require.Len(t, equips, 1)
	assert.Equal(t, "1003", equips[0].Body["itemId"])
	assert.Len(t, f.bungie.Calls(http.MethodPost, pathTransfer), 2)

	loadouts := f.bungie.Calls(http.MethodPost, pathLoadout)
	require.Len(t, loadouts, 1)
	assert.Equal(t, map[string]any{
		"loadoutIndex":   float64(0),
		"characterId":    bungietest.TitanID,
		"membershipType": float64(3),
	}, loadouts[0].Body)

	got, isError = call(t, f.h.handleEquipLoadout, EquipLoadoutInput{Character: bungietest.HunterID, Loadout: 3})
	assert.False(t, isError)
	assert.Equal(t, "- Moved Fatebringer (1001) from Vault to Hunter.\n"+
		"Equipped loadout 3 on Hunter: Fatebringer (1001), Celestial Nighthawk (1004). "+
		"The inventory does not hold these loadout items: 9999.", got)

	loadouts = f.bungie.Calls(http.MethodPost, pathLoadout)
	require.Len(t, loadouts, 2)
	assert.Equal(t, map[string]any{
		"loadoutIndex":   float64(2),
		"characterId":    bungietest.HunterID,
		"membershipType": float64(3),
	}, loadouts[1].Body)

	// The snapshot carries the equips, so the next read does not ask Bungie
	// for a profile that might predate them.
	got, isError = call(t, f.h.handleListItems, ListItemsInput{Kind: inventory.KindWeapon})
	assert.False(t, isError)
	assert.Contains(t, got, "1006 | Fatebringer | Kinetic Hand Cannon | Legendary | 1980 |  | Titan (equipped) |  | \n")
	assert.Contains(
		t,
		got,
		"1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 |  | Hunter (equipped) | keep | ",
	)
	assert.Contains(t, got, "1003 | Midnight Coup | Kinetic Hand Cannon | Legendary | 2005 |  | Hunter |  | ")
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1)
}

func TestEquipLoadoutToolErrors(t *testing.T) {
	t.Parallel()

	notHere := `{"ErrorCode": 1671, "ErrorStatus": "DestinyCannotPerformActionAtThisLocation", ` +
		`"Message": "Not here."}`

	tests := map[string]struct {
		bungieError     string
		bungieErrorPath string
		want            string
		wantVaultCannon string
		in              EquipLoadoutInput
		wantFetched     bool
	}{
		"loadout that holds nothing": {
			in: EquipLoadoutInput{Character: "hunter", Loadout: 1},
			want: "unknown loadout: Hunter has no saved loadout with the number 1. " +
				"Its saved loadouts have the numbers 2, 3",
			wantVaultCannon: "Vault",
		},
		"no character": {
			in:              EquipLoadoutInput{Loadout: 2},
			want:            "unknown location: pass the character of the loadout as a character id or a class name",
			wantVaultCannon: "Vault",
		},
		"vault": {
			in:              EquipLoadoutInput{Character: "vault", Loadout: 1},
			want:            "unknown location: the vault is not a character, so pass a character id or a class name",
			wantVaultCannon: "Vault",
		},
		"bungie refuses the equip": {
			in:          EquipLoadoutInput{Character: "hunter", Loadout: 2},
			bungieError: notHere,
			want: "Loadout 2 is not equipped on Hunter: equipping the loadout: " +
				"bungie: DestinyCannotPerformActionAtThisLocation: Not here.",
			wantVaultCannon: "Vault",
		},
		"bungie refuses the equip after a move": {
			in:              EquipLoadoutInput{Character: "hunter", Loadout: 3},
			bungieError:     notHere,
			bungieErrorPath: pathLoadout,
			want: "- Moved Fatebringer (1001) from Vault to Hunter.\n" +
				"Loadout 3 is not equipped on Hunter: equipping the loadout: " +
				"bungie: DestinyCannotPerformActionAtThisLocation: Not here.",
			wantVaultCannon: "Hunter",
			wantFetched:     true,
		},
		"bungie refuses a move": {
			in:          EquipLoadoutInput{Character: "titan", Loadout: 1},
			bungieError: `{"ErrorCode": 1623, "ErrorStatus": "DestinyItemNotFound", "Message": "gone"}`,
			want: "- Fatebringer (1006) did not reach Titan. It is equipped on Hunter.\n" +
				"Loadout 1 is not equipped on Titan: moving Fatebringer (1006) to Titan: " +
				"equipping Midnight Coup in its place: bungie: DestinyItemNotFound: gone.",
			wantVaultCannon: "Vault",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
			require.False(t, isError)

			later := bungietest.Now.Add(10 * time.Second)
			f.h.now = func() time.Time { return later }
			f.bungie.ActionError = tc.bungieError
			f.bungie.ActionErrorPath = tc.bungieErrorPath

			got, isError := call(t, f.h.handleEquipLoadout, tc.in)
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)

			if tc.wantFetched {
				assert.Equal(
					t,
					later,
					f.h.snapshot.Fetched,
					"a call that changed the game restarts the age of the snapshot",
				)
			} else {
				assert.Equal(
					t,
					bungietest.Now,
					f.h.snapshot.Fetched,
					"a call that changed nothing keeps the age of the snapshot",
				)
			}

			cannon, ok := f.h.snapshot.Find("1001")
			require.True(t, ok)
			assert.Equal(t, tc.wantVaultCannon, f.h.snapshot.LocationLabel(cannon), "the vault hand cannon")

			item, ok := f.h.snapshot.Find("1006")
			require.True(t, ok)
			assert.Equal(t, bungietest.HunterID, item.Owner)
			assert.True(t, item.Equipped)
		})
	}
}

func TestEquipLoadoutToolSnapshotAge(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	require.False(t, isError)

	later := bungietest.Now.Add(10 * time.Second)
	f.h.now = func() time.Time { return later }

	_, isError = call(t, f.h.handleEquipLoadout, EquipLoadoutInput{Character: "hunter", Loadout: 2})
	require.False(t, isError)
	assert.Equal(t, later, f.h.snapshot.Fetched, "an equip restarts the age of the snapshot")
}

// snapshotBody returns the body of a SnapshotLoadout request of the fake
// account.
func snapshotBody(index int, characterID string, name, icon, color uint32) map[string]any {
	return map[string]any{
		"loadoutIndex":   float64(index),
		"characterId":    characterID,
		"membershipType": float64(3),
		"nameHash":       float64(name),
		"iconHash":       float64(icon),
		"colorHash":      float64(color),
	}
}

// hunterLoadoutRows returns the list_loadouts rows of loadout number of the
// fake Hunter. With equipped, the loadout holds the gear that the Hunter
// has equipped. Without it, the loadout holds the items that the fake
// profile saved in slot 3.
func hunterLoadoutRows(number int, equipped bool) string {
	if equipped {
		return fmt.Sprintf("%d | 1006 | Fatebringer | Kinetic Weapons | Hunter (equipped)\n"+
			"%d | 1004 | Celestial Nighthawk | Helmet | Hunter (equipped)", number, number)
	}

	return fmt.Sprintf("%d | 1001 | Fatebringer | Kinetic Weapons | Vault\n"+
		"%d | 1004 | Celestial Nighthawk | Helmet | Hunter (equipped)\n"+
		"%d | 9999 | not in the inventory |  | ", number, number, number)
}

func TestSaveLoadoutTool(t *testing.T) {
	t.Parallel()

	hunterGear := " It holds the gear that Hunter has equipped: Fatebringer (1006), Celestial Nighthawk (1004)."
	hunterList := loadoutsIntro + "\n\nHunter (id c1):\nloadout | instance id | name | slot | location\n"
	titanList := loadoutsIntro + "\n\nTitan (id c2): no saved loadouts"

	tests := map[string]struct {
		edit     func(f *fakes)
		wantBody map[string]any
		want     string
		// wantList is the list_loadouts result for the character of the
		// save, right after the save.
		wantList string
		in       SaveLoadoutInput
		// emptySlot marks a save into an empty slot, the only save that
		// downloads the icon and the color tables.
		emptySlot bool
	}{
		"empty slot takes the first entries": {
			in:       SaveLoadoutInput{Character: "hunter", Number: 1},
			wantBody: snapshotBody(0, bungietest.HunterID, 1, 20, 31),
			want:     "Saved loadout 1 on Hunter with the name Raid Night." + hunterGear,
			wantList: hunterList + "Loadout 1: Raid Night\n" + hunterLoadoutRows(1, true) +
				"\nLoadout 2: Raid Night\n" + hunterLoadoutRows(2, true) +
				"\nLoadout 3: Trials\n" + hunterLoadoutRows(3, false),
			emptySlot: true,
		},
		"empty slot with a name": {
			in:       SaveLoadoutInput{Character: "hunter", Number: 1, Name: "Trials"},
			wantBody: snapshotBody(0, bungietest.HunterID, 4, 20, 31),
			want:     "Saved loadout 1 on Hunter with the name Trials." + hunterGear,
			wantList: hunterList + "Loadout 1: Trials\n" + hunterLoadoutRows(1, true) +
				"\nLoadout 2: Raid Night\n" + hunterLoadoutRows(2, true) +
				"\nLoadout 3: Trials\n" + hunterLoadoutRows(3, false),
			emptySlot: true,
		},
		"saved loadout keeps its identifiers": {
			in:       SaveLoadoutInput{Character: bungietest.HunterID, Number: 2},
			wantBody: snapshotBody(1, bungietest.HunterID, 1, 2, 3),
			want:     "Saved loadout 2 on Hunter with the name Raid Night." + hunterGear,
			wantList: hunterList + "Loadout 2: Raid Night\n" + hunterLoadoutRows(2, true) +
				"\nLoadout 3: Trials\n" + hunterLoadoutRows(3, false),
		},
		"saved loadout with a name": {
			in:       SaveLoadoutInput{Character: "hunter", Number: 3, Name: "Crucible"},
			wantBody: snapshotBody(2, bungietest.HunterID, 7, 5, 6),
			want:     "Saved loadout 3 on Hunter with the name Crucible." + hunterGear,
			wantList: hunterList + "Loadout 2: Raid Night\n" + hunterLoadoutRows(2, true) +
				"\nLoadout 3: Crucible\n" + hunterLoadoutRows(3, true),
		},
		"name in another letter case": {
			in:       SaveLoadoutInput{Character: "hunter", Number: 2, Name: "tRIALS"},
			wantBody: snapshotBody(1, bungietest.HunterID, 4, 2, 3),
			want:     "Saved loadout 2 on Hunter with the name Trials." + hunterGear,
			wantList: hunterList + "Loadout 2: Trials\n" + hunterLoadoutRows(2, true) +
				"\nLoadout 3: Trials\n" + hunterLoadoutRows(3, false),
		},
		"character without equipped gear": {
			// A save of no gear leaves the slot empty.
			in:       SaveLoadoutInput{Character: "titan", Number: 1},
			wantBody: snapshotBody(0, bungietest.TitanID, 7, 8, 9),
			want:     "Saved loadout 1 on Titan with the name Crucible. The inventory shows no gear that Titan has equipped.",
			wantList: titanList,
		},
		"saved loadout whose name the table lacks": {
			edit: func(f *fakes) {
				f.bungie.Data[bungietest.PathLoadoutNames] = `{"1": {"hash": 1, "index": 0, "name": "Raid Night"}}`
			},
			in:       SaveLoadoutInput{Character: "titan", Number: 1},
			wantBody: snapshotBody(0, bungietest.TitanID, 7, 8, 9),
			want:     "Saved loadout 1 on Titan without a name. The inventory shows no gear that Titan has equipped.",
			wantList: titanList,
		},
		"saved loadout without the name table": {
			edit:     func(f *fakes) { delete(f.bungie.Data, bungietest.PathLoadoutNames) },
			in:       SaveLoadoutInput{Character: "hunter", Number: 2},
			wantBody: snapshotBody(1, bungietest.HunterID, 1, 2, 3),
			want: "Saved loadout 2 on Hunter, which keeps its name." + hunterGear +
				"\nLoadout names unavailable: fetching " + bungietest.PathLoadoutNames +
				": bungie: content file returned status 404",
			wantList: loadoutsIntro + "\nLoadout names unavailable: fetching " + bungietest.PathLoadoutNames +
				": bungie: content file returned status 404\n\n" +
				"Hunter (id c1):\nloadout | instance id | name | slot | location\n" +
				"Loadout 2\n" + hunterLoadoutRows(2, true) + "\nLoadout 3\n" + hunterLoadoutRows(3, false),
		},
		"saved loadout without the icon and color tables": {
			edit: func(f *fakes) {
				delete(f.bungie.Data, bungietest.PathLoadoutIcons)
				delete(f.bungie.Data, bungietest.PathLoadoutColors)
			},
			in:       SaveLoadoutInput{Character: "hunter", Number: 3, Name: "trials"},
			wantBody: snapshotBody(2, bungietest.HunterID, 4, 5, 6),
			want:     "Saved loadout 3 on Hunter with the name Trials." + hunterGear,
			wantList: hunterList + "Loadout 2: Raid Night\n" + hunterLoadoutRows(2, true) +
				"\nLoadout 3: Trials\n" + hunterLoadoutRows(3, true),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			if tc.edit != nil {
				tc.edit(f)
			}

			got, isError := call(t, f.h.handleSaveLoadout, tc.in)
			assert.False(t, isError)
			assert.Equal(t, tc.want, got)

			requests := bungiePosts(f)
			require.Len(t, requests, 1)
			assert.Equal(t, pathSnapshot, requests[0].Path)
			assert.Equal(t, tc.wantBody, requests[0].Body)

			// Every save downloads the name table, and only a save into an
			// empty slot downloads the icon and the color tables.
			tableGets := 0
			if tc.emptySlot {
				tableGets = 1
			}

			assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathLoadoutNames), 1)
			assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathLoadoutIcons), tableGets)
			assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathLoadoutColors), tableGets)

			// The fake profile keeps the loadouts from before the save, so
			// the list shows the saved loadout from the snapshot that the
			// save updated.
			got, isError = call(t, f.h.handleListLoadouts, ListLoadoutsInput{Character: tc.in.Character})
			require.False(t, isError)
			assert.Equal(t, tc.wantList, got)
			assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1)
		})
	}
}

func TestSaveLoadoutToolAfterEquip(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, isError := call(t, f.h.handleMoveItems, MoveItemsInput{
		InstanceIDs: []string{"1001"},
		Target:      "hunter",
		Equip:       true,
	})
	require.False(t, isError)

	got, isError := call(t, f.h.handleSaveLoadout, SaveLoadoutInput{Character: "hunter", Number: 1})
	assert.False(t, isError)
	assert.Equal(t, "Saved loadout 1 on Hunter with the name Raid Night. It holds the gear that Hunter "+
		"has equipped: Fatebringer (1001), Celestial Nighthawk (1004).", got)

	// The fake profile keeps the state from before both writes, so the
	// list reads the equip and the save from the snapshot.
	got, isError = call(t, f.h.handleListLoadouts, ListLoadoutsInput{Character: "hunter"})
	require.False(t, isError)
	assert.Equal(t, loadoutsIntro+"\n\nHunter (id c1):\nloadout | instance id | name | slot | location\n"+
		"Loadout 1: Raid Night\n"+
		"1 | 1001 | Fatebringer | Kinetic Weapons | Hunter (equipped)\n"+
		"1 | 1004 | Celestial Nighthawk | Helmet | Hunter (equipped)\n"+
		"Loadout 2: Raid Night\n"+
		"2 | 1006 | Fatebringer | Kinetic Weapons | Hunter\n"+
		"2 | 1004 | Celestial Nighthawk | Helmet | Hunter (equipped)\n"+
		"Loadout 3: Trials\n"+
		"3 | 1001 | Fatebringer | Kinetic Weapons | Hunter (equipped)\n"+
		"3 | 1004 | Celestial Nighthawk | Helmet | Hunter (equipped)\n"+
		"3 | 9999 | not in the inventory |  | ", got)
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1)
}

func TestLoadoutWriteSnapshotAge(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		write func(t *testing.T, f *fakes) bool
	}{
		"save": {
			write: func(t *testing.T, f *fakes) bool {
				t.Helper()

				_, isError := call(t, f.h.handleSaveLoadout, SaveLoadoutInput{Character: "hunter", Number: 1})

				return isError
			},
		},
		"clear": {
			write: func(t *testing.T, f *fakes) bool {
				t.Helper()

				_, isError := call(t, f.h.handleClearLoadout, ClearLoadoutInput{Character: "hunter", Number: 2})

				return isError
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
			require.False(t, isError)

			later := bungietest.Now.Add(10 * time.Second)
			f.h.now = func() time.Time { return later }

			require.False(t, tc.write(t, f))
			require.NotNil(t, f.h.snapshot, "a loadout write keeps the snapshot")
			assert.Equal(t, later, f.h.snapshot.Fetched, "a loadout write restarts the age of the snapshot")
		})
	}
}

func TestSaveLoadoutToolRefusals(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		edit func(f *fakes)
		want string
		in   SaveLoadoutInput
	}{
		"no character": {
			in:   SaveLoadoutInput{Number: 1},
			want: "unknown location: pass the character of the loadout as a character id or a class name",
		},
		"vault": {
			in:   SaveLoadoutInput{Character: "vault", Number: 1},
			want: "unknown location: the vault is not a character, so pass a character id or a class name",
		},
		"number after the last slot": {
			in: SaveLoadoutInput{Character: "hunter", Number: 4},
			want: "no loadout slot: Hunter has no loadout slot with the number 4. " +
				"Its 3 loadout slots have the numbers 1 to 3",
		},
		"number zero": {
			in: SaveLoadoutInput{Character: "hunter"},
			want: "no loadout slot: Hunter has no loadout slot with the number 0. " +
				"Its 3 loadout slots have the numbers 1 to 3",
		},
		"character with fewer slots": {
			in: SaveLoadoutInput{Character: "titan", Number: 2},
			want: "no loadout slot: Titan has no loadout slot with the number 2. " +
				"Its only loadout slot has the number 1",
		},
		"unknown name": {
			in: SaveLoadoutInput{Character: "hunter", Number: 2, Name: "Gambit"},
			want: `unknown loadout name: "Gambit". ` +
				"Pass one of these names in any letter case: Raid Night, Trials, Crucible",
		},
		"part of a name": {
			in: SaveLoadoutInput{Character: "hunter", Number: 1, Name: "Raid"},
			want: `unknown loadout name: "Raid". ` +
				"Pass one of these names in any letter case: Raid Night, Trials, Crucible",
		},
		"name table without names that the game shows": {
			edit: func(f *fakes) {
				f.bungie.Data[bungietest.PathLoadoutNames] = `{"10": {"hash": 10, "index": 0, "name": "", "redacted": true}}`
			},
			in:   SaveLoadoutInput{Character: "hunter", Number: 2, Name: "Trials"},
			want: `unknown loadout name: "Trials". The name table holds no name that the game shows`,
		},
		"name without the name table": {
			edit: func(f *fakes) { delete(f.bungie.Data, bungietest.PathLoadoutNames) },
			in:   SaveLoadoutInput{Character: "hunter", Number: 2, Name: "Trials"},
			want: "loadout names unavailable: fetching " + bungietest.PathLoadoutNames +
				": bungie: content file returned status 404",
		},
		"empty slot without the name table": {
			edit: func(f *fakes) {
				f.bungie.Data[bungietest.PathManifest] = `{"ErrorCode": 1, "Response": {"jsonWorldComponentContentPaths": {
					"en": {"DestinyLoadoutIconDefinition": "` + bungietest.PathLoadoutIcons + `",
						"DestinyLoadoutColorDefinition": "` + bungietest.PathLoadoutColors + `"}}}}`
			},
			in:   SaveLoadoutInput{Character: "hunter", Number: 1},
			want: "loadout names unavailable: the Bungie manifest lists no table of loadout names",
		},
		"empty slot without the icon table": {
			edit: func(f *fakes) { delete(f.bungie.Data, bungietest.PathLoadoutIcons) },
			in:   SaveLoadoutInput{Character: "hunter", Number: 1, Name: "Trials"},
			want: "loadout icons unavailable: fetching " + bungietest.PathLoadoutIcons +
				": bungie: content file returned status 404",
		},
		"empty slot without the color table": {
			edit: func(f *fakes) { f.bungie.Data[bungietest.PathLoadoutColors] = `{"31": {"hash": 31,` },
			in:   SaveLoadoutInput{Character: "hunter", Number: 1},
			want: "loadout colors unavailable: decoding " + bungietest.PathLoadoutColors +
				": reading the loadout color table: unexpected EOF",
		},
		"empty slot with only redacted icons": {
			edit: func(f *fakes) {
				f.bungie.Data[bungietest.PathLoadoutIcons] = `{"21": {"hash": 21, "index": 0, "redacted": true}}`
			},
			in:   SaveLoadoutInput{Character: "hunter", Number: 1},
			want: "loadout icons unavailable: the table holds no entry that the game shows",
		},
		"manifest does not load": {
			edit: func(f *fakes) { delete(f.bungie.Data, bungietest.PathManifest) },
			in:   SaveLoadoutInput{Character: "hunter", Number: 1},
			want: "loadout names unavailable: reading the Bungie manifest: bungie: ParameterParseFailure: no route",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			if tc.edit != nil {
				tc.edit(f)
			}

			got, isError := call(t, f.h.handleSaveLoadout, tc.in)
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)
			assert.Empty(t, bungiePosts(f), "a refusal sends no request")
		})
	}
}

func TestSaveLoadoutToolBungieError(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.bungie.ActionError = `{"ErrorCode": 1671, "ErrorStatus": "DestinyCannotPerformActionAtThisLocation", ` +
		`"Message": "Not here."}`

	got, isError := call(t, f.h.handleSaveLoadout, SaveLoadoutInput{Character: "hunter", Number: 2, Name: "Trials"})
	assert.True(t, isError)
	assert.Equal(t,
		"saving loadout 2 on Hunter: bungie: DestinyCannotPerformActionAtThisLocation: Not here.", got)

	requests := bungiePosts(f)
	require.Len(t, requests, 1)
	assert.Equal(t, pathSnapshot, requests[0].Path)
	assert.Equal(t, snapshotBody(1, bungietest.HunterID, 4, 2, 3), requests[0].Body)
	assert.NotNil(t, f.h.snapshot, "a save that Bungie refused keeps the snapshot")
}

func TestClearLoadoutTool(t *testing.T) {
	t.Parallel()

	hunterList := loadoutsIntro + "\n\nHunter (id c1):\nloadout | instance id | name | slot | location\n"

	tests := map[string]struct {
		wantBody map[string]any
		profile  string
		want     string
		// wantList is the list_loadouts result for the character of the
		// clear, right after the clear.
		wantList string
		in       ClearLoadoutInput
	}{
		"held and missing items": {
			in: ClearLoadoutInput{Character: "hunter", Number: 3},
			want: "Cleared loadout 3 on Hunter, which held Fatebringer (1001), Celestial Nighthawk (1004). " +
				"The inventory does not hold these loadout items: 9999.",
			wantBody: map[string]any{
				"loadoutIndex":   float64(2),
				"characterId":    bungietest.HunterID,
				"membershipType": float64(3),
			},
			wantList: hunterList + "Loadout 2: Raid Night\n" + hunterLoadoutRows(2, true),
		},
		"only held items": {
			in:   ClearLoadoutInput{Character: "hunter", Number: 2},
			want: "Cleared loadout 2 on Hunter, which held Fatebringer (1006), Celestial Nighthawk (1004).",
			wantBody: map[string]any{
				"loadoutIndex":   float64(1),
				"characterId":    bungietest.HunterID,
				"membershipType": float64(3),
			},
			wantList: hunterList + "Loadout 3: Trials\n" + hunterLoadoutRows(3, false),
		},
		"only missing items": {
			// The Titan saved a loadout whose items all left the
			// inventory.
			profile: `{"ErrorCode": 1, "Response": {
				"characters": {"data": {
					"c2": {"characterId": "c2", "classType": 0, "light": 1990, "dateLastPlayed": "2026-11-01T12:00:00Z"}
				}},
				"characterLoadouts": {"data": {"c2": {"loadouts": [
					{"nameHash": 7, "iconHash": 8, "colorHash": 9, "items": [
						{"itemInstanceId": "8001", "plugItemHashes": [2166136261]},
						{"itemInstanceId": "8002", "plugItemHashes": [2166136261]}
					]}
				]}}}
			}}`,
			in:   ClearLoadoutInput{Character: "titan", Number: 1},
			want: "Cleared loadout 1 on Titan. The inventory does not hold these loadout items: 8001, 8002.",
			wantBody: map[string]any{
				"loadoutIndex":   float64(0),
				"characterId":    bungietest.TitanID,
				"membershipType": float64(3),
			},
			wantList: loadoutsIntro + "\n\nTitan (id c2): no saved loadouts",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.bungie.ProfileBody = tc.profile

			got, isError := call(t, f.h.handleClearLoadout, tc.in)
			assert.False(t, isError)
			assert.Equal(t, tc.want, got)

			requests := bungiePosts(f)
			require.Len(t, requests, 1)
			assert.Equal(t, pathClear, requests[0].Path)
			assert.Equal(t, tc.wantBody, requests[0].Body)

			// The fake profile keeps the loadouts from before the clear, so
			// the list leaves out the loadout because the clear updated the
			// snapshot.
			got, isError = call(t, f.h.handleListLoadouts, ListLoadoutsInput{Character: tc.in.Character})
			require.False(t, isError)
			assert.Equal(t, tc.wantList, got)
			assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1)
		})
	}
}

func TestClearLoadoutToolErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		wantBody    map[string]any
		bungieError string
		want        string
		in          ClearLoadoutInput
		wantPosts   int
	}{
		"no character": {
			in:   ClearLoadoutInput{Number: 2},
			want: "unknown location: pass the character of the loadout as a character id or a class name",
		},
		"empty slot": {
			in:   ClearLoadoutInput{Character: "hunter", Number: 1},
			want: "empty loadout slot: Hunter has no saved loadout in slot 1, so there is nothing to clear",
		},
		"number after the last slot": {
			in: ClearLoadoutInput{Character: "titan", Number: 2},
			want: "no loadout slot: Titan has no loadout slot with the number 2. " +
				"Its only loadout slot has the number 1",
		},
		"bungie refuses the clear": {
			in: ClearLoadoutInput{Character: "hunter", Number: 2},
			bungieError: `{"ErrorCode": 1671, "ErrorStatus": "DestinyCannotPerformActionAtThisLocation", ` +
				`"Message": "Not here."}`,
			want:      "clearing loadout 2 on Hunter: bungie: DestinyCannotPerformActionAtThisLocation: Not here.",
			wantPosts: 1,
			wantBody: map[string]any{
				"loadoutIndex":   float64(1),
				"characterId":    bungietest.HunterID,
				"membershipType": float64(3),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.bungie.ActionError = tc.bungieError

			got, isError := call(t, f.h.handleClearLoadout, tc.in)
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)

			requests := bungiePosts(f)
			require.Len(t, requests, tc.wantPosts)

			if tc.wantPosts > 0 {
				assert.Equal(t, pathClear, requests[0].Path)
				assert.Equal(t, tc.wantBody, requests[0].Body)
				assert.NotNil(t, f.h.snapshot, "a clear that Bungie refused keeps the snapshot")
			}
		})
	}
}

func TestSetTag(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want     string
		wantRows []any
		in       SetTagInput
		isError  bool
	}{
		"replace and add": {
			in:   SetTagInput{InstanceIDs: []string{"1001", "1002"}, Tag: "Junk"},
			want: "Tagged 2 items as junk.",
			wantRows: []any{
				map[string]any{"instanceId": "1001", "itemHash": float64(100), "tagId": "keep", "removed": true},
				map[string]any{"instanceId": "1001", "itemHash": float64(100), "tagId": "junk", "removed": false},
				map[string]any{"instanceId": "1002", "itemHash": float64(101), "tagId": "junk", "removed": false},
			},
		},
		"tag one item": {
			in:   SetTagInput{InstanceIDs: []string{"1002"}, Tag: "keep"},
			want: "Tagged 1 item as keep.",
			wantRows: []any{
				map[string]any{"instanceId": "1002", "itemHash": float64(101), "tagId": "keep", "removed": false},
			},
		},
		"clear keeps the favorite": {
			in:   SetTagInput{InstanceIDs: []string{"1001"}, Tag: "none"},
			want: "Cleared the tag of 1 item.",
			wantRows: []any{
				map[string]any{"instanceId": "1001", "itemHash": float64(100), "tagId": "keep", "removed": true},
			},
		},
		"clear two items": {
			in:   SetTagInput{InstanceIDs: []string{"1001", "1002"}, Tag: "none"},
			want: "Cleared the tag of 2 items.",
			wantRows: []any{
				map[string]any{"instanceId": "1001", "itemHash": float64(100), "tagId": "keep", "removed": true},
			},
		},
		"unknown tag": {
			in:      SetTagInput{InstanceIDs: []string{"1001"}, Tag: "wishlist"},
			want:    `invalid input: tag "wishlist" is not none or one of keep, pvp, pve, junk, infuse, archive`,
			isError: true,
		},
		"unknown instance": {
			in:      SetTagInput{InstanceIDs: []string{"9"}, Tag: "keep"},
			want:    "invalid input: no item in the inventory has the instance id 9",
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			got, isError := call(t, f.h.handleSetTag, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)

			writes := f.service.Calls(http.MethodPost, pathTags)
			if tc.wantRows == nil {
				assert.Empty(t, writes)

				return
			}

			require.Len(t, writes, 1)
			assert.Equal(t, "Bearer session-1", writes[0].Auth)
			assert.Equal(t, map[string]any{"defs": []any{}, "items": tc.wantRows}, writes[0].Body)
		})
	}
}

func TestSetTagClearsRule(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		tag  string
		want string
	}{
		"the same tag":  {tag: "keep", want: "keep"},
		"another tag":   {tag: dvm.TagJunk, want: dvm.TagJunk},
		"no tag at all": {tag: inventory.TagNone},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.Data[pathTags] = tagRows(t, dvm.ItemTag{
				InstanceID: "1001",
				ItemHash:   100,
				TagID:      "keep",
				RuleID:     "rule-1",
				UpdatedAt:  5,
			})

			s, _, err := f.h.loadAnnotated(t.Context(), false)
			require.NoError(t, err)

			items, err := findItems(s, []string{"1001"})
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.Equal(t, "rule-1", items[0].TagRule)

			_, isError := call(t, f.h.handleSetTag, SetTagInput{InstanceIDs: []string{"1001"}, Tag: tc.tag})
			require.False(t, isError)

			// The cached snapshot holds the hand tag without the rule id.
			assert.Same(t, s, f.h.snapshot)
			assert.Equal(t, tc.want, items[0].Tag)
			assert.Empty(t, items[0].TagRule)
		})
	}
}

func TestSetFavorite(t *testing.T) {
	t.Parallel()

	favorite := func(id string, hash float64, removed bool) map[string]any {
		return map[string]any{"instanceId": id, "itemHash": hash, "tagId": "favorite", "removed": removed}
	}

	tests := map[string]struct {
		want     string
		wantRows []any
		in       SetFavoriteInput
	}{
		"mark one item": {
			in:       SetFavoriteInput{InstanceIDs: []string{"1002"}, Favorite: true},
			want:     "Marked 1 item as a favorite.",
			wantRows: []any{favorite("1002", 101, false)},
		},
		"mark two items": {
			in:       SetFavoriteInput{InstanceIDs: []string{"1001", "1002"}, Favorite: true},
			want:     "Marked 2 items as favorites.",
			wantRows: []any{favorite("1001", 100, false), favorite("1002", 101, false)},
		},
		"clear a repeated id once": {
			in:       SetFavoriteInput{InstanceIDs: []string{"1001", "1001"}, Favorite: false},
			want:     "Cleared the favorite mark of 1 item.",
			wantRows: []any{favorite("1001", 100, true)},
		},
		"clear two items": {
			in:       SetFavoriteInput{InstanceIDs: []string{"1001", "1002"}, Favorite: false},
			want:     "Cleared the favorite mark of 2 items.",
			wantRows: []any{favorite("1001", 100, true), favorite("1002", 101, true)},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			got, isError := call(t, f.h.handleSetFavorite, tc.in)
			assert.False(t, isError)
			assert.Equal(t, tc.want, got)

			writes := f.service.Calls(http.MethodPost, pathTags)
			require.Len(t, writes, 1)
			assert.Equal(t, tc.wantRows, writes[0].Body["items"])
		})
	}
}

func TestListTagsAndNotes(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleListTags, EmptyInput{})
	assert.False(t, isError)
	assert.Contains(t, got, "keep | Keep | 1\n")
	assert.Contains(t, got, "junk | Junk | 0\n")
	assert.True(t, strings.HasSuffix(got, "\nFavorites: 1 item"), got)

	got, isError = call(t, f.h.handleListNotes, EmptyInput{})
	assert.False(t, isError)
	assert.Equal(t, "- instance 1001 (Fatebringer): god roll\n- set set-key: farm this", got)
}

func TestSetNote(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		wantBody map[string]any
		want     string
		// notes replaces the notes that the fake service stores.
		notes   string
		in      SetNoteInput
		isError bool
	}{
		"instance note carries the item hash": {
			in:   SetNoteInput{TargetID: "1002", Body: "keep for raids"},
			want: "Saved the note.",
			wantBody: map[string]any{
				"kind": "instance", "targetId": "1002", "itemHash": float64(101),
				"body": "keep for raids", "removed": false,
			},
		},
		"blank body removes": {
			in:   SetNoteInput{TargetID: "set-key", Kind: "set", Body: "  "},
			want: "Removed the note.",
			wantBody: map[string]any{
				"kind": "set", "targetId": "set-key", "body": "", "removed": true,
			},
		},
		"unknown instance": {
			in:      SetNoteInput{TargetID: "9", Body: "x"},
			want:    "invalid input: no item in the inventory has the instance id 9",
			isError: true,
		},
		"removal takes the item hash of the stored note": {
			in: SetNoteInput{TargetID: "9"},
			notes: `{"notes": [
				{"kind": "instance", "targetId": "9", "itemHash": 555, "body": "dismantled", "removed": false}
			]}`,
			want: "Removed the note.",
			wantBody: map[string]any{
				"kind": "instance", "targetId": "9", "itemHash": float64(555), "body": "", "removed": true,
			},
		},
		"removal of an unknown instance without a note": {
			in:      SetNoteInput{TargetID: "9"},
			want:    "invalid input: no item in the inventory and no saved note has the instance id 9",
			isError: true,
		},
		"unknown kind": {
			in:      SetNoteInput{TargetID: "1001", Kind: "vendor", Body: "x"},
			want:    `invalid input: kind "vendor" is not instance, item, or set`,
			isError: true,
		},
		"body too long": {
			in:      SetNoteInput{TargetID: "1001", Body: strings.Repeat("x", maxNoteLength+1)},
			want:    "invalid input: a note needs a target_id and a body of at most 2000 characters",
			isError: true,
		},
		"body too long in UTF-16 code units": {
			in:      SetNoteInput{TargetID: "1001", Body: strings.Repeat("\U0001F600", maxNoteLength/2+1)},
			want:    "invalid input: a note needs a target_id and a body of at most 2000 characters",
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			if tc.notes != "" {
				f.service.Data[pathNotes] = tc.notes
			}

			got, isError := call(t, f.h.handleSetNote, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)

			writes := f.service.Calls(http.MethodPost, pathNotes)
			if tc.wantBody == nil {
				assert.Empty(t, writes)

				return
			}

			require.Len(t, writes, 1)
			assert.Equal(t, tc.wantBody, writes[0].Body)
		})
	}
}

func TestPerkPreferences(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleListPerkPreferences, EmptyInput{})
	assert.False(t, isError)
	assert.Equal(t, "pve up: Firefly\npvp down: Rampage", got)

	_, isError = call(t, f.h.handleSetPerkPreferences, SetPerkPreferencesInput{
		Preferences: []PerkPreferenceInput{
			{Mode: "PvP", Perk: "Kill Clip Enhanced", Stance: "up"},
			{Mode: "pve", Perk: "firefly", Stance: "none"},
			{Mode: "pve", Perk: " chill clip ", Stance: "Down"},
			{Mode: "pve", Perk: "Retired Perk", Stance: "none"},
		},
	})
	assert.False(t, isError)

	writes := f.service.Calls(http.MethodPost, pathPrefs)
	require.Len(t, writes, 1)
	assert.Equal(t, map[string]any{"preferences": []any{
		map[string]any{"mode": "pvp", "perk": "Kill Clip", "stance": "up", "removed": false},
		map[string]any{"mode": "pve", "perk": "Firefly", "stance": "up", "removed": true},
		map[string]any{"mode": "pve", "perk": "Chill Clip", "stance": "down", "removed": false},
		map[string]any{"mode": "pve", "perk": "Retired Perk", "stance": "up", "removed": true},
	}}, writes[0].Body)

	tests := map[string]struct {
		want string
		in   []PerkPreferenceInput
	}{
		"unknown mode": {
			in:   []PerkPreferenceInput{{Mode: "gambit", Perk: "Firefly", Stance: "up"}},
			want: `invalid input: mode "gambit" is not pve or pvp`,
		},
		"unknown stance": {
			in:   []PerkPreferenceInput{{Mode: "pve", Perk: "Firefly", Stance: "love"}},
			want: `invalid input: stance "love" is not up, down, or none`,
		},
		"no perk": {
			in:   []PerkPreferenceInput{{Mode: "pve", Stance: "up"}},
			want: "invalid input: a preference needs a perk name",
		},
		"partial name lists the traits that contain it": {
			in:   []PerkPreferenceInput{{Mode: "pve", Perk: "clip", Stance: "up"}},
			want: `invalid input: perk "clip" is not a weapon trait. Similar traits: Chill Clip, Kill Clip`,
		},
		"perk of another type": {
			in:   []PerkPreferenceInput{{Mode: "pve", Perk: "Arrowhead Brake", Stance: "up"}},
			want: `invalid input: perk "Arrowhead Brake" is not a weapon trait`,
		},
		"one bad entry rejects the batch": {
			in: []PerkPreferenceInput{
				{Mode: "pve", Perk: "Firefly", Stance: "up"},
				{Mode: "pve", Perk: "Fireflyy", Stance: "up"},
			},
			want: `invalid input: perk "Fireflyy" is not a weapon trait`,
		},
		"empty": {
			want: "invalid input: pass between 1 and 200 preferences",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			got, isError := call(t, f.h.handleSetPerkPreferences, SetPerkPreferencesInput{Preferences: tc.in})
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)
			assert.Empty(t, f.service.Calls(http.MethodPost, pathPrefs))
		})
	}
}

func TestPerkChangeFoldsNames(t *testing.T) {
	t.Parallel()

	defs := &definitions.Defs{Items: map[uint32]definitions.ItemDef{
		1: {Name: "Déjà Vu", TypeName: "Trait"},
		2: {Name: "Kill Clip", TypeName: "Trait"},
		3: {Name: "Kill Clip Enhanced", TypeName: "Enhanced Trait"},
	}}
	traits := defs.TraitNames()

	tests := map[string]struct {
		perk string
		want string
		err  string
	}{
		"name without its accents": {perk: "deja vu", want: "Déjà Vu"},
		"name with accents":        {perk: "KÍLL CLIP", want: "Kill Clip"},
		"enhanced name in another letter case": {
			perk: "kill clip enhanced",
			want: "Kill Clip",
		},
		"enhanced name with accents": {
			perk: "KILL CLÍP ÉNHANCED",
			want: "Kill Clip",
		},
		"part of a name without its accents": {
			perk: "deja",
			err:  "invalid input: perk \"deja\" is not a weapon trait. Similar traits: Déjà Vu",
		},
		"part of a name with accents": {
			perk: "DÉJÀ",
			err:  "invalid input: perk \"DÉJÀ\" is not a weapon trait. Similar traits: Déjà Vu",
		},
		"part of an enhanced name in another letter case": {
			perk: "KILL CL ENHANCED",
			err:  "invalid input: perk \"KILL CL ENHANCED\" is not a weapon trait. Similar traits: Kill Clip",
		},
		"accent marks only": {
			perk: "^",
			err:  "invalid input: perk \"^\" is not a weapon trait",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := perkChange(PerkPreferenceInput{Mode: "pve", Perk: tc.perk, Stance: "up"}, traits)
			if tc.err != "" {
				require.ErrorIs(t, err, ErrInvalidInput)
				require.EqualError(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, dvm.PerkPreference{Mode: "pve", Perk: tc.want, Stance: "up"}, got)
		})
	}
}

func TestPerkChangeReadsDigitsAsLetters(t *testing.T) {
	t.Parallel()

	// The trait of item 4 differs from Kill Clip only in a digit that
	// stands for a letter.
	defs := &definitions.Defs{Items: map[uint32]definitions.ItemDef{
		1: {Name: "Déjà Vu", TypeName: "Trait"},
		2: {Name: "Kill Clip", TypeName: "Trait"},
		3: {Name: "Kill Clip Enhanced", TypeName: "Enhanced Trait"},
		4: {Name: "K1ll Clip", TypeName: "Trait"},
		5: {Name: "Rampage", TypeName: "Trait"},
	}}
	traits := defs.TraitNames()

	tests := map[string]struct {
		perk string
		want string
		err  string
	}{
		"digit for a letter":                {perk: "rampag3", want: "Rampage"},
		"digits for letters and no accents": {perk: "D3JA VU", want: "Déjà Vu"},
		"enhanced suffix with a digit":      {perk: "Rampage 3nhanced", want: "Rampage"},
		"name in letters before a digit":    {perk: "kill clip", want: "Kill Clip"},
		"name with a digit before letters":  {perk: "K1LL CLIP", want: "K1ll Clip"},
		"enhanced name with a digit in it":  {perk: "k1ll clip enhanced", want: "K1ll Clip"},
		"enhanced suffix with a digit on two such traits": {
			perk: "kill clip 3nhanced",
			err:  "invalid input: perk \"kill clip 3nhanced\" is not a weapon trait. Similar traits: K1ll Clip, Kill Clip",
		},
		"traits that differ only in a digit and a letter": {
			perk: "kill cl1p",
			err:  "invalid input: perk \"kill cl1p\" is not a weapon trait. Similar traits: K1ll Clip, Kill Clip",
		},
		"part of a name with a digit": {
			perk: "rampa9",
			err:  "invalid input: perk \"rampa9\" is not a weapon trait. Similar traits: Rampage",
		},
		"another digit": {
			perk: "rampag2",
			err:  "invalid input: perk \"rampag2\" is not a weapon trait",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := perkChange(PerkPreferenceInput{Mode: "pve", Perk: tc.perk, Stance: "up"}, traits)
			if tc.err != "" {
				require.ErrorIs(t, err, ErrInvalidInput)
				require.EqualError(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, dvm.PerkPreference{Mode: "pve", Perk: tc.want, Stance: "up"}, got)
		})
	}
}

// fixtureRuleWithoutAction is an autotag body whose one rule has neither a
// tag nor the vault flag. The service accepts such a rule, so a client other
// than the web client can store one.
const fixtureRuleWithoutAction = `{"rules": [
	{"id": "r3", "query": "is:armor", "vault": false, "enabled": true, "position": 0, "removed": false, "updatedAt": 5}
], "state": {"enabled": true, "mark": "00000000000000001006", "updatedAt": 5}, "now": 10}`

// fixtureRuleWithOpenQuote is an autotag body whose one rule has a query
// with a quote that never closes.
const fixtureRuleWithOpenQuote = `{"rules": [
	{"id": "r3", "query": "name:\"abc", "tagId": "junk", "vault": false, "enabled": true, "position": 0, "removed": false, "updatedAt": 5}
], "state": {"enabled": true, "mark": "00000000000000001006", "updatedAt": 5}, "now": 10}`

func TestListAutotagRules(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		autotag string
		want    []string
	}{
		"rules in evaluation order": {
			want: []string{
				`- 0. [enabled] "rate:>=a": tag pve (id r1)`,
				`- 1. [disabled] "is:dupe": move to vault (id r2)`,
			},
		},
		"stored rule without an action": {
			autotag: fixtureRuleWithoutAction,
			want:    []string{`- 0. [enabled] "is:armor": no action (id r3)`},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			if tc.autotag != "" {
				f.service.Data[pathAutotag] = tc.autotag
			}

			got, isError := call(t, f.h.handleListAutotagRules, ListAutotagRulesInput{})
			assert.False(t, isError)
			assert.Equal(t, strings.Join(append([]string{
				"Autotagging is on.",
				"",
				"Rules in evaluation order. The first enabled rule that matches a new item wins.",
			}, tc.want...), "\n"), got)
		})
	}
}

// The items that the profile of newItemsProfile adds to bungietest.ProfileJSON.
// Each has an instance id above the stored mark of the fixture autotag
// body. The Hunter gains a copy of Fatebringer, Thorn, and Wormhusk Crown.
// No other item shares the name of Thorn or Wormhusk Crown. The vault
// gains a copy of Gjallarhorn.
const (
	newFatebringer = `{"itemHash": 100, "itemInstanceId": "1007", "lockable": true, "quantity": 1, ` +
		`"location": 1, "bucketHash": 1498876634}`
	newThorn = `{"itemHash": 103, "itemInstanceId": "1008", "lockable": true, "quantity": 1, ` +
		`"location": 1, "bucketHash": 1498876634}`
	newHelmet = `{"itemHash": 201, "itemInstanceId": "1009", "lockable": true, "quantity": 1, ` +
		`"location": 1, "bucketHash": 3448274439}`
	newVaultGjallarhorn = `{"itemHash": 101, "itemInstanceId": "1010", "lockable": true, "quantity": 1, ` +
		`"location": 2, "bucketHash": 138197802}`
)

// boundFatebringer is a new copy of Fatebringer on the Hunter that Bungie
// reports as not transferable. Its instance id is the id of newHelmet, so
// a profile holds at most one of the two.
const boundFatebringer = `{"itemHash": 100, "itemInstanceId": "1009", "lockable": true, "quantity": 1, ` +
	`"location": 1, "bucketHash": 1498876634, "transferStatus": 2}`

// newItemsProfile returns a Bungie profile response body that adds items
// to bungietest.ProfileJSON. Held holds the JSON items that the inventory of
// the Hunter gains, and vault holds the JSON items that the vault gains.
func newItemsProfile(t *testing.T, held, vault []string) string {
	t.Helper()

	body := bungietest.ProfileJSON

	for marker, items := range map[string][]string{
		"\"characterInventories\": {\"data\": {\n\t\t\"c1\": {\"items\": [": held,
		`"profileInventory": {"data": {"items": [`:                          vault,
	} {
		require.Equal(t, 1, strings.Count(body, marker))

		if len(items) > 0 {
			body = strings.Replace(body, marker, marker+"\n"+strings.Join(items, ",\n")+",", 1)
		}
	}

	return `{"ErrorCode": 1, "Response": ` + body + `}`
}

// addNewItemDefs adds to d the definitions of Thorn and Wormhusk Crown,
// which the profile of newItemsProfile can hold.
func addNewItemDefs(d *definitions.Defs) {
	d.Items[103] = definitions.ItemDef{
		Name:       "Thorn",
		TypeName:   "Hand Cannon",
		Tier:       "Exotic",
		Bucket:     bungie.BucketKinetic,
		ItemType:   bungie.ItemTypeWeapon,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}
	d.Items[201] = definitions.ItemDef{
		Name:       "Wormhusk Crown",
		TypeName:   "Helmet",
		Tier:       "Exotic",
		Bucket:     bungie.BucketHelmet,
		ItemType:   bungie.ItemTypeArmor,
		ClassType:  1,
		Equippable: true,
	}
}

// autotagBody returns an /api/autotag response body with rules, the
// JSON rules, and the state of the fixture.
func autotagBody(rules ...string) string {
	return `{"rules": [` + strings.Join(rules, ", ") +
		`], "state": {"enabled": true, "mark": "00000000000000001006", "updatedAt": 5}, "now": 10}`
}

// storedRule returns the JSON of an autotag rule that the service stores.
// The rule does not move items to the vault. Query and tagID hold ASCII
// characters only, which the %q verb quotes as JSON does.
func storedRule(id, query, tagID string, position int, enabled bool) string {
	return fmt.Sprintf(`{"id": %q, "query": %q, "tagId": %q, "vault": false, "enabled": %t, `+
		`"position": %d, "removed": false, "updatedAt": 5}`, id, query, tagID, enabled, position)
}

func TestListAutotagRulesNew(t *testing.T) {
	t.Parallel()

	const (
		header = "Autotagging is on. The next claim moves the stored mark from 00000000000000001006 " +
			"to 00000000000000001009 and offers 3 new items to the rules. Nothing was claimed."
		fatebringer = "  - Fatebringer (1007), Hunter"
		thorn       = "  - Thorn (1008), Hunter"
		helmet      = "  - Wormhusk Crown (1009), Hunter"
		noVault     = "New items already in the vault, which the next claim covers and no rule takes: 0"
		footer      = "The web client or run_autotag_rules claims these items and applies the rules, " +
			"whichever runs first with autotagging on."
		after = "The server does not evaluate the rules after it."
	)

	held := []string{newFatebringer, newThorn, newHelmet}
	weapon := storedRule("r2", "is:weapon", "keep", 1, true)

	tests := map[string]struct {
		autotag string
		profile string
		matches string
		want    []string
		// sheet makes the service publish previewAegisSheet as the only
		// roll sheet.
		sheet bool
		// rated marks a case that reads the roll sheets and the perk
		// preferences.
		rated   bool
		isError bool
		// helmetSet gives the helmet an item set that the definitions
		// lack.
		helmetSet uint32
	}{
		"first match": {
			autotag: autotagBody(storedRule("r1", "is:dupe", "junk", 0, true), weapon),
			want: []string{
				header, "",
				`- 0. [enabled] "is:dupe": tag junk (id r1). New items it takes: 1`, fatebringer,
				`- 1. [enabled] "is:weapon": tag keep (id r2). New items it takes: 1`, thorn, "",
				"New items that no rule takes: 1", helmet, "",
				noVault, "",
				footer,
			},
		},
		"the matches value in upper case with spaces": {
			autotag: autotagBody(storedRule("r1", "is:dupe", "junk", 0, true), weapon),
			matches: " NEW ",
			want: []string{
				header, "",
				`- 0. [enabled] "is:dupe": tag junk (id r1). New items it takes: 1`, fatebringer,
				`- 1. [enabled] "is:weapon": tag keep (id r2). New items it takes: 1`, thorn, "",
				"New items that no rule takes: 1", helmet, "",
				noVault, "",
				footer,
			},
		},
		"a rule without action": {
			autotag: autotagBody(storedRule("r3", "is:weapon", "", 0, true)),
			want: []string{
				header, "",
				`- 0. [enabled] "is:weapon": no action (id r3). New items it takes: 2`, fatebringer, thorn, "",
				"New items that no rule takes: 1", helmet, "",
				noVault, "",
				footer,
			},
		},
		"an unsupported rule blocks": {
			autotag: autotagBody(
				storedRule("r1", "is:masterwork", "junk", 0, true),
				storedRule("r3", "is:dupe", "junk", 2, true),
				weapon,
			),
			want: []string{
				header, "",
				`- 0. [enabled] "is:masterwork": tag junk (id r1). The server cannot evaluate this rule: ` +
					`the server does not evaluate is:masterwork. ` +
					`It cannot tell which rule takes the 3 new items that reach this rule:`,
				fatebringer, thorn, helmet,
				after, "",
				noVault, "",
				footer,
			},
		},
		"a server-only refusal blocks": {
			autotag: autotagBody(storedRule("r1", "is:weapon ) is:armor", "junk", 0, true), weapon),
			want: []string{
				header, "",
				`- 0. [enabled] "is:weapon ) is:armor": tag junk (id r1). The server cannot evaluate this rule: ` +
					`the web client runs this rule, and the server cannot evaluate it: ` +
					`the query has a ")" that closes no "(", and the web client ignores ") is:armor". ` +
					`It cannot tell which rule takes the 3 new items that reach this rule:`,
				fatebringer, thorn, helmet,
				after, "",
				noVault, "",
				footer,
			},
		},
		"a server-only refusal blocks before an invalid value": {
			autotag: autotagBody(storedRule("r1", "count:=<2 )", "junk", 0, true), weapon),
			want: []string{
				header, "",
				`- 0. [enabled] "count:=<2 )": tag junk (id r1). The server cannot evaluate this rule: ` +
					`the web client runs this rule, and the server cannot evaluate it: ` +
					`the query has a ")" that closes no "(", and the web client ignores ")". ` +
					`It cannot tell which rule takes the 3 new items that reach this rule:`,
				fatebringer, thorn, helmet,
				after, "",
				noVault, "",
				footer,
			},
		},
		"a DIM parse error skips": {
			autotag: autotagBody(storedRule("r1", "is:weapon -", "junk", 0, true), weapon),
			want: []string{
				header,
				"",
				`- 0. [enabled] "is:weapon -": tag junk (id r1). The web client skips this rule, so it takes nothing: ` +
					`the query ends after "-", which needs a search term after it.`,
				`- 1. [enabled] "is:weapon": tag keep (id r2). New items it takes: 2`,
				fatebringer,
				thorn,
				"",
				"New items that no rule takes: 1",
				helmet,
				"",
				noVault,
				"",
				footer,
			},
		},
		"an empty query skips": {
			autotag: autotagBody(storedRule("r1", " ", "junk", 0, true)),
			want: []string{
				header, "",
				`- 0. [enabled] " ": tag junk (id r1). The web client skips this rule, so it takes nothing: ` +
					`the query has no search term.`,
				"",
				"New items that no rule takes: 3", fatebringer, thorn, helmet, "",
				noVault, "",
				footer,
			},
		},
		"an invalid value skips": {
			autotag: autotagBody(storedRule("r1", "count:=<2", "junk", 0, true), weapon),
			want: []string{
				header, "",
				`- 0. [enabled] "count:=<2": tag junk (id r1). The web client skips this rule, so it takes nothing: ` +
					`the web client rejects count:=<2.`,
				`- 1. [enabled] "is:weapon": tag keep (id r2). New items it takes: 2`, fatebringer, thorn, "",
				"New items that no rule takes: 1", helmet, "",
				noVault, "",
				footer,
			},
		},
		"an undecided item stops": {
			autotag: autotagBody(storedRule("r1", "rate:>=a", "junk", 0, true), weapon),
			sheet:   true,
			rated:   true,
			want: []string{
				header, "",
				`- 0. [enabled] "rate:>=a": tag junk (id r1). New items it takes: 0`,
				`- 1. [enabled] "is:weapon": tag keep (id r2). New items it takes: 1`, thorn, "",
				"New items that the server cannot place: 1",
				`  - Fatebringer (1007), Hunter: undecided at rule 0 "rate:>=a": ` + search.ReasonNoPerks, "",
				"New items that no rule takes: 1", helmet, "",
				noVault, "",
				footer,
			},
		},
		// The web client builds no item whose item set its definitions
		// lack, so neither the mark nor the new items count the helmet.
		"a new item that the web client does not build": {
			autotag:   autotagBody(weapon),
			helmetSet: 4001,
			want: []string{
				"Autotagging is on. The next claim moves the stored mark from 00000000000000001006 " +
					"to 00000000000000001008 and offers 2 new items to the rules. Nothing was claimed.",
				"",
				`- 1. [enabled] "is:weapon": tag keep (id r2). New items it takes: 2`, fatebringer, thorn, "",
				"New items that no rule takes: 0", "",
				noVault, "",
				footer,
			},
		},
		"a game mode without ratings blocks": {
			autotag: autotagBody(storedRule("r1", "pvprate:>=a", "junk", 0, true)),
			sheet:   true,
			rated:   true,
			want: []string{
				header, "",
				`- 0. [enabled] "pvprate:>=a": tag junk (id r1). The server cannot evaluate this rule: ` +
					noPvPSheet + `. It cannot tell which rule takes the 3 new items that reach this rule:`,
				fatebringer, thorn, helmet, "",
				noVault, "",
				footer,
			},
		},
		"a disabled rule takes nothing": {
			autotag: autotagBody(storedRule("r1", "is:weapon", "junk", 0, false), weapon),
			want: []string{
				header, "",
				`- 0. [disabled] "is:weapon": tag junk (id r1). It is disabled and takes nothing.`,
				`- 1. [enabled] "is:weapon": tag keep (id r2). New items it takes: 2`, fatebringer, thorn, "",
				"New items that no rule takes: 1", helmet, "",
				noVault, "",
				footer,
			},
		},
		"a disabled rule that would block": {
			autotag: autotagBody(storedRule("r1", "is:masterwork", "junk", 0, false), weapon),
			want: []string{
				header, "",
				`- 0. [disabled] "is:masterwork": tag junk (id r1). It is disabled and takes nothing.`,
				`- 1. [enabled] "is:weapon": tag keep (id r2). New items it takes: 2`, fatebringer, thorn, "",
				"New items that no rule takes: 1", helmet, "",
				noVault, "",
				footer,
			},
		},
		"no rule": {
			autotag: autotagBody(),
			want: []string{
				header, "",
				"New items that no rule takes: 3", fatebringer, thorn, helmet, "",
				noVault, "",
				footer,
			},
		},
		"a new vault item": {
			autotag: autotagBody(weapon),
			profile: newItemsProfile(t, held, []string{newVaultGjallarhorn}),
			want: []string{
				"Autotagging is on. The next claim moves the stored mark from 00000000000000001006 " +
					"to 00000000000000001010 and offers 3 new items to the rules. Nothing was claimed.",
				"",
				`- 1. [enabled] "is:weapon": tag keep (id r2). New items it takes: 2`, fatebringer, thorn, "",
				"New items that no rule takes: 1", helmet, "",
				"New items already in the vault, which the next claim covers and no rule takes: 1", "",
				footer,
			},
		},
		"no new item": {
			autotag: autotagBody(weapon),
			profile: newItemsProfile(t, nil, nil),
			want: []string{
				"No owned item has an instance id above the stored mark 00000000000000001006, " +
					"so the next claim offers no item to the rules.",
			},
		},
		"autotagging off": {
			autotag: `{"rules": [], "state": {"enabled": false, "mark": "00000000000000001006", "updatedAt": 5}, "now": 10}`,
			want:    []string{"Autotagging is off, so no item waits for the rules."},
		},
		"autotagging never on": {
			autotag: `{"rules": [], "state": null, "now": 10}`,
			want:    []string{"Autotagging has never been turned on, so no item waits for the rules."},
		},
		"no character": {
			autotag: autotagBody(weapon),
			profile: `{"ErrorCode": 1, "Response": {
				"profileInventory": {"data": {"items": [` + newVaultGjallarhorn + `]}},
				"characters": {"data": {}}
			}}`,
			want: []string{"The account has no character, so the server cannot tell which items the rules would take."},
		},
		"a matches value of spaces lists the rules": {
			autotag: autotagBody(weapon),
			matches: "  ",
			want: []string{
				"Autotagging is on.", "",
				"Rules in evaluation order. The first enabled rule that matches a new item wins.",
				`- 1. [enabled] "is:weapon": tag keep (id r2)`,
			},
		},
		"another matches value": {
			autotag: autotagBody(weapon),
			matches: "all",
			want:    []string{`invalid input: matches "all" is not new or owned`},
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.Data[pathAutotag] = tc.autotag
			addNewItemDefs(f.defs)

			helmet := f.defs.Items[201]
			helmet.SetHash = tc.helmetSet
			f.defs.Items[201] = helmet

			if tc.sheet {
				withAegisSheet(f)
			}

			// The handler caches the fixture profile, which holds no new
			// item, so only a fresh profile finds the new items.
			_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
			require.False(t, isError)

			f.bungie.ProfileBody = tc.profile
			if f.bungie.ProfileBody == "" {
				f.bungie.ProfileBody = newItemsProfile(t, held, nil)
			}

			matches := tc.matches
			if matches == "" {
				matches = matchesNew
			}

			got, isError := call(t, f.h.handleListAutotagRules, ListAutotagRulesInput{Matches: matches})
			assert.Equal(t, tc.isError, isError, got)
			assert.Equal(t, strings.Join(tc.want, "\n"), got)

			if tc.rated {
				assert.NotEmpty(t, ratingRequests(f))
			} else {
				assert.Empty(t, ratingRequests(f))
			}

			assert.Empty(t, f.service.Calls(http.MethodPost, "/api/autotag/state"), "nothing is claimed")
			assert.Empty(t, f.service.Calls(http.MethodPost, pathTags), "nothing is tagged")
			assert.Empty(t, f.service.Calls(http.MethodPost, pathAutotag), "no rule changes")

			for _, req := range f.bungie.All() {
				assert.Equal(t, http.MethodGet, req.Method, "nothing moves: %s", req.Path)
			}
		})
	}
}

func TestRunAutotagRules(t *testing.T) {
	t.Parallel()

	const (
		stored = "00000000000000001006"
		once   = "A claim happens at most once. No runner offers the claimed items to an autotag rule again, " +
			"also when a step after the claim failed."
		claimed = "Claimed the new items. The stored mark moved from 00000000000000001006 " +
			"to 00000000000000001008."
		refusal = "run_autotag_rules claimed nothing, because the server cannot apply every enabled rule " +
			"the way the web client does:"
		stillClaims = "The web client still claims these items when it runs with autotagging on."
		claimFailed = "The claim request failed: service: POST /api/autotag/state returned status 500: " +
			"Internal error."
		unless   = "Unless the web client tagged them already, tag these items with set_tag:"
		noVault  = "New items already in the vault, which no rule takes: 0"
		noneLeft = "New items that no rule took: 0"
		turnOn   = "Turn it on with set_autotag_enabled first."
	)

	weapon := storedRule("r1", "is:weapon", "keep", 0, true)
	weaponLine := `- 0. [enabled] "is:weapon": tag keep (id r1)`
	thorn := []string{newThorn}
	internal := dvmtest.Answer{Status: http.StatusInternalServerError, Body: `{"error": "Internal error"}`}
	keepRow := map[string]any{
		"instanceId": "1008", "itemHash": float64(103), "tagId": "keep", "ruleId": "r1", "removed": false,
	}

	tests := map[string]struct {
		setup func(f *fakes)
		// held holds the JSON items that the inventory of the Hunter
		// gains. Profile replaces the whole profile when it is set.
		held    []string
		profile string
		autotag string
		want    []string
		// newest is the mark that the claim stores. A case with an empty
		// newest sends no claim.
		newest string
		// wantTags holds the rows of the one tag write. A nil wantTags
		// expects no tag write.
		wantTags []any
		// tagged holds the tag and the rule id that the cached snapshot
		// gives each instance id after the call.
		tagged map[string][2]string
		// sheet makes the service publish previewAegisSheet as the only
		// roll sheet.
		sheet bool
		// reread marks a failed claim request, after which the handler
		// reads the autotag state again.
		reread  bool
		isError bool
	}{
		"claims and tags": {
			autotag: autotagBody(weapon),
			held:    thorn,
			newest:  "00000000000000001008",
			want: []string{
				claimed, "",
				weaponLine + ". New items it took: 1", "  - Thorn (1008), Hunter", "",
				noneLeft, "",
				noVault, "",
				once,
			},
			wantTags: []any{keepRow},
			tagged:   map[string][2]string{"1008": {"keep", "r1"}},
		},
		"no enabled rule": {
			autotag: autotagBody(storedRule("r1", "is:weapon", "keep", 0, false)),
			held:    thorn,
			newest:  "00000000000000001008",
			want: []string{
				claimed, "",
				"New items that no rule took: 1", "  - Thorn (1008), Hunter", "",
				noVault, "",
				once,
			},
			tagged: map[string][2]string{"1008": {"", ""}},
		},
		"no rule": {
			autotag: autotagBody(),
			held:    thorn,
			newest:  "00000000000000001008",
			want: []string{
				claimed, "",
				"New items that no rule took: 1", "  - Thorn (1008), Hunter", "",
				noVault, "",
				once,
			},
			tagged: map[string][2]string{"1008": {"", ""}},
		},
		"replaces a hand tag and keeps the favorite": {
			autotag: autotagBody(weapon),
			held:    thorn,
			setup: func(f *fakes) {
				f.service.Data[pathTags] = tagRows(t,
					dvm.ItemTag{InstanceID: "1008", ItemHash: 103, TagID: dvm.TagJunk, UpdatedAt: 5},
					dvm.ItemTag{InstanceID: "1008", ItemHash: 103, TagID: dvm.TagFavorite, UpdatedAt: 5},
				)
			},
			newest: "00000000000000001008",
			want: []string{
				claimed, "",
				weaponLine + ". New items it took: 1", "  - Thorn (1008), Hunter", "",
				noneLeft, "",
				noVault, "",
				once,
			},
			wantTags: []any{
				map[string]any{"instanceId": "1008", "itemHash": float64(103), "tagId": "junk", "removed": true},
				keepRow,
			},
			tagged: map[string][2]string{"1008": {"keep", "r1"}},
		},
		"each item goes to its first rule": {
			autotag: autotagBody(
				storedRule("r1", "is:dupe", "junk", 0, true),
				storedRule("r2", "is:masterwork", "junk", 1, false),
				storedRule("r3", "is:weapon", "keep", 2, true),
			),
			held:   []string{newFatebringer, newThorn, newHelmet},
			newest: "00000000000000001009",
			want: []string{
				"Claimed the new items. The stored mark moved from 00000000000000001006 to 00000000000000001009.",
				"",
				`- 0. [enabled] "is:dupe": tag junk (id r1). New items it took: 1`, "  - Fatebringer (1007), Hunter",
				`- 2. [enabled] "is:weapon": tag keep (id r3). New items it took: 1`, "  - Thorn (1008), Hunter",
				"",
				"New items that no rule took: 1", "  - Wormhusk Crown (1009), Hunter", "",
				noVault, "",
				once,
			},
			wantTags: []any{
				map[string]any{
					"instanceId": "1007", "itemHash": float64(100), "tagId": "junk", "ruleId": "r1", "removed": false,
				},
				map[string]any{
					"instanceId": "1008", "itemHash": float64(103), "tagId": "keep", "ruleId": "r3", "removed": false,
				},
			},
			tagged: map[string][2]string{"1007": {"junk", "r1"}, "1008": {"keep", "r3"}, "1009": {"", ""}},
		},
		"a rule without action": {
			autotag: autotagBody(storedRule("r3", "is:weapon", "", 0, true)),
			held:    thorn,
			newest:  "00000000000000001008",
			want: []string{
				claimed, "",
				`- 0. [enabled] "is:weapon": no action (id r3). New items it took: 1`, "  - Thorn (1008), Hunter", "",
				noneLeft, "",
				noVault, "",
				once,
			},
			tagged: map[string][2]string{"1008": {"", ""}},
		},
		"only vault items are new": {
			autotag: autotagBody(weapon),
			profile: newItemsProfile(t, nil, []string{newVaultGjallarhorn}),
			newest:  "00000000000000001010",
			want: []string{
				"Claimed the new items. The stored mark moved from 00000000000000001006 to 00000000000000001010.",
				"",
				weaponLine + ". New items it took: 0", "",
				noneLeft, "",
				"New items already in the vault, which no rule takes: 1", "",
				once,
			},
		},
		"the autotag state is unreadable": {
			setup: func(f *fakes) { delete(f.service.Data, pathAutotag) },
			want: []string{
				"run_autotag_rules claimed nothing, because the server could not read the autotag state: " +
					"service: GET /api/autotag?since=0 returned status 404: Not found",
			},
			isError: true,
		},
		"autotagging never on": {
			autotag: `{"rules": [], "state": null, "now": 10}`,
			want:    []string{"Autotagging has never been turned on, so run_autotag_rules claimed nothing. " + turnOn},
			isError: true,
		},
		"autotagging off": {
			autotag: `{"rules": [], "state": {"enabled": false, "mark": "00000000000000001006", "updatedAt": 5}}`,
			want:    []string{"Autotagging is off, so run_autotag_rules claimed nothing. " + turnOn},
			isError: true,
		},
		"the inventory is unreadable": {
			autotag: autotagBody(weapon),
			profile: `{"ErrorCode": 5, "ErrorStatus": "SystemDisabled", "Message": "down"}`,
			want: []string{
				"run_autotag_rules claimed nothing, because the server could not read the inventory: " +
					"fetching the profile: bungie: SystemDisabled: down",
			},
			isError: true,
		},
		"no character": {
			autotag: autotagBody(weapon),
			profile: `{"ErrorCode": 1, "Response": {
				"profileInventory": {"data": {"items": [` + newVaultGjallarhorn + `]}},
				"characters": {"data": {}}
			}}`,
			want:    []string{"The account has no character, so run_autotag_rules claimed nothing."},
			isError: true,
		},
		"no new item": {
			autotag: autotagBody(weapon),
			profile: newItemsProfile(t, nil, nil),
			want: []string{
				"No owned item has an instance id above the stored mark 00000000000000001006, " +
					"so run_autotag_rules claimed nothing.",
			},
			isError: true,
		},
		"an empty query": {
			autotag: autotagBody(
				storedRule("r1", " ", "junk", 0, true),
				storedRule("r2", "is:weapon", "keep", 1, true),
			),
			held: thorn,
			want: []string{
				refusal,
				`- 0. [enabled] " ": tag junk (id r1). The web client skips this rule: the query has no search term.`,
				stillClaims,
			},
			isError: true,
		},
		"a DIM parse error": {
			autotag: autotagBody(storedRule("r1", "is:weapon -", "junk", 0, true)),
			held:    thorn,
			want: []string{
				refusal,
				`- 0. [enabled] "is:weapon -": tag junk (id r1). The web client skips this rule: ` +
					`the query ends after "-", which needs a search term after it.`,
				stillClaims,
			},
			isError: true,
		},
		"an invalid value": {
			autotag: autotagBody(storedRule("r1", "count:=<2", "junk", 0, true)),
			held:    thorn,
			want: []string{
				refusal,
				`- 0. [enabled] "count:=<2": tag junk (id r1). The web client skips this rule: ` +
					`the web client rejects count:=<2.`,
				stillClaims,
			},
			isError: true,
		},
		"an unsupported term": {
			autotag: autotagBody(storedRule("r1", "is:masterwork", "junk", 0, true)),
			held:    thorn,
			want: []string{
				refusal,
				`- 0. [enabled] "is:masterwork": tag junk (id r1). The server cannot evaluate this rule: ` +
					`the server does not evaluate is:masterwork.`,
				stillClaims,
			},
			isError: true,
		},
		"a server-only refusal": {
			autotag: autotagBody(storedRule("r1", "is:weapon ) is:armor", "junk", 0, true)),
			held:    thorn,
			want: []string{
				refusal,
				`- 0. [enabled] "is:weapon ) is:armor": tag junk (id r1). The server cannot evaluate this rule: ` +
					`the web client runs this rule, and the server cannot evaluate it: ` +
					`the query has a ")" that closes no "(", and the web client ignores ") is:armor".`,
				stillClaims,
			},
			isError: true,
		},
		"a PvP rating without the PvP sheet": {
			autotag: autotagBody(storedRule("r1", "pvprate:>=a", "junk", 0, true)),
			held:    thorn,
			sheet:   true,
			want: []string{
				refusal,
				`- 0. [enabled] "pvprate:>=a": tag junk (id r1). The server cannot evaluate this rule: ` +
					noPvPSheet + ".",
				stillClaims,
			},
			isError: true,
		},
		"an undecided item": {
			autotag: autotagBody(
				storedRule("r1", "rate:>=a", "junk", 0, true),
				storedRule("r2", "is:weapon", "keep", 1, true),
			),
			held:  []string{newFatebringer},
			sheet: true,
			want: []string{
				refusal,
				`- Fatebringer (1007): the server cannot decide whether rule 0 "rate:>=a" takes it: ` +
					search.ReasonNoPerks + ".",
				stillClaims,
			},
			isError: true,
		},
		"every cause is listed": {
			autotag: autotagBody(
				storedRule("r1", "is:masterwork", "junk", 0, true),
				storedRule("r2", "count:=<2", "junk", 1, true),
			),
			held: thorn,
			want: []string{
				refusal,
				`- 0. [enabled] "is:masterwork": tag junk (id r1). The server cannot evaluate this rule: ` +
					`the server does not evaluate is:masterwork.`,
				`- 1. [enabled] "count:=<2": tag junk (id r2). The web client skips this rule: ` +
					`the web client rejects count:=<2.`,
				stillClaims,
			},
			isError: true,
		},
		"the claim does not apply": {
			autotag: autotagBody(weapon),
			held:    thorn,
			setup:   func(f *fakes) { f.service.Claim = dvmtest.Answer{Status: http.StatusOK, Body: `{"applied": false}`} },
			newest:  "00000000000000001008",
			want: []string{
				"The service did not apply the claim, so run_autotag_rules tagged nothing. " +
					"Another runner, such as the open web client, claimed these items first, " +
					"or autotagging was turned off.",
			},
			isError: true,
		},
		"the claim does not apply with a vault rule": {
			autotag: autotagBody(vaultRule("r1", "is:weapon", "junk", 0)),
			held:    thorn,
			setup:   func(f *fakes) { f.service.Claim = dvmtest.Answer{Status: http.StatusOK, Body: `{"applied": false}`} },
			newest:  "00000000000000001008",
			want: []string{
				"The service did not apply the claim, so run_autotag_rules tagged nothing. " +
					"Another runner, such as the open web client, claimed these items first, " +
					"or autotagging was turned off.",
			},
			isError: true,
		},
		"the claim fails and the mark stays": {
			reread:  true,
			autotag: autotagBody(weapon),
			held:    thorn,
			setup: func(f *fakes) {
				f.service.Claim = internal
				f.service.ClaimedAutotag = dvmtest.Answer{Status: http.StatusOK, Body: autotagBody(weapon)}
			},
			newest: "00000000000000001008",
			want: []string{
				claimFailed + " The stored mark is still 00000000000000001006, " +
					"so run_autotag_rules claimed nothing and tagged nothing.",
			},
			isError: true,
		},
		"the claim fails and the mark stays with a vault rule": {
			reread:  true,
			autotag: autotagBody(vaultRule("r1", "is:weapon", "junk", 0)),
			held:    thorn,
			setup: func(f *fakes) {
				f.service.Claim = internal
				f.service.ClaimedAutotag = dvmtest.Answer{
					Status: http.StatusOK,
					Body:   autotagBody(vaultRule("r1", "is:weapon", "junk", 0)),
				}
			},
			newest: "00000000000000001008",
			want: []string{
				claimFailed + " The stored mark is still 00000000000000001006, " +
					"so run_autotag_rules claimed nothing and tagged nothing.",
			},
			isError: true,
		},
		"the claim fails and the mark moved": {
			reread:  true,
			autotag: autotagBody(weapon),
			held:    thorn,
			setup: func(f *fakes) {
				f.service.Claim = internal
				f.service.ClaimedAutotag = dvmtest.Answer{Status: http.StatusOK, Body: `{"rules": [` + weapon + `], ` +
					`"state": {"enabled": true, "mark": "00000000000000001008", "updatedAt": 6}}`}
			},
			newest: "00000000000000001008",
			want: []string{
				claimFailed + " The stored mark moved from 00000000000000001006 to 00000000000000001008, " +
					"so the claim might have applied, for this call or for another runner. " +
					"run_autotag_rules tagged nothing.",
				"",
				unless,
				`- Thorn (1008): tag keep (rule 0 "is:weapon")`,
				"",
				once,
			},
			isError: true,
		},
		"the claim fails and the mark moved with a vault rule": {
			reread:  true,
			autotag: autotagBody(vaultRule("r1", "is:weapon", "junk", 0)),
			held:    []string{newThorn, boundFatebringer},
			setup: func(f *fakes) {
				f.service.Claim = internal
				f.service.ClaimedAutotag = dvmtest.Answer{Status: http.StatusOK, Body: `{"rules": [], ` +
					`"state": {"enabled": true, "mark": "00000000000000001009", "updatedAt": 6}}`}
			},
			newest: "00000000000000001009",
			want: []string{
				claimFailed + " The stored mark moved from 00000000000000001006 to 00000000000000001009, " +
					"so the claim might have applied, for this call or for another runner. " +
					"run_autotag_rules tagged nothing.",
				"",
				unless,
				`- Thorn (1008): tag junk (rule 0 "is:weapon")`,
				`- Fatebringer (1009): tag junk (rule 0 "is:weapon")`,
				"",
				"Unless the web client moved them already, move these items to the vault with move_items:",
				`- Thorn (1008) (rule 0 "is:weapon")`,
				"",
				once,
			},
			isError: true,
		},
		"the claim fails and the mark moved without a rule tag": {
			reread:  true,
			autotag: autotagBody(storedRule("r3", "is:weapon", "", 0, true)),
			held:    thorn,
			setup: func(f *fakes) {
				f.service.Claim = internal
				f.service.ClaimedAutotag = dvmtest.Answer{Status: http.StatusOK, Body: `{"rules": [], ` +
					`"state": {"enabled": true, "mark": "00000000000000001008", "updatedAt": 6}}`}
			},
			newest: "00000000000000001008",
			want: []string{
				claimFailed + " The stored mark moved from 00000000000000001006 to 00000000000000001008, " +
					"so the claim might have applied, for this call or for another runner. " +
					"run_autotag_rules tagged nothing.",
				"",
				once,
			},
			isError: true,
		},
		"the claim fails and the state is unreadable": {
			reread:  true,
			autotag: autotagBody(weapon),
			held:    thorn,
			setup: func(f *fakes) {
				f.service.Claim = internal
				f.service.ClaimedAutotag = internal
			},
			newest: "00000000000000001008",
			want: []string{
				claimFailed + " The server could not read the autotag state again: " +
					"service: GET /api/autotag?since=0 returned status 500: Internal error. " +
					"The claim might have applied. run_autotag_rules tagged nothing.",
				"",
				unless,
				`- Thorn (1008): tag keep (rule 0 "is:weapon")`,
				"",
				once,
			},
			isError: true,
		},
		"the claim fails and the state is unreadable with a vault rule": {
			reread:  true,
			autotag: autotagBody(vaultRule("r1", "is:weapon", "", 0)),
			held:    thorn,
			setup: func(f *fakes) {
				f.service.Claim = internal
				f.service.ClaimedAutotag = internal
			},
			newest: "00000000000000001008",
			want: []string{
				claimFailed + " The server could not read the autotag state again: " +
					"service: GET /api/autotag?since=0 returned status 500: Internal error. " +
					"The claim might have applied. run_autotag_rules tagged nothing.",
				"",
				"Unless the web client moved them already, move these items to the vault with move_items:",
				`- Thorn (1008) (rule 0 "is:weapon")`,
				"",
				once,
			},
			isError: true,
		},
		"the claim fails and the state is gone": {
			reread:  true,
			autotag: autotagBody(weapon),
			held:    thorn,
			setup: func(f *fakes) {
				f.service.Claim = internal
				f.service.ClaimedAutotag = dvmtest.Answer{
					Status: http.StatusOK,
					Body:   `{"rules": [], "state": null, "now": 10}`,
				}
			},
			newest: "00000000000000001008",
			want: []string{
				claimFailed + " The server could not read the autotag state again: " +
					"service: the service returned no autotag state. " +
					"The claim might have applied. run_autotag_rules tagged nothing.",
				"",
				unless,
				`- Thorn (1008): tag keep (rule 0 "is:weapon")`,
				"",
				once,
			},
			isError: true,
		},
		"the tag write fails": {
			autotag: autotagBody(weapon),
			held:    thorn,
			setup:   func(f *fakes) { f.service.TagWriteStatus = http.StatusInternalServerError },
			newest:  "00000000000000001008",
			want: []string{
				claimed,
				"",
				"The tag write failed: writing tags: service: POST /api/tags returned status 500: " +
					"Tag store unavailable. Tag these items with set_tag:",
				"- Thorn (1008): tag keep",
				"",
				once,
			},
			wantTags: []any{keepRow},
			tagged:   map[string][2]string{"1008": {"", ""}},
			isError:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.Data[pathAutotag] = tc.autotag
			addNewItemDefs(f.defs)

			if tc.sheet {
				withAegisSheet(f)
			}

			if tc.setup != nil {
				tc.setup(f)
			}

			// The handler caches the fixture profile, which holds no new
			// item, so only a fresh profile finds the new items.
			_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
			require.False(t, isError)

			f.bungie.ProfileBody = tc.profile
			if f.bungie.ProfileBody == "" {
				f.bungie.ProfileBody = newItemsProfile(t, tc.held, nil)
			}

			got, isError := call(t, f.h.handleRunAutotagRules, EmptyInput{})
			assert.Equal(t, tc.isError, isError, got)
			assert.Equal(t, strings.Join(tc.want, "\n"), got)

			claims := f.service.Claims()
			if tc.newest == "" {
				assert.Empty(t, claims, "nothing is claimed")
			} else {
				require.Len(t, claims, 1)
				assert.Equal(t, "Bearer session-1", claims[0].Auth)
				assert.Equal(t, map[string]any{"kind": "claim", "from": stored, "to": tc.newest}, claims[0].Body)
			}

			writes := f.service.Calls(http.MethodPost, pathTags)
			if tc.wantTags == nil {
				assert.Empty(t, writes, "nothing is tagged")
			} else {
				require.Len(t, writes, 1)
				assert.Equal(t, map[string]any{"defs": []any{}, "items": tc.wantTags}, writes[0].Body)
			}

			// The cached snapshot holds the tags that the call wrote.
			for id, want := range tc.tagged {
				items, err := findItems(f.h.snapshot, []string{id})
				require.NoError(t, err)
				assert.Equal(t, want, [2]string{items[0].Tag, items[0].TagRule}, id)
			}

			reads := 1
			if tc.reread {
				reads = 2
			}

			assert.Len(t, f.service.Calls(http.MethodGet, pathAutotag), reads)
			assert.Empty(t, f.service.Calls(http.MethodPost, pathAutotag), "no rule changes")

			for _, req := range f.bungie.All() {
				assert.Equal(t, http.MethodGet, req.Method, "nothing moves: %s", req.Path)
			}
		})
	}
}

// vaultRule returns the JSON of an enabled autotag rule that the service
// stores with the vault flag. An empty tagID gives a rule without a tag.
// Query and tagID hold ASCII characters only, which the %q verb quotes as
// JSON does.
func vaultRule(id, query, tagID string, position int) string {
	return fmt.Sprintf(`{"id": %q, "query": %q, "tagId": %q, "vault": true, "enabled": true, `+
		`"position": %d, "removed": false, "updatedAt": 5}`, id, query, tagID, position)
}

func TestRunAutotagRulesVault(t *testing.T) {
	t.Parallel()

	const (
		pathPull = "/Destiny2/Actions/Items/PullFromPostmaster/"
		once     = "A claim happens at most once. No runner offers the claimed items to an autotag rule again, " +
			"also when a step after the claim failed."
		claimed = "Claimed the new items. The stored mark moved from 00000000000000001006 " +
			"to 00000000000000001008."
		noVault     = "New items already in the vault, which no rule takes: 0"
		noneLeft    = "New items that no rule took: 0"
		ruleLine    = `- 0. [enabled] "is:weapon": tag junk, move to vault (id r1)`
		thornOut    = "  - Thorn (1008)"
		thornFailed = "  - Thorn (1008) did not move: transferring to the vault: " +
			"bungie: DestinyItemNotFound: gone. It is on Hunter."
		fatebringerFailed = "  - Fatebringer (1007) did not move: transferring to the vault: " +
			"bungie: DestinyItemNotFound: gone. It is on Hunter."
		toVault = "1008 to the vault from c1"
		// postmasterThorn is the new Thorn 1008 at the Postmaster of the
		// Hunter, in the bucket of the lost items.
		postmasterThorn = `{"itemHash": 103, "itemInstanceId": "1008", "lockable": true, "quantity": 1, ` +
			`"location": 4, "bucketHash": 215593132, "transferStatus": 0}`
		// equipmentMarker opens the equipment of the Hunter in
		// bungietest.ProfileJSON.
		equipmentMarker = "\"characterEquipment\": {\"data\": {\n\t\t\"c1\": {\"items\": ["
	)

	// heldThorn returns the JSON of the new Thorn 1008 in the kinetic
	// bucket of the Hunter. Status is the transfer status of the item.
	heldThorn := func(status int) string {
		return fmt.Sprintf(`{"itemHash": 103, "itemInstanceId": "1008", "lockable": true, "quantity": 1, `+
			`"location": 1, "bucketHash": 1498876634, "transferStatus": %d}`, status)
	}

	junkRow := func(id string, hash float64) map[string]any {
		return map[string]any{"instanceId": id, "itemHash": hash, "tagId": "junk", "ruleId": "r1", "removed": false}
	}

	weaponJunk := autotagBody(vaultRule("r1", "is:weapon", "junk", 0))

	tests := map[string]struct {
		setup func(f *fakes)
		// held holds the JSON items that the inventory of the Hunter
		// gains, and equipped holds those that its equipment gains.
		held     []string
		equipped []string
		autotag  string
		newest   string
		want     []string
		// wantTags holds the rows of the one tag write. A nil wantTags
		// expects no tag write.
		wantTags []any
		// steps describes each Bungie action of the call in order.
		steps []string
		// locations holds the location that the cached snapshot gives
		// each instance id after the call.
		locations map[string]string
		isError   bool
		// moved marks a call whose moves changed an item, which resets
		// the fetch time of the cached snapshot.
		moved bool
	}{
		"moves a vault rule item": {
			autotag: weaponJunk,
			held:    []string{newThorn},
			newest:  "00000000000000001008",
			want: []string{
				claimed, "",
				ruleLine + ". New items it took: 1", "  - Thorn (1008), Vault", "",
				noneLeft, "",
				noVault, "",
				"Moved to the vault: 1", thornOut, "",
				once,
			},
			wantTags:  []any{junkRow("1008", 103)},
			steps:     []string{toVault},
			locations: map[string]string{"1008": "Vault"},
			moved:     true,
		},
		"leaves an equipped item in place": {
			autotag: weaponJunk,
			equipped: []string{`{"itemHash": 101, "itemInstanceId": "1008", "lockable": true, "quantity": 1, ` +
				`"location": 1, "bucketHash": 953998645, "transferStatus": 1}`},
			newest: "00000000000000001008",
			want: []string{
				claimed, "",
				ruleLine + ". New items it took: 1", "  - Gjallarhorn (1008), Hunter (equipped)", "",
				noneLeft, "",
				noVault, "",
				"Left in place: 1", "  - Gjallarhorn (1008) at Hunter (equipped): it is equipped", "",
				once,
			},
			wantTags:  []any{junkRow("1008", 101)},
			locations: map[string]string{"1008": "Hunter (equipped)"},
		},
		"leaves a status 2 item in place": {
			autotag: weaponJunk,
			held:    []string{heldThorn(2)},
			newest:  "00000000000000001008",
			want: []string{
				claimed, "",
				ruleLine + ". New items it took: 1", "  - Thorn (1008), Hunter", "",
				noneLeft, "",
				noVault, "",
				"Left in place: 1", "  - Thorn (1008) at Hunter: Bungie reports it as not transferable", "",
				once,
			},
			wantTags:  []any{junkRow("1008", 103)},
			locations: map[string]string{"1008": "Hunter"},
		},
		"pulls from the Postmaster": {
			autotag: weaponJunk,
			held:    []string{postmasterThorn},
			newest:  "00000000000000001008",
			want: []string{
				claimed, "",
				ruleLine + ". New items it took: 1", "  - Thorn (1008), Vault", "",
				noneLeft, "",
				noVault, "",
				"Moved to the vault: 1", thornOut, "",
				once,
			},
			wantTags:  []any{junkRow("1008", 103)},
			steps:     []string{"pull 1008 to c1", toVault},
			locations: map[string]string{"1008": "Vault"},
			moved:     true,
		},
		"leaves a non-transferable definition in place": {
			autotag: weaponJunk,
			held:    []string{heldThorn(0)},
			setup: func(f *fakes) {
				thorn := f.defs.Items[103]
				thorn.NonTransferable = true
				f.defs.Items[103] = thorn
			},
			newest: "00000000000000001008",
			want: []string{
				claimed, "",
				ruleLine + ". New items it took: 1", "  - Thorn (1008), Hunter", "",
				noneLeft, "",
				noVault, "",
				"Left in place: 1",
				"  - Thorn (1008) at Hunter: " +
					"the server treats it as not transferable, but the web client tries the transfer",
				"",
				once,
			},
			wantTags:  []any{junkRow("1008", 103)},
			locations: map[string]string{"1008": "Hunter"},
		},
		"a failed transfer": {
			autotag: weaponJunk,
			held:    []string{newFatebringer, newThorn},
			setup: func(f *fakes) {
				f.bungie.ActionError = goneError
				f.bungie.ActionErrorIf = func(body map[string]any) bool { return body["itemId"] == "1007" }
			},
			newest: "00000000000000001008",
			want: []string{
				claimed, "",
				ruleLine + ". New items it took: 2", "  - Fatebringer (1007), Hunter", "  - Thorn (1008), Vault", "",
				noneLeft, "",
				noVault, "",
				"Moved to the vault: 1", thornOut, "",
				"Moves that failed: 1", fatebringerFailed, "",
				once,
			},
			wantTags:  []any{junkRow("1007", 100), junkRow("1008", 103)},
			steps:     []string{"1007 to the vault from c1", toVault},
			locations: map[string]string{"1007": "Hunter", "1008": "Vault"},
			isError:   true,
			moved:     true,
		},
		"moves, leaves in place, and fails in one call": {
			autotag: weaponJunk,
			held:    []string{newFatebringer, newThorn, boundFatebringer},
			setup: func(f *fakes) {
				f.bungie.ActionError = goneError
				f.bungie.ActionErrorIf = func(body map[string]any) bool { return body["itemId"] == "1007" }
			},
			newest: "00000000000000001009",
			want: []string{
				"Claimed the new items. The stored mark moved from 00000000000000001006 to 00000000000000001009.",
				"",
				ruleLine + ". New items it took: 3",
				"  - Fatebringer (1007), Hunter", "  - Thorn (1008), Vault", "  - Fatebringer (1009), Hunter", "",
				noneLeft, "",
				noVault, "",
				"Moved to the vault: 1", thornOut, "",
				"Left in place: 1", "  - Fatebringer (1009) at Hunter: Bungie reports it as not transferable", "",
				"Moves that failed: 1", fatebringerFailed, "",
				once,
			},
			wantTags:  []any{junkRow("1007", 100), junkRow("1008", 103), junkRow("1009", 100)},
			steps:     []string{"1007 to the vault from c1", toVault},
			locations: map[string]string{"1007": "Hunter", "1008": "Vault", "1009": "Hunter"},
			isError:   true,
			moved:     true,
		},
		"a failed transfer alone": {
			autotag: weaponJunk,
			held:    []string{newThorn},
			setup:   func(f *fakes) { f.bungie.ActionError = goneError },
			newest:  "00000000000000001008",
			want: []string{
				claimed, "",
				ruleLine + ". New items it took: 1", "  - Thorn (1008), Hunter", "",
				noneLeft, "",
				noVault, "",
				"Moves that failed: 1", thornFailed, "",
				once,
			},
			wantTags:  []any{junkRow("1008", 103)},
			steps:     []string{toVault},
			locations: map[string]string{"1008": "Hunter"},
			isError:   true,
		},
		"a failed transfer after a Postmaster pull": {
			autotag: weaponJunk,
			held:    []string{postmasterThorn},
			setup: func(f *fakes) {
				f.bungie.ActionError = goneError
				f.bungie.ActionErrorIf = func(body map[string]any) bool {
					_, ok := body["transferToVault"]

					return ok
				}
			},
			newest: "00000000000000001008",
			want: []string{
				claimed, "",
				ruleLine + ". New items it took: 1", "  - Thorn (1008), Hunter", "",
				noneLeft, "",
				noVault, "",
				"Moves that failed: 1", thornFailed, "",
				once,
			},
			wantTags:  []any{junkRow("1008", 103)},
			steps:     []string{"pull 1008 to c1", toVault},
			locations: map[string]string{"1008": "Hunter"},
			isError:   true,
			// The pull changed the item, although the transfer failed.
			moved: true,
		},
		"moves in instance id order": {
			autotag: autotagBody(vaultRule("r1", "is:armor", "", 0), vaultRule("r2", "is:weapon", "", 1)),
			held:    []string{newHelmet, newThorn},
			newest:  "00000000000000001009",
			want: []string{
				"Claimed the new items. The stored mark moved from 00000000000000001006 to 00000000000000001009.",
				"",
				`- 0. [enabled] "is:armor": move to vault (id r1). New items it took: 1`,
				"  - Wormhusk Crown (1009), Vault",
				`- 1. [enabled] "is:weapon": move to vault (id r2). New items it took: 1`,
				"  - Thorn (1008), Vault",
				"",
				noneLeft, "",
				noVault, "",
				"Moved to the vault: 2", thornOut, "  - Wormhusk Crown (1009)", "",
				once,
			},
			steps:     []string{toVault, "1009 to the vault from c1"},
			locations: map[string]string{"1008": "Vault", "1009": "Vault"},
			moved:     true,
		},
		"the item of another rule stays": {
			autotag: autotagBody(
				storedRule("r2", "is:armor", "keep", 0, true),
				vaultRule("r1", "is:weapon", "junk", 1),
			),
			held:   []string{newThorn, newHelmet},
			newest: "00000000000000001009",
			want: []string{
				"Claimed the new items. The stored mark moved from 00000000000000001006 to 00000000000000001009.",
				"",
				`- 0. [enabled] "is:armor": tag keep (id r2). New items it took: 1`,
				"  - Wormhusk Crown (1009), Hunter",
				`- 1. [enabled] "is:weapon": tag junk, move to vault (id r1). New items it took: 1`,
				"  - Thorn (1008), Vault",
				"",
				noneLeft, "",
				noVault, "",
				"Moved to the vault: 1", thornOut, "",
				once,
			},
			wantTags: []any{
				map[string]any{
					"instanceId": "1009", "itemHash": float64(201), "tagId": "keep", "ruleId": "r2", "removed": false,
				},
				junkRow("1008", 103),
			},
			steps:     []string{toVault},
			locations: map[string]string{"1008": "Vault", "1009": "Hunter"},
			moved:     true,
		},
		"the tag write fails": {
			autotag: weaponJunk,
			held:    []string{newThorn},
			setup:   func(f *fakes) { f.service.TagWriteStatus = http.StatusInternalServerError },
			newest:  "00000000000000001008",
			want: []string{
				claimed,
				"",
				"The tag write failed: writing tags: service: POST /api/tags returned status 500: " +
					"Tag store unavailable. Tag these items with set_tag:",
				"- Thorn (1008): tag junk",
				"",
				"Moved to the vault: 1", thornOut, "",
				once,
			},
			wantTags:  []any{junkRow("1008", 103)},
			steps:     []string{toVault},
			locations: map[string]string{"1008": "Vault"},
			isError:   true,
			moved:     true,
		},
		"a vault rule without a tag": {
			autotag: autotagBody(vaultRule("r1", "is:weapon", "", 0)),
			held:    []string{newThorn},
			newest:  "00000000000000001008",
			want: []string{
				claimed,
				"",
				`- 0. [enabled] "is:weapon": move to vault (id r1). New items it took: 1`,
				"  - Thorn (1008), Vault",
				"",
				noneLeft,
				"",
				noVault,
				"",
				"Moved to the vault: 1",
				thornOut,
				"",
				once,
			},
			steps:     []string{toVault},
			locations: map[string]string{"1008": "Vault"},
			moved:     true,
		},
		"no refusal for a vault rule": {
			autotag: autotagBody(vaultRule("r1", "is:armor", "", 0)),
			held:    []string{newThorn},
			newest:  "00000000000000001008",
			want: []string{
				claimed, "",
				`- 0. [enabled] "is:armor": move to vault (id r1). New items it took: 0`, "",
				"New items that no rule took: 1", "  - Thorn (1008), Hunter", "",
				noVault, "",
				once,
			},
			locations: map[string]string{"1008": "Hunter"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.Data[pathAutotag] = tc.autotag
			addNewItemDefs(f.defs)

			if tc.setup != nil {
				tc.setup(f)
			}

			// The handler caches the fixture profile, which holds no new
			// item, so only a fresh profile finds the new items.
			_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
			require.False(t, isError)

			f.bungie.ProfileBody = newItemsProfile(t, tc.held, nil)
			if len(tc.equipped) > 0 {
				require.Equal(t, 1, strings.Count(f.bungie.ProfileBody, equipmentMarker))

				f.bungie.ProfileBody = strings.Replace(f.bungie.ProfileBody, equipmentMarker,
					equipmentMarker+"\n"+strings.Join(tc.equipped, ",\n")+",", 1)
			}

			// The clock moves 10 seconds with each Bungie action, so the
			// fetch time tells a snapshot that a move changed from a
			// fresh one.
			f.h.now = func() time.Time { return bungietest.Now.Add(time.Duration(len(bungiePosts(f))) * 10 * time.Second) }

			// writesAtMove holds the service writes that the fake service
			// had received when each Bungie action arrived.
			var (
				mu           sync.Mutex
				writesAtMove [][]string
			)

			f.bungie.OnAction = func() {
				writes := serviceWrites(f)

				mu.Lock()
				defer mu.Unlock()

				writesAtMove = append(writesAtMove, writes)
			}

			got, isError := call(t, f.h.handleRunAutotagRules, EmptyInput{})
			assert.Equal(t, tc.isError, isError, got)
			assert.Equal(t, strings.Join(tc.want, "\n"), got)

			// The claim and the tag write, also a failed one, precede every
			// Bungie action, as in the web client.
			mu.Lock()
			atMoves := slices.Clone(writesAtMove)
			mu.Unlock()

			require.Len(t, atMoves, len(tc.steps))

			for i, at := range atMoves {
				assert.Equal(t, serviceWrites(f), at, "service writes before Bungie action %d", i)
			}

			claims := f.service.Claims()
			require.Len(t, claims, 1)
			assert.Equal(t, map[string]any{
				"kind": "claim", "from": "00000000000000001006", "to": tc.newest,
			}, claims[0].Body)

			writes := f.service.Calls(http.MethodPost, pathTags)
			if tc.wantTags == nil {
				assert.Empty(t, writes, "nothing is tagged")
			} else {
				require.Len(t, writes, 1)
				assert.Equal(t, map[string]any{"defs": []any{}, "items": tc.wantTags}, writes[0].Body)
			}

			var steps []string

			for _, req := range bungiePosts(f) {
				switch req.Path {
				case pathPull:
					steps = append(steps, fmt.Sprintf("pull %v to %v", req.Body["itemId"], req.Body["characterId"]))
				case pathTransfer:
					steps = append(steps, transferSteps([]httpfake.Request{req})...)
				default:
					steps = append(steps, req.Path)
				}
			}

			assert.Equal(t, tc.steps, steps)

			for id, want := range tc.locations {
				items, err := findItems(f.h.snapshot, []string{id})
				require.NoError(t, err)
				assert.Equal(t, want, f.h.snapshot.LocationLabel(items[0]), id)
			}

			fetched := bungietest.Now
			if tc.moved {
				fetched = f.h.now()
			}

			assert.Equal(t, fetched, f.h.snapshot.Fetched)
		})
	}
}

func TestListAutotagRulesReadErrors(t *testing.T) {
	t.Parallel()

	listItems := func(t *testing.T, f *fakes) (string, bool) {
		t.Helper()

		return call(t, f.h.handleListItems, ListItemsInput{})
	}

	listRules := func(t *testing.T, f *fakes) (string, bool) {
		t.Helper()

		return call(t, f.h.handleListAutotagRules, ListAutotagRulesInput{})
	}

	tests := map[string]struct {
		setup func(f *fakes)
		// baseline returns the error result of another call that makes
		// the same read.
		baseline func(t *testing.T, f *fakes) (string, bool)
	}{
		"the autotag state": {
			setup:    func(f *fakes) { delete(f.service.Data, pathAutotag) },
			baseline: listRules,
		},
		"the profile": {
			setup: func(f *fakes) {
				f.bungie.ProfileBody = `{"ErrorCode": 5, "ErrorStatus": "SystemDisabled", "Message": "down"}`
			},
			baseline: listItems,
		},
		"the tags": {
			setup:    func(f *fakes) { delete(f.service.Data, pathTags) },
			baseline: listItems,
		},
	}

	for name, tc := range tests {
		for _, matches := range []string{matchesNew, matchesOwned} {
			t.Run(name+" with matches="+matches, func(t *testing.T) {
				t.Parallel()

				f := newFakes(t)
				tc.setup(f)

				want, isError := tc.baseline(t, f)
				require.True(t, isError)

				got, isError := call(t, f.h.handleListAutotagRules, ListAutotagRulesInput{Matches: matches})
				assert.True(t, isError)
				assert.Equal(t, want, got)
			})
		}
	}
}

func TestListAutotagRulesOwned(t *testing.T) {
	t.Parallel()

	const (
		header = "Owned items that each rule matches after the enabled rules before it took theirs, " +
			"as the rule editor of the web client counts them:"
		notCounted = ". Not counted, because the server cannot tell which items the rules before it take."
		skips      = ". The web client skips this rule and counts nothing for it: "
		parseSkip  = `the query ends after "-", which needs a search term after it.`
		masterwork = ". Cannot count: the server does not evaluate is:masterwork."
		// weaponLine is the rule line of weapon. The fixture profile
		// holds five weapons, two of them in the vault.
		weaponLine = `- 9. [enabled] "is:weapon": tag keep (id r9)`
		// undecidedCount is the count of the query is:weapon rate:>=a
		// with the Aegis sheet. The two copies of Fatebringer without
		// perks are undecided.
		undecidedCount = ". Owned items it matches: 2. Owned items that the server cannot decide for it: 2 (" +
			search.ReasonNoPerks + ")"
	)

	weapon := storedRule("r9", "is:weapon", "keep", 9, true)

	tests := map[string]struct {
		autotag string
		profile string
		matches string
		// sameAsSave is the query whose save_autotag_rule count equals
		// the count of the first rule.
		sameAsSave string
		want       []string
		// sheet makes the service publish previewAegisSheet as the only
		// roll sheet.
		sheet bool
		// noProfile marks a case that reads no profile.
		noProfile bool
		isError   bool
	}{
		"a later rule counts what is left": {
			autotag: autotagBody(
				storedRule("r1", "is:weapon", "junk", 0, true),
				storedRule("r2", "is:weapon is:invault", "keep", 1, true),
			),
			want: []string{
				header,
				`- 0. [enabled] "is:weapon": tag junk (id r1). Owned items it matches: 5`,
				`- 1. [enabled] "is:weapon is:invault": tag keep (id r2). Owned items it matches: 0`,
			},
		},
		"the matches value in upper case with spaces": {
			autotag: autotagBody(weapon),
			matches: " OWNED ",
			want:    []string{header, weaponLine + ". Owned items it matches: 5"},
		},
		"a disabled rule takes nothing": {
			autotag: autotagBody(
				storedRule("r1", "is:weapon", "junk", 0, false),
				storedRule("r2", "is:weapon is:invault", "keep", 1, true),
			),
			want: []string{
				header,
				`- 0. [disabled] "is:weapon": tag junk (id r1). Owned items it matches: 5`,
				`- 1. [enabled] "is:weapon is:invault": tag keep (id r2). Owned items it matches: 2`,
			},
		},
		"an unsupported enabled rule": {
			autotag: autotagBody(
				storedRule("r1", "is:masterwork", "junk", 0, true),
				storedRule("r2", "is:masterwork", "junk", 1, false),
				weapon,
			),
			want: []string{
				header,
				`- 0. [enabled] "is:masterwork": tag junk (id r1)` + masterwork,
				`- 1. [disabled] "is:masterwork": tag junk (id r2)` + notCounted,
				weaponLine + notCounted,
			},
		},
		"an unsupported disabled rule": {
			autotag: autotagBody(storedRule("r1", "is:masterwork", "junk", 0, false), weapon),
			want: []string{
				header,
				`- 0. [disabled] "is:masterwork": tag junk (id r1)` + masterwork,
				weaponLine + ". Owned items it matches: 5",
			},
		},
		"a server-only refusal blocks": {
			autotag: autotagBody(storedRule("r1", "is:weapon ) is:armor", "junk", 0, true), weapon),
			want: []string{
				header,
				`- 0. [enabled] "is:weapon ) is:armor": tag junk (id r1). Cannot count: ` +
					`the web client runs this rule, and the server cannot evaluate it: ` +
					`the query has a ")" that closes no "(", and the web client ignores ") is:armor".`,
				weaponLine + notCounted,
			},
		},
		"a DIM parse error skips": {
			autotag: autotagBody(
				storedRule("r1", "is:weapon -", "junk", 0, true),
				storedRule("r2", "is:weapon -", "junk", 1, false),
				weapon,
			),
			want: []string{
				header,
				`- 0. [enabled] "is:weapon -": tag junk (id r1)` + skips + parseSkip,
				`- 1. [disabled] "is:weapon -": tag junk (id r2)` + skips + parseSkip,
				weaponLine + ". Owned items it matches: 5",
			},
		},
		"an invalid value skips": {
			autotag: autotagBody(storedRule("r1", "count:=<2", "junk", 0, true), weapon),
			want: []string{
				header,
				`- 0. [enabled] "count:=<2": tag junk (id r1)` + skips + "the web client rejects count:=<2.",
				weaponLine + ". Owned items it matches: 5",
			},
		},
		"a skip after a block": {
			autotag: autotagBody(
				storedRule("r1", "is:masterwork", "junk", 0, true),
				storedRule("r2", "is:weapon -", "junk", 1, true),
				weapon,
			),
			want: []string{
				header,
				`- 0. [enabled] "is:masterwork": tag junk (id r1)` + masterwork,
				`- 1. [enabled] "is:weapon -": tag junk (id r2)` + skips + parseSkip,
				weaponLine + notCounted,
			},
		},
		"an undecided item": {
			autotag: autotagBody(storedRule("r1", "is:weapon rate:>=a", "junk", 0, true), weapon),
			sheet:   true,
			want: []string{
				header,
				`- 0. [enabled] "is:weapon rate:>=a": tag junk (id r1)` + undecidedCount,
				weaponLine + notCounted,
			},
		},
		"an earlier rule takes the undecided items": {
			autotag: autotagBody(
				storedRule("r1", "is:weapon", "junk", 0, true),
				storedRule("r2", "is:weapon rate:>=a", "junk", 1, true),
				weapon,
			),
			sheet: true,
			want: []string{
				header,
				`- 0. [enabled] "is:weapon": tag junk (id r1). Owned items it matches: 5`,
				`- 1. [enabled] "is:weapon rate:>=a": tag junk (id r2). Owned items it matches: 0`,
				weaponLine + ". Owned items it matches: 0",
			},
		},
		"an undecided disabled rule": {
			autotag: autotagBody(storedRule("r1", "is:weapon rate:>=a", "junk", 0, false), weapon),
			sheet:   true,
			want: []string{
				header,
				`- 0. [disabled] "is:weapon rate:>=a": tag junk (id r1)` + undecidedCount,
				weaponLine + ". Owned items it matches: 5",
			},
		},
		"a game mode without ratings": {
			autotag: autotagBody(storedRule("r1", "pvprate:>=a", "junk", 0, true), weapon),
			sheet:   true,
			want: []string{
				header,
				`- 0. [enabled] "pvprate:>=a": tag junk (id r1). Cannot count: ` + noPvPSheet + ".",
				weaponLine + notCounted,
			},
		},
		"a rule without action": {
			autotag: autotagBody(storedRule("r1", "is:weapon", "", 0, true), weapon),
			want: []string{
				header,
				`- 0. [enabled] "is:weapon": no action (id r1). Owned items it matches: 5`,
				weaponLine + ". Owned items it matches: 0",
			},
		},
		"the first count matches the save count": {
			autotag:    autotagBody(storedRule("r1", "is:weapon is:dupe", "junk", 0, true), weapon),
			sameAsSave: "is:weapon is:dupe",
			want: []string{
				header,
				`- 0. [enabled] "is:weapon is:dupe": tag junk (id r1). Owned items it matches: 3`,
				weaponLine + ". Owned items it matches: 2",
			},
		},
		"autotagging off": {
			autotag: `{"rules": [` + weapon + `], ` +
				`"state": {"enabled": false, "mark": "00000000000000001006", "updatedAt": 5}, "now": 10}`,
			want: []string{header, weaponLine + ". Owned items it matches: 5"},
		},
		"no rules": {
			autotag:   autotagBody(),
			want:      []string{"No rules, so nothing is counted."},
			noProfile: true,
		},
		"only removed rules": {
			autotag: autotagBody(`{"id": "r0", "query": "is:weapon", "vault": false, "enabled": true, ` +
				`"position": 0, "removed": true, "updatedAt": 5}`),
			want:      []string{"No rules, so nothing is counted."},
			noProfile: true,
		},
		"no character": {
			autotag: autotagBody(weapon),
			profile: `{"ErrorCode": 1, "Response": {
				"profileInventory": {"data": {"items": []}},
				"characters": {"data": {}}
			}}`,
			want: []string{
				"The account has no character, so the server cannot count the items that the rules match.",
			},
		},
		"an unknown matches value": {
			autotag:   autotagBody(weapon),
			matches:   "all",
			want:      []string{`invalid input: matches "all" is not new or owned`},
			noProfile: true,
			isError:   true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.Data[pathAutotag] = tc.autotag
			f.bungie.ProfileBody = tc.profile

			if tc.sheet {
				withAegisSheet(f)
			}

			matches := tc.matches
			if matches == "" {
				matches = matchesOwned
			}

			got, isError := call(t, f.h.handleListAutotagRules, ListAutotagRulesInput{Matches: matches})
			assert.Equal(t, tc.isError, isError, got)
			assert.Equal(t, strings.Join(tc.want, "\n"), got)

			profiles := f.bungie.Calls(http.MethodGet, bungietest.PathProfile)
			assert.Equal(t, tc.noProfile, len(profiles) == 0, "profile reads: %d", len(profiles))

			for _, req := range f.service.All() {
				assert.Equal(t, http.MethodGet, req.Method, "nothing is written: %s", req.Path)
			}

			for _, req := range f.bungie.All() {
				assert.Equal(t, http.MethodGet, req.Method, "nothing moves: %s", req.Path)
			}

			if tc.sameAsSave != "" {
				_, after, ok := strings.Cut(got, "Owned items it matches: ")
				require.True(t, ok)

				count, _, _ := strings.Cut(after, "\n")

				saved, isError := saveJunkRule(t, newFakes(t), tc.sameAsSave)
				require.False(t, isError)
				assert.Contains(t, saved, "The query matches "+count+" owned items now.")
			}
		})
	}
}

func TestListAutotagRulesOwnedCachedProfile(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.service.Data[pathAutotag] = autotagBody(storedRule("r9", "is:weapon", "keep", 9, true))

	_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	require.False(t, isError)

	got, isError := call(t, f.h.handleListAutotagRules, ListAutotagRulesInput{Matches: matchesOwned})
	require.False(t, isError, got)

	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1,
		"the counts reuse the snapshot of list_characters")
}

func TestSaveAutotagRule(t *testing.T) {
	t.Parallel()

	const (
		noAction = "invalid input: a rule needs a tag_id or vault=true, " +
			"so add one or remove the rule with remove_autotag_rule"
		badQuery = "invalid input: a rule query has between 1 and 1000 characters"
		unclosed = `invalid input: the quote in "name:\"abc" never closes`
		bareWord = `invalid input: the web client reads the unquoted word %q as a search for that word, ` +
			`not as an operator. Quote the word to search for it`
		endsAfter  = `invalid input: the query ends after %q, which needs a search term after it`
		misplaced  = `invalid input: the query has ")" where a search term belongs`
		noOperator = `invalid input: the query has no space or operator before %q`
		strayParen = `invalid input: the query has a ")" that closes no "(", and the web client ignores %q`
	)

	query := "is:weapon misses:2"
	openQuote := `name:"abc`
	badEscape := `name:"a\b"`
	noValue := "is:"
	noValueInside := "is:weapon name: x"
	quoteAfterParen := `is:weapon ) "abc`
	endsAfterNegation := `is:weapon -`
	endsAfterParen := `is:weapon (`
	emptyGroup := `()`
	parenAfterOr := `is:weapon or )`
	adjacentQuotes := `"a""b"`
	parenAfterFilter := `is:weapon(`
	closeParen := `is:weapon )`
	termsAfterParen := `is:weapon ) is:armor`
	commentOnly := `/* note */`
	trailingOr := `is:weapon or`
	trailingAnd := `cluster and`
	droppedQuote := `is:weapon (is:arc is:void)"abc`
	openGroup := `is:blue (is:rocketlauncher`
	notWord := `not forgotten`
	quotedOr := `"or"`
	// The query is the comment case of the parser tests of DIM.
	comments := `/* My cool search */ (/* armor */ is:armor and is:blue) or ` +
		`(/*weapons*/ is:weapon and perkname:"Kill Clip")`
	byteOrderMark := "\ufeff"
	empty := ""
	spaces := "  "
	junk := "junk"
	yes := true
	no := false
	position := 0

	// The query is 600 UTF-16 code units and 1800 bytes.
	wide := strings.Repeat("\u706b", 600)
	long := strings.Repeat("\U0001F600", maxQueryLength/2+1)

	tests := map[string]struct {
		wantRule map[string]any
		// autotag replaces the autotag body of the fixture when it is set.
		autotag string
		want    string
		in      SaveAutotagRuleInput
		isError bool
	}{
		"create appends after the last live rule": {
			in: SaveAutotagRuleInput{Query: &query, TagID: &junk, Vault: &yes},
			wantRule: map[string]any{
				"query": "is:weapon misses:2", "tagId": "junk", "vault": true,
				"enabled": true, "position": float64(2), "removed": false,
			},
		},
		"create with a tag only": {
			in: SaveAutotagRuleInput{Query: &query, TagID: &junk},
			wantRule: map[string]any{
				"query": "is:weapon misses:2", "tagId": "junk", "vault": false,
				"enabled": true, "position": float64(2), "removed": false,
			},
		},
		"create with vault only": {
			in: SaveAutotagRuleInput{Query: &query, Vault: &yes},
			wantRule: map[string]any{
				"query": "is:weapon misses:2", "vault": true,
				"enabled": true, "position": float64(2), "removed": false,
			},
		},
		"update overlays the given fields": {
			in: SaveAutotagRuleInput{ID: "r2", Enabled: &yes, Position: &position},
			wantRule: map[string]any{
				"id": "r2", "query": "is:dupe", "vault": true,
				"enabled": true, "position": float64(0), "removed": false,
			},
		},
		"update swaps the tag for the vault flag": {
			in: SaveAutotagRuleInput{ID: "r1", TagID: &empty, Vault: &yes},
			wantRule: map[string]any{
				"id": "r1", "query": "rate:>=a", "vault": true,
				"enabled": true, "position": float64(0), "removed": false,
			},
		},
		"update swaps the vault flag for a tag": {
			in: SaveAutotagRuleInput{ID: "r2", Vault: &no, TagID: &junk},
			wantRule: map[string]any{
				"id": "r2", "query": "is:dupe", "tagId": "junk", "vault": false,
				"enabled": false, "position": float64(1), "removed": false,
			},
		},
		"update adds vault to a stored rule without an action": {
			autotag: fixtureRuleWithoutAction,
			in:      SaveAutotagRuleInput{ID: "r3", Vault: &yes},
			wantRule: map[string]any{
				"id": "r3", "query": "is:armor", "vault": true,
				"enabled": true, "position": float64(0), "removed": false,
			},
		},
		"update adds a tag to a stored rule without an action": {
			autotag: fixtureRuleWithoutAction,
			in:      SaveAutotagRuleInput{ID: "r3", TagID: &junk},
			wantRule: map[string]any{
				"id": "r3", "query": "is:armor", "tagId": "junk", "vault": false,
				"enabled": true, "position": float64(0), "removed": false,
			},
		},
		"create with a query only": {
			in:      SaveAutotagRuleInput{Query: &query},
			want:    noAction,
			isError: true,
		},
		"create with an empty tag and no vault": {
			in:      SaveAutotagRuleInput{Query: &query, TagID: &empty},
			want:    noAction,
			isError: true,
		},
		"create with a tag of spaces": {
			in:      SaveAutotagRuleInput{Query: &query, TagID: &spaces},
			want:    noAction,
			isError: true,
		},
		"create with vault false": {
			in:      SaveAutotagRuleInput{Query: &query, Vault: &no},
			want:    noAction,
			isError: true,
		},
		"update clears the tag": {
			in:      SaveAutotagRuleInput{ID: "r1", TagID: &empty},
			want:    noAction,
			isError: true,
		},
		"update turns the vault flag off": {
			in:      SaveAutotagRuleInput{ID: "r2", Vault: &no},
			want:    noAction,
			isError: true,
		},
		"update of a stored rule without an action": {
			autotag: fixtureRuleWithoutAction,
			in:      SaveAutotagRuleInput{ID: "r3", Enabled: &no},
			want:    noAction,
			isError: true,
		},
		"query update of a stored rule without an action": {
			autotag: fixtureRuleWithoutAction,
			in:      SaveAutotagRuleInput{ID: "r3", Query: &query},
			want:    noAction,
			isError: true,
		},
		"empty query wins over the missing action": {
			in:      SaveAutotagRuleInput{ID: "r1", Query: &empty, TagID: &empty},
			want:    badQuery,
			isError: true,
		},
		"missing query wins over the missing action": {
			in:      SaveAutotagRuleInput{},
			want:    "invalid input: a new rule needs a query",
			isError: true,
		},
		"create needs a query": {
			in:      SaveAutotagRuleInput{TagID: &junk},
			want:    "invalid input: a new rule needs a query",
			isError: true,
		},
		"removed rule is unknown": {
			in:      SaveAutotagRuleInput{ID: "r0", Enabled: &yes},
			want:    `invalid input: no autotag rule has the id "r0"`,
			isError: true,
		},
		"unknown tag": {
			in:      SaveAutotagRuleInput{ID: "r1", TagID: &query},
			want:    `invalid input: tag_id "is:weapon misses:2" is not one of keep, pvp, pve, junk, infuse, archive`,
			isError: true,
		},
		"empty query": {
			in:      SaveAutotagRuleInput{ID: "r1", Query: &empty},
			want:    badQuery,
			isError: true,
		},
		"query within the limit in UTF-16 code units": {
			in: SaveAutotagRuleInput{ID: "r1", Query: &wide},
			wantRule: map[string]any{
				"id": "r1", "query": wide, "tagId": "pve", "vault": false,
				"enabled": true, "position": float64(0), "removed": false,
			},
		},
		"query too long in UTF-16 code units": {
			in:      SaveAutotagRuleInput{ID: "r1", Query: &long},
			want:    badQuery,
			isError: true,
		},
		"quote that never closes": {
			in:      SaveAutotagRuleInput{Query: &openQuote, TagID: &junk},
			want:    unclosed,
			isError: true,
		},
		"unknown escape inside quotes": {
			in: SaveAutotagRuleInput{Query: &badEscape, TagID: &junk},
			want: `invalid input: the query puts a backslash before "b" inside quotes, ` +
				"and inside quotes a backslash escapes only a double quote, a single quote, or a backslash",
			isError: true,
		},
		"filter name without a value": {
			in:      SaveAutotagRuleInput{Query: &noValue, TagID: &junk},
			want:    `invalid input: the filter "is:" has no value`,
			isError: true,
		},
		"filter name without a value inside the query": {
			in:      SaveAutotagRuleInput{Query: &noValueInside, TagID: &junk},
			want:    `invalid input: the filter "name:" has no value`,
			isError: true,
		},
		"quote after an unmatched paren": {
			in:      SaveAutotagRuleInput{Query: &quoteAfterParen, TagID: &junk},
			want:    fmt.Sprintf(strayParen, `) "abc`),
			isError: true,
		},
		"query ends after a negation": {
			in:      SaveAutotagRuleInput{Query: &endsAfterNegation, TagID: &junk},
			want:    fmt.Sprintf(endsAfter, "-"),
			isError: true,
		},
		"query ends after an open paren": {
			in:      SaveAutotagRuleInput{Query: &endsAfterParen, TagID: &junk},
			want:    fmt.Sprintf(endsAfter, "("),
			isError: true,
		},
		"query ends after a comment": {
			in:      SaveAutotagRuleInput{Query: &commentOnly, TagID: &junk},
			want:    fmt.Sprintf(endsAfter, "/* note */"),
			isError: true,
		},
		"empty group": {
			in:      SaveAutotagRuleInput{Query: &emptyGroup, TagID: &junk},
			want:    misplaced,
			isError: true,
		},
		"close paren after or": {
			in:      SaveAutotagRuleInput{Query: &parenAfterOr, TagID: &junk},
			want:    misplaced,
			isError: true,
		},
		"quoted strings without a space": {
			in:      SaveAutotagRuleInput{Query: &adjacentQuotes, TagID: &junk},
			want:    fmt.Sprintf(noOperator, `"b"`),
			isError: true,
		},
		"open paren right after a filter": {
			in:      SaveAutotagRuleInput{Query: &parenAfterFilter, TagID: &junk},
			want:    fmt.Sprintf(noOperator, "("),
			isError: true,
		},
		"close paren that closes no open paren": {
			in:      SaveAutotagRuleInput{Query: &closeParen, TagID: &junk},
			want:    fmt.Sprintf(strayParen, ")"),
			isError: true,
		},
		"terms after a close paren": {
			in:      SaveAutotagRuleInput{Query: &termsAfterParen, TagID: &junk},
			want:    fmt.Sprintf(strayParen, ") is:armor"),
			isError: true,
		},
		"unquoted or": {
			in:      SaveAutotagRuleInput{Query: &trailingOr, TagID: &junk},
			want:    fmt.Sprintf(bareWord, "or"),
			isError: true,
		},
		"unquoted and": {
			in:      SaveAutotagRuleInput{Query: &trailingAnd, TagID: &junk},
			want:    fmt.Sprintf(bareWord, "and"),
			isError: true,
		},
		"lexer error that the web client drops": {
			in: SaveAutotagRuleInput{Query: &droppedQuote, TagID: &junk},
			want: `invalid input: the quote in "\"abc" never closes. ` +
				`The web client drops this error and ignores "\"abc"`,
			isError: true,
		},
		"group that never closes": {
			in: SaveAutotagRuleInput{Query: &openGroup, TagID: &junk},
			wantRule: map[string]any{
				"query": openGroup, "tagId": "junk", "vault": false,
				"enabled": true, "position": float64(2), "removed": false,
			},
		},
		"not before a bare word": {
			in: SaveAutotagRuleInput{Query: &notWord, TagID: &junk},
			wantRule: map[string]any{
				"query": notWord, "tagId": "junk", "vault": false,
				"enabled": true, "position": float64(2), "removed": false,
			},
		},
		"quoted operator word": {
			in: SaveAutotagRuleInput{Query: &quotedOr, TagID: &junk},
			wantRule: map[string]any{
				"query": quotedOr, "tagId": "junk", "vault": false,
				"enabled": true, "position": float64(2), "removed": false,
			},
		},
		"comments and groups": {
			in: SaveAutotagRuleInput{Query: &comments, TagID: &junk},
			wantRule: map[string]any{
				"query": comments, "tagId": "junk", "vault": false,
				"enabled": true, "position": float64(2), "removed": false,
			},
		},
		"query without a search term": {
			in:      SaveAutotagRuleInput{Query: &byteOrderMark, TagID: &junk},
			want:    "invalid input: the query has no search term",
			isError: true,
		},
		"query check wins over the missing action": {
			in:      SaveAutotagRuleInput{Query: &openQuote},
			want:    unclosed,
			isError: true,
		},
		"update keeps a stored query that does not lex": {
			autotag: fixtureRuleWithOpenQuote,
			in:      SaveAutotagRuleInput{ID: "r3", Enabled: &no},
			wantRule: map[string]any{
				"id": "r3", "query": `name:"abc`, "tagId": "junk", "vault": false,
				"enabled": false, "position": float64(0), "removed": false,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			if tc.autotag != "" {
				f.service.Data[pathAutotag] = tc.autotag
			}

			got, isError := call(t, f.h.handleSaveAutotagRule, tc.in)
			assert.Equal(t, tc.isError, isError)

			writes := f.service.Calls(http.MethodPost, pathAutotag)
			if tc.wantRule == nil {
				assert.Equal(t, tc.want, got)
				assert.Empty(t, writes)

				return
			}

			require.Len(t, writes, 1)

			rules, ok := writes[0].Body["rules"].([]any)
			require.True(t, ok)
			require.Len(t, rules, 1)

			rule, ok := rules[0].(map[string]any)
			require.True(t, ok)

			if tc.in.ID == "" {
				assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, rule["id"])
				delete(rule, "id")
			}

			assert.Equal(t, tc.wantRule, rule)
		})
	}
}

func TestSaveAutotagRuleReport(t *testing.T) {
	t.Parallel()

	const (
		bungieDown   = `{"ErrorCode": 5, "ErrorStatus": "SystemDisabled", "Message": "down"}`
		noCharacters = `{"ErrorCode": 1, "Response": {
			"profileInventory": {"data": {"items": [
				{"itemHash": 100, "itemInstanceId": "1001", "quantity": 1, "location": 2, "bucketHash": 138197802}
			]}},
			"characters": {"data": {}}
		}}`
		countLine = "The query matches %d owned items now. " +
			"The count ignores the other rules, so an earlier rule can take some of these items first."
		unsupportedLine = "The server does not evaluate %s, so it did not count the items that the query matches. " +
			"The web client might still accept those terms and run the rule."
		readFailedLine = "The rule is saved, but the server could not read the inventory " +
			"to count the items that the query matches: %s"
		noCharacterLine = "The rule is saved, but the account has no character, " +
			"so the server did not count the items that the query matches."
		invalidText = "invalid input: the web client rejects %s, so it skips an autotag rule with this query"
	)

	// created returns the text of a result that saved a new rule with query
	// and the tag junk. The test writes "new" in place of the random id.
	created := func(query, line string) string {
		return fmt.Sprintf("Saved the rule:\n- 2. [enabled] %q: tag junk (id new)\n%s", query, line)
	}

	dupes, _, _ := preview(t, newFakes(t), "is:weapon is:dupe")
	require.Positive(t, dupes)

	weapons, _, _ := preview(t, newFakes(t), "is:weapon")
	require.Positive(t, weapons)

	down := newFakes(t)
	down.bungie.ProfileBody = bungieDown

	readErr, isError := call(t, down.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:weapon"})
	require.True(t, isError)
	require.True(t, strings.HasPrefix(readErr, "fetching the profile:"), readErr)

	dupeQuery := "is:weapon is:dupe"
	weaponQuery := "is:weapon"
	masterwork := "is:masterwork"
	badCount := "count:=<2"
	badDupe := "dupe:foo"
	mixed := "is:masterwork count:=<2"
	emptyVault := "is:armor is:invault"
	junk := "junk"
	no := false

	tests := map[string]struct {
		// profile replaces the profile of the fake Bungie platform when it
		// is set.
		profile string
		want    string
		in      SaveAutotagRuleInput
		isError bool
		// fetches reports whether the call reads the profile.
		fetches bool
	}{
		"counts the matches of a query": {
			in:      SaveAutotagRuleInput{Query: &dupeQuery, TagID: &junk},
			want:    created(dupeQuery, fmt.Sprintf(countLine, dupes)),
			fetches: true,
		},
		"counts the matches of the query of an updated rule": {
			in: SaveAutotagRuleInput{ID: "r1", Query: &weaponQuery},
			want: "Saved the rule:\n- 0. [enabled] \"is:weapon\": tag pve (id r1)\n" +
				fmt.Sprintf(countLine, weapons),
			fetches: true,
		},
		"names an unsupported term": {
			in:   SaveAutotagRuleInput{Query: &masterwork, TagID: &junk},
			want: created(masterwork, fmt.Sprintf(unsupportedLine, "is:masterwork")),
		},
		"refuses an invalid count value": {
			in:      SaveAutotagRuleInput{Query: &badCount, TagID: &junk},
			want:    fmt.Sprintf(invalidText, "count:=<2"),
			isError: true,
		},
		"refuses an invalid dupe value": {
			in:      SaveAutotagRuleInput{Query: &badDupe, TagID: &junk},
			want:    fmt.Sprintf(invalidText, "dupe:foo"),
			isError: true,
		},
		"refuses an invalid value beside an unsupported term": {
			in:      SaveAutotagRuleInput{Query: &mixed, TagID: &junk},
			want:    fmt.Sprintf(invalidText, "count:=<2"),
			isError: true,
		},
		"keeps the rule when the inventory read fails": {
			profile: bungieDown,
			in:      SaveAutotagRuleInput{Query: &weaponQuery, TagID: &junk},
			want:    created(weaponQuery, fmt.Sprintf(readFailedLine, readErr)),
			fetches: true,
		},
		"reports an account without a character": {
			profile: noCharacters,
			in:      SaveAutotagRuleInput{Query: &weaponQuery, TagID: &junk},
			want:    created(weaponQuery, noCharacterLine),
			fetches: true,
		},
		"a call that changes only the enabled flag": {
			in:   SaveAutotagRuleInput{ID: "r1", Enabled: &no},
			want: "Saved the rule:\n- 0. [disabled] \"rate:>=a\": tag pve (id r1)",
		},
		"the empty count reads as a number": {
			in:      SaveAutotagRuleInput{Query: &emptyVault, TagID: &junk},
			want:    created(emptyVault, fmt.Sprintf(countLine, 0)),
			fetches: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.bungie.ProfileBody = tc.profile

			got, isError := call(t, f.h.handleSaveAutotagRule, tc.in)
			assert.Equal(t, tc.isError, isError)

			profiles := f.bungie.Calls(http.MethodGet, bungietest.PathProfile)
			if tc.fetches {
				assert.NotEmpty(t, profiles)
			} else {
				assert.Empty(t, profiles)
			}

			writes := f.service.Calls(http.MethodPost, pathAutotag)
			if tc.isError {
				assert.Equal(t, tc.want, got)
				assert.Empty(t, writes)

				return
			}

			require.Len(t, writes, 1)

			if tc.in.ID == "" {
				rules, ok := writes[0].Body["rules"].([]any)
				require.True(t, ok)
				require.Len(t, rules, 1)

				rule, ok := rules[0].(map[string]any)
				require.True(t, ok)

				id, ok := rule["id"].(string)
				require.True(t, ok)

				got = strings.Replace(got, "(id "+id+")", "(id new)", 1)
			}

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRemoveAutotagRule(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleRemoveAutotagRule, RemoveAutotagRuleInput{ID: "r1"})
	assert.False(t, isError)
	assert.Equal(t, `Removed the rule "rate:>=a".`, got)

	writes := f.service.Calls(http.MethodPost, pathAutotag)
	require.Len(t, writes, 1)
	assert.Equal(t, map[string]any{"rules": []any{
		map[string]any{
			"id": "r1", "query": "rate:>=a", "tagId": "pve", "vault": false,
			"enabled": true, "position": float64(0), "removed": true,
		},
	}}, writes[0].Body)

	got, isError = call(t, f.h.handleRemoveAutotagRule, RemoveAutotagRuleInput{ID: "nope"})
	assert.True(t, isError)
	assert.Equal(t, `invalid input: no autotag rule has the id "nope"`, got)
}

// The texts of the preview_autotag_rule tool.
const (
	previewNoMatch = "No owned item matches the query."
	previewHeader  = "instance id | name | type | tier | power | stat total | location | tag | flags"
)

// tagRows returns a tags response body of the fake service that holds
// rows.
func tagRows(t *testing.T, rows ...dvm.ItemTag) string {
	t.Helper()

	if rows == nil {
		rows = []dvm.ItemTag{}
	}

	raw, err := json.Marshal(map[string]any{"defs": []any{}, "items": rows, "now": 10})
	require.NoError(t, err)

	return string(raw)
}

// preview runs preview_autotag_rule with query. It returns the number of
// matches, the number of owned items, and the instance id of each row. The
// result must not be an error.
func preview(t *testing.T, f *fakes, query string) (int, int, []string) {
	t.Helper()

	got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: query})
	require.False(t, isError, got)

	if got == previewNoMatch {
		return 0, len(search.OwnedItems(f.h.snapshot)), []string{}
	}

	var matched, owned int

	_, err := fmt.Sscanf(got, "%d of %d owned items match.", &matched, &owned)
	require.NoError(t, err, got)

	_, table, ok := strings.Cut(got, previewHeader+"\n")
	require.True(t, ok, got)

	rows := []string{}
	for line := range strings.SplitSeq(table, "\n") {
		id, _, _ := strings.Cut(line, " | ")
		rows = append(rows, id)
	}

	return matched, owned, rows
}

func TestPreviewNegates(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	weapons, owned, _ := preview(t, f, "is:weapon")
	assert.Equal(t, 5, weapons)
	assert.Equal(t, 6, owned)

	others, owned, rows := preview(t, f, "-is:weapon")
	assert.Equal(t, owned-weapons, others)
	assert.Equal(t, []string{"1004"}, rows)
}

func TestPreviewInVault(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	matched, _, rows := preview(t, f, "is:invault")
	assert.Positive(t, matched)
	assert.Equal(t, []string{"1001", "1002"}, rows)

	for _, id := range rows {
		item, ok := f.h.snapshot.Find(id)
		require.True(t, ok)
		assert.Equal(t, inventory.OwnerVault, item.Owner)
	}
}

func TestPreviewTagKeep(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.service.Data[pathTags] = tagRows(t)

	_, _, rows := preview(t, f, "tag:keep")
	assert.Empty(t, rows)

	f.service.Data[pathTags] = tagRows(t, dvm.ItemTag{InstanceID: "1003", ItemHash: 102, TagID: "keep", UpdatedAt: 5})

	_, _, rows = preview(t, f, "tag:keep")
	assert.Equal(t, []string{"1003"}, rows)

	f.service.Data[pathTags] = tagRows(
		t,
		dvm.ItemTag{InstanceID: "1003", ItemHash: 102, TagID: "keep", UpdatedAt: 6, Removed: true},
	)

	_, _, rows = preview(t, f, "tag:keep")
	assert.Empty(t, rows)
}

func TestPreviewFavorite(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	favorite := dvm.ItemTag{InstanceID: "1003", ItemHash: 102, TagID: dvm.TagFavorite, UpdatedAt: 5}
	junk := dvm.ItemTag{InstanceID: "1003", ItemHash: 102, TagID: dvm.TagJunk, UpdatedAt: 5}
	f.service.Data[pathTags] = tagRows(t, favorite, junk)

	for _, query := range []string{"is:favorite", "tag:favorite", "tag:junk"} {
		_, _, rows := preview(t, f, query)
		assert.Equal(t, []string{"1003"}, rows, query)
	}

	junk.Removed = true
	f.service.Data[pathTags] = tagRows(t, favorite, junk)

	_, _, rows := preview(t, f, "is:favorite")
	assert.Equal(t, []string{"1003"}, rows)

	favorite.Removed = true
	f.service.Data[pathTags] = tagRows(t, favorite, junk)

	_, _, rows = preview(t, f, "is:favorite")
	assert.Empty(t, rows)
}

func TestPreviewName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  []string
	}{
		"start of a word":        {query: "name:coup", want: []string{"1003"}},
		"inside a word":          {query: "name:oup", want: []string{}},
		"quoted words":           {query: `name:"Midnight Coup"`, want: []string{"1003"}},
		"exact name":             {query: `exactname:"MIDNIGHT COUP"`, want: []string{"1003"}},
		"exact name of one word": {query: "exactname:midnight", want: []string{}},
		"with another filter":    {query: "name:celestial is:armor", want: []string{"1004"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, rows := preview(t, newFakes(t), tc.query)
			assert.Equal(t, tc.want, rows)
		})
	}
}

func TestPreviewNoMatch(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "tag:none"})
	assert.False(t, isError)
	assert.Equal(t, previewNoMatch, got)
}

func TestPreviewPaging(t *testing.T) {
	t.Parallel()

	const (
		first   = "5 of 6 owned items match."
		row1001 = "1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 |  | Vault | keep | " +
			"favorite, locked, masterwork, note"
		row1005 = "1005 | Fatebringer | Kinetic Hand Cannon | Legendary | 1970 |  | Hunter (postmaster) |  | "
		row1006 = "1006 | Fatebringer | Kinetic Hand Cannon | Legendary | 1980 |  | Hunter (equipped) |  | "
		row1002 = "1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 |  | Vault |  | "
		row1003 = "1003 | Midnight Coup | Kinetic Hand Cannon | Legendary | 2005 |  | Hunter |  | "
	)

	// The Hunter of manyProfile holds one kinetic weapon more than a page
	// holds at most. manyRows holds the rows of the first maxItemLimit of
	// those weapons.
	manyItems := make([]string, 0, maxItemLimit+1)
	manyRows := make([]string, 0, maxItemLimit)

	for i := range maxItemLimit + 1 {
		manyItems = append(manyItems, fmt.Sprintf(
			`{"itemHash": 100, "itemInstanceId": "%d", "quantity": 1, "location": 1, "bucketHash": 1498876634}`,
			5000+i,
		))

		if i < maxItemLimit {
			manyRows = append(
				manyRows,
				fmt.Sprintf("%d | Fatebringer | Kinetic Hand Cannon | Legendary |  |  | Hunter |  | ", 5000+i),
			)
		}
	}

	manyPage := slices.Concat([]string{
		"201 of 201 owned items match.", "Items 1-200 of 201. Pass offset=200 for the next page.", "", previewHeader,
	}, manyRows)
	defaultPage := slices.Concat([]string{
		"201 of 201 owned items match.", "Items 1-50 of 201. Pass offset=50 for the next page.", "", previewHeader,
	}, manyRows[:defaultItemLimit])

	manyProfile := `{"ErrorCode": 1, "Response": {
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"characterInventories": {"data": {"c1": {"items": [` + strings.Join(manyItems, ",\n") + `]}}}
	}}`

	tests := map[string]struct {
		in      PreviewAutotagRuleInput
		profile string
		want    []string
	}{
		"every match": {
			in:   PreviewAutotagRuleInput{Query: "is:weapon"},
			want: []string{first, "Items 1-5 of 5.", "", previewHeader, row1001, row1005, row1006, row1002, row1003},
		},
		"first page": {
			in:   PreviewAutotagRuleInput{Query: "is:weapon", Limit: 1},
			want: []string{first, "Items 1-1 of 5. Pass offset=1 for the next page.", "", previewHeader, row1001},
		},
		"middle page": {
			in: PreviewAutotagRuleInput{Query: "is:weapon", Limit: 2, Offset: 1},
			want: []string{
				first, "Items 2-3 of 5. Pass offset=3 for the next page.", "", previewHeader, row1005, row1006,
			},
		},
		"last page": {
			in:   PreviewAutotagRuleInput{Query: "is:weapon", Limit: 2, Offset: 4},
			want: []string{first, "Items 5-5 of 5.", "", previewHeader, row1003},
		},
		"default limit": {
			in:      PreviewAutotagRuleInput{Query: "is:weapon"},
			profile: manyProfile,
			want:    defaultPage,
		},
		"limit past the maximum": {
			in:      PreviewAutotagRuleInput{Query: "is:weapon", Limit: maxItemLimit + 1},
			profile: manyProfile,
			want:    manyPage,
		},
		"negative offset": {
			in:   PreviewAutotagRuleInput{Query: "is:weapon", Limit: 1, Offset: -3},
			want: []string{first, "Items 1-1 of 5. Pass offset=1 for the next page.", "", previewHeader, row1001},
		},
		"offset past the last match": {
			in:   PreviewAutotagRuleInput{Query: "is:weapon", Offset: 9},
			want: []string{first, "Offset 5 is past the last of 5 matching items."},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.bungie.ProfileBody = tc.profile

			got, isError := call(t, f.h.handlePreviewAutotagRule, tc.in)
			assert.False(t, isError)
			assert.Equal(t, strings.Join(tc.want, "\n"), got)
		})
	}
}

func TestPreviewRefresh(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	for range 2 {
		_, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:weapon"})
		require.False(t, isError)
	}

	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 1, "the second call reuses the profile")

	_, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:weapon", Refresh: true})
	require.False(t, isError)
	assert.Len(t, f.bungie.Calls(http.MethodGet, bungietest.PathProfile), 2, "refresh fetches the profile again")
}

func TestPreviewUnsupported(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(
		t,
		f.h.handlePreviewAutotagRule,
		PreviewAutotagRuleInput{Query: "is:weapon or is:masterwork"},
	)
	assert.True(t, isError)
	assert.Equal(t, "unsupported query: the server does not evaluate is:masterwork, so it previews nothing. "+
		"The web client might still accept these terms. "+
		"The server evaluates is:weapon, is:armor, is:equipment, is:equippable, is:equipped, is:invault, "+
		"is:postmaster, is:inpostmaster, is:locked, is:unlocked, is:titan, is:hunter, is:warlock, "+
		"is:favorite, is:autorifle, is:handcannon, is:pulserifle, is:scoutrifle, is:fusionrifle, "+
		"is:sniperrifle, is:shotgun, is:machinegun, is:lmg, is:rocketlauncher, is:sidearm, is:sword, "+
		"is:grenadelauncher, is:heavygrenadelauncher, is:specialgrenadelauncher, is:tracerifle, "+
		"is:linearfusionrifle, is:lfr, is:submachine, is:smg, is:bow, is:glaive, is:transmat, is:weaponmod, "+
		"is:armormod, is:reptoken, is:primary, is:special, is:heavy, is:dupe, is:incurrentchar, "+
		"is:currentclass, is:common, is:uncommon, is:rare, is:legendary, is:exotic, is:white, is:green, "+
		"is:blue, is:purple, is:yellow, is:kinetic, is:arc, is:solar, is:void, is:stasis, is:strand, "+
		"is:light, is:dark, is:engrams, is:lostitems, is:messages, is:specialorders, is:kineticslot, "+
		"is:energy, is:power, is:helmet, is:gauntlets, is:chest, is:leg, is:classitem, is:subclass, "+
		"is:seasonalartifacts, is:ghost, is:emblems, is:ships, is:vehicle, is:emotes, is:finishers, "+
		"is:accessories, is:consumables, is:modifications, is:haspower, is:haslight, is:autotagged, tag:, "+
		"dupe:item, count:, id:, hash:, name:, exactname:, power:, light:, rate:, rating:, ratebase:, "+
		"ratingbase:, misses:, setbonus:, verdict:, pvprate:, pvprating:, pvpratebase:, pvpratingbase:, "+
		"pvpmisses:, pvpsetbonus:, and pvpverdict:. "+
		"Each is: word also works as not:<word>.", got)
	assert.Empty(t, f.service.All())
	assert.Empty(t, f.bungie.All())
}

func TestInvalidQuery(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want  string
		terms []string
	}{
		"one term": {
			terms: []string{"dupe:foo"},
			want:  "invalid input: the web client rejects dupe:foo, so it skips an autotag rule with this query",
		},
		"two terms": {
			terms: []string{"count:=<2", "dupe:foo"},
			want: "invalid input: the web client rejects count:=<2, dupe:foo, " +
				"so it skips an autotag rule with this query",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := invalidQuery(tc.terms)
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Equal(t, tc.want, err.Error())
		})
	}
}

func TestPreviewInvalidValue(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  string
	}{
		"count: operators in the wrong order": {
			query: "count:=<2",
			want: "invalid input: the web client rejects count:=<2, " +
				"so it skips an autotag rule with this query",
		},
		"unknown dupe: value": {
			query: "dupe:foo",
			want:  "invalid input: the web client rejects dupe:foo, so it skips an autotag rule with this query",
		},
		"invalid and unsupported terms": {
			query: "is:masterwork dupe:foo or count:abc",
			want: "invalid input: the web client rejects dupe:foo, count:abc, " +
				"so it skips an autotag rule with this query",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: tc.query})
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)
			assert.Empty(t, f.service.All())
			assert.Empty(t, f.bungie.All())
		})
	}
}

func TestPreviewUnsupportedDupeValue(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:weapon dupe:stats"})
	assert.True(t, isError)
	assert.True(t, strings.HasPrefix(got, "unsupported query: the server does not evaluate dupe:stats, "), got)
	assert.Empty(t, f.service.All())
	assert.Empty(t, f.bungie.All())
}

func TestPreviewDupe(t *testing.T) {
	t.Parallel()

	// The Hunter holds one copy of a pair of hand cannons, and the vault
	// holds the other copy. Midnight Coup has one copy. The helmets are a
	// pair of armor pieces, so is:weapon leaves them out.
	f := newFakes(t)
	f.bungie.ProfileBody = `{"ErrorCode": 1, "Response": {
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"profileInventory": {"data": {"items": [
			{"itemHash": 100, "itemInstanceId": "2002", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 200, "itemInstanceId": "2004", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802}
		]}},
		"characterInventories": {"data": {"c1": {"items": [
			{"itemHash": 100, "itemInstanceId": "2001", "lockable": true, "quantity": 1, "location": 1, "bucketHash": 1498876634},
			{"itemHash": 102, "itemInstanceId": "2003", "lockable": true, "quantity": 1, "location": 1, "bucketHash": 1498876634},
			{"itemHash": 200, "itemInstanceId": "2005", "lockable": true, "quantity": 1, "location": 1, "bucketHash": 3448274439}
		]}}}
	}}`

	matched, owned, rows := preview(t, f, "is:weapon is:dupe")
	assert.Equal(t, 2, matched)
	assert.Equal(t, 5, owned)
	assert.Equal(t, []string{"2001", "2002"}, rows)

	_, _, rows = preview(t, f, "is:dupe")
	assert.Equal(t, []string{"2004", "2005", "2001", "2002"}, rows)
}

func TestPreviewDupeGroupsBuiltItems(t *testing.T) {
	t.Parallel()

	// The Hunter holds one copy of Midnight Coup and a stack with the same
	// key, which the game cannot lock. Hash 403 is a weapon whose
	// definition names the bucket that holds dummy items. DIM builds its
	// lost copy at the Postmaster, but not its vault copy, so the lost
	// copy has no other copy to match.
	f := newFakes(t)
	f.defs.Items[403] = definitions.ItemDef{
		Name:       "Dummy",
		Bucket:     bungie.BucketForbidden,
		ItemType:   bungie.ItemTypeWeapon,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}
	f.bungie.ProfileBody = `{"ErrorCode": 1, "Response": {
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"profileInventory": {"data": {"items": [
			{"itemHash": 403, "itemInstanceId": "2005", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802}
		]}},
		"characterInventories": {"data": {"c1": {"items": [
			{"itemHash": 102, "itemInstanceId": "2003", "lockable": true, "quantity": 1, "location": 1, "bucketHash": 1498876634},
			{"itemHash": 102, "quantity": 1, "location": 1, "bucketHash": 1498876634},
			{"itemHash": 403, "itemInstanceId": "2006", "lockable": true, "quantity": 1, "location": 4, "bucketHash": 215593132}
		]}}}
	}}`

	matched, owned, rows := preview(t, f, "is:dupe")
	assert.Equal(t, 1, matched)
	assert.Equal(t, 2, owned)
	assert.Equal(t, []string{"2003"}, rows)

	_, _, rows = preview(t, f, "count:2")
	assert.Empty(t, rows)

	_, _, rows = preview(t, f, "count:1")
	assert.Equal(t, []string{"2006", "2003"}, rows)
}

// inventoryFieldFakes returns fakes whose vault holds Gjallarhorn and two
// copies of Fatebringer. The definitions give Gjallarhorn the exotic tier
// type and Fatebringer the legendary one. Gjallarhorn deals Solar damage
// and has 1950 power. One Fatebringer deals Arc damage and has 1850
// power, and the other deals Kinetic damage and has 1900 power.
func inventoryFieldFakes(t *testing.T) *fakes {
	t.Helper()

	f := newFakes(t)
	defs := f.defs
	defs.DamageEnums = map[uint32]int{
		3373582085: bungie.DamageKinetic,
		2303181850: bungie.DamageArc,
		1847026933: bungie.DamageThermal,
	}

	for hash, tierType := range map[uint32]int{100: bungie.TierTypeLegendary, 101: bungie.TierTypeExotic} {
		def := defs.Items[hash]
		def.TierType = tierType
		defs.Items[hash] = def
	}

	f.bungie.ProfileBody = `{"ErrorCode": 1, "Response": {
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"profileInventory": {"data": {"items": [
			{"itemHash": 101, "itemInstanceId": "2001", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 100, "itemInstanceId": "2002", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 100, "itemInstanceId": "2003", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802}
		]}},
		"itemComponents": {"instances": {"data": {
			"2001": {"damageType": 3, "damageTypeHash": 1847026933, "primaryStat": {"statHash": 1480404414, "value": 1950}},
			"2002": {"damageType": 2, "damageTypeHash": 2303181850, "primaryStat": {"statHash": 1480404414, "value": 1850}},
			"2003": {"damageType": 1, "damageTypeHash": 3373582085, "primaryStat": {"statHash": 1480404414, "value": 1900}}
		}}}
	}}`

	return f
}

func TestPreviewInventoryFields(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  []string
	}{
		"exotic weapon":          {query: "is:exotic is:weapon", want: []string{"2001"}},
		"legendary":              {query: "is:legendary", want: []string{"2002", "2003"}},
		"power below 1900":       {query: "is:weapon power:<1900", want: []string{"2002"}},
		"light of at least 1900": {query: "light:>=1900", want: []string{"2003", "2001"}},
		"arc":                    {query: "is:arc", want: []string{"2002"}},
		"light damage":           {query: "is:light", want: []string{"2002", "2001"}},
		"power weapon slot":      {query: "is:power", want: []string{"2001"}},
		"has power":              {query: "is:haspower", want: []string{"2002", "2003", "2001"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, rows := preview(t, inventoryFieldFakes(t), tc.query)
			assert.Equal(t, tc.want, rows)
		})
	}
}

func TestPreviewUnsupportedPowerWord(t *testing.T) {
	t.Parallel()

	f := inventoryFieldFakes(t)

	got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "power:>=pinnaclecap"})
	assert.True(t, isError)
	assert.True(t, strings.HasPrefix(got, "unsupported query: the server does not evaluate power:>=pinnaclecap, "), got)

	got, isError = call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "power:=<pinnaclecap"})
	assert.True(t, isError)
	assert.Equal(t, "invalid input: the web client rejects power:=<pinnaclecap, "+
		"so it skips an autotag rule with this query", got)
	assert.Empty(t, f.service.All())
	assert.Empty(t, f.bungie.All())
}

func TestPreviewAutotagged(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.service.Data[pathTags] = tagRows(t, dvm.ItemTag{
		InstanceID: "1003",
		ItemHash:   102,
		TagID:      dvm.TagJunk,
		RuleID:     "rule-1",
		UpdatedAt:  5,
	})

	_, _, rows := preview(t, f, "is:autotagged")
	assert.Equal(t, []string{"1003"}, rows)

	// A tag that a person sets carries no rule id.
	f.service.Data[pathTags] = tagRows(
		t,
		dvm.ItemTag{InstanceID: "1003", ItemHash: 102, TagID: dvm.TagJunk, UpdatedAt: 6},
	)

	_, _, rows = preview(t, f, "is:autotagged")
	assert.Empty(t, rows)

	_, _, rows = preview(t, f, "tag:junk")
	assert.Equal(t, []string{"1003"}, rows)
}

func TestPreviewCurrentCharacter(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, _, rows := preview(t, f, "is:incurrentchar")
	assert.Equal(t, []string{"1004", "1005", "1006", "1003"}, rows)

	_, _, rows = preview(t, f, "is:currentclass")
	assert.Equal(t, []string{"1004"}, rows)
}

func TestPreviewParseError(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  string
	}{
		"stray paren": {
			query: "is:weapon )",
			want:  `invalid input: the query has a ")" that closes no "(", and the web client ignores ")"`,
		},
		"trailing or with a space": {
			query: "is:weapon or ",
			want: `invalid input: the web client reads the unquoted word "or" as a search for that word, ` +
				`not as an operator. Quote the word to search for it`,
		},
		"unclosed quote": {
			query: `name:"abc`,
			want:  `invalid input: the quote in "name:\"abc" never closes`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: tc.query})
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)
			assert.Empty(t, f.service.All())
			assert.Empty(t, f.bungie.All())

			query := tc.query
			tag := dvm.TagJunk

			saved, isError := call(t, f.h.handleSaveAutotagRule, SaveAutotagRuleInput{Query: &query, TagID: &tag})
			assert.True(t, isError)
			assert.Equal(t, saved, got)
		})
	}
}

func TestPreviewEmpty(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
	}{
		"empty":  {query: ""},
		"spaces": {query: "   "},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: tc.query})
			assert.True(t, isError)
			assert.Equal(t, "invalid input: the query has no search term", got)
			assert.Empty(t, f.service.All())
			assert.Empty(t, f.bungie.All())
		})
	}
}

func TestPreviewNoCharacter(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.bungie.ProfileBody = `{"ErrorCode": 1, "Response": {
		"profileInventory": {"data": {"items": [
			{"itemHash": 100, "itemInstanceId": "1001", "quantity": 1, "location": 2, "bucketHash": 138197802}
		]}},
		"characters": {"data": {}}
	}}`

	got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:weapon"})
	assert.True(t, isError)
	assert.Equal(t, "the account has no character, so the server previews nothing. "+
		"The web client matches every item when it has no current character.", got)
}

func TestPreviewProfileError(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.bungie.ProfileBody = `{"ErrorCode": 5, "ErrorStatus": "SystemDisabled", "Message": "down"}`

	want, isError := call(t, f.h.handleListItems, ListItemsInput{})
	require.True(t, isError)

	got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:weapon"})
	assert.True(t, isError)
	assert.Equal(t, want, got)
	assert.True(t, strings.HasPrefix(got, "fetching the profile:"), got)
}

// The texts of the rating filters of preview_autotag_rule and
// save_autotag_rule.
const (
	// previewAegisSheet rates Fatebringer (100) S with two perk columns,
	// Gjallarhorn (101) A without columns, and Midnight Coup (102)
	// without a tier or columns. Instance 1001 hits one column and rates
	// A. The fixture profile lists no perks for the other two copies of
	// Fatebringer, so their rolls are unknown.
	previewAegisSheet = `{"rolls": [` +
		`{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900], [904]]}, ` +
		`{"category": "Exotic Weapons", "tier": "A", "hashes": [101], "slots": []}, ` +
		`{"category": "HCs", "hashes": [102], "slots": []}]}`

	// previewPvPSheet rates Fatebringer (100) C with one perk column,
	// which picks Rampage (903). The fake perk preferences dislike
	// Rampage for PvP, so instance 1001 misses the column.
	previewPvPSheet = `{"rolls": [{"category": "Hand Cannon", "tier": "C", "hashes": [100], "slots": [[903]]}]}`

	undecidedHeading = "The server cannot decide these items:"
	undecidedHeader  = "instance id | name | location | reason"
	undecided1005    = "1005 | Fatebringer | Hunter (postmaster) | " + search.ReasonNoPerks
	undecided1006    = "1006 | Fatebringer | Hunter (equipped) | " + search.ReasonNoPerks

	noPvPSheet  = "PvP: rating unavailable: the service publishes no PvP sheet"
	unratedText = "the query reads a game mode without ratings. "
)

// withAegisSheet makes the service of f publish previewAegisSheet as the
// only roll sheet.
func withAegisSheet(f *fakes) {
	f.service.ArtifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
	f.service.Data["/artifacts/rolls.a.json"] = previewAegisSheet
}

// withBothSheets makes the service of f publish previewAegisSheet and
// previewPvPSheet.
func withBothSheets(f *fakes) {
	withAegisSheet(f)

	f.service.ArtifactFiles["pvprolls"] = []string{"pvprolls.a.json"}
	f.service.Data["/artifacts/pvprolls.a.json"] = previewPvPSheet
}

// ratingRequests returns the paths of the requests of f for a roll sheet
// or for the perk preferences.
func ratingRequests(f *fakes) []string {
	var paths []string

	for _, req := range f.service.All() {
		if strings.HasPrefix(req.Path, "/artifacts/") || req.Path == pathPrefs {
			paths = append(paths, req.Path)
		}
	}

	return paths
}

func TestPreviewUndecided(t *testing.T) {
	t.Parallel()

	const (
		first   = "2 of 6 owned items match, and the server cannot decide 2 more."
		row1001 = "1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 |  | Vault | keep | " +
			"favorite, locked, masterwork, note"
		row1002 = "1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 |  | Vault |  | "
	)

	tests := map[string]struct {
		in   PreviewAutotagRuleInput
		want []string
	}{
		"every item": {
			in: PreviewAutotagRuleInput{Query: "rate:>=a"},
			want: []string{
				first, "Items 1-2 of 2.", "", previewHeader, row1001, row1002, "",
				undecidedHeading, undecidedHeader, undecided1005, undecided1006,
			},
		},
		"one row of each": {
			in: PreviewAutotagRuleInput{Query: "rate:>=a", Limit: 1},
			want: []string{
				first, "Items 1-1 of 2. Pass offset=1 for the next page.", "", previewHeader, row1001, "",
				undecidedHeading, undecidedHeader, undecided1005, "1 more undecided items are not shown.",
			},
		},
		"the offset pages only the matches": {
			in: PreviewAutotagRuleInput{Query: "rate:>=a", Limit: 1, Offset: 1},
			want: []string{
				first, "Items 2-2 of 2.", "", previewHeader, row1002, "",
				undecidedHeading, undecidedHeader, undecided1005, "1 more undecided items are not shown.",
			},
		},
		"an offset past the last match": {
			in: PreviewAutotagRuleInput{Query: "rate:>=a", Offset: 5},
			want: []string{
				first, "Offset 2 is past the last of 2 matching items.", "",
				undecidedHeading, undecidedHeader, undecided1005, undecided1006,
			},
		},
		"a decided query keeps its text": {
			in:   PreviewAutotagRuleInput{Query: "is:weapon ratebase:a"},
			want: []string{"1 of 6 owned items match.", "Items 1-1 of 1.", "", previewHeader, row1002},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			withAegisSheet(f)

			got, isError := call(t, f.h.handlePreviewAutotagRule, tc.in)
			assert.False(t, isError)
			assert.Equal(t, strings.Join(tc.want, "\n"), got)
		})
	}
}

func TestPreviewUndecidedNoMatch(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	withAegisSheet(f)

	// No roll of the sheet rates S, so rate:s matches no decided item.
	got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "rate:s"})
	assert.False(t, isError)
	assert.Equal(t, strings.Join([]string{
		"No owned item matches the query, and the server cannot decide 2 more.", "",
		undecidedHeading, undecidedHeader, undecided1005, undecided1006,
	}, "\n"), got)

	got, isError = call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:weapon rate:f"})
	assert.False(t, isError)
	assert.Equal(t, strings.Join([]string{
		"No owned item matches the query, and the server cannot decide 2 more.", "",
		undecidedHeading, undecidedHeader, undecided1005, undecided1006,
	}, "\n"), got)

	_, _, rows := preview(t, f, "is:weapon ratebase:f")
	assert.Empty(t, rows, "a decided query without a match keeps the no-match text")
}

// The lines that end a preview or a save count that leaves out items that
// the web client does not build.
const (
	unbuiltNoArtifact = "The server leaves out 1 owned items that belong to an item set, " +
		"because the service publishes no EquipableItemSet artifact " +
		"and the web client builds no such item without it."
	unbuiltMissingSet = "The server leaves out 1 owned items whose item set the EquipableItemSet definitions lack, " +
		"because the web client builds no such item."
)

// withUnbuiltHelmet gives the Hunter of f a second helmet, instance 1007,
// whose definition names the item set 7. The definitions hold no item
// set, so the web client never builds the helmet. Listed records whether
// the service index lists the EquipableItemSet artifact.
func withUnbuiltHelmet(f *fakes, listed bool) {
	defs := f.defs
	defs.Items[201] = definitions.ItemDef{
		Name:       "Iron Truage Helm",
		TypeName:   "Helmet",
		Tier:       "Legendary",
		TierType:   bungie.TierTypeLegendary,
		Bucket:     bungie.BucketHelmet,
		ItemType:   bungie.ItemTypeArmor,
		ClassType:  bungie.ClassHunter,
		Equippable: true,
		SetHash:    7,
	}
	defs.ItemSetsListed = listed

	held := `{"itemHash": 201, "itemInstanceId": "1007", "lockable": true, "quantity": 1, ` +
		`"location": 1, "bucketHash": 3448274439}, `
	f.bungie.ProfileBody = `{"ErrorCode": 1, "Response": ` +
		strings.Replace(bungietest.ProfileJSON, `"c1": {"items": [`, `"c1": {"items": [`+held, 1) + `}`
}

func TestPreviewLeavesOutUnbuilt(t *testing.T) {
	t.Parallel()

	const row1004 = "1004 | Celestial Nighthawk | Helmet | Exotic | 2000 | 55 | Hunter (equipped) |  | "

	tests := map[string]struct {
		query  string
		want   []string
		listed bool
	}{
		"the service publishes no item sets": {
			query: "is:armor",
			want: []string{
				"1 of 6 owned items match.", "Items 1-1 of 1.", "", previewHeader, row1004, "", unbuiltNoArtifact,
			},
		},
		"the definitions lack the item set": {
			query:  "is:armor",
			listed: true,
			want: []string{
				"1 of 6 owned items match.", "Items 1-1 of 1.", "", previewHeader, row1004, "", unbuiltMissingSet,
			},
		},
		"no match": {
			query: "is:titan",
			want:  []string{previewNoMatch, "", unbuiltNoArtifact},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			withUnbuiltHelmet(f, tc.listed)

			got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: tc.query})
			assert.False(t, isError)
			assert.Equal(t, strings.Join(tc.want, "\n"), got)

			// The other tools keep the helmet that the preview leaves out.
			_, ok := f.h.snapshot.Find("1007")
			assert.True(t, ok)
		})
	}

	// A preview without such an item ends with its table.
	f := newFakes(t)

	got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:armor"})
	assert.False(t, isError)
	assert.True(t, strings.HasSuffix(got, "\n"+row1004), got)
}

func TestPreviewRatingModeMissing(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// files holds the artifact index of the service.
		files   map[string][]string
		profile string
		query   string
		// want is the text of an error result. An empty want marks a
		// call that succeeds.
		want string
		// requests lists the paths of the requests for a roll sheet or
		// for the perk preferences.
		requests []string
	}{
		"a query that reads the mode": {
			files:    map[string][]string{"rolls": {"rolls.a.json"}},
			query:    "pvprate:>=a",
			want:     unratedText + noPvPSheet,
			requests: []string{"/artifacts/rolls.a.json", pathPrefs},
		},
		"a query that reads another mode": {
			files:    map[string][]string{"rolls": {"rolls.a.json"}},
			query:    "rate:>=a",
			requests: []string{"/artifacts/rolls.a.json", pathPrefs},
		},
		"a query without a rating filter": {
			files: map[string][]string{"rolls": {"rolls.a.json"}},
			query: "is:weapon",
		},
		"the first mode in the order of the game modes": {
			files:    map[string][]string{},
			query:    "pvpmisses:>=1 or ratebase:a",
			want:     unratedText + "PvE: rating unavailable: the service publishes no Aegis sheet",
			requests: []string{pathPrefs},
		},
		"an account without a character": {
			files: map[string][]string{"rolls": {"rolls.a.json"}},
			profile: `{"ErrorCode": 1, "Response": {
				"profileInventory": {"data": {"items": []}},
				"characters": {"data": {}}
			}}`,
			query: "pvprate:>=a",
			want: "the account has no character, so the server previews nothing. " +
				"The web client matches every item when it has no current character.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.ArtifactFiles = tc.files
			f.service.Data["/artifacts/rolls.a.json"] = previewAegisSheet
			f.bungie.ProfileBody = tc.profile

			got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: tc.query})
			assert.Equal(t, tc.want != "", isError, got)

			if tc.want != "" {
				assert.Equal(t, tc.want, got)
			}

			assert.Equal(t, tc.requests, ratingRequests(f))
		})
	}
}

// saveJunkRule saves a new rule with query and the tag junk on f. It
// returns the text of the result, with "new" in place of the random id of
// the rule, and whether the result is an error.
func saveJunkRule(t *testing.T, f *fakes, query string) (string, bool) {
	t.Helper()

	junk := dvm.TagJunk

	got, isError := call(t, f.h.handleSaveAutotagRule, SaveAutotagRuleInput{Query: &query, TagID: &junk})

	for _, write := range f.service.Calls(http.MethodPost, pathAutotag) {
		rules, ok := write.Body["rules"].([]any)
		require.True(t, ok)
		require.Len(t, rules, 1)

		rule, ok := rules[0].(map[string]any)
		require.True(t, ok)

		id, ok := rule["id"].(string)
		require.True(t, ok)

		got = strings.Replace(got, "(id "+id+")", "(id new)", 1)
	}

	return got, isError
}

func TestSaveAutotagRuleUndecided(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	withAegisSheet(f)

	got, isError := saveJunkRule(t, f, "rate:>=a")
	assert.False(t, isError)
	assert.Equal(t, "Saved the rule:\n- 2. [enabled] \"rate:>=a\": tag junk (id new)\n"+
		"The query matches 2 owned items now, and the server cannot decide 2 more. "+
		"The count ignores the other rules, so an earlier rule can take some of these items first.", got)
	assert.Len(t, f.service.Calls(http.MethodPost, pathAutotag), 1)
}

func TestSaveAutotagRuleLeavesOutUnbuilt(t *testing.T) {
	t.Parallel()

	// noCharacters holds the helmet 1007 of withUnbuiltHelmet in the vault
	// of an account without a character.
	const noCharacters = `{"ErrorCode": 1, "Response": {
		"profileInventory": {"data": {"items": [
			{"itemHash": 201, "itemInstanceId": "1007", "lockable": true, "quantity": 1, "location": 2,
				"bucketHash": 138197802}
		]}},
		"characters": {"data": {}}
	}}`

	tests := map[string]struct {
		// profile replaces the profile of withUnbuiltHelmet when it is set.
		profile string
		query   string
		want    string
	}{
		"a count": {
			query: "is:armor",
			want: "Saved the rule:\n- 2. [enabled] \"is:armor\": tag junk (id new)\n" +
				"The query matches 1 owned items now. " +
				"The count ignores the other rules, so an earlier rule can take some of these items first.\n" +
				unbuiltNoArtifact,
		},
		"a line without a count": {
			query: "is:masterwork",
			want: "Saved the rule:\n- 2. [enabled] \"is:masterwork\": tag junk (id new)\n" +
				"The server does not evaluate is:masterwork, so it did not count the items that the query matches. " +
				"The web client might still accept those terms and run the rule.",
		},
		"an account without a character": {
			profile: noCharacters,
			query:   "is:armor",
			want: "Saved the rule:\n- 2. [enabled] \"is:armor\": tag junk (id new)\n" +
				"The rule is saved, but the account has no character, " +
				"so the server did not count the items that the query matches.",
		},
		"a game mode without ratings": {
			query: "pvprate:>=a",
			want: "Saved the rule:\n- 2. [enabled] \"pvprate:>=a\": tag junk (id new)\n" +
				"The rule is saved, but the query reads a game mode without ratings, " +
				"so the server did not count the items that the query matches. " + noPvPSheet,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			withUnbuiltHelmet(f, false)

			if tc.profile != "" {
				f.bungie.ProfileBody = tc.profile
			}

			got, isError := saveJunkRule(t, f, tc.query)
			assert.False(t, isError)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSaveAutotagRuleRatingModeMissing(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	withAegisSheet(f)

	got, isError := saveJunkRule(t, f, "pvprate:>=a")
	assert.False(t, isError)
	assert.Equal(t, "Saved the rule:\n- 2. [enabled] \"pvprate:>=a\": tag junk (id new)\n"+
		"The rule is saved, but the query reads a game mode without ratings, "+
		"so the server did not count the items that the query matches. "+noPvPSheet, got)
	assert.Len(t, f.service.Calls(http.MethodPost, pathAutotag), 1)
}

func TestSaveAutotagRuleInvalidRating(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
	}{
		"a rate: value that is no tier":  {query: "rate:>=z"},
		"a misses: value that is a tier": {query: "misses:a"},
		"a pvpratebase: operator":        {query: "pvpratebase:=>a"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			withAegisSheet(f)

			got, isError := saveJunkRule(t, f, tc.query)
			assert.True(t, isError)
			assert.Equal(t, "invalid input: the web client rejects "+tc.query+
				", so it skips an autotag rule with this query", got)
			assert.Empty(t, f.service.Calls(http.MethodPost, pathAutotag))
			assert.Empty(t, f.bungie.All())
		})
	}
}

func TestPreviewExampleRules(t *testing.T) {
	t.Parallel()

	const verdictRows = "1001 | Fatebringer | Vault | " + search.ReasonVerdictNoPerks + "\n" +
		"1005 | Fatebringer | Hunter (postmaster) | " + search.ReasonVerdictNoPerks + "\n" +
		"1006 | Fatebringer | Hunter (equipped) | " + search.ReasonVerdictNoPerks

	// Each query is the query of an example rule of the rule editor of
	// the web client. The profile lists no perks for two of the three
	// copies of Fatebringer, so the server cannot decide the verdict of
	// any copy.
	tests := map[string]struct {
		query string
		// want opens the text of the result.
		want string
	}{
		"keep better copies": {
			query: "verdict:better or pvpverdict:better",
			want: "No owned item matches the query, and the server cannot decide 3 more.\n\n" +
				undecidedHeading + "\n" + undecidedHeader + "\n" + verdictRows,
		},
		"keep top PvE rolls": {
			query: "rate:>=a",
			want:  "2 of 6 owned items match, and the server cannot decide 2 more.",
		},
		"keep top PvP rolls": {
			query: "pvprate:>=a",
			want:  "No owned item matches the query, and the server cannot decide 2 more.",
		},
		"junk worse copies": {
			query: "verdict:worse pvpverdict:worse",
			want: "No owned item matches the query, and the server cannot decide 3 more.\n\n" +
				undecidedHeading + "\n" + undecidedHeader + "\n" + verdictRows,
		},
		"junk spare rolls that miss both columns": {
			query: "is:weapon misses:2 is:dupe",
			want:  "No owned item matches the query, and the server cannot decide 2 more.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			withBothSheets(f)

			got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: tc.query})
			assert.False(t, isError, got)
			assert.True(t, strings.HasPrefix(got, tc.want), got)
		})
	}
}

func TestCompleteLoginTool(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleCompleteLogin, CompleteLoginInput{RedirectURL: ""})
	assert.True(t, isError)
	assert.Equal(t, "login: nothing pasted", got)
}

func TestReadLine(t *testing.T) {
	t.Parallel()

	got, err := readLine(t.Context(), strings.NewReader("first\nsecond\n"))
	require.NoError(t, err)
	assert.Equal(t, "first\n", got)

	got, err = readLine(t.Context(), strings.NewReader("no newline"))
	require.NoError(t, err)
	assert.Equal(t, "no newline", got, "the end of the input ends the line")

	// A pipe without a writer blocks like a terminal that waits for input.
	blocked, w := io.Pipe()
	t.Cleanup(func() { require.NoError(t, w.Close()) })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = readLine(ctx, blocked)
	require.ErrorIs(t, err, context.Canceled)
}

func TestOpenLoggerDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "state")

	_, closeLog, err := openLogger(filepath.Join(dir, "destiny.log"))
	require.NoError(t, err)
	t.Cleanup(closeLog)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestDispatch(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := dispatch(t.Context(), []string{"export"}, strings.NewReader(""), &out)
	require.ErrorIs(t, err, ErrUnknownSubcommand)

	dir := t.TempDir()

	err = dispatch(t.Context(), []string{subLogout, "--state-dir", dir}, strings.NewReader(""), &out)
	require.NoError(t, err)
	assert.Equal(t, "Signed out.\n", out.String())
}

// statsGapNote is the line of the get_stats output that names the changes
// that the web client has not posted.
const statsGapNote = "The web client records changes only while it is open in a browser. " +
	"It posts the changes of each 5-minute bucket after the bucket closes, " +
	"so the current bucket and every bucket that it has not posted yet are missing."

// statsFixture returns an /api/stats page of changes from the day before
// bungietest.Now.
func statsFixture(t *testing.T) string {
	t.Helper()

	ago := func(hours float64) int64 {
		return bungietest.Now.Add(-time.Duration(hours * float64(time.Hour))).UnixMilli()
	}

	change := func(kind string, hash uint32, hours float64, before, after int) map[string]any {
		at := ago(hours)

		return map[string]any{
			"kind":     kind,
			"itemHash": hash,
			"from":     at - 15_000,
			"at":       at,
			"before":   before,
			"after":    after,
		}
	}

	kills := func(instanceID string, itemHash, objectiveHash uint32, hours float64, before, after int) map[string]any {
		out := change("objective", itemHash, hours, before, after)
		out["instanceId"] = instanceID
		out["objectiveHash"] = objectiveHash

		return out
	}

	return dvmtest.StatPage(t, false,
		dvmtest.Chunk{StartsAt: 0, UpdatedAt: 1, Changes: []map[string]any{
			// Before the lookback of the period.
			change("currency", bungietest.ItemGlimmer, 30, 0, 1_000),
			change("currency", bungietest.ItemGlimmer, 23, 1_000, 2_000),
			change("currency", bungietest.ItemGlimmer, 22, 2_000, 1_500),
			kills("9999", 102, bungietest.TrackerGambit, 6, 0, 20),
			kills("1001", 100, bungietest.TrackerPvE, 5, 100, 110),
		}},
		dvmtest.Chunk{StartsAt: 300_000, UpdatedAt: 2, Changes: []map[string]any{
			// A stale recorder logged the gain from 1000.
			change("currency", bungietest.ItemGlimmer, 21, 1_000, 1_700),
			change("stack", 300, 4, 40, 52),
			kills("1001", 100, bungietest.TrackerPvP, 4, 7, 9),
			change("currency", 77, 3, 3, 5),
			// The service withholds the definitions of both stacks, and
			// only the vendor item names name the second one.
			change("stack", definitionstest.ItemWithheld, 3, 0, 1),
			change("stack", 501, 3, 10, 5),
			change("currency", bungietest.ItemBrightDust, 2, 1_500, 1_400),
			{
				"kind":       "item",
				"instanceId": "1007",
				"itemHash":   101,
				"added":      true,
				"from":       ago(1) - 15_000,
				"at":         ago(1),
			},
		}},
	)
}

func TestGetStats(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.service.Data[pathStats] = statsFixture(t)
	f.bungie.Data[bungietest.PathItemDefinition(77)] = bungietest.NoItemDefinitionJSON
	f.bungie.Data[bungietest.PathItemDefinition(definitionstest.ItemWithheld)] = bungietest.ItemDefinitionJSON(
		definitionstest.ItemWithheld, "Wish-Keeper Snare Refit", false,
	)

	got, isError := call(t, f.h.handleGetStats, GetStatsInput{})
	require.False(t, isError, got)
	assert.Equal(t, strings.Join([]string{
		"Period: Last 24 hours, 2026-11-11 12:00 UTC to 2026-11-12 12:00 UTC.",
		"Newest recorded change: 2026-11-12 11:00 UTC.",
		statsGapNote,
		"",
		"Currencies and materials, currencies first:",
		"name | kind | item hash | net | gained | spent",
		"Glimmer | currency | 3159615086 | +700 | 1200 | 500",
		"Bright Dust | currency | 2817410917 | -100 | 0 | 100",
		"Unknown item 77 | currency | 77 | +2 | 2 | 0",
		"Enhancement Core | stack | 300 | +12 | 12 | 0",
		"Legendary Shards | stack | 501 | -5 | 0 | 5",
		"Wish-Keeper Snare Refit | stack | 78 | +1 | 1 | 0",
		"",
		"Weapon kills, most first:",
		"instance id | name | total | PvE | PvP",
		"9999 | Midnight Coup | 20 | 20 | 0",
		"1001 | Fatebringer | 12 | 10 | 2",
		"",
		"The definitions of manifest version v1 and the live manifest of Bungie both lack " +
			"the item with the hash 77, so the server cannot name it.",
	}, "\n"), got)

	reads := f.service.Calls(http.MethodGet, pathStats)
	require.Len(t, reads, 1)
	assert.Equal(t, "Bearer session-1", reads[0].Auth)
	assert.Equal(t, fmt.Sprintf("since=%d&after=0", bungietest.Now.Add(-27*time.Hour).UnixMilli()), reads[0].Query)

	// The tool reads no profile. It looks up only the hashes that the
	// definitions do not name, and it keeps each answer.
	_, isError = call(t, f.h.handleGetStats, GetStatsInput{})
	require.False(t, isError)
	assert.ElementsMatch(t, []string{
		bungietest.PathItemDefinition(77),
		bungietest.PathItemDefinition(definitionstest.ItemWithheld),
	}, bungiePaths(f))
}

// bungiePaths returns the path of each request that the fake Bungie server
// received, in the order of arrival.
func bungiePaths(f *fakes) []string {
	var paths []string

	for _, req := range f.bungie.All() {
		paths = append(paths, req.Path)
	}

	return paths
}

// TestItemNamesFromBungie pins that the tools that render items name an
// item that the definitions do not name with the name that the live
// manifest of Bungie gives it. The fixture profile holds the currency 77,
// which the definitions lack. The test adds a stack of the withheld item
// 78 to the kinetic slot of the Hunter.
func TestItemNamesFromBungie(t *testing.T) {
	t.Parallel()

	stack := `{"itemHash": 78, "quantity": 34, "location": 1, "bucketHash": 1498876634}, `
	profile := `{"ErrorCode": 1, "Response": ` +
		strings.Replace(bungietest.ProfileJSON, `"c1": {"items": [`, `"c1": {"items": [`+stack, 1) + `}`

	tests := map[string]struct {
		run  func(t *testing.T, f *fakes) string
		want []string
	}{
		"list_characters": {
			run: func(t *testing.T, f *fakes) string {
				t.Helper()

				got, isError := call(t, f.h.handleListCharacters, EmptyInput{})
				require.False(t, isError, got)

				return got
			},
			want: []string{"- Strange Coin: 3"},
		},
		"list_items": {
			run: func(t *testing.T, f *fakes) string {
				t.Helper()

				got, isError := call(
					t,
					f.h.handleListItems,
					ListItemsInput{Kind: inventory.KindOther, Location: "hunter"},
				)
				require.False(t, isError, got)

				return got
			},
			want: []string{"stack of 34 (item hash 78) | Wish-Keeper Snare Refit | "},
		},
		"stage_junk": {
			run: func(t *testing.T, f *fakes) string {
				t.Helper()

				f.service.Data[pathTags] = junkTags(t, "1003")

				got, isError := call(t, f.h.handleStageJunk, StageJunkInput{Slot: "kinetic"})
				require.False(t, isError, got)

				return got
			},
			want: []string{"so stage_junk leaves it in place: Wish-Keeper Snare Refit (stack of 34, item hash 78)."},
		},
		"get_stats": {
			run: func(t *testing.T, f *fakes) string {
				t.Helper()

				f.service.Data[pathStats] = statsFixture(t)

				got, isError := call(t, f.h.handleGetStats, GetStatsInput{})
				require.False(t, isError, got)
				assert.True(
					t,
					strings.HasSuffix(got, "1001 | Fatebringer | 12 | 10 | 2"),
					"no reason follows the tables",
				)

				return got
			},
			want: []string{
				"Strange Coin | currency | 77 | +2 | 2 | 0",
				"Wish-Keeper Snare Refit | stack | 78 | +1 | 1 | 0",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.bungie.ProfileBody = profile
			f.bungie.Data[bungietest.PathItemDefinition(77)] = bungietest.ItemDefinitionJSON(77, "Strange Coin", false)
			f.bungie.Data[bungietest.PathItemDefinition(78)] = bungietest.ItemDefinitionJSON(
				78, "Wish-Keeper Snare Refit", false,
			)

			got := tc.run(t, f)
			for _, want := range tc.want {
				assert.Contains(t, got, want)
			}

			assert.NotContains(t, got, "Unknown item")

			// A later profile keeps the answers, so each hash costs one
			// lookup.
			_, err := f.h.loadSnapshot(t.Context(), true)
			require.NoError(t, err)

			lookups := slices.DeleteFunc(bungiePaths(f), func(path string) bool {
				return !strings.HasPrefix(path, "/Destiny2/Manifest/")
			})
			assert.ElementsMatch(t, []string{
				bungietest.PathItemDefinition(77),
				bungietest.PathItemDefinition(78),
			}, lookups)
		})
	}
}

// TestGetStatsFailedLookups pins that the lookups at Bungie that fail for
// one cause share a sentence after the tables. The error of each request
// names the path of its own item hash, so the sentence must not quote it.
func TestGetStatsFailedLookups(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// answer answers each request of the fake Bungie platform. The
		// tool sends only the lookups to Bungie.
		answer func(t *testing.T, done <-chan struct{}) http.HandlerFunc
		reason string
	}{
		"gateway error": {
			answer: func(*testing.T, <-chan struct{}) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "<html>Bad Gateway</html>", http.StatusBadGateway)
				}
			},
			reason: "Bungie answered with the HTTP status 502 and no platform response",
		},
		"maintenance": {
			answer: func(t *testing.T, _ <-chan struct{}) http.HandlerFunc {
				t.Helper()

				return func(w http.ResponseWriter, _ *http.Request) {
					// The message of Bungie ends with its own period.
					httpfake.WriteJSON(
						t,
						w,
						http.StatusServiceUnavailable,
						`{"ErrorCode": 5, "ErrorStatus": "SystemDisabled", `+
							`"Message": "This system is temporarily disabled for maintenance."}`,
					)
				}
			},
			reason: "bungie: SystemDisabled: This system is temporarily disabled for maintenance",
		},
		"body cut short": {
			answer: func(t *testing.T, _ <-chan struct{}) http.HandlerFunc {
				t.Helper()

				return func(w http.ResponseWriter, _ *http.Request) {
					// The server closes the connection after a body
					// shorter than its declared length.
					w.Header().Set("Content-Length", "100")
					httpfake.WriteJSON(t, w, http.StatusOK, `{"ErrorCode": 1`)
				}
			},
			reason: "reading the item definition: unexpected EOF",
		},
		"response that does not decode": {
			answer: func(t *testing.T, _ <-chan struct{}) http.HandlerFunc {
				t.Helper()

				return func(w http.ResponseWriter, _ *http.Request) {
					httpfake.WriteJSON(
						t,
						w,
						http.StatusOK,
						`{"ErrorCode": 1, "ErrorStatus": "Success", "Response": [1]}`,
					)
				}
			},
			reason: "bungie: decoding the item definition: " +
				"json: cannot unmarshal array into Go value of type bungie.ItemDefinition",
		},
		"stalled lookups": {
			answer: func(_ *testing.T, done <-chan struct{}) http.HandlerFunc {
				return func(_ http.ResponseWriter, r *http.Request) {
					select {
					case <-r.Context().Done():
					case <-done:
					}
				}
			},
			reason: "the lookups ran out of time",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.Data[pathStats] = statsFixture(t)

			// The cleanups run in reverse order, so the stalled handlers
			// return before the server closes.
			done := make(chan struct{})
			platform := httptest.NewServer(tc.answer(t, done))
			t.Cleanup(platform.Close)
			t.Cleanup(func() { close(done) })

			f.h.bungie = bungie.New(bungie.WithURL(platform.URL))
			f.h.names = definitions.NewNameStore(
				bungieItems{h: f.h},
				"",
				definitions.WithLookupTimeout(200*time.Millisecond),
			)

			got, isError := call(t, f.h.handleGetStats, GetStatsInput{})
			require.False(t, isError, got)

			want := "The service withholds the definitions of dummy items, classified items, " +
				"and items without a name, so the server cannot name the item with the hash 78. " +
				"The definitions of manifest version v1 lack the item with the hash 77, so the server cannot name it. " +
				"The game can hold an item that Bungie added in a later manifest version. " +
				"The lookup of the items with the hashes 77 and 78 at Bungie failed: " + tc.reason + ". " +
				"A later call looks them up again."
			assert.Equal(t, want, got[strings.LastIndex(got, "\n")+1:])
		})
	}
}

func TestGetStatsPeriods(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		period    string
		wantQuery string
		want      []string
	}{
		"all": {
			period:    "all",
			wantQuery: "since=0&after=0",
			want: []string{
				"Period: All time, up to 2026-11-12 12:00 UTC.",
				"Glimmer | currency | 3159615086 | +1700 | 2200 | 500",
			},
		},
		"7d": {
			period:    "7d",
			wantQuery: fmt.Sprintf("since=%d&after=0", bungietest.Now.Add(-7*24*time.Hour-3*time.Hour).UnixMilli()),
			want: []string{
				"Period: Last 7 days, 2026-11-05 12:00 UTC to 2026-11-12 12:00 UTC.",
				"Glimmer | currency | 3159615086 | +1700 | 2200 | 500",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.Data[pathStats] = statsFixture(t)

			got, isError := call(t, f.h.handleGetStats, GetStatsInput{Period: tc.period})
			require.False(t, isError, got)

			for _, want := range tc.want {
				assert.Contains(t, got, want)
			}

			reads := f.service.Calls(http.MethodGet, pathStats)
			require.Len(t, reads, 1)
			assert.Equal(t, tc.wantQuery, reads[0].Query)
		})
	}
}

func TestGetStatsEmpty(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		period string
		want   []string
	}{
		"a read from 3 hours before the period start": {
			period: "30d",
			want: []string{
				"Period: Last 30 days, 2026-10-13 12:00 UTC to 2026-11-12 12:00 UTC.",
				"Newest recorded change since 2026-10-13 09:00 UTC: none.",
			},
		},
		"a read of the whole history": {
			period: "all",
			want: []string{
				"Period: All time, up to 2026-11-12 12:00 UTC.",
				"Newest recorded change: none.",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.service.Data[pathStats] = dvmtest.StatPage(t, false)

			got, isError := call(t, f.h.handleGetStats, GetStatsInput{Period: tc.period})
			require.False(t, isError, got)
			assert.Equal(t, strings.Join(append(tc.want,
				statsGapNote,
				"",
				"The web client recorded no currency, material, or weapon kill change in this period.",
			), "\n"), got)
		})
	}
}

func TestGetStatsRefreshesSession(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.service.Data[pathStats] = statsFixture(t)
	f.service.StaleSession = "session-1"

	got, isError := call(t, f.h.handleGetStats, GetStatsInput{})
	require.False(t, isError, got)
	assert.Contains(t, got, "Glimmer | currency | 3159615086 | +700 | 1200 | 500")

	assert.Len(t, f.service.Calls(http.MethodPost, "/api/auth/refresh"), 1)

	reads := f.service.Calls(http.MethodGet, pathStats)
	require.Len(t, reads, 2)
	assert.Equal(t, "Bearer session-1", reads[0].Auth)
	assert.Equal(t, "Bearer session-2", reads[1].Auth)

	// The lookups of the unnamed hashes carry the refreshed access token.
	lookups := f.bungie.All()
	assert.Len(t, lookups, 2)

	for _, req := range lookups {
		assert.Equal(t, "Bearer access-2", req.Auth, req.Path)
	}
}

func TestGetStatsErrors(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleGetStats, GetStatsInput{Period: "1y"})
	assert.True(t, isError)
	assert.Contains(t, got, stats.ErrInvalidInput.Error())
	assert.Contains(t, got, "24h, 7d, 30d, all")
	assert.Empty(t, f.service.All())
	assert.Empty(t, f.bungie.All())

	f.service.Data[pathStats] = `{"chunks": [{"startsAt": 0, "endsAt": 300000, "updatedAt": 1, "body": "{}"}], "more": false}`

	got, isError = call(t, f.h.handleGetStats, GetStatsInput{})
	assert.True(t, isError)
	assert.Contains(t, got, dvm.ErrService.Error())
}
