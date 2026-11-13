package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Bucket hashes of the fixture slots that no production code reads.
const (
	bucketConsumables uint32 = 1469714392
	bucketQuests      uint32 = 1345459588
	bucketMaterials   uint32 = 3865314626
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
//
// The activities of the Hunter come out of rank order. Vault of Glass
// offers the most bonus drops across two reward mappings. Its first
// focused reward has no reward record, and two of its bonus loot items
// share a name. It also lists bonus loot with a quantity of zero and bonus
// loot without a reward record. Control offers only a focused reward
// without a reward record. Warlord's Ruin recommends a power of zero, and
// The Coil recommends a null power. Vanguard Ops and Battleground: Oracle
// leave out isVisible.
//
// The Hunter can also play activities that the tool leaves out. The game
// hides Pantheon, and 4005 has no definition. The definition of 3334 has
// no name. The Witness has the Story mode, Excision: Grandmaster has a
// Story type, and The Tower has the Social mode. The Titan can play
// Control.
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
	"characterActivities": {"data": {
		"c1": {"dateActivityStarted": "2026-11-10T11:00:00Z", "availableActivities": [
			{"activityHash": 4444, "isVisible": true, "isFocusedActivity": false, "canLead": true, "visibleRewards": [
				{"displayBehavior": 0, "rewardItems": [
					{"uiStyle": "daily_grind_guaranteed", "itemQuantity": {"itemHash": 698, "quantity": 1}}
				]}
			]},
			{"activityHash": 3022541210, "isVisible": true, "recommendedLight": 2010, "visibleRewards": [
				{"rewardItems": [
					{"uiStyle": "extra_engram", "itemQuantity": {"itemHash": 3956025454, "quantity": 2}},
					{"uiStyle": "daily_grind_guaranteed", "itemQuantity": {"itemHash": 698, "quantity": 1}},
					{"uiStyle": "daily_grind_guaranteed", "itemQuantity": {"itemHash": 601, "quantity": 1}},
					{"itemQuantity": {"itemHash": 600, "quantity": 1}}
				]},
				{"rewardItems": [
					{"uiStyle": "extra_engram", "itemQuantity": {"itemHash": 3956025454, "quantity": 1}},
					{"uiStyle": "", "itemQuantity": {"itemHash": 605, "quantity": 1}},
					{"itemQuantity": {"itemHash": 603, "quantity": 0}},
					{"itemQuantity": {"itemHash": 699, "quantity": 1}}
				]}
			]},
			{"activityHash": 4009, "recommendedLight": 1980},
			{"activityHash": 2222, "visibleRewards": [{"rewardItems": [
				{"itemQuantity": {"itemHash": 604, "quantity": 2}}
			]}]},
			{"activityHash": 4002, "isVisible": true, "isFocusedActivity": true, "recommendedLight": null},
			{"activityHash": 4004, "isVisible": false, "visibleRewards": [{"rewardItems": [
				{"uiStyle": "extra_engram", "itemQuantity": {"itemHash": 3956025454, "quantity": 5}}
			]}]},
			{"activityHash": 4005, "isVisible": true, "visibleRewards": [{"rewardItems": [
				{"uiStyle": "extra_engram", "itemQuantity": {"itemHash": 3956025454, "quantity": 4}}
			]}]},
			{"activityHash": 3334, "isVisible": true},
			{"activityHash": 4006, "isVisible": true},
			{"activityHash": 4007, "isVisible": true},
			{"activityHash": 4008, "isVisible": true},
			{"activityHash": 4003, "isVisible": true, "recommendedLight": 2020, "visibleRewards": [{"rewardItems": [
				{"uiStyle": "daily_grind_guaranteed", "itemQuantity": {"itemHash": 606, "quantity": 1}}
			]}]},
			{"activityHash": 4001, "isVisible": true, "recommendedLight": 0, "visibleRewards": [{"rewardItems": [
				{"uiStyle": "extra_engram", "itemQuantity": {"itemHash": 3956025454, "quantity": 1}}
			]}]}
		]},
		"c2": {"availableActivities": [
			{"activityHash": 4444, "isVisible": true, "recommendedLight": 1990}
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
			"1001": {"plugs": {"0": [{"plugItemHash": 900, "canInsert": true}, {"plugItemHash": 903, "canInsert": true}]}}
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

// Hashes of the fixture vendors. Two vendors share the name Xûr. The
// definitions do not name the sub-vendor of Eververse.
const (
	vendorBanshee   uint32 = 672118013
	vendorTess      uint32 = 3361454721
	vendorXur       uint32 = 2190858386
	vendorXurOffers uint32 = 3751514131
	vendorTessRow   uint32 = 2168194999
	vendorNameless  uint32 = 1111
)

// Vendor paths of the fixture characters.
const (
	pathHunterVendors = "/Destiny2/3/Profile/4611/Character/c1/Vendors/"
	pathTitanVendors  = "/Destiny2/3/Profile/4611/Character/c2/Vendors/"
)

// fixtureHunterVendors is the Bungie GetVendors response body of the
// Hunter. Banshee-44 lists its categories out of display order, and its
// third category has no name. Its fourth category lists only a weapon that
// Banshee-44 displays without selling it, and the status of that sale also
// carries the NoUnlock failure bit. Banshee-44 sells a weapon that the
// Hunter cannot buy yet, three items that no category lists, and an item
// that no definition names. The failure indexes of the weapon that the
// Hunter cannot buy include a negative index and an index past the last
// failure string of Banshee-44. The sales list the sub-vendor of
// Eververse, and the vendors do not. Xûr is disabled, and the stock of the
// other Xûr never rotates.
const fixtureHunterVendors = `{"ErrorCode": 1, "Response": {
	"vendors": {"data": {
		"672118013": {"vendorHash": 672118013, "enabled": true, "nextRefreshDate": "2026-11-13T17:00:00Z"},
		"3361454721": {"vendorHash": 3361454721, "enabled": true, "nextRefreshDate": "2026-11-17T17:00:00Z"},
		"2190858386": {"vendorHash": 2190858386, "enabled": false, "nextRefreshDate": "2026-11-14T17:00:00Z"},
		"3751514131": {"vendorHash": 3751514131, "enabled": true, "nextRefreshDate": "9999-12-31T23:59:59.999Z"},
		"1111": {"vendorHash": 1111, "enabled": true, "nextRefreshDate": "2026-11-13T17:00:00Z"}
	}},
	"categories": {"data": {
		"672118013": {"categories": [
			{"displayCategoryIndex": 1, "itemIndexes": [5, 6]},
			{"displayCategoryIndex": 3, "itemIndexes": [3]},
			{"displayCategoryIndex": 0, "itemIndexes": [2, 1, 4]},
			{"displayCategoryIndex": 2, "itemIndexes": [8]}
		]},
		"3361454721": {"categories": [{"displayCategoryIndex": 0, "itemIndexes": [0]}]},
		"2168194999": {"categories": [{"displayCategoryIndex": 0, "itemIndexes": [0]}]}
	}},
	"sales": {"data": {
		"672118013": {"saleItems": {
			"0": {"vendorItemIndex": 0, "itemHash": 100, "quantity": 1, "saleStatus": 0,
			 "costs": [{"itemHash": 3159615086, "quantity": 5}]},
			"1": {"vendorItemIndex": 1, "itemHash": 100, "quantity": 1, "saleStatus": 0, "costs": [
				{"itemHash": 3159615086, "quantity": 50000},
				{"itemHash": 501, "quantity": 10},
				{"itemHash": 0, "quantity": 5},
				{"itemHash": 300, "quantity": 0}
			]},
			"2": {"vendorItemIndex": 2, "itemHash": 102, "quantity": 1, "saleStatus": 8, "failureIndexes": [1, 0, -1, 2, 9],
			 "costs": [{"itemHash": 3159615086, "quantity": 1000}]},
			"3": {"vendorItemIndex": 3, "itemHash": 101, "quantity": 1, "saleStatus": 8200, "costs": []},
			"4": {"vendorItemIndex": 4, "itemHash": 999, "quantity": 1, "saleStatus": 0,
			 "costs": [{"itemHash": 3159615086, "quantity": 10}]},
			"5": {"vendorItemIndex": 5, "itemHash": 500, "quantity": 5, "saleStatus": 0,
			 "costs": [{"itemHash": 501, "quantity": 2}]},
			"6": {"vendorItemIndex": 6, "itemHash": 300, "quantity": 1, "saleStatus": 0},
			"7": {"vendorItemIndex": 7, "itemHash": 300, "quantity": 3, "saleStatus": 2,
			 "costs": [{"itemHash": 77, "quantity": 4}]},
			"8": {"vendorItemIndex": 8, "itemHash": 500, "quantity": 1, "saleStatus": 0,
			 "costs": [{"itemHash": 3159615086, "quantity": 500}]},
			"10": {"vendorItemIndex": 10, "itemHash": 501, "quantity": 2, "saleStatus": 0,
			 "costs": [{"itemHash": 3159615086, "quantity": 100}]}
		}},
		"3361454721": {"saleItems": {
			"0": {"vendorItemIndex": 0, "itemHash": 502, "quantity": 1, "saleStatus": 0,
			 "costs": [{"itemHash": 2817410917, "quantity": 500}]}
		}},
		"2168194999": {"saleItems": {
			"0": {"vendorItemIndex": 0, "itemHash": 503, "quantity": 1, "saleStatus": 0,
			 "costs": [{"itemHash": 2817410917, "quantity": 700}]}
		}}
	}}
}}`

// fixtureTitanVendors is the Bungie GetVendors response body of the Titan.
// Banshee-44 sells the Titan a weapon that the Hunter cannot buy yet.
const fixtureTitanVendors = `{"ErrorCode": 1, "Response": {
	"vendors": {"data": {
		"672118013": {"vendorHash": 672118013, "enabled": true, "nextRefreshDate": "2026-11-13T17:00:00Z"}
	}},
	"categories": {"data": {
		"672118013": {"categories": [{"displayCategoryIndex": 0, "itemIndexes": [2]}]}
	}},
	"sales": {"data": {
		"672118013": {"saleItems": {
			"2": {"vendorItemIndex": 2, "itemHash": 102, "quantity": 1, "saleStatus": 0,
			 "costs": [{"itemHash": 3159615086, "quantity": 1000}]}
		}}
	}}
}}`

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
			908:            plug("Chill Clip", "Enhanced Trait"),
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
			vendorBanshee: {
				Name:           "Banshee-44",
				Subtitle:       "Gunsmith",
				FailureStrings: []string{"", "Requires Gunsmith rank 10.", "Already Purchased on Account"},
				Categories:     map[int]string{0: "Weapons", 1: "Materials", 2: "", 3: "Exotic Weapons"},
			},
			vendorTess: {
				Name:       "Tess Everis",
				Subtitle:   "Eververse Trading Company",
				Categories: map[int]string{0: "Featured"},
			},
			vendorXur:       {Name: "Xûr", Subtitle: "Agent of the Nine"},
			vendorXurOffers: {Name: "Xûr", Subtitle: "Strange Gear Offers"},
			vendorNameless:  {},
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

// all returns every recorded request.
func (r *recorder) all() []request {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.requests)
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

// configBody returns the /api/config response body whose artifact index
// lists files.
func configBody(t *testing.T, files map[string][]string) string {
	t.Helper()

	raw, err := json.Marshal(files)
	require.NoError(t, err)

	return `{"apiKey": "key", "clientId": "54520", "artifacts": {"manifestVersion": "v1", "files": ` +
		string(raw) + `}}`
}

// fakes is a handler wired to a fake Agg's Destiny Vault Manager service
// and a fake Bungie platform.
type fakes struct {
	h       *handler
	service *recorder
	bungie  *recorder

	// serviceData holds the GET response body of each service path.
	serviceData map[string]string
	// artifactFiles holds the file list of each artifact that the index
	// of the service lists. The service serves a listed file only when
	// serviceData holds a body for its path under /artifacts/.
	artifactFiles map[string][]string
	// bungieData holds the GET response body of each Bungie path.
	bungieData map[string]string
	// bungieErrorIf limits bungieError to the actions whose request body
	// it selects, such as the transfers of one item. A nil bungieErrorIf
	// selects every action.
	bungieErrorIf func(body map[string]any) bool
	// activitiesBody is the response body of the profile request that
	// asks only for the activities. When it is empty, that request gets
	// the body of the snapshot request: profileBody when it is set, and
	// fixtureProfile otherwise.
	activitiesBody string
	// profileBody is the response body of every other profile request.
	// When it is empty, those requests get fixtureProfile.
	profileBody string
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
		service:       &recorder{},
		bungie:        &recorder{},
		artifactFiles: map[string][]string{},
		bungieData: map[string]string{
			pathHunterHistory: fixtureHunterHistory,
			pathTitanHistory:  fixtureTitanHistory,
			pathHunterVendors: fixtureHunterVendors,
			pathTitanVendors:  fixtureTitanVendors,
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
			writeJSON(t, w, http.StatusOK, configBody(t, f.artifactFiles))

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

		case req.Path == "/Destiny2/3/Profile/4611/" && f.activitiesBody != "" &&
			strings.HasPrefix(req.Query, "components=204&"):
			writeJSON(t, w, http.StatusOK, f.activitiesBody)
		case req.Path == "/Destiny2/3/Profile/4611/" && f.profileBody != "":
			writeJSON(t, w, http.StatusOK, f.profileBody)
		case req.Path == "/Destiny2/3/Profile/4611/":
			writeJSON(t, w, http.StatusOK, `{"ErrorCode": 1, "Response": `+fixtureProfile+`}`)
		case req.Method == http.MethodGet && f.bungieData[req.Path] != "":
			writeJSON(t, w, http.StatusOK, f.bungieData[req.Path])
		case req.Method == http.MethodPost && f.bungieError != "" &&
			(f.bungieErrorPath == "" || req.Path == f.bungieErrorPath) &&
			(f.bungieErrorIf == nil || f.bungieErrorIf(req.Body)):
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
		sheets:  NewSheetStore(client),
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
