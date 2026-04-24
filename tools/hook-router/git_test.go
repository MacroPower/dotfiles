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

func TestFingerprint_EditsToSameDirtyFileChangeHash(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}
	ctx := t.Context()

	readme := filepath.Join(dir, "README.md")

	require.NoError(t, os.WriteFile(readme, []byte("v1\n"), 0o644))

	_, wt1, err := git.Fingerprint(ctx)
	require.NoError(t, err)

	porcelain1 := runPorcelain(t, ctx, dir)

	require.NoError(t, os.WriteFile(readme, []byte("v2\n"), 0o644))

	_, wt2, err := git.Fingerprint(ctx)
	require.NoError(t, err)

	porcelain2 := runPorcelain(t, ctx, dir)

	// Pins the regression: porcelain output is identical across content
	// edits to the same already-dirty file, so a future refactor that
	// reverts to hashing porcelain output would fail this assertion.
	assert.Equal(t, porcelain1, porcelain2, "git status --porcelain is stable across content edits to a dirty file")
	assert.NotEqual(t, wt1, wt2, "working-tree hash must change when dirty file content changes")
}

func TestFingerprint_NewUntrackedFileChangesHash(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}
	ctx := t.Context()

	_, wt1, err := git.Fingerprint(ctx)
	require.NoError(t, err)

	untracked := filepath.Join(dir, "new.txt")
	require.NoError(t, os.WriteFile(untracked, []byte("hello\n"), 0o644))

	_, wt2, err := git.Fingerprint(ctx)
	require.NoError(t, err)

	require.NoError(t, os.Remove(untracked))

	_, wt3, err := git.Fingerprint(ctx)
	require.NoError(t, err)

	assert.NotEqual(t, wt1, wt2, "appearance of untracked file must change hash")
	assert.Equal(t, wt1, wt3, "removal of untracked file must restore hash")
}

func runPorcelain(t *testing.T, ctx context.Context, dir string) string {
	t.Helper()

	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dir

	out, err := cmd.Output()
	require.NoError(t, err)

	return string(out)
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
