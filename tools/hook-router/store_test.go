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
	ctx := context.Background()

	count, planPath, baseSHA, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, "", planPath)
	assert.Equal(t, "", baseSHA)
}

func TestStoreSession_Idempotent(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

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
