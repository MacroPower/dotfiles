package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
)

// fakeArtifacts is an [ArtifactSource] that serves artifacts from memory
// and counts the fetches. Index lists the files of each artifact.
type fakeArtifacts struct {
	files   map[string]string
	index   map[string][]string
	version string
	fetched []string
}

func newFakeArtifacts() *fakeArtifacts {
	return &fakeArtifacts{
		version: "245114.26/1",
		index: map[string][]string{
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
		},
		files: map[string]string{
			"core-0.json": `[
				{"hash": 100, "displayProperties": {"name": "Fatebringer", "description": "Fortune favors.", "icon": "/a.png"},
				 "itemTypeDisplayName": "Hand Cannon", "itemType": 3, "classType": 3, "defaultDamageType": 1,
				 "inventory": {"tierTypeName": "Legendary", "tierType": 5, "bucketTypeHash": 1498876634},
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
			// Midnight Coup has no socket entry.
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
				{"hash": 402, "sockets": {"socketEntries": [{"socketTypeHash": 1, "reusablePlugSetHash": 45}]}}
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
		},
	}
}

// fixtureSockets returns the socket lists that the loader builds from
// the detail files of [newFakeArtifacts].
func fixtureSockets() map[uint32][]uint32 {
	return map[uint32][]uint32{100: {40, 41, 0}, 402: {45}}
}

func (f *fakeArtifacts) Config(context.Context) (dvm.Config, error) {
	return dvm.Config{Artifacts: dvm.ArtifactIndex{ManifestVersion: f.version, Files: f.index}}, nil
}

func (f *fakeArtifacts) Artifact(_ context.Context, file string) (io.ReadCloser, error) {
	f.fetched = append(f.fetched, file)

	body, ok := f.files[file]
	if !ok {
		return nil, dvm.ErrService
	}

	return io.NopCloser(strings.NewReader(body)), nil
}

func TestDefsLoader(t *testing.T) {
	t.Parallel()

	source := newFakeArtifacts()
	dir := t.TempDir()
	now := bungietest.Now
	clock := WithDefsClock(func() time.Time { return now })

	// A cache file of an older layout stays behind after an upgrade. The
	// layout without the item sets has the prefix defs-v9-. Its file holds
	// the manifest version of the service, and the loader does not read
	// it.
	stale := filepath.Join(dir, "defs-v9-245114.26_1.json")
	require.NoError(t, os.WriteFile(stale, []byte(`{"version": "245114.26/1"}`), 0o644))

	loader := NewDefsLoader(source, dir, clock)

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
		ItemSets: map[uint32][]uint32{3727270518: {1360604600, 1360604601}, 12: {}},
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
		ItemSetsListed: true,
	}, got)
	assert.Len(t, source.fetched, 20)

	// Writing the cache removes the file of the older layout.
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, defsCachePrefix+"245114.26_1.json")}, files)

	// A second call inside the recheck window asks the service nothing.
	source.fetched = nil

	again, err := loader.Get(t.Context())
	require.NoError(t, err)
	assert.Same(t, got, again)
	assert.Empty(t, source.fetched)

	// Another process reads the cache file instead of the artifacts.
	cached, err := NewDefsLoader(source, dir, clock).Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, got, cached)
	assert.Empty(t, source.fetched)

	// A manifest version change rebuilds and replaces the cache file.
	source.version = "245200.1"
	now = now.Add(2 * defsRecheck)

	rebuilt, err := loader.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "245200.1", rebuilt.Version)
	assert.Len(t, source.fetched, 20)

	files, err = filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, defsCachePrefix+"245200.1.json")}, files)
}

func TestDefsLoaderWithoutOptionalArtifacts(t *testing.T) {
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
			delete(source.index, tc.artifact)

			got, err := NewDefsLoader(source, "").Get(t.Context())
			require.NoError(t, err)
			assert.Empty(t, tc.field(got))
			assert.Len(t, got.Items, 8)

			for _, file := range tc.files {
				assert.NotContains(t, source.fetched, file)
			}

			if tc.check != nil {
				tc.check(t, got, source.fetched)
			}
		})
	}
}

func TestDefsLoaderErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		break_ func(*fakeArtifacts)
		err    error
	}{
		"artifact is not an array": {
			break_: func(f *fakeArtifacts) { f.files["core-0.json"] = `{"hash": 1}` },
			err:    ErrDefs,
		},
		"table key is not a hash": {
			break_: func(f *fakeArtifacts) { f.files["stats.json"] = `{"range": {}}` },
			err:    ErrDefs,
		},
		"activity table is not an object": {
			break_: func(f *fakeArtifacts) { f.files["activities.json"] = `[]` },
			err:    ErrDefs,
		},
		"activity type table is not an object": {
			break_: func(f *fakeArtifacts) { f.files["activity-types.json"] = `[]` },
			err:    ErrDefs,
		},
		"destination table is not an object": {
			break_: func(f *fakeArtifacts) { f.files["destinations.json"] = `[]` },
			err:    ErrDefs,
		},
		"place table is not an object": {
			break_: func(f *fakeArtifacts) { f.files["places.json"] = `[]` },
			err:    ErrDefs,
		},
		"second activity reward file is not an array": {
			break_: func(f *fakeArtifacts) { f.files["activity-rewards-1.json"] = `{"hash": 601}` },
			err:    ErrDefs,
		},
		"activity reward fetch fails": {
			break_: func(f *fakeArtifacts) { delete(f.files, "activity-rewards-1.json") },
			err:    dvm.ErrService,
		},
		"item set table is not an object": {
			break_: func(f *fakeArtifacts) { f.files["item-sets.json"] = `[]` },
			err:    ErrDefs,
		},
		"vendor table is not an object": {
			break_: func(f *fakeArtifacts) { f.files["vendors.json"] = `[]` },
			err:    ErrDefs,
		},
		"second vendor item file is not an array": {
			break_: func(f *fakeArtifacts) { f.files["vendor-items-1.json"] = `{"hash": 501}` },
			err:    ErrDefs,
		},
		"vendor item fetch fails": {
			break_: func(f *fakeArtifacts) { delete(f.files, "vendor-items-1.json") },
			err:    dvm.ErrService,
		},
		"second detail file is not an array": {
			break_: func(f *fakeArtifacts) { f.files["detail-1.json"] = `{"hash": 402}` },
			err:    ErrDefs,
		},
		"detail fetch fails": {
			break_: func(f *fakeArtifacts) { delete(f.files, "detail-1.json") },
			err:    dvm.ErrService,
		},
		"second plug set file is not an array": {
			break_: func(f *fakeArtifacts) { f.files["plugsets-1.json"] = `{"hash": 45}` },
			err:    ErrDefs,
		},
		"plug set fetch fails": {
			break_: func(f *fakeArtifacts) { delete(f.files, "plugsets-1.json") },
			err:    dvm.ErrService,
		},
		"table artifact is missing from the index": {
			break_: func(f *fakeArtifacts) { delete(f.index, artifactStats) },
			err:    ErrDefs,
		},
		"artifact fetch fails": {
			break_: func(f *fakeArtifacts) { delete(f.files, "core-1.json") },
			err:    dvm.ErrService,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := newFakeArtifacts()
			tc.break_(source)

			_, err := NewDefsLoader(source, "").Get(t.Context())
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestDefsLoaderIgnoresCorruptCache(t *testing.T) {
	t.Parallel()

	source := newFakeArtifacts()
	dir := t.TempDir()
	loader := NewDefsLoader(source, dir)

	require.NoError(t, os.WriteFile(loader.cachePath(source.version), []byte("{"), 0o644))

	got, err := loader.Get(t.Context())
	require.NoError(t, err)
	assert.Len(t, got.Items, 8)
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

			got, ok := tc.defs.plugSetSocket(100, tc.column)

			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
