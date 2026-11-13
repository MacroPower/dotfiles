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
		},
		files: map[string]string{
			"core-0.json": `[
				{"hash": 100, "displayProperties": {"name": "Fatebringer", "description": "Fortune favors.", "icon": "/a.png"},
				 "itemTypeDisplayName": "Hand Cannon", "itemType": 3, "classType": 3, "defaultDamageType": 1,
				 "inventory": {"tierTypeName": "Legendary", "bucketTypeHash": 1498876634},
				 "equippingBlock": {"ammoType": 1}, "stats": {"stats": {}}},
				{"hash": 900, "displayProperties": {"name": "Firefly", "description": "Precision final blows explode."},
				 "itemTypeDisplayName": "Trait",
				 "itemType": 19, "classType": 3, "inventory": {"tierTypeName": "Common"}}
			]`,
			"core-1.json": `[
				{"hash": 400, "displayProperties": {"name": "Hawkmoon"}, "itemTypeDisplayName": "Hand Cannon",
				 "itemType": 3, "classType": 3,
				 "inventory": {"tierTypeName": "Exotic", "bucketTypeHash": 1498876634, "nonTransferrableOriginal": true}}
			]`,
			"buckets.json": `{
				"1498876634": {"displayProperties": {"name": "Kinetic Weapons"}, "scope": 0, "category": 3,
				 "bucketOrder": 20, "itemCount": 10, "location": 1},
				"138197802": {"displayProperties": {"name": "General"}, "scope": 1, "category": 1,
				 "bucketOrder": 30, "itemCount": 700, "location": 2}
			}`,
			"damage.json":  `{"3373582085": {"displayProperties": {"name": "Kinetic"}, "enumValue": 1}}`,
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
		},
	}
}

func (f *fakeArtifacts) Config(context.Context) (ServiceConfig, error) {
	return ServiceConfig{Artifacts: ArtifactIndex{ManifestVersion: f.version, Files: f.index}}, nil
}

func (f *fakeArtifacts) Artifact(_ context.Context, file string) (io.ReadCloser, error) {
	f.fetched = append(f.fetched, file)

	body, ok := f.files[file]
	if !ok {
		return nil, ErrService
	}

	return io.NopCloser(strings.NewReader(body)), nil
}

func TestDefsLoader(t *testing.T) {
	t.Parallel()

	source := newFakeArtifacts()
	dir := t.TempDir()
	now := fixtureNow
	clock := WithDefsClock(func() time.Time { return now })

	// A cache file of an older layout stays behind after an upgrade.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "defs-v2-245114.26_1.json"), []byte("{}"), 0o644))

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
				Bucket:     bucketKinetic,
				ItemType:   itemTypeWeapon,
				ClassType:  classAny,
				DamageType: 1,
				Equippable: true,
			},
			400: {
				Name:            "Hawkmoon",
				TypeName:        "Hand Cannon",
				Tier:            "Exotic",
				Bucket:          bucketKinetic,
				ItemType:        itemTypeWeapon,
				ClassType:       classAny,
				NonTransferable: true,
			},
			900: {
				Name:        "Firefly",
				Description: "Precision final blows explode.",
				TypeName:    "Trait",
				Tier:        "Common",
				ItemType:    itemTypePlug,
				ClassType:   classAny,
			},
		},
		Buckets: map[uint32]BucketDef{
			bucketKinetic: {Name: "Kinetic Weapons", Capacity: 10, Category: bucketCategoryEquippable, Order: 20},
			bucketVault: {
				Name:     "General",
				Capacity: 700,
				Category: bucketCategoryItem,
				Order:    30,
				Account:  true,
			},
		},
		Stats: map[uint32]string{1240592695: "Range"},
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
			500:         "Ammo Synthesis",
			itemGlimmer: "Glimmer",
			501:         "Legendary Shards",
		},
		DamageTypes: map[int]string{1: "Kinetic"},
		Classes:     map[int]string{1: "Hunter"},
	}, got)
	assert.Len(t, source.fetched, 15)

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
	assert.Len(t, source.fetched, 15)

	files, err = filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, defsCachePrefix+"245200.1.json")}, files)
}

func TestDefsLoaderWithoutOptionalArtifacts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		field    func(*Defs) any
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
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := newFakeArtifacts()
			delete(source.index, tc.artifact)

			got, err := NewDefsLoader(source, "").Get(t.Context())
			require.NoError(t, err)
			assert.Empty(t, tc.field(got))
			assert.Len(t, got.Items, 3)

			for _, file := range tc.files {
				assert.NotContains(t, source.fetched, file)
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
			err:    ErrService,
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
			err:    ErrService,
		},
		"table artifact is missing from the index": {
			break_: func(f *fakeArtifacts) { delete(f.index, artifactStats) },
			err:    ErrDefs,
		},
		"artifact fetch fails": {
			break_: func(f *fakeArtifacts) { delete(f.files, "core-1.json") },
			err:    ErrService,
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
	assert.Len(t, got.Items, 3)
}
