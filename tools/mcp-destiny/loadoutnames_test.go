package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeManifest is a [ManifestSource] for the store tests. Paths is the
// table list that Manifest returns, and files holds the body of each
// content file that Content serves. A set manifestErr makes Manifest
// return that error. Fetched lists the paths that Content opened, and
// manifests counts the Manifest calls.
type fakeManifest struct {
	manifestErr error
	paths       map[string]string
	files       map[string]string
	fetched     []string
	manifests   int
}

// newFakeManifest returns a source that serves two name tables and whose
// manifest lists the first one. Each table gives hash 1 its own name, so
// a test can tell the loaded tables apart.
func newFakeManifest() *fakeManifest {
	return &fakeManifest{
		paths: map[string]string{loadoutNameTable: "/names-a.json"},
		files: map[string]string{
			"/names-a.json": `{"1": {"hash": 1, "name": "Raid Night"}, "2": {"hash": 2, "redacted": true}}`,
			"/names-b.json": `{"1": {"hash": 1, "name": "Trials"}}`,
		},
	}
}

func (f *fakeManifest) Manifest(context.Context) (map[string]string, error) {
	f.manifests++

	if f.manifestErr != nil {
		return nil, f.manifestErr
	}

	return f.paths, nil
}

func (f *fakeManifest) Content(_ context.Context, path string) (io.ReadCloser, error) {
	f.fetched = append(f.fetched, path)

	body, ok := f.files[path]
	if !ok {
		return nil, fmt.Errorf("%w: content file returned status 404", ErrBungie)
	}

	return io.NopCloser(strings.NewReader(body)), nil
}

// nameStep is one Load of a store case. Edit changes the source before
// the Load. Fetched lists the paths that the Load fetches. A set err is
// the text of the reason, and sentinels lists the errors that the reason
// wraps. Without err, name is the name that the table gives hash 1.
type nameStep struct {
	edit      func(f *fakeManifest)
	err       string
	name      string
	sentinels []error
	fetched   []string
}

func TestLoadoutNameStoreLoad(t *testing.T) {
	t.Parallel()

	// loaded loads the table that the manifest lists first.
	loaded := nameStep{fetched: []string{"/names-a.json"}, name: "Raid Night"}

	// kept expects the table of loaded again, without a fetch.
	kept := nameStep{name: "Raid Night"}

	// unreachable makes the manifest read return an error.
	unreachable := nameStep{
		edit: func(f *fakeManifest) {
			f.manifestErr = fmt.Errorf("%w: SystemDisabled: Maintenance.", ErrBungie)
		},
		err:       "reading the Bungie manifest: bungie: SystemDisabled: Maintenance.",
		sentinels: []error{ErrBungie},
	}

	// moved lists a path that the source does not serve.
	moved := nameStep{
		edit:      func(f *fakeManifest) { f.paths[loadoutNameTable] = "/names-x.json" },
		fetched:   []string{"/names-x.json"},
		err:       "fetching /names-x.json: bungie: content file returned status 404",
		sentinels: []error{ErrBungie},
	}

	tests := map[string]struct {
		steps []nameStep
	}{
		"table loads": {
			steps: []nameStep{loaded},
		},
		"second load reuses the table": {
			steps: []nameStep{loaded, kept},
		},
		"another path fetches the other table": {
			steps: []nameStep{loaded, {
				edit:    func(f *fakeManifest) { f.paths[loadoutNameTable] = "/names-b.json" },
				fetched: []string{"/names-b.json"},
				name:    "Trials",
			}},
		},
		"manifest read does not succeed": {
			steps: []nameStep{loaded, unreachable, {
				edit: func(f *fakeManifest) { f.manifestErr = nil },
				name: "Raid Night",
			}},
		},
		"manifest lists no name table": {
			steps: []nameStep{{
				edit:      func(f *fakeManifest) { delete(f.paths, loadoutNameTable) },
				err:       "the Bungie manifest lists no loadout name table",
				sentinels: []error{errNoNameTable},
			}},
		},
		"manifest lists no English tables": {
			steps: []nameStep{{
				edit:      func(f *fakeManifest) { f.paths = nil },
				err:       "the Bungie manifest lists no loadout name table",
				sentinels: []error{errNoNameTable},
			}},
		},
		"download does not succeed": {
			steps: []nameStep{loaded, moved, {
				edit: func(f *fakeManifest) {
					f.files["/names-x.json"] = `{"1": {"hash": 1, "name": "Gambit"}}`
				},
				fetched: []string{"/names-x.json"},
				name:    "Gambit",
			}},
		},
		"manifest lists the kept path again after a download that did not succeed": {
			steps: []nameStep{loaded, moved, {
				edit: func(f *fakeManifest) { f.paths[loadoutNameTable] = "/names-a.json" },
				name: "Raid Night",
			}},
		},
		"body does not decode": {
			steps: []nameStep{
				{
					edit:      func(f *fakeManifest) { f.files["/names-a.json"] = `{"1": {"hash": 1,` },
					fetched:   []string{"/names-a.json"},
					err:       "decoding /names-a.json: reading the loadout name table: unexpected EOF",
					sentinels: []error{io.ErrUnexpectedEOF},
				},
				{
					fetched:   []string{"/names-a.json"},
					err:       "decoding /names-a.json: reading the loadout name table: unexpected EOF",
					sentinels: []error{io.ErrUnexpectedEOF},
				},
			},
		},
		"body holds no name": {
			steps: []nameStep{{
				edit:      func(f *fakeManifest) { f.files["/names-a.json"] = `null` },
				fetched:   []string{"/names-a.json"},
				err:       "decoding /names-a.json: the table lists no loadout name",
				sentinels: []error{errEmptyNameTable},
			}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := newFakeManifest()
			store := NewLoadoutNameStore(source)

			for i, step := range tc.steps {
				if step.edit != nil {
					step.edit(source)
				}

				source.fetched = nil

				got := store.Load(t.Context())
				assert.Equalf(t, step.fetched, source.fetched, "fetches of load %d", i)
				assert.Equalf(t, i+1, source.manifests, "every load reads the manifest, load %d", i)
				require.NotEqualf(t, got.Table == nil, got.Err == nil,
					"exactly one field of the state is set, load %d", i)

				if step.err != "" {
					require.EqualErrorf(t, got.Err, step.err, "load %d", i)

					for _, target := range step.sentinels {
						require.ErrorIsf(t, got.Err, target, "load %d", i)
					}

					continue
				}

				require.NoErrorf(t, got.Err, "load %d", i)
				assert.Equalf(t, step.name, got.Table[1].Name, "load %d", i)
			}
		})
	}
}

func TestDecodeLoadoutNames(t *testing.T) {
	t.Parallel()

	got, err := decodeLoadoutNames(strings.NewReader(`{
		"1": {"hash": 1, "index": 0, "name": "Raid Night", "redacted": false},
		"2": {"hash": 2, "index": 1, "name": "", "redacted": true}
	}`))
	require.NoError(t, err)
	assert.Equal(t, map[uint32]LoadoutName{
		1: {Name: "Raid Night", Index: 0},
		2: {Index: 1, Redacted: true},
	}, got, "the table keeps redacted entries")
}
