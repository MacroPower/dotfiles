package definitions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
)

// errLookup is the error of a lookup that the fake item source refuses.
var errLookup = errors.New("bungie: SystemDisabled: down")

// fakeItems is an [ItemSource] that answers from a table and records each
// lookup. Defs maps a hash to the definition that Bungie returns, and a
// hash without an entry returns no definition. Fail lists the hashes whose
// lookups fail with [errLookup]. Block makes every lookup wait until its
// context ends.
type fakeItems struct {
	defs   map[uint32]*bungie.ItemDefinition
	fail   []uint32
	calls  []uint32
	mu     sync.Mutex
	block  bool
	active int
	most   int
}

func (f *fakeItems) ItemDefinition(ctx context.Context, hash uint32) (*bungie.ItemDefinition, error) {
	f.mu.Lock()
	f.calls = append(f.calls, hash)
	f.active++
	f.most = max(f.most, f.active)
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()

	if f.block {
		<-ctx.Done()

		return nil, fmt.Errorf("looking up %d: %w", hash, ctx.Err())
	}

	if slices.Contains(f.fail, hash) {
		return nil, errLookup
	}

	return f.defs[hash], nil
}

// lookups returns the hashes that the source looked up, in ascending
// order. The store runs lookups at a time, so the order of arrival varies.
func (f *fakeItems) lookups() []uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()

	calls := slices.Clone(f.calls)
	slices.Sort(calls)

	return calls
}

// itemDef returns a definition of the live manifest with name.
func itemDef(name string, redacted bool) *bungie.ItemDefinition {
	def := &bungie.ItemDefinition{Redacted: redacted}
	def.DisplayProperties.Name = name

	return def
}

// newFakeItems returns a source whose live manifest names the withheld
// items 78 and 2576899547. It classifies item 79 and defines item 80
// without a name. Item 81 has no definition, and the lookup of item 82
// fails.
func newFakeItems() *fakeItems {
	return &fakeItems{
		defs: map[uint32]*bungie.ItemDefinition{
			78:         itemDef("Wish-Keeper Snare Refit", false),
			79:         itemDef("", true),
			80:         itemDef("", false),
			2576899547: itemDef("Guided Game Ticket", false),
		},
		fail: []uint32{82},
	}
}

// namedDefs returns definitions of the manifest version that name the hand
// cannon 100 and, through the vendor item names, the withheld item 501.
func namedDefs(version string) *Defs {
	return &Defs{
		Version:     version,
		Items:       map[uint32]ItemDef{100: {Name: "Fatebringer"}},
		VendorItems: map[uint32]string{501: "Legendary Shards"},
	}
}

func TestNameStoreName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want    map[uint32]Lookup
		names   map[uint32]string
		opts    []NameOption
		hashes  []uint32
		lookups []uint32
	}{
		"the definitions name every hash": {
			hashes: []uint32{100, 501},
			names:  map[uint32]string{100: "Fatebringer", 501: "Legendary Shards"},
		},
		"Bungie names the hashes that the definitions do not name": {
			hashes:  []uint32{100, 78, 501, 2576899547},
			lookups: []uint32{78, 2576899547},
			want: map[uint32]Lookup{
				78:         {Name: "Wish-Keeper Snare Refit", Defined: true},
				2576899547: {Name: "Guided Game Ticket", Defined: true},
			},
			names: map[uint32]string{
				100:        "Fatebringer",
				78:         "Wish-Keeper Snare Refit",
				501:        "Legendary Shards",
				2576899547: "Guided Game Ticket",
			},
		},
		"a hash comes one time": {
			hashes:  []uint32{78, 78},
			lookups: []uint32{78},
			want:    map[uint32]Lookup{78: {Name: "Wish-Keeper Snare Refit", Defined: true}},
			names:   map[uint32]string{78: "Wish-Keeper Snare Refit"},
		},
		"answers without a name": {
			hashes:  []uint32{79, 80, 81},
			lookups: []uint32{79, 80, 81},
			want: map[uint32]Lookup{
				79: {Defined: true, Redacted: true},
				80: {Defined: true},
				81: {},
			},
			names: map[uint32]string{79: "", 80: "", 81: ""},
		},
		"failed lookup": {
			hashes:  []uint32{82, 78},
			lookups: []uint32{78, 82},
			want: map[uint32]Lookup{
				78: {Name: "Wish-Keeper Snare Refit", Defined: true},
				82: {Err: errLookup},
			},
			names: map[uint32]string{78: "Wish-Keeper Snare Refit", 82: ""},
		},
		"the limit skips the later hashes": {
			opts:    []NameOption{WithLookupLimit(2)},
			hashes:  []uint32{100, 81, 2576899547, 78, 79},
			lookups: []uint32{81, 2576899547},
			want: map[uint32]Lookup{
				81:         {},
				2576899547: {Name: "Guided Game Ticket", Defined: true},
				78:         {Skipped: true},
				79:         {Skipped: true},
			},
			names: map[uint32]string{2576899547: "Guided Game Ticket", 78: "", 79: ""},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := newFakeItems()
			defs := namedDefs("v1")

			got := NewNameStore(source, "", tc.opts...).Name(t.Context(), defs, tc.hashes)

			assert.Equal(t, tc.lookups, source.lookups())
			assert.Nil(t, defs.Lookups, "the store leaves the definitions of the caller unchanged")

			if tc.want == nil {
				assert.Same(t, defs, got, "the store returns the definitions when they name every hash")
			} else {
				assert.Equal(t, tc.want, got.Lookups)
				assert.Equal(t, defs.Items, got.Items)
			}

			for hash, want := range tc.names {
				assert.Equal(t, want, got.ItemName(hash), "name of %d", hash)
			}
		})
	}
}

// TestNameStoreKeepsLookups pins that a second call keeps the lookups of
// the definitions that the first call returned.
func TestNameStoreKeepsLookups(t *testing.T) {
	t.Parallel()

	source := newFakeItems()
	store := NewNameStore(source, "")

	first := store.Name(t.Context(), namedDefs("v1"), []uint32{78})
	second := store.Name(t.Context(), first, []uint32{79})

	assert.Equal(t, map[uint32]Lookup{
		78: {Name: "Wish-Keeper Snare Refit", Defined: true},
		79: {Defined: true, Redacted: true},
	}, second.Lookups)
	assert.Equal(t, map[uint32]Lookup{78: {Name: "Wish-Keeper Snare Refit", Defined: true}}, first.Lookups)
	assert.Equal(t, []uint32{78, 79}, source.lookups())
}

func TestNameStoreCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hashes := []uint32{78, 81, 82}

	// The cache files of the definitions share the directory, and the
	// store leaves them in place. A file of an older layout of the store
	// stays behind after an upgrade.
	defsFile := filepath.Join(dir, defsCachePrefix+"v1.json")
	require.NoError(t, os.WriteFile(defsFile, []byte(`{"version": "v1"}`), 0o644))

	stale := filepath.Join(dir, "names-v0-v1.json")
	require.NoError(t, os.WriteFile(stale, []byte(`{}`), 0o644))

	// The lock file of the name cache files stays in place.
	lock := filepath.Join(dir, namesLockFile)

	source := newFakeItems()
	store := NewNameStore(source, dir)

	got := store.Name(t.Context(), namedDefs("v1"), hashes)
	assert.Equal(t, "Wish-Keeper Snare Refit", got.ItemName(78))
	assert.Equal(t, []uint32{78, 81, 82}, source.lookups())

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{defsFile, lock, filepath.Join(dir, "names-v1-v1.json")}, files)

	// The store keeps each answer in memory, and it looks up the failed
	// hash again.
	store.Name(t.Context(), namedDefs("v1"), hashes)
	assert.Equal(t, []uint32{78, 81, 82, 82}, source.lookups())

	// A new process reads the answers from the cache file.
	again := newFakeItems()

	got = NewNameStore(again, dir).Name(t.Context(), namedDefs("v1"), hashes)
	assert.Equal(t, "Wish-Keeper Snare Refit", got.ItemName(78))
	assert.Equal(t, Lookup{}, got.Lookups[81])
	assert.Equal(t, []uint32{82}, again.lookups())

	// Another manifest version looks every hash up again, and its file
	// replaces the file of the old version.
	got = store.Name(t.Context(), namedDefs("v2"), hashes)
	assert.Equal(t, "Wish-Keeper Snare Refit", got.ItemName(78))
	assert.Equal(t, []uint32{78, 78, 81, 81, 82, 82, 82}, source.lookups())

	files, err = filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{defsFile, lock, filepath.Join(dir, "names-v1-v2.json")}, files)
}

// TestNameStoreSharedCache pins that the stores of two processes that
// share the cache directory keep the answers of each other.
func TestNameStoreSharedCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first, second := newFakeItems(), newFakeItems()
	a, b := NewNameStore(first, dir), NewNameStore(second, dir)

	a.Name(t.Context(), namedDefs("v1"), []uint32{78})
	b.Name(t.Context(), namedDefs("v1"), []uint32{79})
	a.Name(t.Context(), namedDefs("v1"), []uint32{80})
	b.Name(t.Context(), namedDefs("v1"), []uint32{2576899547})

	// A running store reads the answers that the other store saved.
	got := b.Name(t.Context(), namedDefs("v1"), []uint32{78, 80})
	assert.Equal(t, "Wish-Keeper Snare Refit", got.ItemName(78))
	assert.Equal(t, []uint32{78, 80}, first.lookups())
	assert.Equal(t, []uint32{79, 2576899547}, second.lookups())

	// A new process reads the answers of both stores from the cache file.
	again := newFakeItems()

	got = NewNameStore(again, dir).Name(t.Context(), namedDefs("v1"), []uint32{78, 79, 80, 2576899547})
	assert.Empty(t, again.lookups())
	assert.Equal(t, map[uint32]Lookup{
		78:         {Name: "Wish-Keeper Snare Refit", Defined: true},
		79:         {Defined: true, Redacted: true},
		80:         {Defined: true},
		2576899547: {Name: "Guided Game Ticket", Defined: true},
	}, got.Lookups)
}

// TestNameStoreUnwritableCache pins that a store whose cache directory it
// cannot write still names the items, keeps the answers in memory, and logs
// the failure.
func TestNameStoreUnwritableCache(t *testing.T) {
	t.Parallel()

	// A regular file stands where the cache directory belongs.
	dir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(dir, nil, 0o644))

	var buf bytes.Buffer

	source := newFakeItems()
	store := NewNameStore(source, dir, WithNameLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

	for range 2 {
		got := store.Name(t.Context(), namedDefs("v1"), []uint32{78})
		assert.Equal(t, "Wish-Keeper Snare Refit", got.ItemName(78))
	}

	assert.Equal(t, []uint32{78}, source.lookups())

	var record struct {
		Level string `json:"level"`
		Msg   string `json:"msg"`
		Path  string `json:"path"`
		Error string `json:"error"`
	}

	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	assert.Equal(t, "WARN", record.Level)
	assert.Equal(t, "writing item name cache", record.Msg)
	assert.Equal(t, cachePath(dir, namesCachePrefix, "v1"), record.Path)
	assert.Contains(t, record.Error, "creating cache directory")
}

// TestNameStoreLogsFailedLookup pins that a store logs each lookup that
// fails and logs nothing for a lookup that Bungie answers.
func TestNameStoreLogsFailedLookup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	store := NewNameStore(newFakeItems(), "", WithNameLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

	got := store.Name(t.Context(), namedDefs("v1"), []uint32{78})
	assert.Equal(t, "Wish-Keeper Snare Refit", got.ItemName(78))
	assert.Empty(t, buf.String(), "a lookup that Bungie answers logs nothing")

	got = store.Name(t.Context(), namedDefs("v1"), []uint32{82})
	require.ErrorIs(t, got.Lookups[82].Err, errLookup)

	var record struct {
		Level string `json:"level"`
		Msg   string `json:"msg"`
		Error string `json:"error"`
		Hash  uint32 `json:"hash"`
	}

	dec := json.NewDecoder(&buf)
	require.NoError(t, dec.Decode(&record))
	assert.Equal(t, "WARN", record.Level)
	assert.Equal(t, "looking up item name", record.Msg)
	assert.Equal(t, uint32(82), record.Hash)
	assert.Contains(t, record.Error, errLookup.Error())
	assert.False(t, dec.More(), "the failed lookup logs one record")
}

// hookItems is an [ItemSource] that runs hook before it passes each lookup
// to source.
type hookItems struct {
	source *fakeItems
	hook   func()
}

func (h hookItems) ItemDefinition(ctx context.Context, hash uint32) (*bungie.ItemDefinition, error) {
	h.hook()

	return h.source.ItemDefinition(ctx, hash)
}

// TestNameStoreKeepsAnswersSavedDuringLookups pins that a store keeps the
// answers that another process saves while the lookups of the store run.
func TestNameStoreKeepsAnswersSavedDuringLookups(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	other := NewNameStore(newFakeItems(), dir)
	source := hookItems{
		source: newFakeItems(),
		hook: func() {
			other.Name(t.Context(), namedDefs("v1"), []uint32{79})
		},
	}

	NewNameStore(source, dir).Name(t.Context(), namedDefs("v1"), []uint32{78})

	again := newFakeItems()

	got := NewNameStore(again, dir).Name(t.Context(), namedDefs("v1"), []uint32{78, 79})
	assert.Empty(t, again.lookups())
	assert.Equal(t, map[uint32]Lookup{
		78: {Name: "Wish-Keeper Snare Refit", Defined: true},
		79: {Defined: true, Redacted: true},
	}, got.Lookups)
}

// TestLockCache pins that the lock of the cache directory has one holder
// at a time.
func TestLockCache(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "cache")

	unlock, err := lockCache(dir)
	require.NoError(t, err)

	done := make(chan error)

	go func() {
		second, err := lockCache(dir)
		if err == nil {
			second()
		}

		done <- err
	}()

	select {
	case <-done:
		require.Fail(t, "a second holder took the lock while the first held it")
	case <-time.After(100 * time.Millisecond):
	}

	unlock()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "the second holder did not take the lock after the first released it")
	}
}

func TestNameStoreIgnoresBadCache(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content string
	}{
		"file that does not decode": {
			content: "{",
		},
		"file of another manifest version": {
			content: `{"version": "v0", "answers": {"78": {"name": "Old Name", "defined": true}}}`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := cachePath(dir, namesCachePrefix, "v1")
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o644))

			source := newFakeItems()

			got := NewNameStore(source, dir).Name(t.Context(), namedDefs("v1"), []uint32{78})
			assert.Equal(t, "Wish-Keeper Snare Refit", got.ItemName(78))
			assert.Equal(t, []uint32{78}, source.lookups())
		})
	}
}

// TestNameStoreTimeout pins that the timeout bounds the lookups of one
// call and that the store runs several lookups at a time.
func TestNameStoreTimeout(t *testing.T) {
	t.Parallel()

	source := newFakeItems()
	source.block = true

	hashes := []uint32{78, 79, 80, 81, 82, 2576899547}
	store := NewNameStore(source, "", WithLookupTimeout(250*time.Millisecond))

	got := store.Name(t.Context(), namedDefs("v1"), hashes)

	for _, hash := range hashes {
		require.ErrorIs(t, got.Lookups[hash].Err, context.DeadlineExceeded, "lookup of %d", hash)
	}

	assert.Len(t, source.lookups(), len(hashes))
	assert.Equal(t, lookupWorkers, source.most)
}

func TestLookupAnswered(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		lookup Lookup
		want   bool
	}{
		"definition": {
			lookup: Lookup{Name: "Guided Game Ticket", Defined: true},
			want:   true,
		},
		"no definition": {
			want: true,
		},
		"failed lookup": {
			lookup: Lookup{Err: errLookup},
		},
		"skipped hash": {
			lookup: Lookup{Skipped: true},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.lookup.Answered())
		})
	}
}
