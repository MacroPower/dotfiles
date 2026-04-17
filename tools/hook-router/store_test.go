package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")

	store, err := OpenStore(dbPath)
	require.NoError(t, err)

	t.Cleanup(func() { store.Close() })

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

func TestSetReviewFingerprint(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	err := store.SetReviewFingerprint(ctx, "s1", "abc123", "def456")
	require.NoError(t, err)

	headSHA, wtHash, err := store.ReviewFingerprint(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "abc123", headSHA)
	assert.Equal(t, "def456", wtHash)
}

func TestSetReviewFingerprint_Overwrite(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	err := store.SetReviewFingerprint(ctx, "s1", "old-head", "old-wt")
	require.NoError(t, err)

	err = store.SetReviewFingerprint(ctx, "s1", "new-head", "new-wt")
	require.NoError(t, err)

	headSHA, wtHash, err := store.ReviewFingerprint(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "new-head", headSHA)
	assert.Equal(t, "new-wt", wtHash)
}

func TestResetSession_ClearsReviewFingerprint(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	err := store.SetReviewFingerprint(ctx, "s1", "abc", "def")
	require.NoError(t, err)

	err = store.ResetSession(ctx, "s1")
	require.NoError(t, err)

	headSHA, wtHash, err := store.ReviewFingerprint(ctx, "s1")
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
