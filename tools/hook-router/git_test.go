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
