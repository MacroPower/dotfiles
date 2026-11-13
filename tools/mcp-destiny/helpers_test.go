package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Bucket hashes of the fixture slots.
const (
	bucketKinetic     uint32 = 1498876634
	bucketPower       uint32 = 953998645
	bucketHelmet      uint32 = 3448274439
	bucketConsumables uint32 = 1469714392
	bucketEngrams     uint32 = 375726501
	bucketQuests      uint32 = 1345459588
	bucketMaterials   uint32 = 3865314626
	bucketMessages    uint32 = 3161908920
)

// Item hashes of the fixture currencies.
const (
	itemGlimmer    uint32 = 3159615086
	itemBrightDust uint32 = 2817410917
)

// Character ids of the fixture profile.
const (
	hunterID = "c1"
	titanID  = "c2"
)

// fixtureProfile is a Bungie profile response body. The Hunter has the
// latest play date. It has a hand cannon and a helmet equipped, a second
// hand cannon in its inventory, and a third at the Postmaster. The vault
// holds a hand cannon with sockets and stats and a rocket launcher. The
// helmet has a stat total of 55. Every instanced item is lockable, and the
// vault hand cannon is locked. The account holds a stack of consumables,
// glimmer, bright dust, and a currency that the definitions lack. The
// consumables stack carries the vault location, because Bungie gives that
// location to every profile item.
//
// The Hunter saved three loadouts in the game. The first holds nothing.
// The second holds the equipped hand cannon and helmet. The third holds the
// vault hand cannon, the helmet, and an item that left the inventory. The
// Titan saved one loadout, which holds the hand cannon that the Hunter has
// equipped.
const fixtureProfile = `{
	"profileInventory": {"data": {"items": [
		{"itemHash": 100, "itemInstanceId": "1001", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802, "state": 5},
		{"itemHash": 101, "itemInstanceId": "1002", "lockable": true, "quantity": 1, "location": 2, "bucketHash": 138197802},
		{"itemHash": 300, "quantity": 42, "location": 2, "bucketHash": 1469714392}
	]}},
	"profileCurrencies": {"data": {"items": [
		{"itemHash": 3159615086, "quantity": 250000, "location": 1, "bucketHash": 2689798308},
		{"itemHash": 2817410917, "quantity": 1500, "location": 1, "bucketHash": 2689798311},
		{"itemHash": 77, "quantity": 3, "location": 1, "bucketHash": 2689798305}
	]}},
	"characters": {"data": {
		"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"},
		"c2": {"characterId": "c2", "classType": 0, "light": 1990, "dateLastPlayed": "2026-11-01T12:00:00Z"}
	}},
	"characterInventories": {"data": {
		"c1": {"items": [
			{"itemHash": 102, "itemInstanceId": "1003", "lockable": true, "quantity": 1, "location": 1, "bucketHash": 1498876634},
			{"itemHash": 100, "itemInstanceId": "1005", "lockable": true, "quantity": 1, "location": 4, "bucketHash": 215593132, "transferStatus": 2}
		]},
		"c2": {"items": []}
	}},
	"characterEquipment": {"data": {
		"c1": {"items": [
			{"itemHash": 200, "itemInstanceId": "1004", "lockable": true, "quantity": 1, "location": 1, "bucketHash": 3448274439, "transferStatus": 1},
			{"itemHash": 100, "itemInstanceId": "1006", "lockable": true, "quantity": 1, "location": 1, "bucketHash": 1498876634, "transferStatus": 1}
		]},
		"c2": {"items": []}
	}},
	"characterLoadouts": {"data": {
		"c1": {"loadouts": [
			{"nameHash": 2166136261, "iconHash": 2166136261, "colorHash": 2166136261, "items": [
				{"itemInstanceId": "0", "plugItemHashes": [2166136261]},
				{"itemInstanceId": "0", "plugItemHashes": [2166136261]}
			]},
			{"nameHash": 1, "iconHash": 2, "colorHash": 3, "items": [
				{"itemInstanceId": "1006", "plugItemHashes": [2166136261]},
				{"itemInstanceId": "1004", "plugItemHashes": [2166136261]}
			]},
			{"nameHash": 4, "iconHash": 5, "colorHash": 6, "items": [
				{"itemInstanceId": "1001", "plugItemHashes": [900, 901]},
				{"itemInstanceId": "1004", "plugItemHashes": [2166136261]},
				{"itemInstanceId": "9999", "plugItemHashes": [2166136261]},
				{"itemInstanceId": "0", "plugItemHashes": [2166136261]}
			]}
		]},
		"c2": {"loadouts": [
			{"nameHash": 7, "iconHash": 8, "colorHash": 9, "items": [
				{"itemInstanceId": "1006", "plugItemHashes": [2166136261]}
			]}
		]}
	}},
	"itemComponents": {
		"instances": {"data": {
			"1001": {"damageType": 1, "primaryStat": {"statHash": 9, "value": 2010}},
			"1002": {"damageType": 3, "primaryStat": {"statHash": 9, "value": 1990}},
			"1003": {"damageType": 1, "primaryStat": {"statHash": 9, "value": 2005}},
			"1004": {"damageType": 0, "primaryStat": {"statHash": 8, "value": 2000}, "isEquipped": true},
			"1005": {"damageType": 1, "primaryStat": {"statHash": 9, "value": 1970}},
			"1006": {"damageType": 1, "primaryStat": {"statHash": 9, "value": 1980}, "isEquipped": true}
		}},
		"stats": {"data": {
			"1001": {"stats": {"1": {"statHash": 1, "value": 55}, "7": {"statHash": 7, "value": 3}}},
			"1004": {"stats": {"2": {"statHash": 2, "value": 30}, "3": {"statHash": 3, "value": 25}}}
		}},
		"sockets": {"data": {
			"1001": {"sockets": [
				{"plugHash": 900, "isEnabled": true, "isVisible": true},
				{"plugHash": 901, "isEnabled": true, "isVisible": true},
				{"plugHash": 902, "isEnabled": true, "isVisible": true},
				{"plugHash": 903, "isEnabled": true, "isVisible": false}
			]}
		}},
		"reusablePlugs": {"data": {
			"1001": {"plugs": {"0": [{"plugItemHash": 900}, {"plugItemHash": 903}]}}
		}}
	}
}`

// Activity history paths of the fixture characters.
const (
	pathHunterHistory = "/Destiny2/3/Account/4611/Character/c1/Stats/Activities/"
	pathTitanHistory  = "/Destiny2/3/Account/4611/Character/c2/Stats/Activities/"
)

// fixtureHunterHistory is the Bungie response body of page 0 of the
// activity history of the Hunter. The newest run is a raid that the
// definitions name by its reference id. The second run comes from a
// playlist, so only its director activity hash has a definition. Neither
// hash of the third run has a definition with a name, and the run lacks
// the kills and the deaths.
const fixtureHunterHistory = `{"ErrorCode": 1, "Response": {"activities": [
	{"period": "2026-11-11T21:00:00Z",
	 "activityDetails": {"referenceId": 3022541210, "directorActivityHash": 3022541210, "instanceId": "9001",
	  "mode": 4, "isPrivate": false},
	 "values": {
		"activityDurationSeconds": {"statId": "activityDurationSeconds", "basic": {"value": 3723, "displayValue": "1h 2m"}},
		"completed": {"basic": {"value": 1}},
		"kills": {"basic": {"value": 84}},
		"deaths": {"basic": {"value": 2}}
	 }},
	{"period": "2026-11-10T19:30:00Z",
	 "activityDetails": {"referenceId": 1111, "directorActivityHash": 2222, "instanceId": "9002", "mode": 3},
	 "values": {
		"activityDurationSeconds": {"basic": {"value": 1290}},
		"completed": {"basic": {"value": 1}},
		"kills": {"basic": {"value": 150}},
		"deaths": {"basic": {"value": 0}}
	 }},
	{"period": "2026-11-09T08:00:00Z",
	 "activityDetails": {"referenceId": 3333, "directorActivityHash": 3334, "instanceId": "9003", "mode": 6},
	 "values": {
		"activityDurationSeconds": {"basic": {"value": 45}},
		"completed": {"basic": {"value": 0}}
	 }}
]}}`

// fixtureTitanHistory is the Bungie response body of page 0 of the
// activity history of the Titan. Both hashes of the newest run have a
// definition, and the two definitions differ.
const fixtureTitanHistory = `{"ErrorCode": 1, "Response": {"activities": [
	{"period": "2026-11-11T18:00:00Z",
	 "activityDetails": {"referenceId": 4444, "directorActivityHash": 5555, "instanceId": "9004", "mode": 10},
	 "values": {
		"activityDurationSeconds": {"basic": {"value": 600}},
		"completed": {"basic": {"value": 0}},
		"kills": {"basic": {"value": 12}},
		"deaths": {"basic": {"value": 9}}
	 }},
	{"period": "2026-11-01T12:00:00Z",
	 "activityDetails": {"referenceId": 2222, "directorActivityHash": 2222, "instanceId": "9005", "mode": 3},
	 "values": {
		"activityDurationSeconds": {"basic": {"value": 1800}},
		"completed": {"basic": {"value": 1}},
		"kills": {"basic": {"value": 60}},
		"deaths": {"basic": {"value": 1}}
	 }}
]}}`

func fixtureDefs() *Defs {
	weapon := func(name, typeName, tier string, bucket uint32, damage int) ItemDef {
		return ItemDef{
			Name:       name,
			TypeName:   typeName,
			Tier:       tier,
			Bucket:     bucket,
			ItemType:   itemTypeWeapon,
			ClassType:  classAny,
			DamageType: damage,
			Equippable: true,
		}
	}

	plug := func(name, typeName string) ItemDef {
		return ItemDef{Name: name, TypeName: typeName, ItemType: itemTypePlug, ClassType: classAny}
	}

	firefly := plug("Firefly", "Trait")
	firefly.Description = "Precision final blows cause targets to explode.\n\n Explosions deal Solar damage. "

	return &Defs{
		Version: "v1",
		Items: map[uint32]ItemDef{
			100: weapon("Fatebringer", "Hand Cannon", "Legendary", bucketKinetic, 1),
			101: weapon("Gjallarhorn", "Rocket Launcher", "Exotic", bucketPower, 3),
			102: weapon("Midnight Coup", "Hand Cannon", "Legendary", bucketKinetic, 1),
			200: {
				Name:       "Celestial Nighthawk",
				TypeName:   "Helmet",
				Tier:       "Exotic",
				Bucket:     bucketHelmet,
				ItemType:   itemTypeArmor,
				ClassType:  1,
				Equippable: true,
			},
			300: {
				Name:      "Enhancement Core",
				TypeName:  "Material",
				Tier:      "Legendary",
				Bucket:    bucketConsumables,
				ClassType: classAny,
			},
			900:            plug("Explosive Payload", "Trait"),
			901:            firefly,
			902:            plug("Default Shader", "Restore Defaults"),
			903:            plug("Rampage", "Trait"),
			904:            plug("Kill Clip", "Trait"),
			905:            plug("Kill Clip", "Enhanced Trait"),
			906:            plug("Chill Clip", "Trait"),
			907:            plug("Arrowhead Brake", "Barrel"),
			itemGlimmer:    {Name: "Glimmer", TypeName: "Currency", ClassType: classAny},
			itemBrightDust: {Name: "Bright Dust", TypeName: "Currency", ClassType: classAny},
		},
		Buckets: map[uint32]BucketDef{
			bucketVault:      {Name: "General", Capacity: 700, Category: bucketCategoryItem, Order: 30, Account: true},
			bucketPostmaster: {Name: "Lost Items", Capacity: 21},
			bucketKinetic:    {Name: "Kinetic Weapons", Capacity: 10, Category: bucketCategoryEquippable, Order: 20},
			bucketPower:      {Name: "Power Weapons", Capacity: 10, Category: bucketCategoryEquippable, Order: 40},
			bucketHelmet:     {Name: "Helmet", Capacity: 10, Category: bucketCategoryEquippable, Order: 50},
			bucketEngrams:    {Name: "Engrams", Capacity: 10, Category: bucketCategoryItem, Order: 40},
			bucketMaterials:  {Name: "Materials", Category: bucketCategoryItem, Order: 10},
			bucketQuests:     {Name: "Quests", Capacity: 63},
			bucketConsumables: {
				Name:     "Consumables",
				Capacity: 50,
				Category: bucketCategoryItem,
				Order:    30,
				Account:  true,
			},
		},
		Stats: map[uint32]string{1: "Range", 2: "Health", 3: "Grenade"},
		Activities: map[uint32]ActivityDef{
			3022541210: {Name: "Vault of Glass: Master"},
			2222:       {Name: "Vanguard Ops"},
			3334:       {},
			4444:       {Name: "Control"},
			5555:       {Name: "Crucible"},
		},
		DamageTypes: map[int]string{1: "Kinetic", 3: "Solar"},
		Classes:     map[int]string{0: "Titan", 1: "Hunter", 2: "Warlock"},
	}
}

func fixtureSnapshot(t *testing.T) *Snapshot {
	t.Helper()

	var profile Profile

	require.NoError(t, json.Unmarshal([]byte(fixtureProfile), &profile))

	return NewSnapshot(&profile, fixtureDefs(), fixtureNow)
}

var fixtureNow = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

// request is one request that a fake server received. Query is the raw
// query string of the URL.
type request struct {
	Body   map[string]any
	Method string
	Path   string
	Query  string
	Auth   string
}

// recorder collects the requests of a fake server.
type recorder struct {
	requests []request
	mu       sync.Mutex
}

func (r *recorder) record(t *testing.T, req *http.Request) request {
	t.Helper()

	raw, err := io.ReadAll(req.Body)
	require.NoError(t, err)

	out := request{
		Method: req.Method,
		Path:   req.URL.Path,
		Query:  req.URL.RawQuery,
		Auth:   req.Header.Get("Authorization"),
	}

	if len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &out.Body))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.requests = append(r.requests, out)

	return out
}

// calls returns the recorded requests for a method and path.
func (r *recorder) calls(method, path string) []request {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []request

	for _, req := range r.requests {
		if req.Method == method && req.Path == path {
			out = append(out, req)
		}
	}

	return out
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_, err := io.WriteString(w, body)
	require.NoError(t, err)
}

// fakes is a handler wired to a fake Agg's Destiny Vault Manager service
// and a fake Bungie platform.
type fakes struct {
	h       *handler
	service *recorder
	bungie  *recorder

	// serviceData holds the GET response body of each service path.
	serviceData map[string]string
	// bungieData holds the GET response body of each Bungie path.
	bungieData map[string]string
	// staleSession and staleAccess are tokens that the fakes reject.
	staleSession string
	staleAccess  string
	// bungieError is the body that the Bungie actions return. A set
	// bungieErrorPath limits it to the action at that path.
	bungieError     string
	bungieErrorPath string
}

func newFakes(t *testing.T) *fakes {
	t.Helper()

	f := &fakes{
		service: &recorder{},
		bungie:  &recorder{},
		bungieData: map[string]string{
			pathHunterHistory: fixtureHunterHistory,
			pathTitanHistory:  fixtureTitanHistory,
		},
		serviceData: map[string]string{
			"/api/tags": `{"defs": [], "items": [
				{"instanceId": "1001", "itemHash": 100, "tagId": "keep", "removed": false, "updatedAt": 5},
				{"instanceId": "1001", "itemHash": 100, "tagId": "favorite", "removed": false, "updatedAt": 5},
				{"instanceId": "1002", "itemHash": 101, "tagId": "junk", "removed": true, "updatedAt": 5}
			], "now": 10}`,
			"/api/notes": `{"notes": [
				{"kind": "instance", "targetId": "1001", "itemHash": 100, "body": "god roll", "removed": false, "updatedAt": 5},
				{"kind": "set", "targetId": "set-key", "body": "farm this", "removed": false, "updatedAt": 5}
			], "now": 10}`,
			"/api/perk-preferences": `{"preferences": [
				{"mode": "pve", "perk": "Firefly", "stance": "up", "removed": false, "updatedAt": 5},
				{"mode": "pvp", "perk": "Rampage", "stance": "down", "removed": false, "updatedAt": 5},
				{"mode": "pvp", "perk": "Kill Clip", "stance": "up", "removed": true, "updatedAt": 5}
			], "now": 10}`,
			"/api/autotag": `{"rules": [
				{"id": "r2", "query": "is:dupe", "vault": true, "enabled": false, "position": 1, "removed": false, "updatedAt": 5},
				{"id": "r1", "query": "rate:>=a", "tagId": "pve", "vault": false, "enabled": true, "position": 0, "removed": false, "updatedAt": 5},
				{"id": "r0", "query": "old", "vault": false, "enabled": true, "position": 2, "removed": true, "updatedAt": 5}
			], "state": {"enabled": true, "mark": "00000000000000001006", "updatedAt": 5}, "now": 10}`,
		},
	}

	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := f.service.record(t, r)

		switch {
		case req.Path == "/api/config":
			writeJSON(t, w, http.StatusOK,
				`{"apiKey": "key", "clientId": "54520", "artifacts": {"manifestVersion": "v1", "files": {}}}`)

		case req.Path == "/api/auth/refresh":
			writeJSON(t, w, http.StatusOK, `{"access_token": "access-2", "expires_in": 3600,
				"refresh_token": "refresh-2", "refresh_expires_in": 7776000, "membership_id": "42", "session": "session-2"}`)

		case f.staleSession != "" && req.Auth == "Bearer "+f.staleSession:
			writeJSON(t, w, http.StatusUnauthorized, `{"error": "Sign in required"}`)
		case req.Method == http.MethodGet && f.serviceData[req.Path] != "":
			writeJSON(t, w, http.StatusOK, f.serviceData[req.Path])
		case req.Path == "/api/autotag/state":
			writeJSON(t, w, http.StatusOK, `{"applied": true}`)
		case req.Method == http.MethodPost:
			writeJSON(t, w, http.StatusOK, `{"now": 11}`)
		default:
			writeJSON(t, w, http.StatusNotFound, `{"error": "Not found"}`)
		}
	}))
	t.Cleanup(service.Close)

	bungie := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := f.bungie.record(t, r)

		switch {
		case f.staleAccess != "" && req.Auth == "Bearer "+f.staleAccess:
			writeJSON(t, w, http.StatusUnauthorized, "")
		case req.Path == "/User/GetMembershipsForCurrentUser/":
			writeJSON(t, w, http.StatusOK, `{"ErrorCode": 1, "Response": {"primaryMembershipId": "4611",
				"destinyMemberships": [{"membershipType": 2, "membershipId": "1"}, {"membershipType": 3, "membershipId": "4611"}]}}`)

		case req.Path == "/Destiny2/3/Profile/4611/":
			writeJSON(t, w, http.StatusOK, `{"ErrorCode": 1, "Response": `+fixtureProfile+`}`)
		case req.Method == http.MethodGet && f.bungieData[req.Path] != "":
			writeJSON(t, w, http.StatusOK, f.bungieData[req.Path])
		case req.Method == http.MethodPost && f.bungieError != "" &&
			(f.bungieErrorPath == "" || req.Path == f.bungieErrorPath):
			writeJSON(t, w, http.StatusOK, f.bungieError)
		case req.Method == http.MethodPost:
			writeJSON(t, w, http.StatusOK, `{"ErrorCode": 1, "Response": 0}`)
		default:
			writeJSON(
				t,
				w,
				http.StatusNotFound,
				`{"ErrorCode": 7, "ErrorStatus": "ParameterParseFailure", "Message": "no route"}`,
			)
		}
	}))
	t.Cleanup(bungie.Close)

	now := func() time.Time { return fixtureNow }

	store := NewTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	require.NoError(t, store.Save(Tokens{
		AccessToken:    "access-1",
		AccessExpires:  fixtureNow.Add(time.Hour),
		RefreshToken:   "refresh-1",
		RefreshExpires: fixtureNow.Add(24 * time.Hour),
		Session:        "session-1",
		BungieID:       "42",
	}))

	client := NewService(WithServiceURL(service.URL))

	defs := NewDefsLoader(client, "", WithDefsClock(now))
	defs.defs = fixtureDefs()
	defs.checked = fixtureNow

	f.h = &handler{
		service: client,
		bungie:  NewBungie(WithBungieURL(bungie.URL)),
		auth:    NewAuth(store, client, WithAuthClock(now)),
		defs:    defs,
		log:     slog.New(slog.DiscardHandler),
		now:     now,
	}

	return f
}

// text returns the text of a tool result and whether it is an error.
func text(t *testing.T, result *mcp.CallToolResult, err error) (string, bool) {
	t.Helper()

	require.NoError(t, err)
	require.Len(t, result.Content, 1)

	content, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	return content.Text, result.IsError
}

// call runs a tool handler and returns the text of its result and whether
// the result is an error.
func call[In any](
	t *testing.T,
	fn func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error),
	in In,
) (string, bool) {
	t.Helper()

	result, _, err := fn(t.Context(), nil, in)

	return text(t, result, err)
}
