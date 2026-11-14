package bungie

import (
	"context"
	"fmt"
	"io"
	"maps"
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

// newFakeManifest returns a source that serves two name tables, an icon
// table, and a color table. Its manifest lists the first name table. Each
// name table gives hash 1 its own name, so a test can tell the loaded
// tables apart.
func newFakeManifest() *fakeManifest {
	return &fakeManifest{
		paths: map[string]string{
			string(LoadoutNameTable):  "/names-a.json",
			string(LoadoutIconTable):  "/icons.json",
			string(LoadoutColorTable): "/colors.json",
		},
		files: map[string]string{
			"/names-a.json": `{"1": {"hash": 1, "name": "Raid Night"}, "2": {"hash": 2, "redacted": true}}`,
			"/names-b.json": `{"1": {"hash": 1, "name": "Trials"}}`,
			"/icons.json":   `{"20": {"hash": 20, "index": 0, "iconImagePath": "/i.png"}}`,
			"/colors.json":  `{"30": {"hash": 30, "index": 0, "colorImagePath": "/c.png"}}`,
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
		return nil, fmt.Errorf("%w: content file returned status 404", ErrPlatform)
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

func TestLoadoutTableStoreLoad(t *testing.T) {
	t.Parallel()

	// loaded loads the table that the manifest lists first.
	loaded := nameStep{fetched: []string{"/names-a.json"}, name: "Raid Night"}

	// kept expects the table of loaded again, without a fetch.
	kept := nameStep{name: "Raid Night"}

	// unreachable makes the manifest read return an error.
	unreachable := nameStep{
		edit: func(f *fakeManifest) {
			f.manifestErr = fmt.Errorf("%w: SystemDisabled: Maintenance.", ErrPlatform)
		},
		err:       "reading the Bungie manifest: bungie: SystemDisabled: Maintenance.",
		sentinels: []error{ErrPlatform},
	}

	// moved lists a path that the source does not serve.
	moved := nameStep{
		edit:      func(f *fakeManifest) { f.paths[string(LoadoutNameTable)] = "/names-x.json" },
		fetched:   []string{"/names-x.json"},
		err:       "fetching /names-x.json: bungie: content file returned status 404",
		sentinels: []error{ErrPlatform},
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
				edit:    func(f *fakeManifest) { f.paths[string(LoadoutNameTable)] = "/names-b.json" },
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
				edit:      func(f *fakeManifest) { delete(f.paths, string(LoadoutNameTable)) },
				err:       "the Bungie manifest lists no table of loadout names",
				sentinels: []error{errNoTable},
			}},
		},
		"manifest lists no English tables": {
			steps: []nameStep{{
				edit:      func(f *fakeManifest) { f.paths = nil },
				err:       "the Bungie manifest lists no table of loadout names",
				sentinels: []error{errNoTable},
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
				edit: func(f *fakeManifest) { f.paths[string(LoadoutNameTable)] = "/names-a.json" },
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
				err:       "decoding /names-a.json: the table lists no entry",
				sentinels: []error{errEmptyTable},
			}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := newFakeManifest()
			store := NewLoadoutTableStore(source)

			for i, step := range tc.steps {
				if step.edit != nil {
					step.edit(source)
				}

				source.fetched = nil

				states := store.Load(t.Context(), LoadoutNameTable)
				require.Lenf(t, states, 1, "load %d", i)

				got := states[LoadoutNameTable]

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

func TestLoadoutTableStoreLoadsSeveralTables(t *testing.T) {
	t.Parallel()

	// Each case loads the three tables twice. WantFetched lists the paths
	// that the first load fetches, and wantRefetched the paths that the
	// second load fetches again.
	tests := map[string]struct {
		edit          func(f *fakeManifest)
		wantErrs      map[LoadoutTable]string
		wantFetched   []string
		wantRefetched []string
	}{
		"every table loads": {
			wantFetched: []string{"/names-a.json", "/icons.json", "/colors.json"},
		},
		"one table does not load": {
			edit:          func(f *fakeManifest) { delete(f.files, "/icons.json") },
			wantFetched:   []string{"/names-a.json", "/icons.json", "/colors.json"},
			wantRefetched: []string{"/icons.json"},
			wantErrs: map[LoadoutTable]string{
				LoadoutIconTable: "fetching /icons.json: bungie: content file returned status 404",
			},
		},
		"manifest lists no color table": {
			edit:        func(f *fakeManifest) { delete(f.paths, string(LoadoutColorTable)) },
			wantFetched: []string{"/names-a.json", "/icons.json"},
			wantErrs: map[LoadoutTable]string{
				LoadoutColorTable: "the Bungie manifest lists no table of loadout colors",
			},
		},
		"manifest read does not succeed": {
			edit: func(f *fakeManifest) {
				f.manifestErr = fmt.Errorf("%w: SystemDisabled: Maintenance.", ErrPlatform)
			},
			wantErrs: map[LoadoutTable]string{
				LoadoutNameTable:  "reading the Bungie manifest: bungie: SystemDisabled: Maintenance.",
				LoadoutIconTable:  "reading the Bungie manifest: bungie: SystemDisabled: Maintenance.",
				LoadoutColorTable: "reading the Bungie manifest: bungie: SystemDisabled: Maintenance.",
			},
		},
		"icon table does not decode": {
			edit:          func(f *fakeManifest) { f.files["/icons.json"] = `[` },
			wantFetched:   []string{"/names-a.json", "/icons.json", "/colors.json"},
			wantRefetched: []string{"/icons.json"},
			wantErrs: map[LoadoutTable]string{
				LoadoutIconTable: "decoding /icons.json: reading the loadout icon table: unexpected EOF",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := newFakeManifest()
			if tc.edit != nil {
				tc.edit(source)
			}

			store := NewLoadoutTableStore(source)

			got := store.Load(t.Context(), LoadoutNameTable, LoadoutIconTable, LoadoutColorTable)
			assert.Equal(t, 1, source.manifests, "one load reads the manifest once")
			assert.Equal(t, tc.wantFetched, source.fetched)
			require.Len(t, got, 3)

			wantHashes := map[LoadoutTable]uint32{LoadoutNameTable: 1, LoadoutIconTable: 20, LoadoutColorTable: 30}

			for table, hash := range wantHashes {
				state := got[table]
				require.NotEqualf(t, state.Table == nil, state.Err == nil,
					"exactly one field of the state of %s is set", table)

				if want, ok := tc.wantErrs[table]; ok {
					require.EqualErrorf(t, state.Err, want, "%s", table)

					continue
				}

				require.NoErrorf(t, state.Err, "%s", table)
				assert.Containsf(t, state.Table, hash, "%s", table)
			}

			source.fetched = nil

			store.Load(t.Context(), LoadoutNameTable, LoadoutIconTable, LoadoutColorTable)
			assert.Equal(t, tc.wantRefetched, source.fetched,
				"the store keeps each table that loaded and fetches each other table again")
		})
	}
}

func TestDecodeLoadoutTable(t *testing.T) {
	t.Parallel()

	got, err := decodeLoadoutTable(strings.NewReader(`{
		"1": {"hash": 1, "index": 0, "name": "Raid Night", "redacted": false},
		"2": {"hash": 2, "index": 1, "name": "", "redacted": true}
	}`), LoadoutNameTable)
	require.NoError(t, err)
	assert.Equal(t, map[uint32]LoadoutDefinition{
		1: {Name: "Raid Night", Index: 0},
		2: {Index: 1, Redacted: true},
	}, got, "the table keeps redacted entries")

	got, err = decodeLoadoutTable(strings.NewReader(`{
		"5": {"hash": 5, "index": 2, "iconImagePath": "/a.png", "redacted": false}
	}`), LoadoutIconTable)
	require.NoError(t, err)
	assert.Equal(t, map[uint32]LoadoutDefinition{5: {Index: 2}}, got, "an icon entry has no name")
}

func TestFirstLoadoutEntry(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err   error
		table map[uint32]LoadoutDefinition
		want  uint32
	}{
		"lowest index": {
			table: map[uint32]LoadoutDefinition{5: {Index: 2}, 9: {Index: 0}, 7: {Index: 1}},
			want:  9,
		},
		"redacted entry with a lower index": {
			table: map[uint32]LoadoutDefinition{5: {Index: 0, Redacted: true}, 9: {Index: 3}, 7: {Index: 1}},
			want:  7,
		},
		"same index": {
			table: map[uint32]LoadoutDefinition{9: {Index: 0}, 4: {Index: 0}},
			want:  4,
		},
		"every entry redacted": {
			table: map[uint32]LoadoutDefinition{5: {Redacted: true}},
			err:   errNoShownEntry,
		},
		"no table": {
			err: errNoShownEntry,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := firstLoadoutEntry(tc.table)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFindLoadoutName(t *testing.T) {
	t.Parallel()

	table := map[uint32]LoadoutDefinition{
		1: {Name: "Raid Night", Index: 1},
		2: {Name: "Trials", Index: 0},
		3: {Name: "Hidden", Index: 2, Redacted: true},
		4: {Name: "raid night", Index: 3},
		5: {Index: 4},
		6: {Name: "Crépuscule", Index: 5},
	}

	tests := map[string]struct {
		name   string
		want   uint32
		wantOK bool
	}{
		"exact name":                 {name: "Trials", want: 2, wantOK: true},
		"another letter case":        {name: "tRIALS", want: 2, wantOK: true},
		"two matches":                {name: "RAID NIGHT", want: 1, wantOK: true},
		"name without its accents":   {name: "crepuscule", want: 6, wantOK: true},
		"name with accents":          {name: "TRIÁLS", want: 2, wantOK: true},
		"redacted name":              {name: "Hidden"},
		"part of a name":             {name: "Raid"},
		"name with surrounding text": {name: " Trials"},
		"name of only accent marks":  {name: "^"},
		"acute accent alone":         {name: "\u00b4"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := findLoadoutName(table, tc.name)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}

	assert.Equal(t, []string{"Trials", "Raid Night", "raid night", "Crépuscule"}, loadoutNameChoices(table),
		"the choices follow the index and leave out redacted entries and entries without a name")
}

func TestChooseLoadoutIdentifiers(t *testing.T) {
	t.Parallel()

	loaded := map[LoadoutTable]LoadoutTableState{
		LoadoutNameTable: {Table: map[uint32]LoadoutDefinition{
			1: {Name: "Raid Night", Index: 1},
			2: {Name: "Trials", Index: 0},
			3: {Name: "Hidden", Index: 2, Redacted: true},
		}},
		LoadoutIconTable: {Table: map[uint32]LoadoutDefinition{
			20: {Index: 1},
			21: {Index: 0},
		}},
		LoadoutColorTable: {Table: map[uint32]LoadoutDefinition{
			30: {Index: 0},
			31: {Index: 1},
		}},
	}

	// withState returns loaded with the state of table t replaced.
	withState := func(t LoadoutTable, state LoadoutTableState) map[LoadoutTable]LoadoutTableState {
		out := maps.Clone(loaded)
		out[t] = state

		return out
	}

	kept := LoadoutIdentifiers{NameHash: 7, IconHash: 8, ColorHash: 9}
	maintenance := fmt.Errorf("%w: SystemDisabled: Maintenance.", ErrPlatform)

	tests := map[string]struct {
		states   map[LoadoutTable]LoadoutTableState
		sentinel error
		name     string
		err      string
		kept     LoadoutIdentifiers
		want     LoadoutIdentifiers
		saved    bool
	}{
		"saved slot keeps its identifiers": {
			states: loaded,
			kept:   kept,
			saved:  true,
			want:   kept,
		},
		"saved slot without a name reads no table": {
			kept:  kept,
			saved: true,
			want:  kept,
		},
		"saved slot takes the name in any letter case": {
			states: loaded,
			kept:   kept,
			saved:  true,
			name:   "raid NIGHT",
			want:   LoadoutIdentifiers{NameHash: 1, IconHash: 8, ColorHash: 9},
		},
		"empty slot takes the first entry of each table": {
			states: loaded,
			want:   LoadoutIdentifiers{NameHash: 2, IconHash: 21, ColorHash: 30},
		},
		"empty slot takes the name": {
			states: loaded,
			name:   "Raid Night",
			want:   LoadoutIdentifiers{NameHash: 1, IconHash: 21, ColorHash: 30},
		},
		"unknown name": {
			states:   loaded,
			kept:     kept,
			saved:    true,
			name:     "Hidden",
			err:      `unknown loadout name: "Hidden". Pass one of these names in any letter case: Trials, Raid Night`,
			sentinel: ErrUnknownLoadoutName,
		},
		"unknown name without a shown name": {
			states: withState(LoadoutNameTable, LoadoutTableState{Table: map[uint32]LoadoutDefinition{
				3: {Name: "Hidden", Redacted: true},
			}}),
			saved:    true,
			name:     "Trials",
			err:      `unknown loadout name: "Trials". The name table holds no name that the game shows`,
			sentinel: ErrUnknownLoadoutName,
		},
		"name table did not load": {
			states:   withState(LoadoutNameTable, LoadoutTableState{Err: maintenance}),
			saved:    true,
			name:     "Trials",
			err:      "loadout names unavailable: bungie: SystemDisabled: Maintenance.",
			sentinel: ErrPlatform,
		},
		"icon table did not load": {
			states:   withState(LoadoutIconTable, LoadoutTableState{Err: maintenance}),
			name:     "Trials",
			err:      "loadout icons unavailable: bungie: SystemDisabled: Maintenance.",
			sentinel: ErrPlatform,
		},
		"color table holds no shown entry": {
			states: withState(LoadoutColorTable, LoadoutTableState{Table: map[uint32]LoadoutDefinition{
				30: {Redacted: true},
			}}),
			err:      "loadout colors unavailable: the table holds no entry that the game shows",
			sentinel: errNoShownEntry,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := ChooseLoadoutIdentifiers(tc.kept, tc.saved, tc.name, tc.states)
			if tc.err != "" {
				require.EqualError(t, err, tc.err)
				require.ErrorIs(t, err, tc.sentinel)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
