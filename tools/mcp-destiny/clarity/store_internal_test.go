package clarity

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/claritytest"
)

// testClock is a clock that a test moves.
type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time { return c.now }

// newTestClock returns a clock that starts after the last upload of the
// fixture database.
func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, time.November, 14, 12, 0, 0, 0, time.UTC)}
}

// newTestStore returns a store that reads site and keeps its cache file
// in dir. A new store stands for a new server process.
func newTestStore(site *claritytest.Server, dir string, clock *testClock, opts ...StoreOption) *Store {
	opts = append([]StoreOption{WithClock(clock.Now)}, opts...)

	return NewStore(NewClient(WithURL(site.URL)), dir, opts...)
}

// logRecord is one entry of a JSON log.
type logRecord struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Path  string `json:"path"`
	Error string `json:"error"`
}

// logRecords decodes the entries that a JSON log handler wrote to buf.
func logRecords(t *testing.T, buf *bytes.Buffer) []logRecord {
	t.Helper()

	var out []logRecord

	dec := json.NewDecoder(buf)
	for dec.More() {
		var record logRecord

		require.NoError(t, dec.Decode(&record))

		out = append(out, record)
	}

	return out
}

// fixtureLines is the text of the plug that the fixture database
// describes.
func fixtureLines() []string {
	return claritytest.Fixture()[0].Lines
}

func TestStoreLoad(t *testing.T) {
	t.Parallel()

	site := claritytest.NewServer(t)
	clock := newTestClock()
	store := newTestStore(site, "", clock)

	state := store.Load(t.Context())
	require.NoError(t, state.Err)
	assert.Equal(t, claritytest.FixtureVersion, state.Database.Version)

	got, ok := state.Database.Description(claritytest.PlugExplosivePayload)
	require.True(t, ok)
	assert.Equal(t, "Explosive Payload", got.Name)
	assert.Equal(t, fixtureLines(), got.Lines)
	assert.Equal(t, claritytest.Fixture()[0].Uploaded, got.Uploaded)

	assert.Equal(t, 1, site.Checks())
	assert.Equal(t, 1, site.Downloads())

	// A call within the hour after the check sends no request.
	clock.now = clock.now.Add(recheck - time.Second)

	state = store.Load(t.Context())
	require.NoError(t, state.Err)
	assert.Equal(t, 1, site.Checks())

	// The next check finds the version of the held database.
	clock.now = clock.now.Add(time.Second)

	held := state.Database
	state = store.Load(t.Context())
	require.NoError(t, state.Err)
	assert.Same(t, held, state.Database)
	assert.Equal(t, 2, site.Checks())
	assert.Equal(t, 1, site.Downloads(), "the store downloads only another version")

	// Clarity publishes another version.
	site.Version = "1.6"
	site.Descriptions = claritytest.Descriptions(t, claritytest.Perk{
		Hash:  claritytest.PlugExplosivePayload,
		Name:  "Explosive Payload",
		Lines: []string{"Each bullet deals a third of its damage as an explosion."},
	})
	clock.now = clock.now.Add(recheck)

	state = store.Load(t.Context())
	require.NoError(t, state.Err)
	assert.Equal(t, "1.6", state.Database.Version)

	got, ok = state.Database.Description(claritytest.PlugExplosivePayload)
	require.True(t, ok)
	assert.Equal(t, []string{"Each bullet deals a third of its damage as an explosion."}, got.Lines)
	assert.Equal(t, 3, site.Checks())
	assert.Equal(t, 2, site.Downloads())
}

// TestStoreCache pins that a new process reads the descriptions from the
// cache file and downloads them only for another version.
func TestStoreCache(t *testing.T) {
	t.Parallel()

	site := claritytest.NewServer(t)
	clock := newTestClock()
	dir := filepath.Join(t.TempDir(), "cache")
	path := filepath.Join(dir, cacheFile)

	// A cache file of another layout and the cache file of another
	// store share the directory.
	stale := filepath.Join(dir, "clarity-v0.json")
	other := filepath.Join(dir, "defs-v11-a.json")

	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(stale, []byte(`{}`), 0o644))
	require.NoError(t, os.WriteFile(other, []byte(`{}`), 0o644))

	state := newTestStore(site, dir, clock).Load(t.Context())
	require.NoError(t, state.Err)
	assert.Equal(t, 1, site.Downloads())

	var content cacheContent

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &content))
	assert.Equal(t, claritytest.FixtureVersion, content.Version)
	assert.JSONEq(t, site.Descriptions, string(content.Descriptions))

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(t, []string{path, other}, files, "the write removes the cache file of the other layout")

	// A second process finds the version of the cache file on the site.
	second := newTestStore(site, dir, clock)

	held := second.Load(t.Context())
	require.NoError(t, held.Err)
	assert.Equal(t, claritytest.FixtureVersion, held.Database.Version)

	got, ok := held.Database.Description(claritytest.PlugExplosivePayload)
	require.True(t, ok)
	assert.Equal(t, fixtureLines(), got.Lines)
	assert.Equal(t, 2, site.Checks())
	assert.Equal(t, 1, site.Downloads(), "the second process reads the cache file")

	// The next check of the second process finds the version of its
	// database, so the process keeps the database and reads no file.
	clock.now = clock.now.Add(recheck)

	assert.Same(t, held.Database, second.Load(t.Context()).Database)
	assert.Equal(t, 3, site.Checks())
	assert.Equal(t, 1, site.Downloads())

	// A third process finds another version on the site.
	site.Version = "1.6"

	state = newTestStore(site, dir, clock).Load(t.Context())
	require.NoError(t, state.Err)
	assert.Equal(t, "1.6", state.Database.Version)
	assert.Equal(t, 2, site.Downloads())

	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &content))
	assert.Equal(t, "1.6", content.Version)
}

// TestStoreSharedCache pins that the stores of two processes share the
// downloads of the cache file. A store reads the version that the other
// store saved and downloads nothing.
func TestStoreSharedCache(t *testing.T) {
	t.Parallel()

	site := claritytest.NewServer(t)
	clock := newTestClock()
	dir := t.TempDir()

	first := newTestStore(site, dir, clock)
	second := newTestStore(site, dir, clock)

	require.NoError(t, first.Load(t.Context()).Err)
	require.NoError(t, second.Load(t.Context()).Err)
	assert.Equal(t, 1, site.Downloads(), "the second store reads the download of the first")

	// Clarity publishes another version, and the second store checks
	// the site first.
	site.Version = "1.6"
	site.Descriptions = claritytest.Descriptions(t, claritytest.Perk{
		Hash:  claritytest.PlugExplosivePayload,
		Name:  "Explosive Payload",
		Lines: []string{"Each bullet deals a third of its damage as an explosion."},
	})
	clock.now = clock.now.Add(recheck)

	state := second.Load(t.Context())
	require.NoError(t, state.Err)
	assert.Equal(t, "1.6", state.Database.Version)
	assert.Equal(t, 2, site.Downloads())

	state = first.Load(t.Context())
	require.NoError(t, state.Err)
	assert.Equal(t, "1.6", state.Database.Version)

	got, ok := state.Database.Description(claritytest.PlugExplosivePayload)
	require.True(t, ok)
	assert.Equal(t, []string{"Each bullet deals a third of its damage as an explosion."}, got.Lines)
	assert.Equal(t, 4, site.Checks())
	assert.Equal(t, 2, site.Downloads(), "the first store reads the download of the second")
}

// TestStoreServesOlderCache pins that a new process serves the cache file
// of an older version when the download of the version that the site
// names does not succeed.
func TestStoreServesOlderCache(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	site := claritytest.NewServer(t)
	clock := newTestClock()
	dir := t.TempDir()

	require.NoError(t, newTestStore(site, dir, clock).Load(t.Context()).Err)

	site.Version = "1.6"
	site.Descriptions = `{}`

	store := newTestStore(site, dir, clock, WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

	state := store.Load(t.Context())
	require.NoError(t, state.Err)
	assert.Equal(t, claritytest.FixtureVersion, state.Database.Version)
	assert.Equal(t, 2, site.Downloads())

	assert.Equal(t, []logRecord{{
		Level: "WARN",
		Msg:   "refreshing Clarity descriptions",
		Error: errNoDescriptions.Error(),
	}}, logRecords(t, &buf))
}

// TestStoreFailedDownload pins that a store without a database reports
// the reason and waits before it asks the site again.
func TestStoreFailedDownload(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	site := claritytest.NewServer(t)
	site.Status = http.StatusServiceUnavailable

	clock := newTestClock()
	dir := t.TempDir()
	store := newTestStore(site, dir, clock, WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

	reason := "descriptions unavailable: reading the version: " + site.URL + "/versions.json returned status 503"

	state := store.Load(t.Context())
	require.ErrorIs(t, state.Err, ErrUnavailable)
	require.EqualError(t, state.Err, reason)
	assert.Nil(t, state.Database)
	assert.Equal(t, 1, site.Checks())
	assert.Equal(t, 0, site.Downloads())

	assert.Equal(t, []logRecord{{
		Level: "WARN",
		Msg:   "refreshing Clarity descriptions",
		Error: "reading the version: " + site.URL + "/versions.json returned status 503",
	}}, logRecords(t, &buf))

	// The site answers again, and the store still waits.
	site.Status = 0
	clock.now = clock.now.Add(retry - time.Second)

	state = store.Load(t.Context())
	require.EqualError(t, state.Err, reason)
	assert.Equal(t, 1, site.Checks(), "a call within the wait sends no request")

	clock.now = clock.now.Add(time.Second)

	state = store.Load(t.Context())
	require.NoError(t, state.Err)
	assert.Equal(t, claritytest.FixtureVersion, state.Database.Version)
	assert.Equal(t, 2, site.Checks())
	assert.Equal(t, 1, site.Downloads())

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	require.NoError(t, err)
	assert.Equal(
		t,
		[]string{filepath.Join(dir, cacheFile)},
		files,
		"only the download that succeeded wrote a cache file",
	)
}

// TestStoreKeepsDatabase pins that a check that does not succeed keeps
// the database that the store holds.
func TestStoreKeepsDatabase(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// fail makes the site fail the second check.
		fail func(t *testing.T, site *claritytest.Server)
		// err is the text of the logged error. "{url}" stands for the
		// base URL of the site.
		err string
		// downloads counts the downloads after the second check.
		downloads int
	}{
		"the site is down": {
			fail:      func(_ *testing.T, site *claritytest.Server) { site.Status = http.StatusBadGateway },
			err:       "reading the version: {url}/versions.json returned status 502",
			downloads: 1,
		},
		"the other version holds no description": {
			fail: func(_ *testing.T, site *claritytest.Server) {
				site.Version = "1.6"
				site.Descriptions = `{}`
			},
			err:       errNoDescriptions.Error(),
			downloads: 2,
		},
		"the other version does not decode": {
			fail: func(_ *testing.T, site *claritytest.Server) {
				site.Version = "1.6"
				site.Descriptions = `[]`
			},
			err: "decoding the descriptions file: " +
				"json: cannot unmarshal array into Go value of type map[string]json.RawMessage",
			downloads: 2,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			site := claritytest.NewServer(t)
			clock := newTestClock()
			dir := t.TempDir()
			store := newTestStore(site, dir, clock, WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

			first := store.Load(t.Context())
			require.NoError(t, first.Err)

			cached, err := os.ReadFile(filepath.Join(dir, cacheFile))
			require.NoError(t, err)

			tc.fail(t, site)

			clock.now = clock.now.Add(recheck)

			state := store.Load(t.Context())
			require.NoError(t, state.Err)
			assert.Same(t, first.Database, state.Database)
			assert.Equal(t, 2, site.Checks())
			assert.Equal(t, tc.downloads, site.Downloads())

			records := logRecords(t, &buf)
			require.Len(t, records, 1)
			assert.Equal(t, "refreshing Clarity descriptions", records[0].Msg)
			assert.Equal(t, strings.ReplaceAll(tc.err, "{url}", site.URL), records[0].Error)

			after, err := os.ReadFile(filepath.Join(dir, cacheFile))
			require.NoError(t, err)
			assert.Equal(t, cached, after, "the cache file keeps the version that decodes")

			// The store asks the site again after the wait, and not
			// on each call.
			clock.now = clock.now.Add(retry - time.Second)

			store.Load(t.Context())
			assert.Equal(t, 2, site.Checks())

			clock.now = clock.now.Add(time.Second)

			store.Load(t.Context())
			assert.Equal(t, 3, site.Checks())
		})
	}
}

// TestStoreCanceledCall pins that a call whose caller canceled it does
// not count as a check, so the next call asks the site again.
func TestStoreCanceledCall(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// cached makes an earlier process write the cache file.
		cached bool
		// checks counts the checks after the call that follows the
		// canceled one.
		checks int
	}{
		"empty cache directory": {
			checks: 1,
		},
		"cache file of an earlier process": {
			cached: true,
			checks: 2,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			site := claritytest.NewServer(t)
			clock := newTestClock()
			dir := t.TempDir()

			if tc.cached {
				require.NoError(t, newTestStore(site, dir, clock).Load(t.Context()).Err)
			}

			store := newTestStore(site, dir, clock, WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			state := store.Load(ctx)
			if tc.cached {
				require.NoError(t, state.Err)
				assert.Equal(t, claritytest.FixtureVersion, state.Database.Version)
			} else {
				require.ErrorIs(t, state.Err, ErrUnavailable)
				require.ErrorIs(t, state.Err, context.Canceled)
				assert.Nil(t, state.Database)
			}

			assert.Empty(t, logRecords(t, &buf), "a canceled call logs nothing")

			// The next call, at the same time, checks the version.
			state = store.Load(t.Context())
			require.NoError(t, state.Err)
			assert.Equal(t, claritytest.FixtureVersion, state.Database.Version)
			assert.Equal(t, tc.checks, site.Checks())
			assert.Equal(t, 1, site.Downloads())
		})
	}
}

// TestStoreServesCacheWhileSiteIsDown pins that a new process serves the
// descriptions of the cache file when the site does not answer.
func TestStoreServesCacheWhileSiteIsDown(t *testing.T) {
	t.Parallel()

	site := claritytest.NewServer(t)
	clock := newTestClock()
	dir := t.TempDir()

	require.NoError(t, newTestStore(site, dir, clock).Load(t.Context()).Err)

	site.Status = http.StatusServiceUnavailable

	state := newTestStore(site, dir, clock).Load(t.Context())
	require.NoError(t, state.Err)
	assert.Equal(t, claritytest.FixtureVersion, state.Database.Version)

	got, ok := state.Database.Description(claritytest.PlugExplosivePayload)
	require.True(t, ok)
	assert.Equal(t, fixtureLines(), got.Lines)
	assert.Equal(t, 1, site.Downloads())
}

// TestStoreIgnoresBadCache pins that a store downloads the descriptions
// when its cache file does not decode, and that it logs the reason.
func TestStoreIgnoresBadCache(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content string
		err     string
	}{
		"half a file": {
			content: `{"version": "1.5", "descriptions": {`,
			err:     "unexpected end of JSON input",
		},
		"no version": {
			content: `{"descriptions": {}}`,
			err:     errNoVersion.Error(),
		},
		"no description": {
			content: `{"version": "1.5", "descriptions": {}}`,
			err:     errNoDescriptions.Error(),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			site := claritytest.NewServer(t)
			dir := t.TempDir()
			path := filepath.Join(dir, cacheFile)

			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o644))

			store := newTestStore(site, dir, newTestClock(), WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

			state := store.Load(t.Context())
			require.NoError(t, state.Err)
			assert.Equal(t, 1, site.Downloads())

			_, ok := state.Database.Description(claritytest.PlugExplosivePayload)
			assert.True(t, ok)

			assert.Equal(t, []logRecord{{
				Level: "WARN",
				Msg:   "ignoring unreadable Clarity cache",
				Path:  path,
				Error: tc.err,
			}}, logRecords(t, &buf))

			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, string(raw), `"version":"1.5"`, "the download replaces the cache file")
		})
	}
}

// TestStoreUnwritableCache pins that a store whose cache directory it
// cannot write still serves the descriptions and logs the failure.
func TestStoreUnwritableCache(t *testing.T) {
	t.Parallel()

	// A regular file stands where the cache directory belongs.
	dir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(dir, nil, 0o644))

	var buf bytes.Buffer

	site := claritytest.NewServer(t)
	store := newTestStore(site, dir, newTestClock(), WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))

	for range 2 {
		state := store.Load(t.Context())
		require.NoError(t, state.Err)
		assert.Equal(t, claritytest.FixtureVersion, state.Database.Version)
	}

	assert.Equal(t, 1, site.Downloads())

	records := logRecords(t, &buf)
	require.Len(t, records, 1)
	assert.Equal(t, "WARN", records[0].Level)
	assert.Equal(t, "writing Clarity cache", records[0].Msg)
	assert.Equal(t, filepath.Join(dir, cacheFile), records[0].Path)
	assert.Contains(t, records[0].Error, "creating cache directory")
}
