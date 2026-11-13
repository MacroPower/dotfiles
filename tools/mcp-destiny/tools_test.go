package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	pathStats    = "/api/stats"
	pathTags     = "/api/tags"
	pathNotes    = "/api/notes"
	pathPrefs    = "/api/perk-preferences"
	pathAutotag  = "/api/autotag"
	pathProfile  = "/Destiny2/3/Profile/4611/"
	pathTransfer = "/Destiny2/Actions/Items/TransferItem/"
	pathEquip    = "/Destiny2/Actions/Items/EquipItem/"
	pathLock     = "/Destiny2/Actions/Items/SetLockState/"
	pathInsert   = "/Destiny2/Actions/Items/InsertSocketPlugFree/"
	pathLoadout  = "/Destiny2/Actions/Loadouts/EquipLoadout/"
)

const loadoutsIntro = "Loadouts saved in the game. A loadout number is the position of the loadout " +
	"on the in-game loadout screen, counted from 1. The server cannot show the icon or the color " +
	"of a loadout, because the game defines both only as images. " +
	"Pass the character and the loadout number to equip_loadout."

// killClipPlugSet gives socket 1 of Fatebringer (100) a plug set that
// lists Firefly and the two Kill Clip plugs. Fatebringer has no plug set
// for socket 0. Socket 1 of instance 1001 holds Firefly, which the fake
// perk preferences like for PvE.
func killClipPlugSet(d *Defs) {
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
		toolRemoveAutotagRule,
		toolMoveStack, toolStageJunk, toolSetLock, toolSetPerk, toolSetAutotagEnabled, toolListActivityHistory,
		toolListLoadouts, toolEquipLoadout, toolGetStats, toolListVendors, toolListActivities,
	}, names)

	for _, tool := range tools.Tools {
		switch tool.Name {
		case toolListVendors, toolListActivities:
			assert.Equal(t, &mcp.ToolAnnotations{ReadOnlyHint: true}, tool.Annotations)
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

	saved.RefreshExpires = fixtureNow.Add(-time.Hour)
	require.NoError(t, f.h.auth.store.Save(saved))

	got, isError = call(t, f.h.handleStatus, EmptyInput{})
	assert.False(t, isError)
	assert.Contains(t, got, "Signed in: no")
	assert.Contains(t, got, "expired on 2026-11-12 11:00 UTC")

	saved.RefreshToken = ""
	saved.RefreshExpires = time.Time{}
	require.NoError(t, f.h.auth.store.Save(saved))

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

	var profile Profile

	require.NoError(t, json.Unmarshal([]byte(fixtureProfile), &profile))

	profile.ProfileCurrencies.Data.Items = nil
	defs := fixtureDefs()
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
	}, "\n"), renderCharacters(NewSnapshot(&profile, defs, fixtureNow)))
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
		f.artifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
		f.serviceData["/artifacts/rolls.a.json"] = aegisSheet
	}

	// bothSheets makes the service publish the Aegis sheet and the PvP
	// sheet.
	bothSheets := func(f *fakes) {
		aegisOnly(f)

		f.artifactFiles["pvprolls"] = []string{"pvprolls.a.json"}
		f.serviceData["/artifacts/pvprolls.a.json"] = pvpSheet
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
			in: ListItemsInput{Sort: sortPower, Limit: 2, Offset: 2},
			want: strings.Join([]string{
				"Items 3-4 of 6. Pass offset=4 for the next page.",
				"",
				"instance id | name | type | tier | power | stat total | location | tag | flags",
				"1004 | Celestial Nighthawk | Helmet | Exotic | 2000 | 55 | Hunter (equipped) |  | ",
				"1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 |  | Vault |  | ",
			}, "\n"),
		},
		"stack": {
			in: ListItemsInput{Kind: kindOther},
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
			in: ListItemsInput{Stat: "HEALTH", Sort: sortStat, Limit: 2},
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
			in:      ListItemsInput{Kind: kindOther, IncludeRatings: true},
			want: strings.Join([]string{
				"Items 1-1 of 1.",
				"",
				ratedHeader,
				"stack of 42 (item hash 300) | Enhancement Core | Material | Legendary |  |  | Account |  |  |  | ",
			}, "\n"),
		},
		"armor has empty rating cells also without a sheet": {
			in: ListItemsInput{Kind: kindArmor, IncludeRatings: true},
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
				f.artifactFiles = map[string][]string{"rolls": {"rolls.x.json"}}
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
				delete(f.serviceData, pathPrefs)
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

				f.serviceData[pathPrefs] = `{"preferences": [], "now": 10}`
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
				killClipPlugSet(f.h.defs.defs)
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
				killClipPlugSet(f.h.defs.defs)
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

				f.serviceData["/artifacts/rolls.a.json"] = `{"rolls": [` +
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
				f.artifactFiles = map[string][]string{"rolls": {"rolls.x.json"}}
			},
			in: ListItemsInput{MinPvERating: "A"},
			want: "min_pve_rating: rating unavailable: fetching artifact rolls.x.json: " +
				"service: {service}/artifacts/rolls.x.json returned status 404",
			isError: true,
		},
		"minimum without the perk preferences": {
			prepare: func(f *fakes) {
				aegisOnly(f)
				delete(f.serviceData, pathPrefs)
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

				f.h.defs.defs = nil
			}

			got, isError := call(t, f.h.handleListItems, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, strings.ReplaceAll(tc.want, "{service}", f.h.service.BaseURL()), got)
		})
	}
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
			f.artifactFiles = map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}}
			f.serviceData["/artifacts/rolls.a.json"] = sheet
			f.serviceData["/artifacts/pvprolls.a.json"] = sheet

			for i, step := range tc.steps {
				_, isError := call(t, f.h.handleListItems, step.in)
				assert.Equalf(t, step.isError, isError, "call %d", i+1)

				var fetched []string

				for _, req := range f.service.all() {
					if strings.HasPrefix(req.Path, "/artifacts/") {
						fetched = append(fetched, req.Path)
					}
				}

				assert.Equalf(t, step.fetched, fetched, "call %d", i+1)
				assert.Lenf(t, f.service.calls(http.MethodGet, "/api/config"), step.configs, "call %d", i+1)
				assert.Lenf(t, f.service.calls(http.MethodGet, pathPrefs), step.prefs, "call %d", i+1)
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
			f.artifactFiles = map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"pvprolls.a.json"}}
			f.serviceData["/artifacts/rolls.a.json"] = sheet
			f.serviceData["/artifacts/pvprolls.a.json"] = sheet

			_, isError := call(t, f.h.handleListItems, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Len(t, f.service.calls(http.MethodGet, pathPrefs), tc.prefs)

			if tc.quiet {
				assert.Empty(t, f.service.all())
				assert.Empty(t, f.bungie.all())
			}
		})
	}
}

func TestRenderItemsStatTotal(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		item *Item
		want string
	}{
		"armor with a total of 0": {
			item: &Item{
				Name:  "Zero Helmet",
				Kind:  kindArmor,
				Stats: []Stat{{Name: "Grenade", Value: 0}, {Name: "Health", Value: 0}},
			},
			want: "0",
		},
		"armor without stats": {
			item: &Item{Name: "Bare Helmet", Kind: kindArmor},
			want: "",
		},
		"weapon with stats": {
			item: &Item{Name: "Ace of Spades", Kind: kindWeapon, Stats: []Stat{{Name: "Range", Value: 60}}},
			want: "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tc.item.InstanceID = "21"
			tc.item.Owner = ownerVault
			tc.item.Transferable = true

			got := renderItems(&Snapshot{}, []*Item{tc.item}, 0, 1, "", false, nil)
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

	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 1, "the second call reuses the profile")
	assert.Len(t, f.service.calls(http.MethodGet, pathTags), 2, "every call reads the tags")

	_, isError := call(t, f.h.handleListItems, ListItemsInput{Refresh: true})
	require.False(t, isError)
	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 2)

	saved, err := f.h.auth.Saved()
	require.NoError(t, err)
	assert.Equal(t, Membership{ID: "4611", Type: 3}, saved.Membership)
	assert.Len(t, f.bungie.calls(http.MethodGet, "/User/GetMembershipsForCurrentUser/"), 1)
}

func TestGetItem(t *testing.T) {
	t.Parallel()

	// fatebringer returns the output for instance 1001 with the given
	// lines of the roll ratings block.
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
			"- Firefly (Trait)",
			"  Precision final blows cause targets to explode.",
			"  Explosions deal Solar damage.",
			"",
			"Roll ratings:",
		}, ratings, []string{
			"",
			"Stats:",
			"- Range: 55",
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
		}, ratings), "\n")
	}

	tests := map[string]struct {
		// files holds the file list of each artifact in the service
		// index.
		files map[string][]string
		// bodies holds the body of each sheet file that the service
		// serves.
		bodies map[string]string
		// defs changes the fixture definitions before the call.
		defs func(*Defs)
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
		"copy without perks has an unknown roll and ends with the ratings": {
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
			f.artifactFiles = tc.files

			for file, body := range tc.bodies {
				f.serviceData["/artifacts/"+file] = body
			}

			if tc.prefsBody != "" {
				f.serviceData[pathPrefs] = tc.prefsBody
			}

			if tc.noPrefs {
				delete(f.serviceData, pathPrefs)
			}

			if tc.defs != nil {
				tc.defs(f.h.defs.defs)
			}

			if tc.noDefs {
				_, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: tc.id})
				require.False(t, isError)

				f.h.defs.defs = nil
			}

			got, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: tc.id})
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, strings.ReplaceAll(tc.want, "{service}", f.h.service.BaseURL()), got)

			var fetched []string

			for _, req := range f.service.all() {
				if strings.HasPrefix(req.Path, "/artifacts/") {
					fetched = append(fetched, req.Path)
				}
			}

			assert.Equal(t, tc.fetched, fetched)
			assert.Len(t, f.service.calls(http.MethodGet, "/api/config"), tc.configs)
			assert.Len(t, f.service.calls(http.MethodGet, pathPrefs), tc.prefs)
		})
	}
}

// TestGetItemRatesOnEachCall pins that get_item rates the copy from the
// cached snapshot on every call. A rating that the server kept from the
// first call names Explosive Payload first after the perk change.
func TestGetItemRatesOnEachCall(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.artifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
	f.serviceData["/artifacts/rolls.a.json"] = `{"rolls": [` +
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
	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 1, "both ratings read one profile")
}

// TestGetItemReadsPerkPreferencesOnEachCall pins that the server keeps
// no copy of the perk preferences. A preference that the user changes in
// the web client counts in the next call.
func TestGetItemReadsPerkPreferencesOnEachCall(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.artifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
	f.serviceData["/artifacts/rolls.a.json"] = `{"rolls": [` +
		`{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[901]]}]}`

	got, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
	require.False(t, isError)
	assert.Contains(t, got, "- PvE: S for this roll, S on the Aegis sheet (in HCs), 1 of 1 perk columns hit\n"+
		"  Column 1: hit with Firefly (liked). The sheet picks Firefly\n")

	f.serviceData[pathPrefs] = `{"preferences": [
		{"mode": "pve", "perk": "Firefly", "stance": "down", "removed": false, "updatedAt": 6}
	], "now": 11}`

	got, isError = call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
	require.False(t, isError)
	assert.Contains(t, got, "- PvE: A for this roll, S on the Aegis sheet (in HCs), 0 of 1 perk columns hit\n"+
		"  Column 1: miss\n")
	assert.Len(t, f.service.calls(http.MethodGet, pathPrefs), 2)
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
			f.artifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
			f.serviceData["/artifacts/rolls.a.json"] = `{"rolls": [` +
				`{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900]]}]}`
			f.h.defs.defs = nil

			got, isError := tc.run(t, f.h)
			assert.True(t, isError)
			assert.Equal(t, "loading definitions: definitions: the service index lists no core artifact", got)
			assert.Empty(t, f.bungie.calls(http.MethodGet, pathProfile), "the call fetches no profile")
			assert.Empty(t, f.service.calls(http.MethodGet, pathPrefs), "the call rates no roll")
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
		// [ErrNoRating].
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
				modePvE: {"Firefly": stanceUp},
				modePvP: {"Rampage": stanceDown},
			},
		},
		"mode without a sheet carries the reason of the sheet": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			stances: map[string]map[string]string{modePvE: {"Firefly": stanceUp}},
			reasons: map[string]string{modePvP: noPvP},
		},
		"unreadable perk preferences are the reason of each mode with a sheet": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			reasons: map[string]string{modePvE: unreadable, modePvP: noPvP},
			causes:  map[string]error{modePvE: ErrService},
			noPrefs: true,
		},
		"unreadable definitions are the reason of each mode with a sheet": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}},
			reasons: map[string]string{modePvE: undefined, modePvP: noPvP},
			causes:  map[string]error{modePvE: ErrDefs},
			noDefs:  true,
		},
		"unreadable perk preferences win against unreadable definitions": {
			files:   map[string][]string{"rolls": {"rolls.a.json"}, "pvprolls": {"rolls.a.json"}},
			reasons: map[string]string{modePvE: unreadable, modePvP: unreadable},
			causes:  map[string]error{modePvE: ErrService, modePvP: ErrService},
			noPrefs: true,
			noDefs:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			f.artifactFiles = tc.files
			f.serviceData["/artifacts/rolls.a.json"] = sheet

			if tc.noPrefs {
				delete(f.serviceData, pathPrefs)
			}

			defs := f.h.defs.defs

			if tc.noDefs {
				f.h.defs.defs = nil
			}

			got := f.h.ratingSources(t.Context())
			require.Len(t, got, len(perkModes))

			for _, mode := range perkModes {
				source := got[mode]

				reason, ok := tc.reasons[mode]
				if !ok {
					require.NoErrorf(t, source.Err, "mode %s", mode)
					assert.NotNilf(t, source.Sheet, "mode %s", mode)
					assert.Equalf(t, tc.stances[mode], source.Stances, "mode %s", mode)
					assert.Samef(t, defs, source.Defs, "mode %s", mode)

					continue
				}

				require.ErrorIsf(t, source.Err, ErrNoRating, "mode %s", mode)
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
	hit := func(names ...string) ColumnVerdict {
		var verdict ColumnVerdict

		for _, name := range names {
			verdict.Hits = append(verdict.Hits, Hit{Name: name})
		}

		return verdict
	}

	// The note holds a blank line at its start and between its two
	// lines, and spaces around its second line.
	const note = "\nAdd clear and boss damage.\n\n  Pairs with Firefly.  \n"

	tests := map[string]struct {
		mode   string
		rating Rating
		want   []string
	}{
		"reason": {
			mode:   modePvE,
			rating: Rating{Err: fmt.Errorf("%w: the service publishes no Aegis sheet", ErrNoRating)},
			want:   []string{"- PvE: rating unavailable: the service publishes no Aegis sheet"},
		},
		"weapon that the sheet does not list": {
			mode: modePvE,
			want: []string{"- PvE: not on the Aegis sheet"},
		},
		"weapon that the PvP sheet does not list": {
			mode: modePvP,
			want: []string{"- PvP: not on the PvP sheet"},
		},
		"row with a tier and no columns": {
			mode:   modePvE,
			rating: Rating{Roll: &Roll{Row: SheetRow{Tier: "A", Category: "Exotic Weapons"}, Overall: "A"}},
			want:   []string{"- PvE: A on the Aegis sheet (in Exotic Weapons), which judges no perk columns"},
		},
		"row without a tier and without columns": {
			mode:   modePvE,
			rating: Rating{Roll: &Roll{Row: SheetRow{Category: "Exotic Weapons"}}},
			want:   []string{"- PvE: no tier on the Aegis sheet (in Exotic Weapons), which judges no perk columns"},
		},
		"unknown roll on a row with a tier": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "S", Category: "HCs", Rank: 3, Ranked: 62},
				Unknown: true,
			}},
			want: []string{
				"- PvE: S on the Aegis sheet (rank 3 of 62 in HCs), " +
					"roll unknown: the profile lists no perks for this copy",
			},
		},
		"unknown roll on a row without a tier": {
			mode:   modePvE,
			rating: Rating{Roll: &Roll{Row: SheetRow{Category: "HCs"}, Unknown: true}},
			want: []string{
				"- PvE: no tier on the Aegis sheet (in HCs), roll unknown: the profile lists no perks for this copy",
			},
		},
		"roll with a tier and no miss prints the tier twice": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "S", Category: "HCs"},
				Overall: "S",
				Columns: []ColumnVerdict{hit("Explosive Payload"), hit("Firefly")},
			}},
			want: []string{
				"- PvE: S for this roll, S on the Aegis sheet (in HCs), 2 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload",
				"  Column 2: hit with Firefly",
			},
		},
		"roll with a tier and one miss": {
			mode: modePvP,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "S", Category: "Hand Cannon"},
				Overall: "A",
				Columns: []ColumnVerdict{{}, hit("Firefly")},
			}},
			want: []string{
				"- PvP: A for this roll, S on the PvP sheet (in Hand Cannon), 1 of 2 perk columns hit",
				"  Column 1: miss",
				"  Column 2: hit with Firefly",
			},
		},
		"roll without a tier has no tier for the roll": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Category: "HCs", Rank: 3, Ranked: 62},
				Columns: []ColumnVerdict{hit("Explosive Payload"), {}},
			}},
			want: []string{
				"- PvE: no tier on the Aegis sheet (rank 3 of 62 in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload",
				"  Column 2: miss",
			},
		},
		"row with one column keeps the plural": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "HCs"},
				Overall: "C",
				Columns: []ColumnVerdict{hit("Firefly")},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly",
			},
		},
		"hit with two names": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "D", Category: "HCs"},
				Overall: "E",
				Columns: []ColumnVerdict{hit("Explosive Payload", "Rampage"), {}},
			}},
			want: []string{
				"- PvE: E for this roll, D on the Aegis sheet (in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload, Rampage",
				"  Column 2: miss",
			},
		},
		"hit with one liked name": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "HCs"},
				Overall: "C",
				Columns: []ColumnVerdict{{Hits: []Hit{{Name: "Firefly", Liked: true}}}},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly (liked)",
			},
		},
		"hit with one liked name and one name without the mark": {
			mode: modePvP,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "Hand Cannon"},
				Overall: "C",
				Columns: []ColumnVerdict{
					{Hits: []Hit{{Name: "Explosive Payload"}, {Name: "Firefly", Liked: true}}},
				},
			}},
			want: []string{
				"- PvP: C for this roll, C on the PvP sheet (in Hand Cannon), 1 of 1 perk columns hit",
				"  Column 1: hit with Explosive Payload, Firefly (liked)",
			},
		},
		"hit with picks names the picks": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "HCs"},
				Overall: "C",
				Columns: []ColumnVerdict{
					{Hits: []Hit{{Name: "Explosive Payload"}}, Picks: []string{"Explosive Payload"}},
				},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Explosive Payload. The sheet picks Explosive Payload",
			},
		},
		"miss with picks names the picks": {
			mode: modePvP,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "Hand Cannon"},
				Overall: "D",
				Columns: []ColumnVerdict{{Picks: []string{"Kill Clip"}}},
			}},
			want: []string{
				"- PvP: D for this roll, C on the PvP sheet (in Hand Cannon), 0 of 1 perk columns hit",
				"  Column 1: miss. The sheet picks Kill Clip",
			},
		},
		"liked hit carries the mark before the picks": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "HCs"},
				Overall: "C",
				Columns: []ColumnVerdict{{
					Hits:  []Hit{{Name: "Explosive Payload"}, {Name: "Firefly", Liked: true}},
					Picks: []string{"Explosive Payload", "Rampage"},
				}},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Explosive Payload, Firefly (liked). The sheet picks Explosive Payload, Rampage",
			},
		},
		"hit without picks beside a hit with picks has no second sentence": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "HCs"},
				Overall: "C",
				Columns: []ColumnVerdict{
					{Hits: []Hit{{Name: "Firefly", Liked: true}}},
					{Hits: []Hit{{Name: "Rampage"}}, Picks: []string{"Rampage"}},
				},
			}},
			want: []string{
				"- PvE: C for this roll, C on the Aegis sheet (in HCs), 2 of 2 perk columns hit",
				"  Column 1: hit with Firefly (liked)",
				"  Column 2: hit with Rampage. The sheet picks Rampage",
			},
		},
		"miss without picks beside a miss with picks has no second sentence": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "HCs"},
				Overall: "E",
				Columns: []ColumnVerdict{{}, {Picks: []string{"Kill Clip"}}},
			}},
			want: []string{
				"- PvE: E for this roll, C on the Aegis sheet (in HCs), 0 of 2 perk columns hit",
				"  Column 1: miss",
				"  Column 2: miss. The sheet picks Kill Clip",
			},
		},
		"two picks print with a comma and a space between them": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "HCs"},
				Overall: "D",
				Columns: []ColumnVerdict{{Picks: []string{"Explosive Payload", "Rampage"}}},
			}},
			want: []string{
				"- PvE: D for this roll, C on the Aegis sheet (in HCs), 0 of 1 perk columns hit",
				"  Column 1: miss. The sheet picks Explosive Payload, Rampage",
			},
		},
		"note follows a column line with picks": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "S", Category: "HCs", Notes: "Add clear and boss damage."},
				Overall: "A",
				Columns: []ColumnVerdict{{Picks: []string{"Kill Clip"}}},
			}},
			want: []string{
				"- PvE: A for this roll, S on the Aegis sheet (in HCs), 0 of 1 perk columns hit",
				"  Column 1: miss. The sheet picks Kill Clip",
				"  Sheet note: Add clear and boss damage.",
			},
		},
		// Rank 1 is the lowest rank that prints.
		"rated roll on a row with the rank 1": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "S", Category: "HCs", Rank: 1, Ranked: 62},
				Overall: "S",
				Columns: []ColumnVerdict{hit("Firefly")},
			}},
			want: []string{
				"- PvE: S for this roll, S on the Aegis sheet (rank 1 of 62 in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly",
			},
		},
		// The category size prints only beside a rank.
		"rated roll on a row without a rank": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "S", Category: "HCs", Ranked: 62},
				Overall: "S",
				Columns: []ColumnVerdict{hit("Firefly")},
			}},
			want: []string{
				"- PvE: S for this roll, S on the Aegis sheet (in HCs), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly",
			},
		},
		"note follows the last column line": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "S", Category: "HCs", Notes: "Add clear and boss damage."},
				Overall: "A",
				Columns: []ColumnVerdict{hit("Explosive Payload"), {}},
			}},
			want: []string{
				"- PvE: A for this roll, S on the Aegis sheet (in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Explosive Payload",
				"  Column 2: miss",
				"  Sheet note: Add clear and boss damage.",
			},
		},
		"note with blank lines and spaces around a line prints its two lines": {
			mode: modePvP,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "Hand Cannon", Notes: note},
				Overall: "C",
				Columns: []ColumnVerdict{hit("Firefly")},
			}},
			want: []string{
				"- PvP: C for this roll, C on the PvP sheet (in Hand Cannon), 1 of 1 perk columns hit",
				"  Column 1: hit with Firefly",
				"  Sheet note: Add clear and boss damage.",
				"  Pairs with Firefly.",
			},
		},
		"note follows a line that judges no perk columns": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "A", Category: "Exotic Weapons", Notes: "Boss damage."},
				Overall: "A",
			}},
			want: []string{
				"- PvE: A on the Aegis sheet (in Exotic Weapons), which judges no perk columns",
				"  Sheet note: Boss damage.",
			},
		},
		"note follows a roll unknown line": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "S", Category: "HCs", Notes: note},
				Unknown: true,
			}},
			want: []string{
				"- PvE: S on the Aegis sheet (in HCs), roll unknown: the profile lists no perks for this copy",
				"  Sheet note: Add clear and boss damage.",
				"  Pairs with Firefly.",
			},
		},
		"note follows the column lines of a row without a tier": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Category: "HCs", Notes: "Add clear and boss damage."},
				Columns: []ColumnVerdict{{}},
			}},
			want: []string{
				"- PvE: no tier on the Aegis sheet (in HCs), 0 of 1 perk columns hit",
				"  Column 1: miss",
				"  Sheet note: Add clear and boss damage.",
			},
		},
		"row without a note prints no note line": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "B", Category: "HCs", Rank: 3, Ranked: 62},
				Overall: "C",
				Columns: []ColumnVerdict{hit("Firefly"), {}},
			}},
			want: []string{
				"- PvE: C for this roll, B on the Aegis sheet (rank 3 of 62 in HCs), 1 of 2 perk columns hit",
				"  Column 1: hit with Firefly",
				"  Column 2: miss",
			},
		},
		"note of blank lines and spaces prints no line": {
			mode: modePvE,
			rating: Rating{Roll: &Roll{
				Row:     SheetRow{Tier: "C", Category: "HCs", Notes: "\n  \n\t\n\n"},
				Overall: "C",
				Columns: []ColumnVerdict{hit("Firefly")},
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

	hit := ColumnVerdict{Hits: []Hit{{Name: "Firefly"}}}

	tests := map[string]struct {
		want   string
		rating Rating
	}{
		"game mode without ratings": {
			rating: Rating{Err: fmt.Errorf("%w: the service publishes no Aegis sheet", ErrNoRating)},
			want:   "unavailable",
		},
		"weapon that the sheet does not list": {
			want: "not listed",
		},
		"roll with a missed column on a row without a tier": {
			rating: Rating{Roll: &Roll{Columns: []ColumnVerdict{hit, {}}}},
			want:   "no tier",
		},
		"unknown roll on a row without a tier": {
			rating: Rating{Roll: &Roll{Unknown: true}},
			want:   "no tier",
		},
		"unknown roll on a row with a tier": {
			rating: Rating{Roll: &Roll{Row: SheetRow{Tier: "S"}, Unknown: true}},
			want:   "roll unknown (sheet S)",
		},
		"roll that keeps the tier of the row": {
			rating: Rating{Roll: &Roll{Row: SheetRow{Tier: "S"}, Overall: "S", Columns: []ColumnVerdict{hit}}},
			want:   "S",
		},
		"roll that lost a tier": {
			rating: Rating{Roll: &Roll{Row: SheetRow{Tier: "S"}, Overall: "A", Columns: []ColumnVerdict{hit, {}}}},
			want:   "A (sheet S)",
		},
		"roll that lost the last tier": {
			rating: Rating{Roll: &Roll{Row: SheetRow{Tier: "E"}, Overall: "F", Columns: []ColumnVerdict{{}, {}}}},
			want:   "F (sheet E)",
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
	listed := RatingSource{Sheet: &Sheet{
		Rows:    map[uint32]SheetRow{7: {Tier: "A"}},
		Columns: map[uint32][][]uint32{},
	}}
	noSheet := RatingSource{Err: fmt.Errorf("%w: the service publishes no Aegis sheet", ErrNoRating)}

	tests := map[string]struct {
		item   *Item
		want   string
		source RatingSource
	}{
		"armor that the sheet lists with a tier": {
			source: listed,
			item:   &Item{Hash: 7, Kind: kindArmor},
			want:   "",
		},
		"armor in a game mode without ratings": {
			source: noSheet,
			item:   &Item{Hash: 7, Kind: kindArmor},
			want:   "",
		},
		"weapon that the sheet lists with a tier": {
			source: listed,
			item:   &Item{Hash: 7, Kind: kindWeapon},
			want:   "A",
		},
		"weapon in a game mode without ratings": {
			source: noSheet,
			item:   &Item{Hash: 7, Kind: kindWeapon},
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
		loaded  = RatingSource{Sheet: &Sheet{}}
		noAegis = RatingSource{Err: fmt.Errorf("%w: the service publishes no Aegis sheet", ErrNoRating)}
		noPvP   = RatingSource{Err: fmt.Errorf("%w: the service publishes no PvP sheet", ErrNoRating)}
		noPrefs = RatingSource{Err: fmt.Errorf("%w: reading perk preferences: %w", ErrNoRating, ErrService)}
	)

	tests := map[string]struct {
		sources map[string]RatingSource
		want    []string
	}{
		"every game mode has ratings": {
			sources: map[string]RatingSource{modePvE: loaded, modePvP: loaded},
		},
		"only PvE has no ratings": {
			sources: map[string]RatingSource{modePvE: noAegis, modePvP: loaded},
			want:    []string{"PvE: rating unavailable: the service publishes no Aegis sheet"},
		},
		"only PvP has no ratings": {
			sources: map[string]RatingSource{modePvE: loaded, modePvP: noPvP},
			want:    []string{"PvP: rating unavailable: the service publishes no PvP sheet"},
		},
		"PvE line comes first": {
			sources: map[string]RatingSource{modePvP: noPvP, modePvE: noAegis},
			want: []string{
				"PvE: rating unavailable: the service publishes no Aegis sheet",
				"PvP: rating unavailable: the service publishes no PvP sheet",
			},
		},
		// The loop compares each line of every case with the line of
		// get_item. This case adds a reason that wraps a cause.
		"line of a mode is the get_item line without the list marker": {
			sources: map[string]RatingSource{modePvE: noPrefs, modePvP: noPvP},
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

			for _, mode := range perkModes {
				err := tc.sources[mode].Err
				if err != nil {
					writeRating(&b, mode, Rating{Err: err})
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

		header   = "instance id | name | type | power | location | tag | flags | pve rating | pvp rating | perks"
		noRating = "unavailable | unavailable"

		// The rating cells of a copy without perks.
		unknownBoth  = "roll unknown (sheet S) | roll unknown (sheet C)"
		unknownAegis = "roll unknown (sheet S) | unavailable"
	)

	// aegisOnly makes the service publish only the Aegis sheet.
	aegisOnly := func(f *fakes) {
		f.artifactFiles = map[string][]string{"rolls": {"rolls.a.json"}}
		f.serviceData["/artifacts/rolls.a.json"] = aegisSheet
	}

	// bothSheets makes the service publish the Aegis sheet and the PvP
	// sheet.
	bothSheets := func(f *fakes) {
		aegisOnly(f)

		f.artifactFiles["pvprolls"] = []string{"pvprolls.a.json"}
		f.serviceData["/artifacts/pvprolls.a.json"] = pvpSheet
	}

	// fatebringer returns the table of the Fatebringer copies in the
	// fixture profile. Cells holds the two rating cells of each copy, in
	// row order.
	fatebringer := func(cells ...string) []string {
		return []string{
			"Fatebringer: 3 copies",
			"",
			header,
			"1001 | Fatebringer | Kinetic Hand Cannon | 2010 | Vault | keep | favorite, locked, masterwork, note | " +
				cells[0] + " | Explosive Payload (can swap to Rampage); Firefly",
			"1005 | Fatebringer | Kinetic Hand Cannon | 1970 | Hunter (postmaster) |  |  | " + cells[1] + " | ",
			"1006 | Fatebringer | Kinetic Hand Cannon | 1980 | Hunter (equipped) |  |  | " + cells[2] + " | ",
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
		"",
		header,
		"1003 | Midnight Coup | Kinetic Hand Cannon | 2005 | Hunter |  |  | " + noRating + " | ",
	}, "\n")

	tests := map[string]struct {
		// prepare changes the fakes before the call.
		prepare func(*fakes)
		// edit changes the cached snapshot before the call.
		edit func(*Snapshot)
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
			edit: func(s *Snapshot) {
				coup, _ := s.Find("1003")
				twin := *coup
				twin.InstanceID = "1007"
				twin.Name = "MIDNIGHT COUP"
				s.add(&twin)
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
			edit: func(s *Snapshot) {
				s.Items = slices.DeleteFunc(s.Items, func(item *Item) bool {
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
			edit: func(s *Snapshot) {
				base, _ := s.Find("1001")
				adept := *base
				adept.InstanceID = "1007"
				adept.Name = "Fatebringer (Adept)"
				adept.Hash = 110
				s.add(&adept)
			},
			in: CompareDuplicatesInput{Name: "fatebringer"},
			want: strings.Join(slices.Concat(
				[]string{"Fatebringer: 4 copies", "", header},
				// The rows of the three copies of the base weapon.
				rated[3:],
				[]string{
					"1007 | Fatebringer (Adept) | Kinetic Hand Cannon | 2010 | Vault |  | locked, masterwork | " +
						"not listed | not listed | Explosive Payload (can swap to Rampage); Firefly",
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
				killClipPlugSet(f.h.defs.defs)

				f.serviceData["/artifacts/rolls.a.json"] = `{"rolls": [` +
					`{"category": "HCs", "tier": "S", "hashes": [100], "slots": [[900], [904]]}]}`
			},
			in:   CompareDuplicatesInput{Name: "fatebringer"},
			want: strings.Join(rated, "\n"),
		},
		// The index lists a file that the service does not serve. A mode
		// whose sheet did not load never reads "not listed".
		"name with a sheet that does not load": {
			prepare: func(f *fakes) {
				f.artifactFiles = map[string][]string{"rolls": {"rolls.x.json"}}
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
				delete(f.serviceData, pathPrefs)
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

				f.h.defs.defs = nil
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
	twin.Owner = ownerVault
	s.add(&twin)

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
		"",
		"instance id | name | type | power | location | tag | flags | pve rating | pvp rating | perks",
		"1003 | Midnight Coup | Kinetic Hand Cannon | 2005 | Hunter |  |  | unavailable | unavailable | ",
		"1007 | Midnight Coup | Kinetic Hand Cannon | 2005 | Vault |  |  | unavailable | unavailable | ",
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
	assert.Len(t, f.service.calls(http.MethodGet, pathPrefs), 1)
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
					s.add(&twin)
				}
			}

			got, isError := call(t, f.h.handleCompareDuplicates, CompareDuplicatesInput{Limit: tc.limit})
			assert.False(t, isError)

			header, _, _ := strings.Cut(got, "\n")
			assert.Equal(t, tc.want, header)
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
				pathHunterHistory: {"count=25&page=0"},
				pathTitanHistory:  {"count=25&page=0"},
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
				pathHunterHistory: {"count=2&page=0"},
				pathTitanHistory:  {"count=2&page=0"},
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
				pathTitanHistory: {"count=250&page=0"},
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
			in: ListActivityHistoryInput{Character: hunterID, Limit: 1},
			queries: map[string][]string{
				pathHunterHistory: {"count=1&page=0"},
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
				pathHunterHistory: `{"ErrorCode": 1, "Response": {}}`,
				pathTitanHistory:  `{"ErrorCode": 1, "Response": {}}`,
			},
			queries: map[string][]string{
				pathHunterHistory: {"count=25&page=0"},
				pathTitanHistory:  {"count=25&page=0"},
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
				pathTitanHistory: `{"ErrorCode": 1665, "ErrorStatus": "DestinyPrivacyRestriction", "Message": "Private."}`,
			},
			queries: map[string][]string{
				pathHunterHistory: {"count=25&page=0"},
				pathTitanHistory:  {"count=25&page=0"},
			},
			want:    "fetching the activity history: character c2: bungie: DestinyPrivacyRestriction: Private.",
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			maps.Copy(f.bungieData, tc.data)

			got, isError := call(t, f.h.handleListActivityHistory, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)

			for _, path := range []string{pathHunterHistory, pathTitanHistory} {
				var queries []string
				for _, req := range f.bungie.calls(http.MethodGet, path) {
					queries = append(queries, req.Query)
				}

				assert.Equal(t, tc.queries[path], queries, path)
			}
		})
	}
}

func TestActivityName(t *testing.T) {
	t.Parallel()

	defs := fixtureDefs()

	tests := map[string]struct {
		activity HistoryActivity
		want     string
	}{
		"reference id": {
			activity: HistoryActivity{ReferenceID: 3022541210},
			want:     "Vault of Glass: Master",
		},
		"reference id first": {
			activity: HistoryActivity{ReferenceID: 4444, DirectorActivityHash: 5555},
			want:     "Control",
		},
		"director hash": {
			activity: HistoryActivity{ReferenceID: 1111, DirectorActivityHash: 2222},
			want:     "Vanguard Ops",
		},
		"definition without a name": {
			activity: HistoryActivity{ReferenceID: 3334, DirectorActivityHash: 2222},
			want:     "Vanguard Ops",
		},
		"no definition": {
			activity: HistoryActivity{ReferenceID: 3333, DirectorActivityHash: 3334},
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

	list := strings.Join([]string{
		"The 6 vendors of Hunter. Pass vendor with a hash or a name to list the sales of one vendor.",
		"",
		"name | subtitle | hash | sales | next refresh | flags",
		"Banshee-44 | Gunsmith | 672118013 | 8 | 2026-11-13 17:00 UTC | ",
		"Tess Everis | Eververse Trading Company | 3361454721 | 1 | 2026-11-17 17:00 UTC | ",
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
			paths: []string{pathHunterVendors},
			want:  list,
		},
		"blank vendor": {
			in:    ListVendorsInput{Vendor: "  "},
			paths: []string{pathHunterVendors},
			want:  list,
		},
		"vendor by exact name": {
			in:    ListVendorsInput{Vendor: "Banshee-44"},
			paths: []string{pathHunterVendors},
			want:  banshee,
		},
		"vendor by part of its name": {
			in:    ListVendorsInput{Vendor: " banshee "},
			paths: []string{pathHunterVendors},
			want:  banshee,
		},
		"vendor by hash": {
			in:    ListVendorsInput{Vendor: "3361454721"},
			paths: []string{pathHunterVendors},
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
			paths: []string{pathHunterVendors},
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
			paths: []string{pathHunterVendors},
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
			paths:   []string{pathHunterVendors},
			want:    `unknown vendor: "Xûr" matches 2 vendors, so pass a vendor hash, such as Xûr (2190858386), Xûr (3751514131)`,
			isError: true,
		},
		"ambiguous part of a name": {
			in:    ListVendorsInput{Vendor: "vendor"},
			paths: []string{pathHunterVendors},
			want: `unknown vendor: "vendor" matches 2 vendors, so pass a vendor hash, ` +
				`such as Vendor 1111 (1111), Vendor 2168194999 (2168194999)`,
			isError: true,
		},
		"unknown vendor name": {
			in:      ListVendorsInput{Vendor: "Zavala"},
			paths:   []string{pathHunterVendors},
			want:    `unknown vendor: no vendor of Hunter has a name that contains "Zavala"`,
			isError: true,
		},
		"unknown vendor hash": {
			in:      ListVendorsInput{Vendor: "42"},
			paths:   []string{pathHunterVendors},
			want:    "unknown vendor: no vendor of Hunter has the hash 42",
			isError: true,
		},
		"character by class name": {
			in:    ListVendorsInput{Character: "TITAN"},
			paths: []string{pathTitanVendors},
			want: strings.Join([]string{
				"The vendor of Titan. Pass vendor with a hash or a name to list the sales of one vendor.",
				"",
				"name | subtitle | hash | sales | next refresh | flags",
				"Banshee-44 | Gunsmith | 672118013 | 1 | 2026-11-13 17:00 UTC | ",
			}, "\n"),
		},
		"vendor of a character by id": {
			in:    ListVendorsInput{Character: titanID, Vendor: "banshee-44"},
			paths: []string{pathTitanVendors},
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
			data:  map[string]string{pathHunterVendors: `{"ErrorCode": 1, "Response": {}}`},
			paths: []string{pathHunterVendors},
			want:  "Bungie lists no vendors for Hunter.",
		},
		"vault": {
			in:      ListVendorsInput{Character: "vault"},
			want:    "unknown location: the vault is not a character, so pass a character id or a class name",
			isError: true,
		},
		"Bungie refuses": {
			data: map[string]string{
				pathHunterVendors: `{"ErrorCode": 1627, "ErrorStatus": "DestinyVendorNotFound", "Message": "Not found."}`,
			},
			paths:   []string{pathHunterVendors},
			want:    "fetching the vendors: bungie: DestinyVendorNotFound: Not found.",
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)
			maps.Copy(f.bungieData, tc.data)

			got, isError := call(t, f.h.handleListVendors, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)

			var paths []string

			for _, path := range []string{pathHunterVendors, pathTitanVendors} {
				for _, req := range f.bungie.calls(http.MethodGet, path) {
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

	assert.Len(t, f.bungie.calls(http.MethodGet, pathHunterVendors), 2)
	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 1)
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

	manyDefs := func(d *Defs) {
		for i := range uint32(maxItemLimit + 1) {
			d.Activities[5000+i] = ActivityDef{Name: fmt.Sprintf("Activity %03d", i)}
		}
	}

	tests := map[string]struct {
		in         ListActivitiesInput
		defs       func(*Defs)
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
			in:      ListActivitiesInput{Character: hunterID},
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
			activities: `{"ErrorCode": 1, "Response": {"characterActivities": {"data": {"` + hunterID +
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
			defs:    func(d *Defs) { d.Activities = map[uint32]ActivityDef{} },
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
			f.activitiesBody = tc.activities

			if tc.defs != nil {
				tc.defs(f.h.defs.defs)
			}

			got, isError := call(t, f.h.handleListActivities, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)

			// The first request fetches the inventory snapshot, and every
			// later one fetches only the activities.
			calls := f.bungie.calls(http.MethodGet, pathProfile)
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
	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 3)
}

func TestRefreshOnUnauthorized(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.staleAccess = "access-1"
	f.staleSession = "session-1"

	got, isError := call(t, f.h.handleListItems, ListItemsInput{Location: "vault"})
	require.False(t, isError, got)
	assert.Contains(t, got, "Fatebringer")

	refreshes := f.service.calls(http.MethodPost, "/api/auth/refresh")
	require.Len(t, refreshes, 1, "one refresh serves Bungie and the service")
	assert.Equal(t, map[string]any{"refreshToken": "refresh-1"}, refreshes[0].Body)

	saved, err := f.h.auth.Saved()
	require.NoError(t, err)
	assert.Equal(t, "access-2", saved.AccessToken)
	assert.Equal(t, "session-2", saved.Session)
}

func TestMoveItems(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleMoveItems, MoveItemsInput{
		InstanceIDs: []string{"1003", "1006"},
		Target:      "titan",
	})
	assert.False(t, isError, "one item moved")
	assert.Equal(t, "- Moved Midnight Coup (1003). It is at Titan.\n"+
		"- Fatebringer (1006) did not move: cannot move: Fatebringer is equipped on Hunter, "+
		"which holds no other item to equip in that slot. It is at Hunter (equipped).", got)

	transfers := f.bungie.calls(http.MethodPost, pathTransfer)
	require.Len(t, transfers, 2)
	assert.Equal(t, map[string]any{
		"itemReferenceHash": float64(102),
		"stackSize":         float64(1),
		"transferToVault":   true,
		"itemId":            "1003",
		"characterId":       hunterID,
		"membershipType":    float64(3),
	}, transfers[0].Body)
	assert.Equal(t, false, transfers[1].Body["transferToVault"])
	assert.Equal(t, titanID, transfers[1].Body["characterId"])

	// The snapshot carries the move, so the next read does not ask Bungie
	// for a profile that might predate it.
	got, isError = call(t, f.h.handleListItems, ListItemsInput{Location: "titan"})
	assert.False(t, isError)
	assert.Contains(t, got, "1003 | Midnight Coup")
	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 1)
}

func TestMoveItemsEquip(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleMoveItems, MoveItemsInput{
		InstanceIDs: []string{"1001"},
		Target:      hunterID,
		Equip:       true,
	})
	assert.False(t, isError)
	assert.Equal(t, "- Equipped Fatebringer (1001). It is at Hunter (equipped).", got)
	assert.Len(t, f.bungie.calls(http.MethodPost, pathTransfer), 1)
	assert.Len(t, f.bungie.calls(http.MethodPost, pathEquip), 1)
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
		"- Moved Fatebringer (1006). It is at Vault. Equipped Midnight Coup (1003) in its place.", got)

	equips := f.bungie.calls(http.MethodPost, pathEquip)
	require.Len(t, equips, 1)
	assert.Equal(t, map[string]any{
		"itemId":         "1003",
		"characterId":    hunterID,
		"membershipType": float64(3),
	}, equips[0].Body)
	assert.Len(t, f.bungie.calls(http.MethodPost, pathTransfer), 1)
}

func TestMoveStackTool(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleMoveStack, MoveStackInput{ItemHash: 300, Target: "Vault", Amount: 12})
	assert.False(t, isError)
	assert.Equal(t, "Moved 12 of Enhancement Core to the vault.", got)

	transfers := f.bungie.calls(http.MethodPost, pathTransfer)
	require.Len(t, transfers, 1)
	assert.Equal(t, map[string]any{
		"itemReferenceHash": float64(300),
		"stackSize":         float64(12),
		"transferToVault":   true,
		"itemId":            "0",
		"characterId":       hunterID,
		"membershipType":    float64(3),
	}, transfers[0].Body)

	got, isError = call(t, f.h.handleListItems, ListItemsInput{Kind: kindOther, Location: "vault"})
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
			assert.Empty(t, f.bungie.calls(http.MethodPost, pathTransfer))
		})
	}
}

func TestSetAutotagEnabled(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		wantBody map[string]any
		want     string
		enabled  bool
	}{
		"on stores the newest instance id as the mark": {
			enabled:  true,
			wantBody: map[string]any{"kind": "enable", "mark": "00000000000000001006"},
			want: "Autotagging is on. The rules apply to items acquired from now on, " +
				"and the web client runs them while it is open.",
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

			// A cached profile must not decide the mark.
			_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
			require.False(t, isError)

			got, isError := call(t, f.h.handleSetAutotagEnabled, SetAutotagEnabledInput{Enabled: tc.enabled})
			assert.False(t, isError)
			assert.Equal(t, tc.want, got)

			writes := f.service.calls(http.MethodPost, "/api/autotag/state")
			require.Len(t, writes, 1)
			assert.Equal(t, tc.wantBody, writes[0].Body)

			wantProfiles := 1
			if tc.enabled {
				wantProfiles = 2
			}

			assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), wantProfiles)
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
				"There are no item slots available to transfer this item.. It is at Vault.",
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
			f.bungieError = tc.bungieError

			got, isError := call(t, f.h.handleMoveItems, tc.in)
			assert.True(t, isError)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMoveItemsSnapshotAge(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	require.False(t, isError)

	later := fixtureNow.Add(10 * time.Second)
	f.h.now = func() time.Time { return later }
	f.bungieError = `{"ErrorCode": 1623, "ErrorStatus": "DestinyItemNotFound", "Message": "gone"}`

	_, isError = call(t, f.h.handleMoveItems, MoveItemsInput{InstanceIDs: []string{"1001"}, Target: "hunter"})
	require.True(t, isError)
	assert.Equal(t, fixtureNow, f.h.snapshot.Fetched, "a call that moved nothing keeps the age of the snapshot")

	f.bungieError = ""

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

func dismantleLine(staged int) string {
	return fmt.Sprintf("Each listed item is tagged junk, and the user dismantles them in the game. "+
		"Do not dismantle when the number of unequipped items that the game shows in the slot is higher than %d, "+
		"because stage_junk did not check an item that reached the slot after the last inventory fetch. "+
		"Call stage_junk again after the user dismantled them. "+
		"A call in the next 30 seconds might reuse this inventory copy and list the same items.", staged)
}

func skippedLine(skipped int) string {
	return fmt.Sprintf("Junk of this slot that stage_junk skips: %d. "+
		"Each one is equipped, at the Postmaster, not transferable, or for another class.", skipped)
}

// junkTags returns a tag body of the fake service that tags each instance
// in ids junk and tags no other instance.
func junkTags(t *testing.T, ids ...string) string {
	t.Helper()

	rows := []ItemTag{}
	for _, id := range ids {
		rows = append(rows, ItemTag{InstanceID: id, TagID: tagJunk, UpdatedAt: 5})
	}

	raw, err := json.Marshal(map[string]any{"defs": []any{}, "items": rows, "now": 10})
	require.NoError(t, err)

	return string(raw)
}

// kineticWeapon returns a weapon of the kinetic slot that every class can
// use, for a test to add to the snapshot.
func kineticWeapon(id, name, owner string) *Item {
	hash := uint32(100)
	if name == "Midnight Coup" {
		hash = 102
	}

	return &Item{
		InstanceID:   id,
		Name:         name,
		TypeName:     "Hand Cannon",
		Slot:         "Kinetic Weapons",
		Owner:        owner,
		Kind:         kindWeapon,
		ClassType:    classAny,
		Quantity:     1,
		Hash:         hash,
		Bucket:       bucketKinetic,
		Transferable: true,
		Equippable:   true,
		Lockable:     true,
	}
}

// transferSteps describes each transfer request as the instance id and
// the direction, such as "1001 to c1" or "1007 to the vault from c2".
func transferSteps(reqs []request) []string {
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
func bungiePosts(f *fakes) []request {
	return slices.DeleteFunc(f.bungie.all(), func(req request) bool { return req.Method != http.MethodPost })
}

func TestStageJunk(t *testing.T) {
	t.Parallel()

	refuseEvery := func(map[string]any) bool { return true }
	moveFailure := func(name, id, target string) string {
		return fmt.Sprintf("- %s (%s) did not move: transferring to %s: bungie: DestinyItemNotFound: gone. "+
			"It is at Vault.", name, id, target)
	}
	kinetic := StageJunkInput{Slot: "kinetic"}

	tests := map[string]struct {
		refuse    func(body map[string]any) bool
		in        StageJunkInput
		want      string
		junk      []string
		add       []*Item
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
			add:     []*Item{kineticWeapon("1007", "Fatebringer", hunterID)},
			want:    fixtureRefusal,
			isError: true,
		},
		"refusal lists every keeper": {
			in:   kinetic,
			junk: []string{"1001"},
			add:  []*Item{kineticWeapon("1007", "Fatebringer", hunterID)},
			want: "cannot move: Kinetic Weapons on Hunter holds unequipped gear that is not tagged junk: " +
				"Fatebringer (1007), Midnight Coup (1003). stage_junk moved nothing. Move that gear to the vault " +
				"with move_items, with target vault and instance_ids 1007, 1003. Then call stage_junk again. " +
				"After stage_junk reports that no junk is left, move that gear back with move_items and target c1.",
			isError: true,
		},
		"stage from the vault": {
			in:   kinetic,
			junk: []string{"1001", "1003"},
			want: "Unequipped items in Kinetic Weapons on Hunter after this call: 2\n" +
				"- Fatebringer (1001), locked\n" +
				"- Midnight Coup (1003)\n" +
				"The game does not let the player dismantle a locked item. set_lock unlocks it.\n" +
				"Junk that waits for the next call: 0\n" +
				"Each listed item is tagged junk, and the user dismantles them in the game. Do not dismantle " +
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
			add:  []*Item{kineticWeapon("1007", "Fatebringer", titanID)},
			want: "Unequipped items in Kinetic Weapons on Hunter after this call: 2\n" +
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
			add: []*Item{
				kineticWeapon("1007", "Midnight Coup", ownerVault),
				kineticWeapon("1008", "Fatebringer", ownerVault),
			},
			capacity: 4,
			want: "Unequipped items in Kinetic Weapons on Hunter after this call: 3\n" +
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
			want: "Unequipped items in Kinetic Weapons on Hunter after this call: 1\n" +
				"- Midnight Coup (1003)\n" +
				"Junk that waits for the next call: 1\n" +
				dismantleLine(1),
		},
		"nothing waits": {
			in:   kinetic,
			junk: []string{"1003"},
			want: "Unequipped items in Kinetic Weapons on Hunter after this call: 1\n" +
				"- Midnight Coup (1003)\n" +
				"Junk that waits for the next call: 0\n" +
				dismantleLine(1),
		},
		"one move does not succeed": {
			in:     kinetic,
			junk:   []string{"1001", "1003", "1007"},
			add:    []*Item{kineticWeapon("1007", "Midnight Coup", ownerVault)},
			refuse: func(body map[string]any) bool { return body["itemId"] == "1001" },
			want: "Unequipped items in Kinetic Weapons on Hunter after this call: 2\n" +
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
			add:    []*Item{kineticWeapon("1007", "Fatebringer", titanID)},
			refuse: func(body map[string]any) bool { return body["transferToVault"] == false },
			want: "Unequipped items in Kinetic Weapons on Hunter after this call: 1\n" +
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
			want: "Unequipped items in Kinetic Weapons on Hunter after this call: 1\n" +
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
			want: "Unequipped items in Kinetic Weapons on Titan after this call: 0\n" +
				moveFailure("Fatebringer", "1001", "Titan") + "\n" +
				"Junk that waits for the next call: 1\n" +
				nothingStaged,
			transfers: []string{"1001 to c2"},
			isError:   true,
		},
		"item count": {
			in:   kinetic,
			junk: []string{"1001", "1003", "1007"},
			add:  []*Item{kineticWeapon("1007", "Midnight Coup", ownerVault)},
			want: "Unequipped items in Kinetic Weapons on Hunter after this call: 3\n" +
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
			want: "Unequipped items in Kinetic Weapons on Titan after this call: 1\n" +
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
			f.serviceData[pathTags] = junkTags(t, tc.junk...)

			s, err := f.h.loadSnapshot(t.Context(), false)
			require.NoError(t, err)

			for _, item := range tc.add {
				s.add(item)
			}

			if tc.capacity > 0 {
				i := slices.IndexFunc(s.Buckets, func(b Bucket) bool { return b.Hash == bucketKinetic })
				require.GreaterOrEqual(t, i, 0)

				s.Buckets[i].Capacity = tc.capacity
			}

			if tc.refuse != nil {
				f.bungieError = goneError
				f.bungieErrorIf = tc.refuse
			}

			later := fixtureNow.Add(10 * time.Second)
			f.h.now = func() time.Time { return later }

			got, isError := call(t, f.h.handleStageJunk, tc.in)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.isError, isError)

			transfers := f.bungie.calls(http.MethodPost, pathTransfer)
			assert.Equal(t, tc.transfers, transferSteps(transfers))
			assert.Len(t, bungiePosts(f), len(transfers), "the call sends no equip and no Postmaster request")

			wantFetched := fixtureNow
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
			f.serviceData[pathTags] = junkTags(t, tc.junk...)

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

	later := fixtureNow.Add(10 * time.Second)
	f.h.now = func() time.Time { return later }
	kinetic := StageJunkInput{Slot: "kinetic"}

	_, isError = call(t, f.h.handleStageJunk, kinetic)
	require.False(t, isError)
	assert.Equal(t, fixtureNow, f.h.snapshot.Fetched, "a call without junk keeps the age of the snapshot")

	f.serviceData[pathTags] = junkTags(t, "1001")

	_, isError = call(t, f.h.handleStageJunk, kinetic)
	require.True(t, isError)
	assert.Equal(t, fixtureNow, f.h.snapshot.Fetched, "a refusal keeps the age of the snapshot")

	f.serviceData[pathTags] = junkTags(t, "1001", "1003")
	f.bungieError = goneError

	_, isError = call(t, f.h.handleStageJunk, kinetic)
	require.True(t, isError)
	assert.Equal(t, fixtureNow, f.h.snapshot.Fetched, "a call that moved nothing keeps the age of the snapshot")

	f.bungieError = ""

	_, isError = call(t, f.h.handleStageJunk, kinetic)
	require.False(t, isError)
	assert.Equal(t, later, f.h.snapshot.Fetched, "a move restarts the age of the snapshot")
}

func TestStageJunkReadsTags(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.serviceData[pathTags] = junkTags(t, "1001")

	got, isError := call(t, f.h.handleStageJunk, StageJunkInput{Slot: "kinetic"})
	assert.True(t, isError)
	assert.Equal(t, fixtureRefusal, got)

	f.serviceData[pathTags] = junkTags(t, "1001", "1003")

	got, isError = call(t, f.h.handleStageJunk, StageJunkInput{Slot: "kinetic"})
	assert.False(t, isError)
	assert.True(t, strings.HasPrefix(got, "Unequipped items in Kinetic Weapons on Hunter after this call: 2\n"), got)

	assert.Len(t, f.service.calls(http.MethodGet, pathTags), 2, "every call reads the tags")
	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 1, "the second call reuses the profile")
}

func TestStageJunkLoadError(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.serviceData[pathTags] = ""

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

	locks := f.bungie.calls(http.MethodPost, pathLock)
	require.Len(t, locks, 2)
	assert.Equal(t, map[string]any{
		"state":          true,
		"itemId":         "1002",
		"characterId":    hunterID,
		"membershipType": float64(3),
	}, locks[0].Body)
	assert.Equal(t, "1003", locks[1].Body["itemId"])

	got, isError = call(t, f.h.handleSetLock, SetLockInput{InstanceIDs: []string{"1001"}})
	assert.False(t, isError)
	assert.Equal(t, "- Unlocked Fatebringer (1001).", got)

	locks = f.bungie.calls(http.MethodPost, pathLock)
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
	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 1)
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
			f.bungieError = tc.bungieError

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

	later := fixtureNow.Add(10 * time.Second)
	f.h.now = func() time.Time { return later }

	stuck, ok := f.h.snapshot.Find("1003")
	require.True(t, ok)

	stuck.Lockable = false
	refusal := "- Midnight Coup (1003) did not lock: cannot change the lock: Midnight Coup cannot be locked."

	got, isError := call(t, f.h.handleSetLock, SetLockInput{InstanceIDs: []string{"1003"}, Locked: true})
	assert.True(t, isError)
	assert.Equal(t, refusal, got)
	assert.Empty(t, f.bungie.calls(http.MethodPost, pathLock))
	assert.Equal(t, fixtureNow, f.h.snapshot.Fetched, "a call that changed nothing keeps the age of the snapshot")

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

	inserts := f.bungie.calls(http.MethodPost, pathInsert)
	require.Len(t, inserts, 1)
	assert.Equal(t, map[string]any{
		"plug": map[string]any{
			"socketIndex":     float64(0),
			"socketArrayType": float64(0),
			"plugItemHash":    float64(903),
		},
		"itemId":         "1001",
		"characterId":    hunterID,
		"membershipType": float64(3),
	}, inserts[0].Body)

	// The snapshot carries the new perk, so the next read does not ask
	// Bungie for a profile that might predate it.
	got, isError = call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
	assert.False(t, isError)
	assert.Contains(t, got, "Perks:\n- Rampage (Trait), can swap to Explosive Payload\n- Firefly (Trait)\n")
	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 1)

	got, isError = call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: "Rampage"})
	assert.False(t, isError)
	assert.Equal(t, "Rampage is already active on Fatebringer (1001).", got)
	assert.Len(t, f.bungie.calls(http.MethodPost, pathInsert), 1, "an active perk sends no request")
}

func TestSetPerkToolSnapshotAge(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	require.False(t, isError)

	later := fixtureNow.Add(10 * time.Second)
	f.h.now = func() time.Time { return later }

	_, isError = call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: "Explosive Payload"})
	require.False(t, isError)
	assert.Equal(t, fixtureNow, f.h.snapshot.Fetched, "a perk that is already active keeps the age of the snapshot")

	_, isError = call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: "Kill Clip"})
	require.True(t, isError)
	assert.Equal(t, fixtureNow, f.h.snapshot.Fetched, "a perk that no socket offers keeps the age of the snapshot")
	assert.Empty(t, f.bungie.calls(http.MethodPost, pathInsert))

	f.bungieError = `{"ErrorCode": 1634, "ErrorStatus": "DestinyCharacterNotInTower", "Message": "not in a social space"}`

	_, isError = call(t, f.h.handleSetPerk, SetPerkInput{InstanceID: "1001", Perk: "Rampage"})
	require.True(t, isError)
	assert.Equal(t, fixtureNow, f.h.snapshot.Fetched, "a swap that Bungie refuses keeps the age of the snapshot")
	assert.Len(t, f.bungie.calls(http.MethodPost, pathInsert), 1)

	f.bungieError = ""

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
			f.bungieError = tc.bungieError

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
			in:       ListLoadoutsInput{Character: titanID},
			hunterOn: true,
			want:     loadoutsIntro + "\n\nTitan (id c2): no saved loadouts",
		},
		"name hash that the table lacks": {
			edit: func(f *fakes) {
				f.bungieData[pathLoadoutNames] = `{"1": {"hash": 1, "name": "Raid Night"}}`
			},
			want: loadoutsIntro + "\n\n" + hunter("Loadout 2: Raid Night", "Loadout 3: unnamed") +
				"\n\n" + titan("Loadout 1: unnamed"),
		},
		"redacted name without text": {
			edit: func(f *fakes) {
				f.bungieData[pathLoadoutNames] = `{"7": {"hash": 7, "name": "", "redacted": true}}`
			},
			in:   ListLoadoutsInput{Character: "titan"},
			want: loadoutsIntro + "\n\n" + titan("Loadout 1: unnamed"),
		},
		"manifest does not load": {
			edit: func(f *fakes) { delete(f.bungieData, pathManifest) },
			want: unavailable + "reading the Bungie manifest: bungie: ParameterParseFailure: no route\n\n" +
				numbered,
		},
		"manifest lists no name table": {
			edit: func(f *fakes) {
				f.bungieData[pathManifest] = `{"ErrorCode": 1, "Response": {"jsonWorldComponentContentPaths": {
					"fr": {"DestinyLoadoutNameDefinition": "/fr.json"}}}}`
			},
			want: unavailable + "the Bungie manifest lists no loadout name table\n\n" + numbered,
		},
		"name table does not download": {
			edit: func(f *fakes) { delete(f.bungieData, pathLoadoutNames) },
			want: unavailable + "fetching " + pathLoadoutNames + ": bungie: content file returned status 404\n\n" +
				numbered,
		},
		"name table does not decode": {
			edit: func(f *fakes) { f.bungieData[pathLoadoutNames] = `{"1": {"hash": 1,` },
			in:   ListLoadoutsInput{Character: "titan"},
			want: unavailable + "decoding " + pathLoadoutNames +
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

				f.h.snapshot.Loadouts = f.h.snapshot.LoadoutsOf(hunterID)
			}

			got, isError := call(t, f.h.handleListLoadouts, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)
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

	equips := f.bungie.calls(http.MethodPost, pathEquip)
	require.Len(t, equips, 1)
	assert.Equal(t, "1003", equips[0].Body["itemId"])
	assert.Len(t, f.bungie.calls(http.MethodPost, pathTransfer), 2)

	loadouts := f.bungie.calls(http.MethodPost, pathLoadout)
	require.Len(t, loadouts, 1)
	assert.Equal(t, map[string]any{
		"loadoutIndex":   float64(0),
		"characterId":    titanID,
		"membershipType": float64(3),
	}, loadouts[0].Body)

	got, isError = call(t, f.h.handleEquipLoadout, EquipLoadoutInput{Character: hunterID, Loadout: 3})
	assert.False(t, isError)
	assert.Equal(t, "- Moved Fatebringer (1001) from Vault to Hunter.\n"+
		"Equipped loadout 3 on Hunter: Fatebringer (1001), Celestial Nighthawk (1004). "+
		"The inventory does not hold these loadout items: 9999.", got)

	loadouts = f.bungie.calls(http.MethodPost, pathLoadout)
	require.Len(t, loadouts, 2)
	assert.Equal(t, map[string]any{
		"loadoutIndex":   float64(2),
		"characterId":    hunterID,
		"membershipType": float64(3),
	}, loadouts[1].Body)

	// The snapshot carries the equips, so the next read does not ask Bungie
	// for a profile that might predate them.
	got, isError = call(t, f.h.handleListItems, ListItemsInput{Kind: kindWeapon})
	assert.False(t, isError)
	assert.Contains(t, got, "1006 | Fatebringer | Kinetic Hand Cannon | Legendary | 1980 |  | Titan (equipped) |  | \n")
	assert.Contains(
		t,
		got,
		"1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 |  | Hunter (equipped) | keep | ",
	)
	assert.Contains(t, got, "1003 | Midnight Coup | Kinetic Hand Cannon | Legendary | 2005 |  | Hunter |  | ")
	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 1)
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
			want: "- Fatebringer (1006) did not reach Titan. It is at Hunter (equipped).\n" +
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

			later := fixtureNow.Add(10 * time.Second)
			f.h.now = func() time.Time { return later }
			f.bungieError = tc.bungieError
			f.bungieErrorPath = tc.bungieErrorPath

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
					fixtureNow,
					f.h.snapshot.Fetched,
					"a call that changed nothing keeps the age of the snapshot",
				)
			}

			cannon, ok := f.h.snapshot.Find("1001")
			require.True(t, ok)
			assert.Equal(t, tc.wantVaultCannon, f.h.snapshot.LocationLabel(cannon), "the vault hand cannon")

			item, ok := f.h.snapshot.Find("1006")
			require.True(t, ok)
			assert.Equal(t, hunterID, item.Owner)
			assert.True(t, item.Equipped)
		})
	}
}

func TestEquipLoadoutToolSnapshotAge(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	_, isError := call(t, f.h.handleListCharacters, EmptyInput{})
	require.False(t, isError)

	later := fixtureNow.Add(10 * time.Second)
	f.h.now = func() time.Time { return later }

	_, isError = call(t, f.h.handleEquipLoadout, EquipLoadoutInput{Character: "hunter", Loadout: 2})
	require.False(t, isError)
	assert.Equal(t, later, f.h.snapshot.Fetched, "an equip restarts the age of the snapshot")
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
		"clear keeps the favorite": {
			in:   SetTagInput{InstanceIDs: []string{"1001"}, Tag: "none"},
			want: "Cleared the tag of 1 items.",
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

			writes := f.service.calls(http.MethodPost, pathTags)
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

func TestSetFavorite(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleSetFavorite, SetFavoriteInput{
		InstanceIDs: []string{"1001", "1001"},
		Favorite:    false,
	})
	assert.False(t, isError)
	assert.Equal(t, "Cleared the favorite mark of 1 items.", got)

	writes := f.service.calls(http.MethodPost, pathTags)
	require.Len(t, writes, 1)
	assert.Equal(t, []any{
		map[string]any{"instanceId": "1001", "itemHash": float64(100), "tagId": "favorite", "removed": true},
	}, writes[0].Body["items"])
}

func TestListTagsAndNotes(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleListTags, EmptyInput{})
	assert.False(t, isError)
	assert.Contains(t, got, "keep | Keep | 1\n")
	assert.Contains(t, got, "junk | Junk | 0\n")
	assert.Contains(t, got, "Favorites: 1 items")

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
				f.serviceData[pathNotes] = tc.notes
			}

			got, isError := call(t, f.h.handleSetNote, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)

			writes := f.service.calls(http.MethodPost, pathNotes)
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

	writes := f.service.calls(http.MethodPost, pathPrefs)
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
			assert.Empty(t, f.service.calls(http.MethodPost, pathPrefs))
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
				f.serviceData[pathAutotag] = tc.autotag
			}

			got, isError := call(t, f.h.handleListAutotagRules, EmptyInput{})
			assert.False(t, isError)
			assert.Equal(t, strings.Join(append([]string{
				"Autotagging is on.",
				"",
				"Rules in evaluation order. The first enabled rule that matches a new item wins.",
			}, tc.want...), "\n"), got)
		})
	}
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
				f.serviceData[pathAutotag] = tc.autotag
			}

			got, isError := call(t, f.h.handleSaveAutotagRule, tc.in)
			assert.Equal(t, tc.isError, isError)

			writes := f.service.calls(http.MethodPost, pathAutotag)
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
	down.profileBody = bungieDown

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
			f.profileBody = tc.profile

			got, isError := call(t, f.h.handleSaveAutotagRule, tc.in)
			assert.Equal(t, tc.isError, isError)

			profiles := f.bungie.calls(http.MethodGet, pathProfile)
			if tc.fetches {
				assert.NotEmpty(t, profiles)
			} else {
				assert.Empty(t, profiles)
			}

			writes := f.service.calls(http.MethodPost, pathAutotag)
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

	writes := f.service.calls(http.MethodPost, pathAutotag)
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
func tagRows(t *testing.T, rows ...ItemTag) string {
	t.Helper()

	if rows == nil {
		rows = []ItemTag{}
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
		return 0, len(ownedItems(f.h.snapshot)), []string{}
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
		assert.Equal(t, ownerVault, item.Owner)
	}
}

func TestPreviewTagKeep(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.serviceData[pathTags] = tagRows(t)

	_, _, rows := preview(t, f, "tag:keep")
	assert.Empty(t, rows)

	f.serviceData[pathTags] = tagRows(t, ItemTag{InstanceID: "1003", ItemHash: 102, TagID: "keep", UpdatedAt: 5})

	_, _, rows = preview(t, f, "tag:keep")
	assert.Equal(t, []string{"1003"}, rows)

	f.serviceData[pathTags] = tagRows(
		t,
		ItemTag{InstanceID: "1003", ItemHash: 102, TagID: "keep", UpdatedAt: 6, Removed: true},
	)

	_, _, rows = preview(t, f, "tag:keep")
	assert.Empty(t, rows)
}

func TestPreviewFavorite(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	favorite := ItemTag{InstanceID: "1003", ItemHash: 102, TagID: tagFavorite, UpdatedAt: 5}
	junk := ItemTag{InstanceID: "1003", ItemHash: 102, TagID: tagJunk, UpdatedAt: 5}
	f.serviceData[pathTags] = tagRows(t, favorite, junk)

	for _, query := range []string{"is:favorite", "tag:favorite", "tag:junk"} {
		_, _, rows := preview(t, f, query)
		assert.Equal(t, []string{"1003"}, rows, query)
	}

	junk.Removed = true
	f.serviceData[pathTags] = tagRows(t, favorite, junk)

	_, _, rows := preview(t, f, "is:favorite")
	assert.Equal(t, []string{"1003"}, rows)

	favorite.Removed = true
	f.serviceData[pathTags] = tagRows(t, favorite, junk)

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
			f.profileBody = tc.profile

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

	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 1, "the second call reuses the profile")

	_, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:weapon", Refresh: true})
	require.False(t, isError)
	assert.Len(t, f.bungie.calls(http.MethodGet, pathProfile), 2, "refresh fetches the profile again")
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
		"is:linearfusionrifle, is:lfr, is:submachine, is:smg, is:bow, is:glaive, is:transmat, "+
		"is:weaponmod, is:armormod, is:reptoken, is:primary, is:special, is:heavy, is:dupe, "+
		"is:incurrentchar, is:currentclass, tag:, dupe:item, count:, id:, hash:, name:, "+
		"and exactname:. Each is: word also works as not:<word>.", got)
	assert.Empty(t, f.service.all())
	assert.Empty(t, f.bungie.all())
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
			assert.Empty(t, f.service.all())
			assert.Empty(t, f.bungie.all())
		})
	}
}

func TestPreviewUnsupportedDupeValue(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:weapon dupe:stats"})
	assert.True(t, isError)
	assert.True(t, strings.HasPrefix(got, "unsupported query: the server does not evaluate dupe:stats, "), got)
	assert.Empty(t, f.service.all())
	assert.Empty(t, f.bungie.all())
}

func TestPreviewDupe(t *testing.T) {
	t.Parallel()

	// The Hunter holds one copy of a pair of hand cannons, and the vault
	// holds the other copy. Midnight Coup has one copy. The helmets are a
	// pair of armor pieces, so is:weapon leaves them out.
	f := newFakes(t)
	f.profileBody = `{"ErrorCode": 1, "Response": {
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
	f.h.defs.defs.Items[403] = ItemDef{
		Name:       "Dummy",
		Bucket:     bucketForbidden,
		ItemType:   itemTypeWeapon,
		ClassType:  classAny,
		Equippable: true,
	}
	f.profileBody = `{"ErrorCode": 1, "Response": {
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
			assert.Empty(t, f.service.all())
			assert.Empty(t, f.bungie.all())

			query := tc.query
			tag := tagJunk

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
			assert.Empty(t, f.service.all())
			assert.Empty(t, f.bungie.all())
		})
	}
}

func TestPreviewNoCharacter(t *testing.T) {
	t.Parallel()

	f := newFakes(t)
	f.profileBody = `{"ErrorCode": 1, "Response": {
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
	f.profileBody = `{"ErrorCode": 5, "ErrorStatus": "SystemDisabled", "Message": "down"}`

	want, isError := call(t, f.h.handleListItems, ListItemsInput{})
	require.True(t, isError)

	got, isError := call(t, f.h.handlePreviewAutotagRule, PreviewAutotagRuleInput{Query: "is:weapon"})
	assert.True(t, isError)
	assert.Equal(t, want, got)
	assert.True(t, strings.HasPrefix(got, "fetching the profile:"), got)
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
// fixtureNow.
func statsFixture(t *testing.T) string {
	t.Helper()

	ago := func(hours float64) int64 {
		return fixtureNow.Add(-time.Duration(hours * float64(time.Hour))).UnixMilli()
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

	return statPageJSON(t, false,
		testChunk{startsAt: 0, updatedAt: 1, changes: []map[string]any{
			// Before the lookback of the period.
			change("currency", itemGlimmer, 30, 0, 1_000),
			change("currency", itemGlimmer, 23, 1_000, 2_000),
			change("currency", itemGlimmer, 22, 2_000, 1_500),
			kills("9999", 102, trackerGambit, 6, 0, 20),
			kills("1001", 100, trackerPvE, 5, 100, 110),
		}},
		testChunk{startsAt: 300_000, updatedAt: 2, changes: []map[string]any{
			// A stale recorder logged the gain from 1000.
			change("currency", itemGlimmer, 21, 1_000, 1_700),
			change("stack", 300, 4, 40, 52),
			kills("1001", 100, trackerPvP, 4, 7, 9),
			change("currency", 77, 3, 3, 5),
			change("currency", itemBrightDust, 2, 1_500, 1_400),
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
	f.serviceData[pathStats] = statsFixture(t)

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
		"",
		"Weapon kills, most first:",
		"instance id | name | total | PvE | PvP",
		"9999 | Midnight Coup | 20 | 20 | 0",
		"1001 | Fatebringer | 12 | 10 | 2",
	}, "\n"), got)

	reads := f.service.calls(http.MethodGet, pathStats)
	require.Len(t, reads, 1)
	assert.Equal(t, "Bearer session-1", reads[0].Auth)
	assert.Equal(t, fmt.Sprintf("since=%d&after=0", fixtureNow.Add(-27*time.Hour).UnixMilli()), reads[0].Query)
	assert.Empty(t, f.bungie.all())
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
			wantQuery: fmt.Sprintf("since=%d&after=0", fixtureNow.Add(-7*24*time.Hour-3*time.Hour).UnixMilli()),
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
			f.serviceData[pathStats] = statsFixture(t)

			got, isError := call(t, f.h.handleGetStats, GetStatsInput{Period: tc.period})
			require.False(t, isError, got)

			for _, want := range tc.want {
				assert.Contains(t, got, want)
			}

			reads := f.service.calls(http.MethodGet, pathStats)
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
			f.serviceData[pathStats] = statPageJSON(t, false)

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
	f.serviceData[pathStats] = statsFixture(t)
	f.staleSession = "session-1"

	got, isError := call(t, f.h.handleGetStats, GetStatsInput{})
	require.False(t, isError, got)
	assert.Contains(t, got, "Glimmer | currency | 3159615086 | +700 | 1200 | 500")

	assert.Len(t, f.service.calls(http.MethodPost, "/api/auth/refresh"), 1)

	reads := f.service.calls(http.MethodGet, pathStats)
	require.Len(t, reads, 2)
	assert.Equal(t, "Bearer session-1", reads[0].Auth)
	assert.Equal(t, "Bearer session-2", reads[1].Auth)
	assert.Empty(t, f.bungie.all())
}

func TestGetStatsErrors(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleGetStats, GetStatsInput{Period: "1y"})
	assert.True(t, isError)
	assert.Contains(t, got, ErrInvalidInput.Error())
	assert.Contains(t, got, "24h, 7d, 30d, all")
	assert.Empty(t, f.service.all())
	assert.Empty(t, f.bungie.all())

	f.serviceData[pathStats] = `{"chunks": [{"startsAt": 0, "endsAt": 300000, "updatedAt": 1, "body": "{}"}], "more": false}`

	got, isError = call(t, f.h.handleGetStats, GetStatsInput{})
	assert.True(t, isError)
	assert.Contains(t, got, ErrService.Error())
}
