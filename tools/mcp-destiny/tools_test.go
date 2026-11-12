package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
		toolStatus, toolLogin, toolCompleteLogin, toolListCharacters, toolListItems, toolGetItem, toolMoveItems,
		toolListTags, toolSetTag, toolSetFavorite, toolListNotes, toolSetNote, toolListPerkPreferences,
		toolSetPerkPreferences, toolListAutotagRules, toolSaveAutotagRule, toolRemoveAutotagRule,
		toolMoveStack, toolSetLock, toolSetAutotagEnabled,
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
		"- Titan | id c2 | power 1990 | last played 2026-11-01 12:00 UTC | " +
			"0 equipped, 0 in inventory, 0 at the Postmaster",
		"",
		"Vault: 2 items",
	}, "\n"), got)
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
				"instance id | name | type | tier | power | location | tag | flags | perks",
				"1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 | Vault | keep | " +
					"favorite, locked, masterwork, note | Explosive Payload, Firefly",
				"1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 | Vault |  |  | ",
			}, "\n"),
		},
		"paging": {
			in: ListItemsInput{Sort: sortPower, Limit: 2, Offset: 2},
			want: strings.Join([]string{
				"Items 3-4 of 6. Pass offset=4 for the next page.",
				"",
				"instance id | name | type | tier | power | location | tag | flags",
				"1004 | Celestial Nighthawk | Helmet | Exotic | 2000 | Hunter (equipped) |  | ",
				"1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 | Vault |  | ",
			}, "\n"),
		},
		"stack": {
			in: ListItemsInput{Kind: kindOther},
			want: strings.Join([]string{
				"Items 1-1 of 1.",
				"",
				"instance id | name | type | tier | power | location | tag | flags",
				"stack of 42 (item hash 300) | Enhancement Core | Material | Legendary |  | Account |  | ",
			}, "\n"),
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
	assert.Contains(t, got, "stack of 12 (item hash 300) | Enhancement Core | Material | Legendary |  | Vault")

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
	assert.Contains(t, got, "1002 | Gjallarhorn | Solar Rocket Launcher | Exotic | 1990 | Vault |  | locked\n")
	assert.Contains(t, got, "1003 | Midnight Coup | Kinetic Hand Cannon | Legendary | 2005 | Hunter |  | locked")
	assert.Contains(t, got, "1001 | Fatebringer | Kinetic Hand Cannon | Legendary | 2010 | Vault | keep | "+
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
