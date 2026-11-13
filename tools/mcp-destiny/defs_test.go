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
			artifactCore:        {"core-0.json", "core-1.json"},
			artifactBuckets:     {"buckets.json"},
			artifactDamage:      {"damage.json"},
			artifactClasses:     {"classes.json"},
			artifactStats:       {"stats.json"},
			artifactActivities:  {"activities.json"},
			artifactVendors:     {"vendors.json"},
			artifactVendorItems: {"vendor-items-0.json", "vendor-items-1.json"},
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
			// The service build projects each activity to a record with a
			// top-level name.
			"activities.json": `{
				"3022541210": {"hash": 3022541210, "name": "Vault of Glass: Master", "description": "Raid.",
				 "activityTypeHash": 2043403989, "modeTypes": [4], "isPlaylist": false},
				"2693136600": {"hash": 2693136600, "name": "Deep Stone Crypt", "modeTypes": [4]}
			}`,
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
			3022541210: {Name: "Vault of Glass: Master"},
			2693136600: {Name: "Deep Stone Crypt"},
		},
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
	assert.Len(t, source.fetched, 10)

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
	assert.Len(t, source.fetched, 10)

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
