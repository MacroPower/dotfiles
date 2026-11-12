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
// and counts the fetches.
type fakeArtifacts struct {
	files   map[string]string
	version string
	fetched []string
}

func newFakeArtifacts() *fakeArtifacts {
	return &fakeArtifacts{
		version: "245114.26/1",
		files: map[string]string{
			"core-0.json": `[
				{"hash": 100, "displayProperties": {"name": "Fatebringer", "icon": "/a.png"},
				 "itemTypeDisplayName": "Hand Cannon", "itemType": 3, "classType": 3, "defaultDamageType": 1,
				 "inventory": {"tierTypeName": "Legendary", "bucketTypeHash": 1498876634},
				 "equippingBlock": {"ammoType": 1}, "stats": {"stats": {}}},
				{"hash": 900, "displayProperties": {"name": "Firefly"}, "itemTypeDisplayName": "Trait",
				 "itemType": 19, "classType": 3, "inventory": {"tierTypeName": "Common"}}
			]`,
			"core-1.json": `[
				{"hash": 400, "displayProperties": {"name": "Hawkmoon"}, "itemTypeDisplayName": "Hand Cannon",
				 "itemType": 3, "classType": 3,
				 "inventory": {"tierTypeName": "Exotic", "bucketTypeHash": 1498876634, "nonTransferrableOriginal": true}}
			]`,
			"buckets.json": `{"1498876634": {"displayProperties": {"name": "Kinetic Weapons"}}}`,
			"damage.json":  `{"3373582085": {"displayProperties": {"name": "Kinetic"}, "enumValue": 1}}`,
			"classes.json": `{"671679327": {"displayProperties": {"name": "Hunter"}, "classType": 1}}`,
			"stats.json":   `{"1240592695": {"displayProperties": {"name": "Range"}}}`,
		},
	}
}

func (f *fakeArtifacts) Config(context.Context) (ServiceConfig, error) {
	return ServiceConfig{Artifacts: ArtifactIndex{
		ManifestVersion: f.version,
		Files: map[string][]string{
			artifactCore:    {"core-0.json", "core-1.json"},
			artifactBuckets: {"buckets.json"},
			artifactDamage:  {"damage.json"},
			artifactClasses: {"classes.json"},
			artifactStats:   {"stats.json"},
		},
	}}, nil
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
			900: {Name: "Firefly", TypeName: "Trait", Tier: "Common", ItemType: 19, ClassType: classAny},
		},
		Buckets:     map[uint32]string{bucketKinetic: "Kinetic Weapons"},
		Stats:       map[uint32]string{1240592695: "Range"},
		DamageTypes: map[int]string{1: "Kinetic"},
		Classes:     map[int]string{1: "Hunter"},
	}, got)
	assert.Len(t, source.fetched, 6)

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
	assert.Len(t, source.fetched, 6)

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(dir, "defs-v1-245200.1.json")}, files)
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
