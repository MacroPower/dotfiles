package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
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
	pathTags     = "/api/tags"
	pathNotes    = "/api/notes"
	pathPrefs    = "/api/perk-preferences"
	pathAutotag  = "/api/autotag"
	pathProfile  = "/Destiny2/3/Profile/4611/"
	pathTransfer = "/Destiny2/Actions/Items/TransferItem/"
	pathEquip    = "/Destiny2/Actions/Items/EquipItem/"
	pathLock     = "/Destiny2/Actions/Items/SetLockState/"
)

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
		toolSetPerkPreferences, toolListAutotagRules, toolSaveAutotagRule, toolRemoveAutotagRule,
		toolMoveStack, toolSetLock, toolSetAutotagEnabled, toolListActivityHistory,
	}, names)

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

	tests := map[string]struct {
		want    string
		in      ListItemsInput
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
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			got, isError := call(t, f.h.handleListItems, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)
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

			got := renderItems(&Snapshot{}, []*Item{tc.item}, 0, 1, "", false)
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

	f := newFakes(t)

	got, isError := call(t, f.h.handleGetItem, GetItemInput{InstanceID: "1001"})
	assert.False(t, isError)
	assert.Equal(t, strings.Join([]string{
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
		"Stats:",
		"- Range: 55",
	}, "\n"), got)

	got, isError = call(t, f.h.handleGetItem, GetItemInput{InstanceID: "9"})
	assert.True(t, isError)
	assert.Equal(t, "invalid input: no item in the inventory has the instance id 9", got)
}

func TestCompareDuplicates(t *testing.T) {
	t.Parallel()

	fatebringer := []string{
		"Fatebringer: 3 copies",
		"",
		"instance id | name | type | power | location | tag | flags | perks",
		"1001 | Fatebringer | Kinetic Hand Cannon | 2010 | Vault | keep | favorite, locked, masterwork, note | " +
			"Explosive Payload (can swap to Rampage); Firefly",
		"1005 | Fatebringer | Kinetic Hand Cannon | 1970 | Hunter (postmaster) |  |  | ",
		"1006 | Fatebringer | Kinetic Hand Cannon | 1980 | Hunter (equipped) |  |  | ",
	}

	everyDuplicate := strings.Join(append([]string{
		"Weapons 1-1 of 1 that have more than one copy.",
		"",
	}, fatebringer...), "\n")

	tests := map[string]struct {
		// edit changes the cached snapshot before the call.
		edit    func(*Snapshot)
		want    string
		in      CompareDuplicatesInput
		isError bool
	}{
		"name": {
			in:   CompareDuplicatesInput{Name: "fatebringer"},
			want: strings.Join(fatebringer, "\n"),
		},
		"instance id": {
			in:   CompareDuplicatesInput{InstanceID: "1006"},
			want: strings.Join(fatebringer, "\n"),
		},
		"one copy": {
			in: CompareDuplicatesInput{Name: "coup"},
			want: strings.Join([]string{
				"Midnight Coup: 1 copy",
				"",
				"instance id | name | type | power | location | tag | flags | perks",
				"1003 | Midnight Coup | Kinetic Hand Cannon | 2005 | Hunter |  |  | ",
			}, "\n"),
		},
		"instance id of a weapon whose name differs only in case": {
			edit: func(s *Snapshot) {
				coup, _ := s.Find("1003")
				twin := *coup
				twin.InstanceID = "1007"
				twin.Name = "MIDNIGHT COUP"
				s.add(&twin)
			},
			in: CompareDuplicatesInput{InstanceID: "1003"},
			want: strings.Join([]string{
				"Midnight Coup: 1 copy",
				"",
				"instance id | name | type | power | location | tag | flags | perks",
				"1003 | Midnight Coup | Kinetic Hand Cannon | 2005 | Hunter |  |  | ",
			}, "\n"),
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
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

			if tc.edit != nil {
				s, err := f.h.loadSnapshot(t.Context(), false)
				require.NoError(t, err)
				tc.edit(s)
			}

			got, isError := call(t, f.h.handleCompareDuplicates, tc.in)
			assert.Equal(t, tc.isError, isError)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCompareDuplicatesPaging(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	s, err := f.h.loadSnapshot(t.Context(), false)
	require.NoError(t, err)

	// A second Midnight Coup gives the inventory two weapons with copies.
	coup, ok := s.Find("1003")
	require.True(t, ok)

	twin := *coup
	twin.InstanceID = "1007"
	twin.Owner = ownerVault
	s.add(&twin)

	got, isError := call(t, f.h.handleCompareDuplicates, CompareDuplicatesInput{Limit: 1, Offset: 1})
	assert.False(t, isError)
	assert.Equal(t, strings.Join([]string{
		"Weapons 2-2 of 2 that have more than one copy.",
		"",
		"Midnight Coup: 2 copies",
		"",
		"instance id | name | type | power | location | tag | flags | perks",
		"1003 | Midnight Coup | Kinetic Hand Cannon | 2005 | Hunter |  |  | ",
		"1007 | Midnight Coup | Kinetic Hand Cannon | 2005 | Vault |  |  | ",
	}, "\n"), got)

	got, isError = call(t, f.h.handleCompareDuplicates, CompareDuplicatesInput{Limit: 1})
	assert.False(t, isError)
	assert.True(t, strings.HasPrefix(got,
		"Weapons 1-1 of 2 that have more than one copy. Pass offset=1 for the next page.\n\nFatebringer: 3 copies"))
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

func TestListAutotagRules(t *testing.T) {
	t.Parallel()

	f := newFakes(t)

	got, isError := call(t, f.h.handleListAutotagRules, EmptyInput{})
	assert.False(t, isError)
	assert.Equal(t, strings.Join([]string{
		"Autotagging is on.",
		"",
		"Rules in evaluation order. The first enabled rule that matches a new item wins.",
		`- 0. [enabled] "rate:>=a": tag pve (id r1)`,
		`- 1. [disabled] "is:dupe": move to vault (id r2)`,
	}, "\n"), got)
}

func TestSaveAutotagRule(t *testing.T) {
	t.Parallel()

	query := "is:weapon misses:2"
	empty := ""
	junk := "junk"
	yes := true
	position := 0

	// The query is 600 UTF-16 code units and 1800 bytes.
	wide := strings.Repeat("\u706b", 600)
	long := strings.Repeat("\U0001F600", maxQueryLength/2+1)

	tests := map[string]struct {
		wantRule map[string]any
		want     string
		in       SaveAutotagRuleInput
		isError  bool
	}{
		"create appends after the last live rule": {
			in: SaveAutotagRuleInput{Query: &query, TagID: &junk, Vault: &yes},
			wantRule: map[string]any{
				"query": "is:weapon misses:2", "tagId": "junk", "vault": true,
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
		"update clears the tag": {
			in: SaveAutotagRuleInput{ID: "r1", TagID: &empty},
			wantRule: map[string]any{
				"id": "r1", "query": "rate:>=a", "vault": false,
				"enabled": true, "position": float64(0), "removed": false,
			},
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
			want:    "invalid input: a rule query has between 1 and 1000 characters",
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
			want:    "invalid input: a rule query has between 1 and 1000 characters",
			isError: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakes(t)

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
