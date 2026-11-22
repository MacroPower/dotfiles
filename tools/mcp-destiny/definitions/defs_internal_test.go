package definitions

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvmtest"
)

// newFakeArtifacts returns a [*dvmtest.Artifacts] that serves the
// definition artifacts of the loader tests and counts the fetches. Index
// lists the files of each artifact.
func newFakeArtifacts() *dvmtest.Artifacts {
	return &dvmtest.Artifacts{
		Version: "245114.26/1",
		Index: map[string][]string{
			artifactCore:            {"core-0.json", "core-1.json"},
			artifactDetail:          {"detail-0.json", "detail-1.json"},
			artifactPlugSets:        {"plugsets-0.json", "plugsets-1.json"},
			artifactBuckets:         {"buckets.json"},
			artifactDamage:          {"damage.json"},
			artifactClasses:         {"classes.json"},
			artifactStats:           {"stats.json"},
			artifactActivities:      {"activities.json"},
			artifactActivityTypes:   {"activity-types.json"},
			artifactDestinations:    {"destinations.json"},
			artifactPlaces:          {"places.json"},
			artifactActivityRewards: {"activity-rewards-0.json", "activity-rewards-1.json"},
			artifactVendors:         {"vendors.json"},
			artifactVendorItems:     {"vendor-items-0.json", "vendor-items-1.json"},
			artifactItemSets:        {"item-sets.json"},
			artifactRecords:         {"records.json"},
			artifactWithheld:        {"withheld-0.json", "withheld-1.json"},
		},
		Files: map[string]string{
			"core-0.json": `[
				{"hash": 100, "displayProperties": {"name": "Fatebringer", "description": "Fortune favors.", "icon": "/a.png"},
				 "itemTypeDisplayName": "Hand Cannon", "itemType": 3, "classType": 3, "defaultDamageType": 1,
				 "inventory": {"tierTypeName": "Legendary", "tierType": 5, "bucketTypeHash": 1498876634,
				  "recipeItemHash": 2563668388},
				 "equippingBlock": {"ammoType": 1}, "stats": {"stats": {}}},
				{"hash": 900, "displayProperties": {"name": "Firefly", "description": "Precision final blows explode."},
				 "itemTypeDisplayName": "Trait",
				 "itemType": 19, "classType": 3, "inventory": {"tierTypeName": "Common"}}
			]`,
			"core-1.json": `[
				{"hash": 400, "displayProperties": {"name": "Hawkmoon"}, "itemTypeDisplayName": "Hand Cannon",
				 "itemType": 3, "classType": 3,
				 "inventory": {"tierTypeName": "Exotic", "tierType": 6, "bucketTypeHash": 1498876634,
				  "nonTransferrableOriginal": true}},
				{"hash": 401, "displayProperties": {"name": "Midnight Coup"}, "itemTypeDisplayName": "Hand Cannon",
				 "itemType": 3, "classType": 3, "itemCategoryHashes": []},
				{"hash": 402, "displayProperties": {"name": "Austringer"}, "itemTypeDisplayName": "Hand Cannon",
				 "itemType": 3, "classType": 3},
				{"hash": 403, "displayProperties": {"name": "Sleeper Simulant"},
				 "itemTypeDisplayName": "Linear Fusion Rifle", "itemType": 3, "classType": 3,
				 "itemCategoryHashes": [1, 4, 9, 1504945536],
				 "inventory": {"tierTypeName": "Exotic", "tierType": 6, "bucketTypeHash": 953998645},
				 "equippingBlock": {"ammoType": 3, "equipmentSlotTypeHash": 953998645}},
				{"hash": 404, "displayProperties": {"name": "Lightkeeper Shell"}, "itemTypeDisplayName": "Ghost Shell",
				 "itemType": 24, "classType": 3,
				 "inventory": {"tierTypeName": "Uncommon", "tierType": 3, "bucketTypeHash": 4023194814},
				 "stats": {"disablePrimaryStatDisplay": true, "primaryBaseStatHash": 0}},
				{"hash": 405, "displayProperties": {"name": "Iron Truage Helm"}, "itemTypeDisplayName": "Helmet",
				 "itemType": 2, "classType": 1,
				 "inventory": {"tierTypeName": "Legendary", "tierType": 5, "bucketTypeHash": 3448274439},
				 "equippingBlock": {"ammoType": 0, "equipableItemSetHash": 3727270518}}
			]`,
			// The service build splits the detail array across files.
			// Fatebringer has a socket with only a randomized plug set,
			// a socket with both plug sets, and a socket with neither.
			// The loader keeps no socket list for the other records of
			// the first file. Firefly is a plug, and the core artifact
			// lacks item 777. The record of Hawkmoon has no sockets, and
			// Midnight Coup has no socket entry. The second file holds
			// the plugs of the two crafting sockets that Deepsight reads
			// and a plug of another category.
			"detail-0.json": `[
				{"hash": 100, "sockets": {"socketEntries": [
					{"socketTypeHash": 1, "randomizedPlugSetHash": 40},
					{"socketTypeHash": 2, "reusablePlugSetHash": 41, "randomizedPlugSetHash": 42},
					{"socketTypeHash": 3}
				]}},
				{"hash": 900, "plug": {"plugCategoryIdentifier": "frames"},
				 "sockets": {"socketEntries": [{"reusablePlugSetHash": 43}]}},
				{"hash": 777, "sockets": {"socketEntries": [{"randomizedPlugSetHash": 44}]}},
				{"hash": 400, "perks": []},
				{"hash": 401, "sockets": {"socketEntries": []}}
			]`,
			"detail-1.json": `[
				{"hash": 402, "sockets": {"socketEntries": [{"socketTypeHash": 1, "reusablePlugSetHash": 45}]}},
				{"hash": 910, "plug": {"plugCategoryIdentifier": "crafting.plugs.weapons.mods.memories",
				 "plugCategoryHash": 2748073883}},
				{"hash": 911, "plug": {"plugCategoryIdentifier": "crafting.plugs.weapons.mods.extractors",
				 "plugCategoryHash": 3520412733}},
				{"hash": 912, "plug": {"plugCategoryIdentifier": "frames", "plugCategoryHash": 7906839}}
			]`,
			// The service build splits the plugsets array across files.
			// Plug set 40 lists Firefly twice and a plug that the core
			// artifact lacks. Plug set 41 lists no plug. No socket of a
			// kept weapon names the plug sets 42, 43, and 44. A socket
			// without a plug set has the hash 0, so no socket names the
			// plug set 0.
			"plugsets-0.json": `[
				{"hash": 40, "reusablePlugItems": [
					{"plugItemHash": 900, "currentlyCanRoll": true}, {"plugItemHash": 12345}, {"plugItemHash": 900}
				]},
				{"hash": 41, "reusablePlugItems": []},
				{"hash": 42, "reusablePlugItems": [{"plugItemHash": 900}]},
				{"hash": 43, "reusablePlugItems": [{"plugItemHash": 900}]},
				{"hash": 44, "reusablePlugItems": [{"plugItemHash": 900}]},
				{"hash": 0, "reusablePlugItems": [{"plugItemHash": 900}]}
			]`,
			"plugsets-1.json": `[
				{"hash": 45, "reusablePlugItems": [{"plugItemHash": 900}]}
			]`,
			"buckets.json": `{
				"1498876634": {"displayProperties": {"name": "Kinetic Weapons"}, "scope": 0, "category": 3,
				 "bucketOrder": 20, "itemCount": 10, "location": 1},
				"138197802": {"displayProperties": {"name": "General"}, "scope": 1, "category": 1,
				 "bucketOrder": 30, "itemCount": 700, "location": 2}
			}`,
			"damage.json": `{
				"3373582085": {"displayProperties": {"name": "Kinetic"}, "enumValue": 1},
				"2303181850": {"displayProperties": {"name": "Arc"}, "enumValue": 2}
			}`,
			"classes.json": `{"671679327": {"displayProperties": {"name": "Hunter"}, "classType": 1}}`,
			"stats.json":   `{"1240592695": {"displayProperties": {"name": "Range"}}}`,
			// The service build projects each activity to a SlimActivity
			// record. A field that the definition lacks stays out of the
			// record.
			"activities.json": `{
				"3022541210": {"hash": 3022541210, "name": "Vault of Glass: Master", "description": "Raid.",
				 "activityTypeHash": 2043403989, "destinationHash": 1993421442, "placeHash": 3747705955,
				 "modeType": 4, "modeTypes": [4], "isPlaylist": false, "leaderRequirements": [""]},
				"2693136600": {"hash": 2693136600, "name": "Deep Stone Crypt", "modeTypes": [4]}
			}`,
			// The service build projects activity types, destinations, and
			// places to records with a top-level name.
			"activity-types.json": `{
				"2043403989": {"hash": 2043403989, "name": "Raid", "icon": "/r.png"},
				"1686739444": {"hash": 1686739444, "name": ""}
			}`,
			"destinations.json": `{"1993421442": {"hash": 1993421442, "name": "Venus"}}`,
			"places.json":       `{"3747705955": {"hash": 3747705955, "name": "Venus"}, "1": {"hash": 1, "name": ""}}`,
			// The service build splits the ActivityReward array across files.
			"activity-rewards-0.json": `[
				{"hash": 600, "name": "Raid Gear", "icon": "/g.png", "gearTier": {"low": 4, "high": 5}},
				{"hash": 602, "name": ""}
			]`,
			"activity-rewards-1.json": `[
				{"hash": 601, "name": "Deepsight Weapon"}
			]`,
			// The service build projects each vendor to a SlimVendor record.
			"vendors.json": `{
				"672118013": {"hash": 672118013,
				 "displayProperties": {"name": "Banshee-44", "subtitle": "Gunsmith", "icon": "/b.png"},
				 "failureStrings": ["", "Requires Gunsmith rank 10."], "acceptedItems": [],
				 "displayCategories": [
					{"index": 0, "identifier": "category_weapons", "name": "Weapons"},
					{"index": 2, "identifier": "category_materials", "name": ""}
				 ],
				 "itemSorts": [0, 1]},
				"2168194999": {"hash": 2168194999, "displayProperties": {"name": "", "subtitle": ""},
				 "failureStrings": [], "acceptedItems": [], "displayCategories": [], "itemSorts": []}
			}`,
			// The service build splits the VendorItem array across files.
			"vendor-items-0.json": `[
				{"hash": 500, "name": "Ammo Synthesis", "icon": "/c.png", "gearTier": {"low": 1, "high": 2}},
				{"hash": 3159615086, "name": "Glimmer"}
			]`,
			"vendor-items-1.json": `[
				{"hash": 501, "name": "Legendary Shards", "standIn": 400},
				{"hash": 502, "name": ""}
			]`,
			// The service build keeps each set definition whole. The set
			// of Iron Truage Helm has a 2-piece and a 4-piece bonus, and
			// the other set has no bonus.
			"item-sets.json": `{
				"3727270518": {"hash": 3727270518, "displayProperties": {"name": "Iron Truage"},
				 "setItems": [405], "setPerks": [
					{"requiredSetCount": 2, "sandboxPerkHash": 1360604600},
					{"requiredSetCount": 4, "sandboxPerkHash": 1360604601}
				 ], "index": 0, "redacted": false},
				"12": {"hash": 12, "setItems": [], "setPerks": [], "index": 1, "redacted": false}
			}`,
			// The service build keeps each record definition whole. Three
			// pattern records share the name Fatebringer, and the loader
			// keeps the one with the highest hash. The other records
			// track no pattern, or they have no name.
			"records.json": `{
				"4999": {"hash": 4999, "displayProperties": {"name": "Fatebringer"},
				 "completionInfo": {"toastStyle": 8, "ScoreValue": 0}},
				"5001": {"hash": 5001, "displayProperties": {"name": "Fatebringer"}, "completionInfo": {"toastStyle": 8}},
				"5000": {"hash": 5000, "displayProperties": {"name": "Fatebringer"}, "completionInfo": {"toastStyle": 8}},
				"5002": {"hash": 5002, "displayProperties": {"name": "Hawkmoon"}, "completionInfo": {"toastStyle": 8}},
				"5003": {"hash": 5003, "displayProperties": {"name": "Raid Triumph"}, "completionInfo": {"toastStyle": 1}},
				"5004": {"hash": 5004, "displayProperties": {"name": "Lore"}},
				"5005": {"hash": 5005, "displayProperties": {"name": ""}, "completionInfo": {"toastStyle": 8}}
			}`,
			// The service build splits the array of withheld hashes across
			// files. The loader sorts the hashes and keeps each one once.
			"withheld-0.json": `[3708253562, 502, 373750294]`,
			"withheld-1.json": `[502, 600]`,
		},
	}
}

// fixtureSockets returns the socket lists that the loader builds from
// the detail files of [newFakeArtifacts].
func fixtureSockets() map[uint32][]uint32 {
	return map[uint32][]uint32{100: {40, 41, 0}, 402: {45}}
}

func TestLoader(t *testing.T) {
	t.Parallel()

	source := newFakeArtifacts()
	dir := t.TempDir()
	now := bungietest.Now
	clock := WithClock(func() time.Time { return now })

	// A cache file of an older layout stays behind after an upgrade. The
	// layout without the pattern records has the prefix defs-v11-. Its
	// file holds the manifest version of the service, and the loader does
	// not read it.
	stale := filepath.Join(dir, "defs-v11-245114.26_1.json")
	require.NoError(t, os.WriteFile(stale, []byte(`{"version": "245114.26/1"}`), 0o644))

	loader := NewLoader(source, dir, clock)

	got, err := loader.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, &Defs{
		Version: "245114.26/1",
		Items: map[uint32]ItemDef{
			100: {
				Name:       "Fatebringer",
				TypeName:   "Hand Cannon",
				Tier:       "Legendary",
				TierType:   5,
				Bucket:     bungie.BucketKinetic,
				ItemType:   bungie.ItemTypeWeapon,
				ClassType:  bungie.ClassAny,
				DamageType: 1,
				AmmoType:   bungie.AmmoPrimary,
				Equippable: true,
				Craftable:  true,
			},
			400: {
				Name:            "Hawkmoon",
				TypeName:        "Hand Cannon",
				Tier:            "Exotic",
				TierType:        6,
				Bucket:          bungie.BucketKinetic,
				ItemType:        bungie.ItemTypeWeapon,
				ClassType:       bungie.ClassAny,
				NonTransferable: true,
			},
			// A record without a tier type gets the tier type that DIM
			// names Common.
			401: {
				Name:      "Midnight Coup",
				TypeName:  "Hand Cannon",
				TierType:  bungie.TierTypeBasic,
				ItemType:  bungie.ItemTypeWeapon,
				ClassType: bungie.ClassAny,
			},
			402: {
				Name:      "Austringer",
				TypeName:  "Hand Cannon",
				TierType:  bungie.TierTypeBasic,
				ItemType:  bungie.ItemTypeWeapon,
				ClassType: bungie.ClassAny,
			},
			// The loader keeps the categories of the definition. The
			// snapshot drops the fusion rifle category of a linear fusion
			// rifle.
			403: {
				Name:       "Sleeper Simulant",
				TypeName:   "Linear Fusion Rifle",
				Tier:       "Exotic",
				Bucket:     bungie.BucketPower,
				ItemType:   bungie.ItemTypeWeapon,
				ClassType:  bungie.ClassAny,
				Categories: []uint32{1, 4, 9, 1504945536},
				TierType:   6,
				AmmoType:   bungie.AmmoHeavy,
				Equippable: true,
			},
			404: {
				Name:                      "Lightkeeper Shell",
				TypeName:                  "Ghost Shell",
				Tier:                      "Uncommon",
				TierType:                  3,
				Bucket:                    bungie.BucketGhost,
				ItemType:                  24,
				ClassType:                 bungie.ClassAny,
				DisablePrimaryStatDisplay: true,
			},
			// The loader keeps the item set of the equipping block.
			405: {
				Name:       "Iron Truage Helm",
				TypeName:   "Helmet",
				Tier:       "Legendary",
				TierType:   5,
				Bucket:     bungie.BucketHelmet,
				ItemType:   bungie.ItemTypeArmor,
				ClassType:  bungie.ClassHunter,
				Equippable: true,
				SetHash:    3727270518,
			},
			900: {
				Name:        "Firefly",
				Description: "Precision final blows explode.",
				TypeName:    "Trait",
				Tier:        "Common",
				TierType:    bungie.TierTypeBasic,
				ItemType:    bungie.ItemTypePlug,
				ClassType:   bungie.ClassAny,
			},
		},
		Buckets: map[uint32]BucketDef{
			bungie.BucketKinetic: {
				Name:     "Kinetic Weapons",
				Capacity: 10,
				Category: bungie.BucketCategoryEquippable,
				Order:    20,
			},
			bungie.BucketVault: {
				Name:     "General",
				Capacity: 700,
				Category: bungie.BucketCategoryItem,
				Order:    30,
				Account:  true,
			},
		},
		Stats:    map[uint32]string{1240592695: "Range"},
		Sockets:  fixtureSockets(),
		PlugSets: map[uint32][]uint32{40: {900, 12345}, 41: {}, 45: {900}},
		PlugCategories: map[uint32]uint32{
			910: bungie.PlugCategoryResonance,
			911: bungie.PlugCategoryHarmonizer,
		},
		ItemSets: map[uint32][]uint32{3727270518: {1360604600, 1360604601}, 12: {}},
		Patterns: map[string]uint32{"Fatebringer": 5001, "Hawkmoon": 5002},
		Activities: map[uint32]ActivityDef{
			3022541210: {
				Name:        "Vault of Glass: Master",
				Mode:        4,
				Type:        2043403989,
				Destination: 1993421442,
				Place:       3747705955,
			},
			2693136600: {Name: "Deep Stone Crypt"},
		},
		ActivityTypes:   map[uint32]string{2043403989: "Raid", 1686739444: ""},
		Destinations:    map[uint32]string{1993421442: "Venus"},
		Places:          map[uint32]string{3747705955: "Venus", 1: ""},
		ActivityRewards: map[uint32]string{600: "Raid Gear", 601: "Deepsight Weapon"},
		Vendors: map[uint32]VendorDef{
			672118013: {
				Name:           "Banshee-44",
				Subtitle:       "Gunsmith",
				FailureStrings: []string{"", "Requires Gunsmith rank 10."},
				Categories:     map[int]string{0: "Weapons", 2: ""},
			},
			2168194999: {},
		},
		VendorItems: map[uint32]string{
			500:                    "Ammo Synthesis",
			bungietest.ItemGlimmer: "Glimmer",
			501:                    "Legendary Shards",
		},
		DamageEnums:    map[uint32]int{3373582085: 1, 2303181850: 2},
		DamageTypes:    map[int]string{1: "Kinetic", 2: "Arc"},
		Classes:        map[int]string{1: "Hunter"},
		Withheld:       []uint32{502, 600, 373750294, 3708253562},
		ItemSetsListed: true,
		WithheldListed: true,
		PatternsListed: true,
	}, got)
	assert.Len(t, source.Fetched, 23)

	// Writing the cache removes the file of the older layout.
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, defsCachePrefix+"245114.26_1.json")}, files)

	// A second call inside the recheck window asks the service nothing.
	source.Fetched = nil

	again, err := loader.Get(t.Context())
	require.NoError(t, err)
	assert.Same(t, got, again)
	assert.Empty(t, source.Fetched)

	// Another process reads the cache file instead of the artifacts.
	cached, err := NewLoader(source, dir, clock).Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, got, cached)
	assert.Empty(t, source.Fetched)

	// A manifest version change rebuilds and replaces the cache file.
	source.Version = "245200.1"
	now = now.Add(2 * defsRecheck)

	rebuilt, err := loader.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "245200.1", rebuilt.Version)
	assert.Len(t, source.Fetched, 23)

	files, err = filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, defsCachePrefix+"245200.1.json")}, files)
}

func TestLoaderWithoutOptionalArtifacts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		field func(*Defs) any
		// check asserts what else the build without the artifact gives.
		// Fetched lists the files that the build fetched.
		check    func(t *testing.T, got *Defs, fetched []string)
		artifact string
		files    []string
	}{
		"activities": {
			artifact: artifactActivities,
			files:    []string{"activities.json"},
			field:    func(d *Defs) any { return d.Activities },
		},
		"activity types": {
			artifact: artifactActivityTypes,
			files:    []string{"activity-types.json"},
			field:    func(d *Defs) any { return d.ActivityTypes },
		},
		"destinations": {
			artifact: artifactDestinations,
			files:    []string{"destinations.json"},
			field:    func(d *Defs) any { return d.Destinations },
		},
		"places": {
			artifact: artifactPlaces,
			files:    []string{"places.json"},
			field:    func(d *Defs) any { return d.Places },
		},
		"activity rewards": {
			artifact: artifactActivityRewards,
			files:    []string{"activity-rewards-0.json", "activity-rewards-1.json"},
			field:    func(d *Defs) any { return d.ActivityRewards },
		},
		"vendors": {
			artifact: artifactVendors,
			files:    []string{"vendors.json"},
			field:    func(d *Defs) any { return d.Vendors },
		},
		"vendor items": {
			artifact: artifactVendorItems,
			files:    []string{"vendor-items-0.json", "vendor-items-1.json"},
			field:    func(d *Defs) any { return d.VendorItems },
		},
		// Without a socket list, no socket names a plug set. The loader
		// still reads the plug set files.
		"detail": {
			artifact: artifactDetail,
			files:    []string{"detail-0.json", "detail-1.json"},
			field:    func(d *Defs) any { return d.Sockets },
			check: func(t *testing.T, got *Defs, fetched []string) {
				t.Helper()

				assert.Empty(t, got.PlugSets)
				assert.Subset(t, fetched, []string{"plugsets-0.json", "plugsets-1.json"})
			},
		},
		// The build still records whether the index lists the item sets.
		"item sets": {
			artifact: artifactItemSets,
			files:    []string{"item-sets.json"},
			field:    func(d *Defs) any { return d.ItemSets },
			check: func(t *testing.T, got *Defs, _ []string) {
				t.Helper()

				assert.False(t, got.ItemSetsListed)
				assert.Equal(t, uint32(3727270518), got.Items[405].SetHash)
			},
		},
		// The build records that the index lists no withheld hashes.
		"withheld hashes": {
			artifact: artifactWithheld,
			files:    []string{"withheld-0.json", "withheld-1.json"},
			field:    func(d *Defs) any { return d.Withheld },
			check: func(t *testing.T, got *Defs, _ []string) {
				t.Helper()

				assert.False(t, got.WithheldListed)
				assert.False(t, got.Withholds(502))
			},
		},
		// The build records that the index lists no records.
		"records": {
			artifact: artifactRecords,
			files:    []string{"records.json"},
			field:    func(d *Defs) any { return d.Patterns },
			check: func(t *testing.T, got *Defs, _ []string) {
				t.Helper()

				assert.False(t, got.PatternsListed)
				assert.True(t, got.Items[100].Craftable)
			},
		},
		"plug sets": {
			artifact: artifactPlugSets,
			files:    []string{"plugsets-0.json", "plugsets-1.json"},
			field:    func(d *Defs) any { return d.PlugSets },
			check: func(t *testing.T, got *Defs, _ []string) {
				t.Helper()

				assert.Equal(t, fixtureSockets(), got.Sockets)
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := newFakeArtifacts()
			delete(source.Index, tc.artifact)

			got, err := NewLoader(source, "").Get(t.Context())
			require.NoError(t, err)
			assert.Empty(t, tc.field(got))
			assert.Len(t, got.Items, 8)

			for _, file := range tc.files {
				assert.NotContains(t, source.Fetched, file)
			}

			if tc.check != nil {
				tc.check(t, got, source.Fetched)
			}
		})
	}
}

func TestLoaderErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		break_ func(*dvmtest.Artifacts)
		err    error
	}{
		"artifact is not an array": {
			break_: func(f *dvmtest.Artifacts) { f.Files["core-0.json"] = `{"hash": 1}` },
			err:    ErrDefs,
		},
		"table key is not a hash": {
			break_: func(f *dvmtest.Artifacts) { f.Files["stats.json"] = `{"range": {}}` },
			err:    ErrDefs,
		},
		"activity table is not an object": {
			break_: func(f *dvmtest.Artifacts) { f.Files["activities.json"] = `[]` },
			err:    ErrDefs,
		},
		"activity type table is not an object": {
			break_: func(f *dvmtest.Artifacts) { f.Files["activity-types.json"] = `[]` },
			err:    ErrDefs,
		},
		"destination table is not an object": {
			break_: func(f *dvmtest.Artifacts) { f.Files["destinations.json"] = `[]` },
			err:    ErrDefs,
		},
		"place table is not an object": {
			break_: func(f *dvmtest.Artifacts) { f.Files["places.json"] = `[]` },
			err:    ErrDefs,
		},
		"second activity reward file is not an array": {
			break_: func(f *dvmtest.Artifacts) { f.Files["activity-rewards-1.json"] = `{"hash": 601}` },
			err:    ErrDefs,
		},
		"activity reward fetch fails": {
			break_: func(f *dvmtest.Artifacts) { delete(f.Files, "activity-rewards-1.json") },
			err:    dvm.ErrService,
		},
		"item set table is not an object": {
			break_: func(f *dvmtest.Artifacts) { f.Files["item-sets.json"] = `[]` },
			err:    ErrDefs,
		},
		"record table is not an object": {
			break_: func(f *dvmtest.Artifacts) { f.Files["records.json"] = `[]` },
			err:    ErrDefs,
		},
		"vendor table is not an object": {
			break_: func(f *dvmtest.Artifacts) { f.Files["vendors.json"] = `[]` },
			err:    ErrDefs,
		},
		"second vendor item file is not an array": {
			break_: func(f *dvmtest.Artifacts) { f.Files["vendor-items-1.json"] = `{"hash": 501}` },
			err:    ErrDefs,
		},
		"vendor item fetch fails": {
			break_: func(f *dvmtest.Artifacts) { delete(f.Files, "vendor-items-1.json") },
			err:    dvm.ErrService,
		},
		"second detail file is not an array": {
			break_: func(f *dvmtest.Artifacts) { f.Files["detail-1.json"] = `{"hash": 402}` },
			err:    ErrDefs,
		},
		"detail fetch fails": {
			break_: func(f *dvmtest.Artifacts) { delete(f.Files, "detail-1.json") },
			err:    dvm.ErrService,
		},
		"second withheld file is not an array": {
			break_: func(f *dvmtest.Artifacts) { f.Files["withheld-1.json"] = `{"hash": 502}` },
			err:    ErrDefs,
		},
		"withheld hash is not a number": {
			break_: func(f *dvmtest.Artifacts) { f.Files["withheld-0.json"] = `["502"]` },
			err:    ErrDefs,
		},
		"withheld fetch fails": {
			break_: func(f *dvmtest.Artifacts) { delete(f.Files, "withheld-1.json") },
			err:    dvm.ErrService,
		},
		"second plug set file is not an array": {
			break_: func(f *dvmtest.Artifacts) { f.Files["plugsets-1.json"] = `{"hash": 45}` },
			err:    ErrDefs,
		},
		"plug set fetch fails": {
			break_: func(f *dvmtest.Artifacts) { delete(f.Files, "plugsets-1.json") },
			err:    dvm.ErrService,
		},
		"table artifact is missing from the index": {
			break_: func(f *dvmtest.Artifacts) { delete(f.Index, artifactStats) },
			err:    ErrDefs,
		},
		"artifact fetch fails": {
			break_: func(f *dvmtest.Artifacts) { delete(f.Files, "core-1.json") },
			err:    dvm.ErrService,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := newFakeArtifacts()
			tc.break_(source)

			_, err := NewLoader(source, "").Get(t.Context())
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestLoaderIgnoresCorruptCache(t *testing.T) {
	t.Parallel()

	source := newFakeArtifacts()
	dir := t.TempDir()
	loader := NewLoader(source, dir)

	require.NoError(t, os.WriteFile(loader.cachePath(source.Version), []byte("{"), 0o644))

	got, err := loader.Get(t.Context())
	require.NoError(t, err)
	assert.Len(t, got.Items, 8)
}

func TestWithLoaded(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// version is the manifest version of the seeded definitions. The
		// service publishes 245114.26/1.
		version string
		// elapsed is the time between the seeded check and the call.
		elapsed time.Duration
		// wantFetched is the number of files that the call fetches, and
		// wantSeed reports whether the call returns the seed.
		wantFetched int
		wantSeed    bool
	}{
		"inside the recheck window the loader serves the seed without a check": {
			version:  "245000.1",
			elapsed:  defsRecheck - time.Minute,
			wantSeed: true,
		},
		"after the recheck window the loader keeps a seed of the current version": {
			version:  "245114.26/1",
			elapsed:  defsRecheck,
			wantSeed: true,
		},
		"after the recheck window the loader replaces a seed of another version": {
			version:     "245000.1",
			elapsed:     defsRecheck,
			wantFetched: 23,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := newFakeArtifacts()
			seed := &Defs{Version: tc.version}
			clock := WithClock(func() time.Time { return bungietest.Now.Add(tc.elapsed) })

			got, err := NewLoader(source, "", clock, WithLoaded(seed, bungietest.Now)).Get(t.Context())
			require.NoError(t, err)
			assert.Len(t, source.Fetched, tc.wantFetched)

			if tc.wantSeed {
				assert.Same(t, seed, got)
			} else {
				assert.Equal(t, "245114.26/1", got.Version)
				assert.Len(t, got.Items, 8)
			}
		})
	}
}

func TestPlugSetSocket(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		defs *Defs
		// column lists the plug hashes of a perk column of item 100.
		column []uint32
		want   int
		ok     bool
	}{
		"weapon without a socket list has no socket": {
			defs: &Defs{
				Sockets:  map[uint32][]uint32{101: {40}},
				PlugSets: map[uint32][]uint32{40: {904}},
			},
			column: []uint32{904},
		},
		"definitions with nil maps have no socket": {
			defs:   &Defs{},
			column: []uint32{904},
		},
		"lower index wins when two plug sets list a hash of the column": {
			defs: &Defs{
				Sockets:  map[uint32][]uint32{100: {40, 41, 42}},
				PlugSets: map[uint32][]uint32{40: {900}, 41: {904}, 42: {904}},
			},
			column: []uint32{904},
			want:   1,
			ok:     true,
		},
		"plug set lists only the last hash of the column": {
			defs: &Defs{
				Sockets:  map[uint32][]uint32{100: {40, 41}},
				PlugSets: map[uint32][]uint32{40: {900}, 41: {901, 906}},
			},
			column: []uint32{904, 905, 906},
			want:   1,
			ok:     true,
		},
		"socket without a plug set does not read the plug set with the hash 0": {
			defs: &Defs{
				Sockets:  map[uint32][]uint32{100: {0, 41}},
				PlugSets: map[uint32][]uint32{0: {904}, 41: {904}},
			},
			column: []uint32{904},
			want:   1,
			ok:     true,
		},
		"socket whose plug set the definitions lack comes before the socket that matches": {
			defs: &Defs{
				Sockets:  map[uint32][]uint32{100: {49, 41}},
				PlugSets: map[uint32][]uint32{41: {904}},
			},
			column: []uint32{904},
			want:   1,
			ok:     true,
		},
		"no plug set lists a hash of the column": {
			defs: &Defs{
				Sockets:  map[uint32][]uint32{100: {40, 41}},
				PlugSets: map[uint32][]uint32{40: {900}, 41: {901}},
			},
			column: []uint32{904, 905},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := tc.defs.PlugSetSocket(100, tc.column)

			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestItemName(t *testing.T) {
	t.Parallel()

	defs := &Defs{
		Items: map[uint32]ItemDef{
			100: {Name: "Fatebringer"},
			101: {},
		},
		VendorItems:     map[uint32]string{100: "Fatebringer", 101: "Nameless Stock", 500: "Rohan's Passkey"},
		ActivityRewards: map[uint32]string{500: "Raid Gear", 600: "Enduring Snare Refit"},
		Lookups: map[uint32]Lookup{
			600: {Name: "Wish-Keeper Snare Refit", Defined: true},
			800: {Name: "Guided Game Ticket", Defined: true},
			801: {Defined: true, Redacted: true},
		},
	}

	tests := map[string]struct {
		want string
		hash uint32
	}{
		"item definition": {
			hash: 100,
			want: "Fatebringer",
		},
		"item definition without a name falls back to the vendor item name": {
			hash: 101,
			want: "Nameless Stock",
		},
		"vendor item name comes before the reward name": {
			hash: 500,
			want: "Rohan's Passkey",
		},
		"reward name comes before the name of a lookup": {
			hash: 600,
			want: "Enduring Snare Refit",
		},
		"name of a lookup at Bungie": {
			hash: 800,
			want: "Guided Game Ticket",
		},
		"lookup without a name": {
			hash: 801,
		},
		"no table names the hash": {
			hash: 700,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, defs.ItemName(tc.hash))
		})
	}
}

func TestWithholds(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		defs *Defs
		hash uint32
		want bool
	}{
		"first withheld hash": {
			defs: &Defs{Withheld: []uint32{5, 12, 700}, WithheldListed: true},
			hash: 5,
			want: true,
		},
		"last withheld hash": {
			defs: &Defs{Withheld: []uint32{5, 12, 700}, WithheldListed: true},
			hash: 700,
			want: true,
		},
		"hash between two withheld hashes": {
			defs: &Defs{Withheld: []uint32{5, 12, 700}, WithheldListed: true},
			hash: 13,
		},
		"index without withheld hashes": {
			defs: &Defs{},
			hash: 5,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.defs.Withholds(tc.hash))
		})
	}
}

func TestTraitNames(t *testing.T) {
	t.Parallel()

	defs := &Defs{Items: map[uint32]ItemDef{
		1: {Name: "Kill Clip", TypeName: "Trait"},
		2: {Name: "Kill Clip Enhanced", TypeName: "Enhanced Trait"},
		3: {Name: "Déjà Vu", TypeName: "Trait"},
		4: {Name: "Arrowhead Brake", TypeName: "Barrel"},
		5: {TypeName: "Trait"},
	}}

	assert.Equal(t, map[string]string{
		"kill clip": "Kill Clip",
		"deja vu":   "Déjà Vu",
	}, defs.TraitNames())
}
