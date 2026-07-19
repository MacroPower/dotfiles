package store_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/store"
)

func newTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")

	st, err := store.Open(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, st.Close())
	})

	return st, dbPath
}

// backdate rewrites the `ts` of rows for host through a second connection
// to the known database file, simulating rows recorded in the past.
// [store.Store.Record] always stamps "now", so aging rows is the one
// thing the public API cannot do.
func backdate(t *testing.T, dbPath, host, interval string) {
	t.Helper()

	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", dbPath))
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, db.Close()) })

	_, err = db.ExecContext(t.Context(),
		`UPDATE fetches SET ts = datetime('now', ?) WHERE host = ?`, interval, host)
	require.NoError(t, err)
}

func TestStoreRecord_AllOutcomes(t *testing.T) {
	t.Parallel()

	st, _ := newTestStore(t)
	ctx := t.Context()

	tests := map[string]struct {
		rec store.FetchRecord
	}{
		"ok with metadata": {
			rec: store.FetchRecord{
				URL: "https://example.com/a", Host: "example.com",
				Outcome: store.OutcomeOK, StatusCode: 200,
				ContentType: "text/plain", ResponseBytes: 100,
				OutputBytes: 50, MaxLength: 5000, DurationMs: 42,
			},
		},
		"denied": {
			rec: store.FetchRecord{
				URL: "https://blocked.com/x", Host: "blocked.com",
				Outcome: store.OutcomeDenied, Error: "rule: blocked",
			},
		},
		"robots_denied": {
			rec: store.FetchRecord{
				URL: "https://example.com/secret", Host: "example.com",
				Outcome: store.OutcomeRobotsDenied, Error: "robots disallow",
			},
		},
		"http_error": {
			rec: store.FetchRecord{
				URL: "https://example.com/404", Host: "example.com",
				Outcome: store.OutcomeHTTPError, StatusCode: 404, Error: "status 404",
			},
		},
		"fetch_error transport zero status": {
			rec: store.FetchRecord{
				URL: "https://gone.example/x", Host: "gone.example",
				Outcome: store.OutcomeFetchError, StatusCode: 0,
				Error: "connection refused",
			},
		},
		"invalid_url no host": {
			rec: store.FetchRecord{
				URL:     "::not-a-url",
				Outcome: store.OutcomeInvalidURL, Error: "bad uri",
			},
		},
		"cache hit zero status": {
			rec: store.FetchRecord{
				URL: "https://example.com/cached", Host: "example.com",
				Outcome: store.OutcomeOK, CacheHit: 1, OutputBytes: 50,
			},
		},
		"raw mode flag": {
			rec: store.FetchRecord{
				URL: "https://example.com/raw", Host: "example.com",
				Outcome: store.OutcomeOK, StatusCode: 200, RawMode: 1,
			},
		},
		"truncated flag": {
			rec: store.FetchRecord{
				URL: "https://example.com/big", Host: "example.com",
				Outcome: store.OutcomeOK, StatusCode: 200, Truncated: 1,
				OutputBytes: 5000,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, st.Record(ctx, tt.rec))
		})
	}
}

func TestStoreSummary_Empty(t *testing.T) {
	t.Parallel()

	st, _ := newTestStore(t)

	got, err := st.Summary(t.Context(), time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 0, got.Total)
	assert.True(t, got.Earliest.IsZero())
	assert.True(t, got.Latest.IsZero())
	assert.Empty(t, got.OutcomeCounts)
}

func TestStoreSummary_AllTime(t *testing.T) {
	t.Parallel()

	st, _ := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://a.test/1", Host: "a.test",
		Outcome: store.OutcomeOK, StatusCode: 200,
		ResponseBytes: 100, OutputBytes: 50, DurationMs: 100,
	}))
	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://a.test/2", Host: "a.test",
		Outcome: store.OutcomeOK, StatusCode: 200,
		ResponseBytes: 200, OutputBytes: 150, DurationMs: 200,
		CacheHit: 1,
	}))
	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://b.test/1", Host: "b.test",
		Outcome: store.OutcomeDenied, Error: "blocked",
	}))

	got, err := st.Summary(ctx, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 3, got.Total)
	assert.Equal(t, 2, got.OutcomeCounts[store.OutcomeOK])
	assert.Equal(t, 1, got.OutcomeCounts[store.OutcomeDenied])
	assert.Equal(t, int64(300), got.TotalResponseBytes)
	assert.Equal(t, int64(200), got.TotalOutputBytes)
	assert.InDelta(t, 100.0, got.AvgDurationMs, 0.01)
	assert.Equal(t, 1, got.CacheHits)
	assert.False(t, got.Earliest.IsZero())
	assert.False(t, got.Latest.IsZero())
}

func TestStoreSummary_SinceWindow(t *testing.T) {
	t.Parallel()

	st, _ := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://a.test/now", Host: "a.test", Outcome: store.OutcomeOK,
	}))

	// A since cutoff in the future excludes the just-recorded row; one in
	// the past includes it. This exercises the `ts >= since` filter
	// without having to age a row.
	future, err := st.Summary(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 0, future.Total, "future since cutoff must exclude the now row")

	past, err := st.Summary(ctx, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, past.Total, "past since cutoff must include the now row")
}

func TestStoreOutcomeCounts(t *testing.T) {
	t.Parallel()

	st, _ := newTestStore(t)
	ctx := t.Context()

	for range 5 {
		require.NoError(t, st.Record(ctx, store.FetchRecord{
			URL: "https://a.test/", Host: "a.test", Outcome: store.OutcomeOK,
		}))
	}

	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://b.test/", Host: "b.test", Outcome: store.OutcomeDenied,
	}))

	got, err := st.OutcomeCounts(ctx, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, 5, got[store.OutcomeOK])
	assert.Equal(t, 1, got[store.OutcomeDenied])
}

func TestStoreTopHosts(t *testing.T) {
	t.Parallel()

	st, _ := newTestStore(t)
	ctx := t.Context()

	for range 3 {
		require.NoError(t, st.Record(ctx, store.FetchRecord{
			URL: "https://a.test/", Host: "a.test", Outcome: store.OutcomeOK,
		}))
	}

	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://b.test/", Host: "b.test", Outcome: store.OutcomeOK,
	}))
	// Empty host (e.g. invalid_url) must be excluded.
	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "::bad", Host: "", Outcome: store.OutcomeInvalidURL,
	}))

	got, err := st.TopHosts(ctx, time.Time{}, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "a.test", got[0].Host)
	assert.Equal(t, 3, got[0].Count)
	assert.Equal(t, "b.test", got[1].Host)
	assert.Equal(t, 1, got[1].Count)
}

func TestStoreTopHosts_LimitDefault(t *testing.T) {
	t.Parallel()

	st, _ := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://a.test/", Host: "a.test", Outcome: store.OutcomeOK,
	}))

	got, err := st.TopHosts(ctx, time.Time{}, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestStoreRecentFetches_OrderAndError(t *testing.T) {
	t.Parallel()

	st, _ := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://a.test/1", Host: "a.test", Outcome: store.OutcomeOK,
		StatusCode: 200,
	}))
	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://a.test/2", Host: "a.test", Outcome: store.OutcomeHTTPError,
		StatusCode: 500, Error: "boom",
	}))

	got, err := st.RecentFetches(ctx, 10)
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

	st, _ := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://a.test/", Host: "a.test", Outcome: store.OutcomeOK,
	}))

	got, err := st.RecentFetches(ctx, 0)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestPrune_Beyond90Days(t *testing.T) {
	t.Parallel()

	st, dbPath := newTestStore(t)
	ctx := t.Context()

	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://fresh.test/", Host: "fresh.test", Outcome: store.OutcomeOK,
	}))
	require.NoError(t, st.Record(ctx, store.FetchRecord{
		URL: "https://stale.test/", Host: "stale.test", Outcome: store.OutcomeOK,
	}))

	backdate(t, dbPath, "stale.test", "-91 days")

	require.NoError(t, st.Prune(ctx))

	rows, err := st.RecentFetches(ctx, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1, "stale row pruned, fresh row survives")
	assert.Equal(t, "fresh.test", rows[0].Host)
}

func TestBoolToInt(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, store.BoolToInt(true))
	assert.Equal(t, 0, store.BoolToInt(false))
}

// v1Schema is the fetches DDL as it shipped at schema version 1, before
// the render columns existed. The migration tests build databases with
// it to prove Open upgrades in place. The version stamp is applied
// separately: the v1 binary wrote the DDL and the stamp in separate
// statements, so a database can hold this shape at user_version 0.
const v1Schema = `
CREATE TABLE IF NOT EXISTS fetches (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              TEXT    NOT NULL DEFAULT (datetime('now')),
    url             TEXT    NOT NULL,
    host            TEXT    NOT NULL DEFAULT '',
    outcome         TEXT    NOT NULL,
    status_code     INTEGER NOT NULL DEFAULT 0,
    content_type    TEXT    NOT NULL DEFAULT '',
    response_bytes  INTEGER NOT NULL DEFAULT 0,
    output_bytes    INTEGER NOT NULL DEFAULT 0,
    raw_mode        INTEGER NOT NULL DEFAULT 0,
    max_length      INTEGER NOT NULL DEFAULT 0,
    start_index     INTEGER NOT NULL DEFAULT 0,
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    cache_hit       INTEGER NOT NULL DEFAULT 0,
    truncated       INTEGER NOT NULL DEFAULT 0,
    error           TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_fetches_ts          ON fetches(ts);
CREATE INDEX IF NOT EXISTS idx_fetches_host        ON fetches(host);
CREATE INDEX IF NOT EXISTS idx_fetches_outcome_ts  ON fetches(outcome, ts);
`

func TestStoreMigrateV1(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stampedVersion int
	}{
		"stamped v1": {stampedVersion: 1},
		// A crash or busy-timeout between the v1 binary's DDL and its
		// version stamp leaves the v1 table shape at user_version 0;
		// Open must still add the render columns rather than trusting
		// the stamp.
		"unstamped v1 shape": {stampedVersion: 0},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			testStoreMigrateV1(t, tc.stampedVersion)
		})
	}
}

func testStoreMigrateV1(t *testing.T, stampedVersion int) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "v1.db")

	db, err := sql.Open("sqlite", "file:"+dbPath)
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(), v1Schema)
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(),
		fmt.Sprintf("PRAGMA user_version = %d", stampedVersion))
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(),
		`INSERT INTO fetches (url, host, outcome) VALUES (?, ?, ?)`,
		"https://old.test/", "old.test", store.OutcomeOK)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	st, err := store.Open(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, st.Close()) })

	// The pre-migration row reads back with zero-value render fields.
	rows, err := st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "old.test", rows[0].Host)
	assert.Equal(t, 0, rows[0].RenderJS)
	assert.Equal(t, 0, rows[0].RenderOK)

	// New rows round-trip the render fields through the added columns.
	require.NoError(t, st.Record(t.Context(), store.FetchRecord{
		URL: "https://new.test/", Host: "new.test", Outcome: store.OutcomeOK,
		RenderJS: 1, RenderOK: 1,
	}))

	rows, err = st.RecentFetches(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, 1, rows[0].RenderJS)
	assert.Equal(t, 1, rows[0].RenderOK)

	// A second Open of the migrated database is a clean no-op.
	st2, err := store.Open(t.Context(), dbPath)
	require.NoError(t, err)
	require.NoError(t, st2.Close())
}

func TestStoreOpen_NewerSchemaVersion(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "future.db")

	db, err := sql.Open("sqlite", "file:"+dbPath)
	require.NoError(t, err)

	_, err = db.ExecContext(t.Context(), `PRAGMA user_version = 999`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = store.Open(t.Context(), dbPath)
	require.ErrorIs(t, err, store.ErrSchemaTooNew)
}
