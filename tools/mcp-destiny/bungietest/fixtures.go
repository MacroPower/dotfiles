package bungietest

import "time"

// Item hashes of the fixture currencies.
const (
	ItemGlimmer    uint32 = 3159615086
	ItemBrightDust uint32 = 2817410917
)

// Character ids of the fixture profile.
const (
	HunterID = "c1"
	TitanID  = "c2"
)

// PathProfile is the path of the profile of the fixture account.
const PathProfile = "/Destiny2/3/Profile/4611/"

// ProfileJSON is a Bungie profile response body. The Hunter has the
// latest play date. It has a hand cannon and a helmet equipped, a second
// hand cannon in its inventory, and a third at the Postmaster. The vault
// holds a hand cannon with sockets and stats and a rocket launcher. The
// helmet has a stat total of 55. The primary stat of each weapon is Attack,
// and the primary stat of the helmet is Defense, so each of them has a
// power. Every instanced item is lockable, and the vault hand cannon is
// locked. The account holds a stack of consumables, glimmer, bright dust,
// and a currency that the definitions lack. The consumables stack carries
// the vault location, because Bungie gives that location to every profile
// item.
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
// The Hunter can also play activities that the list_activities tool of the
// mcp-destiny server leaves out. The game hides Pantheon, and 4005 has no
// definition. The definition of 3334 has no name. The Witness has the Story
// mode, Excision: Grandmaster has a Story type, and The Tower has the
// Social mode. The Titan can play Control.
const ProfileJSON = `{
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
			"1001": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2010}},
			"1002": {"damageType": 3, "primaryStat": {"statHash": 1480404414, "value": 1990}},
			"1003": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 2005}},
			"1004": {"damageType": 0, "primaryStat": {"statHash": 3897883278, "value": 2000}, "isEquipped": true},
			"1005": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 1970}},
			"1006": {"damageType": 1, "primaryStat": {"statHash": 1480404414, "value": 1980}, "isEquipped": true}
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

// Paths of the fixture manifest and of its English loadout tables.
const (
	PathManifest      = "/Destiny2/Manifest/"
	PathLoadoutNames  = "/common/destiny2_content/json/en/DestinyLoadoutNameDefinition-a1.json"
	PathLoadoutIcons  = "/common/destiny2_content/json/en/DestinyLoadoutIconDefinition-a1.json"
	PathLoadoutColors = "/common/destiny2_content/json/en/DestinyLoadoutColorDefinition-a1.json"
)

// ManifestJSON is the Bungie GetDestinyManifest response body. Its
// English tables include the loadout name table at [PathLoadoutNames], the
// loadout icon table at [PathLoadoutIcons], and the loadout color table at
// [PathLoadoutColors]. The French tables name other files.
const ManifestJSON = `{"ErrorCode": 1, "Response": {"version": "v1", "jsonWorldComponentContentPaths": {
	"en": {
		"DestinyInventoryItemDefinition": "/common/destiny2_content/json/en/DestinyInventoryItemDefinition-a1.json",
		"DestinyLoadoutNameDefinition": "` + PathLoadoutNames + `",
		"DestinyLoadoutIconDefinition": "` + PathLoadoutIcons + `",
		"DestinyLoadoutColorDefinition": "` + PathLoadoutColors + `"
	},
	"fr": {
		"DestinyLoadoutNameDefinition": "/common/destiny2_content/json/fr/DestinyLoadoutNameDefinition-a1.json"
	}
}}}`

// LoadoutNamesJSON is the content file of the loadout name table. It
// names the saved loadouts of [ProfileJSON], and it holds a redacted
// entry without a name.
const LoadoutNamesJSON = `{
	"1": {"hash": 1, "index": 0, "name": "Raid Night", "redacted": false},
	"4": {"hash": 4, "index": 1, "name": "Trials", "redacted": false},
	"7": {"hash": 7, "index": 2, "name": "Crucible", "redacted": false},
	"10": {"hash": 10, "index": 3, "name": "", "redacted": true}
}`

// LoadoutIconsJSON is the content file of the loadout icon table. Its
// first entry is 20, because the entry with the lowest index is redacted.
const LoadoutIconsJSON = `{
	"21": {"hash": 21, "index": 0, "iconImagePath": "/img/21.png", "redacted": true},
	"20": {"hash": 20, "index": 1, "iconImagePath": "/img/20.png", "redacted": false},
	"22": {"hash": 22, "index": 2, "iconImagePath": "/img/22.png", "redacted": false}
}`

// LoadoutColorsJSON is the content file of the loadout color table.
// Its first entry is 31, which has the lowest index but not the lowest
// hash.
const LoadoutColorsJSON = `{
	"30": {"hash": 30, "index": 2, "colorImagePath": "/img/30.png", "redacted": false},
	"31": {"hash": 31, "index": 0, "colorImagePath": "/img/31.png", "redacted": false},
	"32": {"hash": 32, "index": 1, "colorImagePath": "/img/32.png", "redacted": false}
}`

// Activity history paths of the fixture characters.
const (
	PathHunterHistory = "/Destiny2/3/Account/4611/Character/c1/Stats/Activities/"
	PathTitanHistory  = "/Destiny2/3/Account/4611/Character/c2/Stats/Activities/"
)

// HunterHistoryJSON is the Bungie response body of page 0 of the
// activity history of the Hunter. The newest run is a raid that the
// definitions name by its reference id. The second run comes from a
// playlist, so only its director activity hash has a definition. Neither
// hash of the third run has a definition with a name, and the run lacks
// the kills and the deaths.
const HunterHistoryJSON = `{"ErrorCode": 1, "Response": {"activities": [
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

// TitanHistoryJSON is the Bungie response body of page 0 of the
// activity history of the Titan. Both hashes of the newest run have a
// definition, and the two definitions differ.
const TitanHistoryJSON = `{"ErrorCode": 1, "Response": {"activities": [
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
	VendorBanshee   uint32 = 672118013
	VendorTess      uint32 = 3361454721
	VendorXur       uint32 = 2190858386
	VendorXurOffers uint32 = 3751514131
	VendorNameless  uint32 = 1111
)

// Vendor paths of the fixture characters.
const (
	PathHunterVendors = "/Destiny2/3/Profile/4611/Character/c1/Vendors/"
	PathTitanVendors  = "/Destiny2/3/Profile/4611/Character/c2/Vendors/"
)

// HunterVendorsJSON is the Bungie GetVendors response body of the
// Hunter. Banshee-44 lists its categories out of display order, and its
// third category has no name. Its fourth category lists only a weapon that
// Banshee-44 displays without selling it, and the status of that sale also
// carries the NoUnlock failure bit. Banshee-44 sells a weapon that the
// Hunter cannot buy yet, three items that no category lists, and an item
// that no definition names. The failure indexes of the weapon that the
// Hunter cannot buy include a negative index and an index past the last
// failure string of Banshee-44. The sales list the sub-vendor of
// Eververse, and the vendors do not. Xûr is disabled, and the stock of the
// other Xûr never rotates. The vendor groups list Banshee-44, Tess Everis,
// and the disabled Xûr, which has no sale. They leave out the other
// vendors, such as the sub-vendor of Eververse, which has a sale.
const HunterVendorsJSON = `{"ErrorCode": 1, "Response": {
	"vendors": {"data": {
		"672118013": {"vendorHash": 672118013, "enabled": true, "nextRefreshDate": "2026-11-13T17:00:00Z"},
		"3361454721": {"vendorHash": 3361454721, "enabled": true, "nextRefreshDate": "2026-11-17T17:00:00Z"},
		"2190858386": {"vendorHash": 2190858386, "enabled": false, "nextRefreshDate": "2026-11-14T17:00:00Z"},
		"3751514131": {"vendorHash": 3751514131, "enabled": true, "nextRefreshDate": "9999-12-31T23:59:59.999Z"},
		"1111": {"vendorHash": 1111, "enabled": true, "nextRefreshDate": "2026-11-13T17:00:00Z"}
	}},
	"vendorGroups": {"data": {"groups": [
		{"vendorGroupHash": 3, "vendorHashes": [2190858386, 672118013]},
		{"vendorGroupHash": 4, "vendorHashes": [3361454721]}
	]}},
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

// TitanVendorsJSON is the Bungie GetVendors response body of the Titan.
// Banshee-44 sells the Titan a weapon that the Hunter cannot buy yet. The
// vendor groups list Banshee-44.
const TitanVendorsJSON = `{"ErrorCode": 1, "Response": {
	"vendors": {"data": {
		"672118013": {"vendorHash": 672118013, "enabled": true, "nextRefreshDate": "2026-11-13T17:00:00Z"}
	}},
	"vendorGroups": {"data": {"groups": [
		{"vendorGroupHash": 3, "vendorHashes": [672118013]}
	]}},
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

// Objective hashes of kill trackers in the Bungie manifest, from the
// kill tracker table of Destiny Item Manager (DIM).
const (
	TrackerPvE       uint32 = 90275515
	TrackerPvP       uint32 = 1501870536
	TrackerGambit    uint32 = 345540971
	TrackerNightfall uint32 = 3387796140
)

// Now is the current time of the fixture account. It falls after the last
// play date and the newest run of each character, and before the next
// refresh of each vendor.
var Now = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
