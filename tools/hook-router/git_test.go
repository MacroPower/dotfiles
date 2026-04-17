package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v: %s", args, out)
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644))

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v: %s", args, out)
	}

	return dir
}

func TestHeadSHA(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}
	ctx := context.Background()

	sha, err := git.HeadSHA(ctx)
	require.NoError(t, err)
	assert.Len(t, sha, 40)
}

func TestHasChanges_NoChanges(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}
	ctx := context.Background()

	sha, err := git.HeadSHA(ctx)
	require.NoError(t, err)

	changed, err := git.HasChanges(ctx, sha)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestHasChanges_WithCommit(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}
	ctx := context.Background()

	baseSHA, err := git.HeadSHA(ctx)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644))

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "add file"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	changed, err := git.HasChanges(ctx, baseSHA)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestHasChanges_UncommittedChanges(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}
	ctx := context.Background()

	sha, err := git.HeadSHA(ctx)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644))

	changed, err := git.HasChanges(ctx, sha)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestHasChanges_EmptyBaseSHA(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}
	ctx := context.Background()

	// No uncommitted changes, empty base SHA.
	changed, err := git.HasChanges(ctx, "")
	require.NoError(t, err)
	assert.False(t, changed)

	// With uncommitted changes, empty base SHA.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644))

	changed, err = git.HasChanges(ctx, "")
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestHasChanges_NotGitRepo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	git := &GitRunner{Dir: dir}
	ctx := context.Background()

	changed, err := git.HasChanges(ctx, "abc123")
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestFingerprint(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}
	ctx := context.Background()

	headSHA, wtHash, err := git.Fingerprint(ctx)
	require.NoError(t, err)
	assert.Len(t, headSHA, 40)
	assert.Len(t, wtHash, 64) // sha256 hex

	// Same state produces the same fingerprint.
	headSHA2, wtHash2, err := git.Fingerprint(ctx)
	require.NoError(t, err)
	assert.Equal(t, headSHA, headSHA2)
	assert.Equal(t, wtHash, wtHash2)
}

func TestFingerprint_ChangesAfterEdit(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}
	ctx := context.Background()

	headBefore, wtBefore, err := git.Fingerprint(ctx)
	require.NoError(t, err)

	// Uncommitted edit changes the working-tree hash but not HEAD.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644))

	headAfter, wtAfter, err := git.Fingerprint(ctx)
	require.NoError(t, err)
	assert.Equal(t, headBefore, headAfter, "HEAD should not change for uncommitted edits")
	assert.NotEqual(t, wtBefore, wtAfter, "working-tree hash should differ after edit")

	// Committing the change updates HEAD too.
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "add file"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	headCommitted, wtCommitted, err := git.Fingerprint(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, headBefore, headCommitted, "HEAD should change after commit")
	assert.NotEqual(t, wtAfter, wtCommitted, "working-tree hash should change after commit")
}
