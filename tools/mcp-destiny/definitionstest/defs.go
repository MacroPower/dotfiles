package definitionstest

import (
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
)

// ItemWithheld is the hash of a stack whose definition the service
// withholds and whose name no table holds.
const ItemWithheld uint32 = 78

// TypeRaid and TypeDungeon are the hashes of the Raid and the Dungeon
// activity types, as the Destiny definitions give them. A raid or a
// dungeon with a challenge counts as featured only under these hashes.
const (
	TypeRaid    uint32 = 2043403989
	TypeDungeon uint32 = 608898761
)

// Bucket hashes of the fixture slots that no production code reads.
const (
	bucketQuests    uint32 = 1345459588
	bucketMaterials uint32 = 3865314626
)

// Defs returns the definitions of the Bungie fixture account. It builds a
// new value on each call, so a test can edit the result without changing
// the definitions of another test.
func Defs() *definitions.Defs {
	weapon := func(name, typeName, tier string, bucket uint32, damage int) definitions.ItemDef {
		return definitions.ItemDef{
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

	plug := func(name, typeName string) definitions.ItemDef {
		return definitions.ItemDef{
			Name:      name,
			TypeName:  typeName,
			ItemType:  bungie.ItemTypePlug,
			ClassType: bungie.ClassAny,
		}
	}

	firefly := plug("Firefly", "Trait")
	firefly.Description = "Precision final blows cause targets to explode.\n\n Explosions deal Solar damage. "

	return &definitions.Defs{
		Version: "v1",
		Items: map[uint32]definitions.ItemDef{
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
		Buckets: map[uint32]definitions.BucketDef{
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
		Activities: map[uint32]definitions.ActivityDef{
			3022541210: {Name: "Vault of Glass: Master", Mode: 4, Type: TypeRaid, Destination: 700, Place: 800},
			2222:       {Name: "Vanguard Ops", Mode: 18, Type: 103},
			3334:       {},
			4444:       {Name: "Control", Mode: 10, Type: 101, Destination: 702, Place: 800},
			5555:       {Name: "Crucible"},
			4001:       {Name: "Warlord's Ruin", Mode: 82, Type: TypeDungeon, Destination: 701, Place: 800},
			4002:       {Name: "The Coil", Mode: 6, Type: 104, Destination: 703},
			4003:       {Name: "Nightfall: Grandmaster", Mode: 46, Type: 103},
			4004:       {Name: "Pantheon", Mode: 4, Type: TypeRaid},
			4006:       {Name: "The Witness", Mode: 2, Type: 104},
			4007:       {Name: "Excision: Grandmaster", Type: 1686739444},
			4008:       {Name: "The Tower", Mode: 40},
			4009:       {Name: "Battleground: Oracle", Mode: 18, Type: 103},
		},
		ActivityTypes: map[uint32]string{
			TypeRaid:    "Raid",
			101:         "The Crucible",
			TypeDungeon: "Dungeon",
			103:         "Strike",
			104:         "Exotic Mission",
			1686739444:  "Story",
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
		Vendors: map[uint32]definitions.VendorDef{
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
			bungietest.VendorXur:       {Name: "X\u00fbr", Subtitle: "Agent of the Nine"},
			bungietest.VendorXurOffers: {Name: "X\u00fbr", Subtitle: "Strange Gear Offers"},
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
		// The service withholds the dummy items that the vendors sell and
		// the dummy rewards of the activities.
		Withheld:       []uint32{ItemWithheld, 500, 501, 502, 503, 600, 601, 603, 604, 605, 606, 999},
		WithheldListed: true,
	}
}
