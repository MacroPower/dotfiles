package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")

	store, err := OpenStore(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	return store
}

func TestStoreRecord_AllOutcomes(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	tests := map[string]struct {
		rec FetchRecord
	}{
		"ok with metadata": {
			rec: FetchRecord{
				URL: "https://example.com/a", Host: "example.com",
				Outcome: OutcomeOK, StatusCode: 200,
				ContentType: "text/plain", ResponseBytes: 100,
				OutputBytes: 50, MaxLength: 5000, DurationMs: 42,
			},
		},
		"denied": {
			rec: FetchRecord{
				URL: "https://blocked.com/x", Host: "blocked.com",
				Outcome: OutcomeDenied, Error: "rule: blocked",
			},
		},
		"robots_denied": {
			rec: FetchRecord{
				URL: "https://example.com/secret", Host: "example.com",
				Outcome: OutcomeRobotsDenied, Error: "robots disallow",
			},
		},
		"http_error": {
			rec: FetchRecord{
				URL: "https://example.com/404", Host: "example.com",
				Outcome: OutcomeHTTPError, StatusCode: 404, Error: "status 404",
			},
		},
		"fetch_error transport zero status": {
			rec: FetchRecord{
				URL: "https://gone.example/x", Host: "gone.example",
				Outcome: OutcomeFetchError, StatusCode: 0,
				Error: "connection refused",
			},
		},
		"invalid_url no host": {
			rec: FetchRecord{
				URL:     "::not-a-url",
				Outcome: OutcomeInvalidURL, Error: "bad uri",
			},
		},
		"cache hit zero status": {
			rec: FetchRecord{
				URL: "https://example.com/cached", Host: "example.com",
				Outcome: OutcomeOK, CacheHit: 1, OutputBytes: 50,
			},
		},
		"raw mode flag": {
			rec: FetchRecord{
				URL: "https://example.com/raw", Host: "example.com",
				Outcome: OutcomeOK, StatusCode: 200, RawMode: 1,
			},
		},
		"truncated flag": {
			rec: FetchRecord{
				URL: "https://example.com/big", Host: "example.com",
				Outcome: OutcomeOK, StatusCode: 200, Truncated: 1,
				OutputBytes: 5000,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, store.Record(ctx, tt.rec))
		})
	}
}

func TestStoreSummary_Empty(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	got, err := store.Summary(ctx, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 0, got.Total)
	assert.True(t, got.Earliest.IsZero())
	assert.True(t, got.Latest.IsZero())
	assert.Empty(t, got.OutcomeCounts)
}

func TestStoreSummary_AllTime(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://a.test/1", Host: "a.test",
		Outcome: OutcomeOK, StatusCode: 200,
		ResponseBytes: 100, OutputBytes: 50, DurationMs: 100,
	}))
	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://a.test/2", Host: "a.test",
		Outcome: OutcomeOK, StatusCode: 200,
		ResponseBytes: 200, OutputBytes: 150, DurationMs: 200,
		CacheHit: 1,
	}))
	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://b.test/1", Host: "b.test",
		Outcome: OutcomeDenied, Error: "blocked",
	}))

	got, err := store.Summary(ctx, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 3, got.Total)
	assert.Equal(t, 2, got.OutcomeCounts[OutcomeOK])
	assert.Equal(t, 1, got.OutcomeCounts[OutcomeDenied])
	assert.Equal(t, int64(300), got.TotalResponseBytes)
	assert.Equal(t, int64(200), got.TotalOutputBytes)
	assert.InDelta(t, 100.0, got.AvgDurationMs, 0.01)
	assert.Equal(t, 1, got.CacheHits)
	assert.False(t, got.Earliest.IsZero())
	assert.False(t, got.Latest.IsZero())
}

func TestStoreSummary_SinceWindow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://a.test/old", Host: "a.test", Outcome: OutcomeOK,
	}))

	// Backdate the row.
	_, err := store.db.ExecContext(ctx,
		`UPDATE fetches SET ts = datetime('now', '-2 hours') WHERE url = ?`,
		"https://a.test/old")
	require.NoError(t, err)

	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://a.test/new", Host: "a.test", Outcome: OutcomeOK,
	}))

	got, err := store.Summary(ctx, time.Now().Add(-30*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, 1, got.Total, "since window must exclude old row")
	assert.Equal(t, 1, got.OutcomeCounts[OutcomeOK])
}

func TestStoreOutcomeCounts(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	for range 5 {
		require.NoError(t, store.Record(ctx, FetchRecord{
			URL: "https://a.test/", Host: "a.test", Outcome: OutcomeOK,
		}))
	}

	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://b.test/", Host: "b.test", Outcome: OutcomeDenied,
	}))

	got, err := store.OutcomeCounts(ctx, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 5, got[OutcomeOK])
	assert.Equal(t, 1, got[OutcomeDenied])
}

func TestStoreTopHosts(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	for range 3 {
		require.NoError(t, store.Record(ctx, FetchRecord{
			URL: "https://a.test/", Host: "a.test", Outcome: OutcomeOK,
		}))
	}

	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://b.test/", Host: "b.test", Outcome: OutcomeOK,
	}))
	// Empty host (e.g. invalid_url) must be excluded.
	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "::bad", Host: "", Outcome: OutcomeInvalidURL,
	}))

	got, err := store.TopHosts(ctx, time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "a.test", got[0].Host)
	assert.Equal(t, 3, got[0].Count)
	assert.Equal(t, "b.test", got[1].Host)
	assert.Equal(t, 1, got[1].Count)
}

func TestStoreTopHosts_LimitDefault(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://a.test/", Host: "a.test", Outcome: OutcomeOK,
	}))

	got, err := store.TopHosts(ctx, time.Time{}, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestStoreRecentFetches_OrderAndError(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://a.test/1", Host: "a.test", Outcome: OutcomeOK,
		StatusCode: 200,
	}))
	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://a.test/2", Host: "a.test", Outcome: OutcomeHTTPError,
		StatusCode: 500, Error: "boom",
	}))

	got, err := store.RecentFetches(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)

	// Newest first.
	assert.Equal(t, "https://a.test/2", got[0].URL)
	assert.Equal(t, "boom", got[0].Error,
		"Error column must be scanned even though the default tail formatter hides it")
	assert.Equal(t, "https://a.test/1", got[1].URL)
	assert.Empty(t, got[1].Error)
}

func TestStoreRecentFetches_LimitZero(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://a.test/", Host: "a.test", Outcome: OutcomeOK,
	}))

	got, err := store.RecentFetches(ctx, 0)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestPruneStale_Beyond90Days(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://fresh.test/", Host: "fresh.test", Outcome: OutcomeOK,
	}))
	require.NoError(t, store.Record(ctx, FetchRecord{
		URL: "https://stale.test/", Host: "stale.test", Outcome: OutcomeOK,
	}))

	_, err := store.db.ExecContext(ctx,
		`UPDATE fetches SET ts = datetime('now', '-91 days') WHERE host = ?`,
		"stale.test")
	require.NoError(t, err)

	require.NoError(t, store.pruneStale(ctx))

	var count int

	err = store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fetches`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "stale row pruned, fresh row survives")
}

func TestParseSinceFlag(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  time.Duration
		err   bool
	}{
		"empty all time":   {input: "", want: 0},
		"hours":            {input: "24h", want: 24 * time.Hour},
		"minutes":          {input: "30m", want: 30 * time.Minute},
		"integer days":     {input: "7d", want: 168 * time.Hour},
		"single day":       {input: "1d", want: 24 * time.Hour},
		"composite no day": {input: "7d12h", err: true},
		"fractional day":   {input: "1.5d", err: true},
		"capital D":        {input: "7D", err: true},
		"unparseable":      {input: "abc", err: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSinceFlag(tt.input)
			if tt.err {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBoolToInt(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, boolToInt(true))
	assert.Equal(t, 0, boolToInt(false))
}
