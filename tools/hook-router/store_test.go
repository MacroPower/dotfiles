package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPID is the canonical Claude-Code-window PID used in tests that
// only exercise a single window's view of pending_plans. Tests that
// validate per-window isolation pass explicit distinct PIDs instead.
const testPID = "12345"

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

func TestStoreSession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	count, planPath, baseSHA, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, "", planPath)
	assert.Equal(t, "", baseSHA)
}

func TestStoreSession_Idempotent(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, err := store.IncrementExitPlanCount(ctx, "s1")
	require.NoError(t, err)

	// Calling Session again should not reset the counter.
	count, _, _, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestStoreIncrementExitPlanCount(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	count, err := store.IncrementExitPlanCount(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	count, err = store.IncrementExitPlanCount(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count, err = store.IncrementExitPlanCount(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestStoreSetPlanPath(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	err := store.SetPlanPath(ctx, "s1", "/path/to/plan.md", "abc123")
	require.NoError(t, err)

	_, planPath, baseSHA, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "/path/to/plan.md", planPath)
	assert.Equal(t, "abc123", baseSHA)
}

func TestStoreSetPlanPath_Overwrite(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	err := store.SetPlanPath(ctx, "s1", "/old.md", "old-sha")
	require.NoError(t, err)

	err = store.SetPlanPath(ctx, "s1", "/new.md", "new-sha")
	require.NoError(t, err)

	_, planPath, baseSHA, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "/new.md", planPath)
	assert.Equal(t, "new-sha", baseSHA)
}

func TestStoreResetSession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, _ = store.IncrementExitPlanCount(ctx, "s1")
	_ = store.SetPlanPath(ctx, "s1", "/plan.md", "sha1")

	err := store.ResetSession(ctx, "s1")
	require.NoError(t, err)

	count, planPath, baseSHA, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, "", planPath)
	assert.Equal(t, "", baseSHA)
}

func TestStoreClearSession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, _ = store.IncrementExitPlanCount(ctx, "s1")

	err := store.ClearSession(ctx, "s1")
	require.NoError(t, err)

	// Session should be fresh after clear.
	count, _, _, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestSetAskFingerprint(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	err := store.SetAskFingerprint(ctx, "s1", "abc123", "def456")
	require.NoError(t, err)

	headSHA, wtHash, err := store.AskFingerprint(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "abc123", headSHA)
	assert.Equal(t, "def456", wtHash)
}

func TestSetAskFingerprint_Overwrite(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	err := store.SetAskFingerprint(ctx, "s1", "old-head", "old-wt")
	require.NoError(t, err)

	err = store.SetAskFingerprint(ctx, "s1", "new-head", "new-wt")
	require.NoError(t, err)

	headSHA, wtHash, err := store.AskFingerprint(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "new-head", headSHA)
	assert.Equal(t, "new-wt", wtHash)
}

func TestResetSession_ClearsAskFingerprint(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	err := store.SetAskFingerprint(ctx, "s1", "abc", "def")
	require.NoError(t, err)

	err = store.ResetSession(ctx, "s1")
	require.NoError(t, err)

	headSHA, wtHash, err := store.AskFingerprint(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "", headSHA)
	assert.Equal(t, "", wtHash)
}

func TestStoreIndependentSessions(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, _ = store.IncrementExitPlanCount(ctx, "s1")
	_, _ = store.IncrementExitPlanCount(ctx, "s1")
	_, _ = store.IncrementExitPlanCount(ctx, "s2")

	count1, _, _, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 2, count1)

	count2, _, _, err := store.Session(ctx, "s2")
	require.NoError(t, err)
	assert.Equal(t, 1, count2)
}

// hammerStore opens a fresh Store per iteration and increments the given
// session, simulating the real per-invocation shape of N concurrent
// hook-router processes hammering the same file.
func hammerStore(ctx context.Context, dbPath, sessionID string, ops int) error {
	for range ops {
		store, err := OpenStore(ctx, dbPath)
		if err != nil {
			return err
		}

		_, err = store.IncrementExitPlanCount(ctx, sessionID)

		closeErr := store.Close()
		if err != nil {
			return err
		}

		if closeErr != nil {
			return closeErr
		}
	}

	return nil
}

func TestSetPendingPlan_UpsertsAndRefreshes(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	overwroteFresh, err := store.SetPendingPlan(ctx, "/cwd", testPID, "/plan.md", "sha1")
	require.NoError(t, err)
	assert.False(t, overwroteFresh, "first write must not report overwrite")

	planPath, baseSHA, found, err := store.ConsumePendingPlan(ctx, "/cwd", testPID, 300)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "/plan.md", planPath)
	assert.Equal(t, "sha1", baseSHA)

	_, err = store.SetPendingPlan(ctx, "/cwd", testPID, "/v1.md", "sha-v1")
	require.NoError(t, err)

	overwroteFresh, err = store.SetPendingPlan(ctx, "/cwd", testPID, "/v2.md", "sha-v2")
	require.NoError(t, err)
	assert.True(t, overwroteFresh, "overwriting a fresh row must report overwrite=true")

	planPath, baseSHA, found, err = store.ConsumePendingPlan(ctx, "/cwd", testPID, 300)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "/v2.md", planPath)
	assert.Equal(t, "sha-v2", baseSHA)
}

func TestConsumePendingPlan_FoundFreshDeletes(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, err := store.SetPendingPlan(ctx, "/cwd", testPID, "/plan.md", "sha1")
	require.NoError(t, err)

	planPath, baseSHA, found, err := store.ConsumePendingPlan(ctx, "/cwd", testPID, 300)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "/plan.md", planPath)
	assert.Equal(t, "sha1", baseSHA)

	// Second consume must return not-found (row was deleted).
	_, _, found, err = store.ConsumePendingPlan(ctx, "/cwd", testPID, 300)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestConsumePendingPlan_StaleReturnsNotFoundAndStaleRowSurvives(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, err := store.SetPendingPlan(ctx, "/cwd", testPID, "/plan.md", "sha1")
	require.NoError(t, err)

	// Backdate the row beyond the TTL we'll pass in.
	_, err = store.db.ExecContext(ctx,
		`UPDATE pending_plans SET updated_at = datetime('now', '-10 seconds') WHERE cwd = ? AND claude_pid = ?`,
		"/cwd", testPID)
	require.NoError(t, err)

	_, _, found, err := store.ConsumePendingPlan(ctx, "/cwd", testPID, 5)
	require.NoError(t, err)
	assert.False(t, found, "stale row must not be returned through the TTL gate")

	// Stale row survives Consume; MaybePruneStale handles eventual cleanup.
	var count int

	err = store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pending_plans WHERE cwd = ? AND claude_pid = ?`, "/cwd", testPID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "stale row must remain for MaybePruneStale to clean up")
}

func TestConsumePendingPlan_AbsentReturnsNotFound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, _, found, err := store.ConsumePendingPlan(ctx, "/never-set", testPID, 300)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestDeletePendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, err := store.SetPendingPlan(ctx, "/cwd", testPID, "/plan.md", "sha1")
	require.NoError(t, err)

	require.NoError(t, store.DeletePendingPlan(ctx, "/cwd", testPID))

	_, _, found, err := store.ConsumePendingPlan(ctx, "/cwd", testPID, 300)
	require.NoError(t, err)
	assert.False(t, found)

	// Idempotent: deleting an absent row is a no-op.
	require.NoError(t, store.DeletePendingPlan(ctx, "/cwd", testPID))
}

// TestPruneStale_PrunesBothTables_Beyond24h verifies the cleanup path
// directly, bypassing MaybePruneStale's probabilistic gate. Both
// `sessions` and `pending_plans` rows older than 24 hours must go;
// fresh rows must survive.
func TestPruneStale_PrunesBothTables_Beyond24h(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	// Fresh pending plan -- must survive.
	_, err := store.SetPendingPlan(ctx, "/fresh", testPID, "/p1.md", "sha1")
	require.NoError(t, err)

	// Stale pending plan (>24h) -- must be pruned.
	_, err = store.SetPendingPlan(ctx, "/stale", testPID, "/p2.md", "sha2")
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx,
		`UPDATE pending_plans SET updated_at = datetime('now', '-25 hours') WHERE cwd = ? AND claude_pid = ?`,
		"/stale", testPID)
	require.NoError(t, err)

	// Fresh session -- must survive.
	require.NoError(t, store.SetPlanPath(ctx, "fresh-sess", "/sess1.md", "sha-a"))

	// Stale session (>24h) -- must be pruned.
	require.NoError(t, store.SetPlanPath(ctx, "stale-sess", "/sess2.md", "sha-b"))

	_, err = store.db.ExecContext(ctx,
		`UPDATE sessions SET updated_at = datetime('now', '-25 hours') WHERE session_id = ?`,
		"stale-sess")
	require.NoError(t, err)

	require.NoError(t, store.pruneStale(ctx))

	var pendingCount, sessionCount int

	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pending_plans`).Scan(&pendingCount))
	assert.Equal(t, 1, pendingCount, "stale pending row must be pruned, fresh must survive")

	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions`).Scan(&sessionCount))
	assert.Equal(t, 1, sessionCount, "stale session row must be pruned, fresh must survive")
}

// TestStore_ConcurrentWriters_DistinctSessions exercises inter-process
// contention on the file lock: each goroutine writes to its own session
// but all share the same database file.
func TestStore_ConcurrentWriters_DistinctSessions(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "concurrent.db")

	seed, err := OpenStore(t.Context(), dbPath)
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	const (
		writers      = 32
		opsPerWriter = 20
	)

	var wg sync.WaitGroup

	errs := make(chan error, writers)

	for i := range writers {
		wg.Go(func() {
			err := hammerStore(t.Context(), dbPath, fmt.Sprintf("s-%d", i), opsPerWriter)
			if err != nil {
				errs <- err
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	store, err := OpenStore(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	for i := range writers {
		count, _, _, err := store.Session(t.Context(), fmt.Sprintf("s-%d", i))
		require.NoError(t, err)
		assert.Equal(t, opsPerWriter, count)
	}
}

// TestStore_ConcurrentWriters_SameSession hammers the UPSERT path: all
// writers target one session_id, so every ExecContext has to acquire the
// write lock and resolve ON CONFLICT.
func TestStore_ConcurrentWriters_SameSession(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "same-session.db")

	seed, err := OpenStore(t.Context(), dbPath)
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	const (
		writers      = 32
		opsPerWriter = 20
	)

	var wg sync.WaitGroup

	errs := make(chan error, writers)

	for range writers {
		wg.Go(func() {
			err := hammerStore(t.Context(), dbPath, "shared", opsPerWriter)
			if err != nil {
				errs <- err
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	store, err := OpenStore(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	count, _, _, err := store.Session(t.Context(), "shared")
	require.NoError(t, err)
	assert.Equal(t, writers*opsPerWriter, count)
}

// TestStore_ShortContextSurfacesBusy guarantees that a regression which
// dropped the DSN pragma (so busy_timeout defaults to 0 on new pool
// connections) still surfaces an error rather than being masked. A side
// connection holds BEGIN IMMEDIATE for longer than the caller's context
// deadline, so the write must either time out on the context or return
// BUSY — either outcome confirms the lock really is contended.
func TestStore_ShortContextSurfacesBusy(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "short-ctx.db")

	holder, err := OpenStore(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, holder.Close())
	})

	writer, err := OpenStore(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, writer.Close())
	})

	conn, err := holder.db.Conn(t.Context())
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})

	_, err = conn.ExecContext(t.Context(), "BEGIN IMMEDIATE")
	require.NoError(t, err)

	released := make(chan struct{})

	go func() {
		time.Sleep(500 * time.Millisecond)

		_, commitErr := conn.ExecContext(t.Context(), "COMMIT")
		if commitErr != nil {
			t.Logf("COMMIT on holder conn: %v", commitErr)
		}

		close(released)
	}()

	shortCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err = writer.IncrementExitPlanCount(shortCtx, "x")
	require.Error(t, err, "short context should surface an error when writer is held")

	msg := strings.ToLower(err.Error())
	assert.True(t,
		strings.Contains(msg, "busy") ||
			strings.Contains(msg, "locked") ||
			strings.Contains(msg, "timeout") ||
			strings.Contains(msg, "deadline"),
		"error should mention busy/locked/timeout/deadline, got: %s", err.Error(),
	)

	<-released
}

// TestSetPendingPlan_DifferentPIDsCoexist verifies that two Claude
// Code windows in the same cwd write distinct rows, and each window's
// consume returns its own row.
func TestSetPendingPlan_DifferentPIDsCoexist(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, err := store.SetPendingPlan(ctx, "/cwd", "A", "/plan-A.md", "sha-A")
	require.NoError(t, err)

	_, err = store.SetPendingPlan(ctx, "/cwd", "B", "/plan-B.md", "sha-B")
	require.NoError(t, err)

	planPath, baseSHA, found, err := store.ConsumePendingPlan(ctx, "/cwd", "A", 300)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "/plan-A.md", planPath)
	assert.Equal(t, "sha-A", baseSHA)

	// B's row is untouched after A's consume.
	planPath, baseSHA, found, err = store.ConsumePendingPlan(ctx, "/cwd", "B", 300)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "/plan-B.md", planPath)
	assert.Equal(t, "sha-B", baseSHA)
}

// TestConsumePendingPlan_OnlyMatchingPID verifies a window cannot
// consume a peer's handoff. Write (cwd, A); Consume(cwd, B) must
// return not-found and leave A's row in place.
func TestConsumePendingPlan_OnlyMatchingPID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, err := store.SetPendingPlan(ctx, "/cwd", "A", "/plan-A.md", "sha-A")
	require.NoError(t, err)

	_, _, found, err := store.ConsumePendingPlan(ctx, "/cwd", "B", 300)
	require.NoError(t, err)
	assert.False(t, found, "consume with non-matching PID must not return a row")

	// A's row is still consumable.
	planPath, _, found, err := store.ConsumePendingPlan(ctx, "/cwd", "A", 300)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "/plan-A.md", planPath)
}

// TestDeletePendingPlan_OnlyMatchingPID verifies Delete(cwd, B) is a
// no-op when only an (cwd, A) row exists; A's row survives.
func TestDeletePendingPlan_OnlyMatchingPID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := t.Context()

	_, err := store.SetPendingPlan(ctx, "/cwd", "A", "/plan-A.md", "sha-A")
	require.NoError(t, err)

	require.NoError(t, store.DeletePendingPlan(ctx, "/cwd", "B"))

	planPath, _, found, err := store.ConsumePendingPlan(ctx, "/cwd", "A", 300)
	require.NoError(t, err)
	require.True(t, found, "Delete with non-matching PID must leave the row")
	assert.Equal(t, "/plan-A.md", planPath)
}

// seedV4 writes the pre-migration shape into dbPath. It opens via
// OpenStore (so `sessions` is created at the current shape), drops the
// v5 pending_plans, re-creates it with the v4 single-column PK,
// optionally inserts the given row, and rolls user_version back to 4.
// Reopening the resulting DB triggers the v4→v5 migration. Pass
// seedRow == nil to skip the row insert.
func seedV4(t *testing.T, dbPath string, seedRow *struct{ cwd, planPath, baseSHA string }) {
	t.Helper()

	seed, err := OpenStore(t.Context(), dbPath)
	require.NoError(t, err)

	_, err = seed.db.ExecContext(t.Context(), `DROP TABLE IF EXISTS pending_plans`)
	require.NoError(t, err)

	_, err = seed.db.ExecContext(t.Context(), `
		CREATE TABLE pending_plans (
		    cwd        TEXT PRIMARY KEY,
		    plan_path  TEXT NOT NULL DEFAULT '',
		    base_sha   TEXT NOT NULL DEFAULT '',
		    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	require.NoError(t, err)

	if seedRow != nil {
		_, err = seed.db.ExecContext(t.Context(),
			`INSERT INTO pending_plans (cwd, plan_path, base_sha) VALUES (?, ?, ?)`,
			seedRow.cwd, seedRow.planPath, seedRow.baseSHA)
		require.NoError(t, err)
	}

	_, err = seed.db.ExecContext(t.Context(), `PRAGMA user_version = 4`)
	require.NoError(t, err)

	require.NoError(t, seed.Close())
}

// TestEnsureSchema_UpgradesV4ToV5 seeds a fresh DB with the v4 shape
// of pending_plans (single-column PK on `cwd`), reopens it, and
// verifies the v5 migration: user_version reaches 5, the table is
// recreated with the composite PK, and a new write with (cwd,
// claude_pid) succeeds.
func TestEnsureSchema_UpgradesV4ToV5(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "v4.db")

	seedV4(t, dbPath, &struct{ cwd, planPath, baseSHA string }{
		cwd: "/seed-cwd", planPath: "/seed.md", baseSHA: "seed-sha",
	})

	// Reopen: triggers v4→v5 migration.
	store, err := OpenStore(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	var version int

	require.NoError(t, store.db.QueryRowContext(t.Context(),
		`PRAGMA user_version`).Scan(&version))
	assert.Equal(t, schemaVersion, version, "user_version must reach v5 after migration")

	// Introspect the new schema: pending_plans must carry both cwd and
	// claude_pid as primary-key columns.
	rows, err := store.db.QueryContext(t.Context(), `PRAGMA table_info(pending_plans)`)
	require.NoError(t, err)

	defer rows.Close()

	pkCols := map[string]int{}

	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue any
			pk        int
		)

		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk))

		if pk > 0 {
			pkCols[name] = pk
		}
	}

	require.NoError(t, rows.Err())
	assert.Contains(t, pkCols, "cwd", "cwd must be part of the primary key")
	assert.Contains(t, pkCols, "claude_pid", "claude_pid must be part of the primary key")

	// The seed row from the v4 shape was dropped by the migration; a
	// fresh write with the new composite key must succeed.
	_, err = store.SetPendingPlan(t.Context(), "/seed-cwd", "fresh-pid", "/fresh.md", "fresh-sha")
	require.NoError(t, err)

	planPath, baseSHA, found, err := store.ConsumePendingPlan(t.Context(),
		"/seed-cwd", "fresh-pid", 300)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "/fresh.md", planPath)
	assert.Equal(t, "fresh-sha", baseSHA)
}

// TestEnsureSchema_ConcurrentOpensConverge exercises the BEGIN IMMEDIATE
// wrapper around the v4→v5 migration. Without the wrapper, a process
// that observed user_version=4 on its initial PRAGMA read could run
// the destructive DROP after a peer had already migrated and written
// a row, destroying live data. Under the wrapper, only one process
// runs the migration; the rest re-check user_version under the lock
// and skip.
//
// Shape: pre-seed a v4 DB, race N goroutines opening it concurrently,
// each writing a unique (cwd, pid) row after OpenStore returns. The
// final state must have exactly N rows in pending_plans.
func TestEnsureSchema_ConcurrentOpensConverge(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "concurrent-migrate.db")

	seedV4(t, dbPath, nil)

	const goroutines = 12

	var (
		wg   sync.WaitGroup
		errs = make(chan error, goroutines)
	)

	for i := range goroutines {
		wg.Go(func() {
			store, openErr := OpenStore(t.Context(), dbPath)
			if openErr != nil {
				errs <- fmt.Errorf("OpenStore: %w", openErr)
				return
			}

			defer func() {
				if closeErr := store.Close(); closeErr != nil {
					errs <- fmt.Errorf("Close: %w", closeErr)
				}
			}()

			_, setErr := store.SetPendingPlan(t.Context(),
				fmt.Sprintf("/cwd-%d", i),
				fmt.Sprintf("pid-%d", i),
				fmt.Sprintf("/plan-%d.md", i),
				fmt.Sprintf("sha-%d", i),
			)
			if setErr != nil {
				errs <- fmt.Errorf("SetPendingPlan: %w", setErr)
				return
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	// Reopen and verify final state.
	store, err := OpenStore(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	var version int

	require.NoError(t, store.db.QueryRowContext(t.Context(),
		`PRAGMA user_version`).Scan(&version))
	assert.Equal(t, schemaVersion, version, "concurrent opens must all settle on v5")

	var count int

	require.NoError(t, store.db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM pending_plans`).Scan(&count))
	assert.Equal(t, goroutines, count,
		"each goroutine's post-migration write must survive; no peer's DROP must have eaten it")
}
