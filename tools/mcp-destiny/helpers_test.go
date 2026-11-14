package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvmtest"
)

// Bucket hashes of the fixture slots that no production code reads.
const (
	bucketQuests    uint32 = 1345459588
	bucketMaterials uint32 = 3865314626
)

func fixtureDefs() *Defs {
	weapon := func(name, typeName, tier string, bucket uint32, damage int) ItemDef {
		return ItemDef{
			Name:       name,
			TypeName:   typeName,
			Tier:       tier,
			Bucket:     bucket,
			ItemType:   bungie.ItemTypeWeapon,
			ClassType:  bungie.ClassAny,
			DamageType: damage,
			Equippable: true,
		}
	}

	plug := func(name, typeName string) ItemDef {
		return ItemDef{Name: name, TypeName: typeName, ItemType: bungie.ItemTypePlug, ClassType: bungie.ClassAny}
	}

	firefly := plug("Firefly", "Trait")
	firefly.Description = "Precision final blows cause targets to explode.\n\n Explosions deal Solar damage. "

	return &Defs{
		Version: "v1",
		Items: map[uint32]ItemDef{
			100: weapon("Fatebringer", "Hand Cannon", "Legendary", bungie.BucketKinetic, 1),
			101: weapon("Gjallarhorn", "Rocket Launcher", "Exotic", bungie.BucketPower, 3),
			102: weapon("Midnight Coup", "Hand Cannon", "Legendary", bungie.BucketKinetic, 1),
			200: {
				Name:       "Celestial Nighthawk",
				TypeName:   "Helmet",
				Tier:       "Exotic",
				Bucket:     bungie.BucketHelmet,
				ItemType:   bungie.ItemTypeArmor,
				ClassType:  1,
				Equippable: true,
			},
			300: {
				Name:      "Enhancement Core",
				TypeName:  "Material",
				Tier:      "Legendary",
				Bucket:    bungie.BucketConsumables,
				ClassType: bungie.ClassAny,
			},
			900:                       plug("Explosive Payload", "Trait"),
			901:                       firefly,
			902:                       plug("Default Shader", "Restore Defaults"),
			903:                       plug("Rampage", "Trait"),
			904:                       plug("Kill Clip", "Trait"),
			905:                       plug("Kill Clip", "Enhanced Trait"),
			906:                       plug("Chill Clip", "Trait"),
			907:                       plug("Arrowhead Brake", "Barrel"),
			908:                       plug("Chill Clip", "Enhanced Trait"),
			bungietest.ItemGlimmer:    {Name: "Glimmer", TypeName: "Currency", ClassType: bungie.ClassAny},
			bungietest.ItemBrightDust: {Name: "Bright Dust", TypeName: "Currency", ClassType: bungie.ClassAny},
		},
		Buckets: map[uint32]BucketDef{
			bungie.BucketVault: {
				Name:     "General",
				Capacity: 700,
				Category: bungie.BucketCategoryItem,
				Order:    30,
				Account:  true,
			},
			bungie.BucketPostmaster: {Name: "Lost Items", Capacity: 21},
			bungie.BucketKinetic: {
				Name:     "Kinetic Weapons",
				Capacity: 10,
				Category: bungie.BucketCategoryEquippable,
				Order:    20,
			},
			bungie.BucketPower: {
				Name:     "Power Weapons",
				Capacity: 10,
				Category: bungie.BucketCategoryEquippable,
				Order:    40,
			},
			bungie.BucketHelmet: {
				Name:     "Helmet",
				Capacity: 10,
				Category: bungie.BucketCategoryEquippable,
				Order:    50,
			},
			bungie.BucketEngrams: {Name: "Engrams", Capacity: 10, Category: bungie.BucketCategoryItem, Order: 40},
			bucketMaterials:      {Name: "Materials", Category: bungie.BucketCategoryItem, Order: 10},
			bucketQuests:         {Name: "Quests", Capacity: 63},
			bungie.BucketConsumables: {
				Name:     "Consumables",
				Capacity: 50,
				Category: bungie.BucketCategoryItem,
				Order:    30,
				Account:  true,
			},
		},
		Stats: map[uint32]string{1: "Range", 2: "Health", 3: "Grenade"},
		Activities: map[uint32]ActivityDef{
			3022541210: {Name: "Vault of Glass: Master", Mode: 4, Type: 100, Destination: 700, Place: 800},
			2222:       {Name: "Vanguard Ops", Mode: 18, Type: 103},
			3334:       {},
			4444:       {Name: "Control", Mode: 10, Type: 101, Destination: 702, Place: 800},
			5555:       {Name: "Crucible"},
			4001:       {Name: "Warlord's Ruin", Mode: 82, Type: 102, Destination: 701, Place: 800},
			4002:       {Name: "The Coil", Mode: 6, Type: 104, Destination: 703},
			4003:       {Name: "Nightfall: Grandmaster", Mode: 46, Type: 103},
			4004:       {Name: "Pantheon", Mode: 4, Type: 100},
			4006:       {Name: "The Witness", Mode: 2, Type: 104},
			4007:       {Name: "Excision: Grandmaster", Type: 1686739444},
			4008:       {Name: "The Tower", Mode: 40},
			4009:       {Name: "Battleground: Oracle", Mode: 18, Type: 103},
		},
		ActivityTypes: map[uint32]string{
			100:        "Raid",
			101:        "The Crucible",
			102:        "Dungeon",
			103:        "Strike",
			104:        "Exotic Mission",
			1686739444: "Story",
		},
		Destinations: map[uint32]string{700: "Venus", 702: "The Crucible", 703: "Neptune"},
		Places:       map[uint32]string{800: "Earth"},
		ActivityRewards: map[uint32]string{
			600: "Raid Gear",
			601: "Deepsight Weapon",
			603: "Powerful Gear",
			604: "Enhancement Prism",
			605: "Raid Gear",
			606: "Adept Weapon",
		},
		Vendors: map[uint32]VendorDef{
			bungietest.VendorBanshee: {
				Name:           "Banshee-44",
				Subtitle:       "Gunsmith",
				FailureStrings: []string{"", "Requires Gunsmith rank 10.", "Already Purchased on Account"},
				Categories:     map[int]string{0: "Weapons", 1: "Materials", 2: "", 3: "Exotic Weapons"},
			},
			bungietest.VendorTess: {
				Name:       "Tess Everis",
				Subtitle:   "Eververse Trading Company",
				Categories: map[int]string{0: "Featured"},
			},
			bungietest.VendorXur:       {Name: "Xûr", Subtitle: "Agent of the Nine"},
			bungietest.VendorXurOffers: {Name: "Xûr", Subtitle: "Strange Gear Offers"},
			bungietest.VendorNameless:  {},
		},
		VendorItems: map[uint32]string{
			500: "Ammo Synthesis",
			501: "Legendary Shards",
			502: "Eververse Engram",
			503: "Ornament of the Hunt",
		},
		DamageTypes: map[int]string{1: "Kinetic", 3: "Solar"},
		Classes:     map[int]string{0: "Titan", 1: "Hunter", 2: "Warlock"},
	}
}

func fixtureSnapshot(t *testing.T) *Snapshot {
	t.Helper()

	var profile bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(bungietest.ProfileJSON), &profile))

	return NewSnapshot(&profile, fixtureDefs(), bungietest.Now)
}

// fakes is a handler wired to a fake Agg's Destiny Vault Manager service
// and a fake Bungie platform.
type fakes struct {
	h       *handler
	service *dvmtest.Server
	bungie  *bungietest.Server
}

func newFakes(t *testing.T) *fakes {
	t.Helper()

	f := &fakes{
		service: dvmtest.NewServer(t),
		bungie:  bungietest.NewServer(t),
	}

	// The bodies name items of the Bungie fixture profile, which the fake
	// service does not know.
	maps.Copy(f.service.Data, map[string]string{
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
	})

	now := func() time.Time { return bungietest.Now }

	store := NewTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	require.NoError(t, store.Save(Tokens{
		AccessToken:    "access-1",
		AccessExpires:  bungietest.Now.Add(time.Hour),
		RefreshToken:   "refresh-1",
		RefreshExpires: bungietest.Now.Add(24 * time.Hour),
		Session:        "session-1",
		BungieID:       "42",
	}))

	client := NewService(WithServiceURL(f.service.URL))

	defs := NewDefsLoader(client, "", WithDefsClock(now))
	defs.defs = fixtureDefs()
	defs.checked = bungietest.Now

	f.h = &handler{
		service: client,
		bungie:  bungie.New(bungie.WithURL(f.bungie.URL), bungie.WithContentURL(f.bungie.URL)),
		auth:    NewAuth(store, client, WithAuthClock(now)),
		defs:    defs,
		sheets:  NewSheetStore(client),
		log:     slog.New(slog.DiscardHandler),
		now:     now,
	}
	f.h.loadoutTables = bungie.NewLoadoutTableStore(bungieManifest{h: f.h})

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
