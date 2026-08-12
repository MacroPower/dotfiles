package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPullArgs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input PullInput
		want  []string
	}{
		"minimal": {
			input: PullInput{Repo: "/r"},
			want:  []string{"pull", "--ff-only", "--", "origin"},
		},
		"with rebase": {
			input: PullInput{Repo: "/r", Rebase: true},
			want:  []string{"pull", "--rebase", "--", "origin"},
		},
		"with branch": {
			input: PullInput{Repo: "/r", Branch: "main"},
			want:  []string{"pull", "--ff-only", "--", "origin", "main"},
		},
		"all options": {
			input: PullInput{
				Repo: "/r", Remote: "up", Branch: "dev", Rebase: true,
			},
			want: []string{"pull", "--rebase", "--", "up", "dev"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := buildPullArgs(remoteOrDefault(tt.input.Remote), tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandlePullValidation(t *testing.T) {
	t.Parallel()

	h := &handler{}

	tests := map[string]struct {
		input PullInput
		want  string
	}{
		"missing repo": {
			input: PullInput{},
			want:  ErrMissingRepo.Error(),
		},
		"dash repo": {
			input: PullInput{Repo: "--upload-pack=evil"},
			want:  ErrDeniedRepoPrefix.Error(),
		},
		"remote url": {
			input: PullInput{Repo: "/tmp/x", Remote: "https://evil/x"},
			want:  "remote name is invalid",
		},
		"refspec branch": {
			input: PullInput{Repo: "/tmp/x", Branch: "+main"},
			want:  "ref name is invalid",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, _, err := h.handlePull(t.Context(), nil, tt.input)
			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.Contains(t, resultText(t, result), tt.want)
		})
	}
}

func TestHandlePull(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)
	b := initWorkClone(t, bare)

	commitFile(t, b, "new.txt", "fresh", "add new file")
	runGitCmds(t, [][]string{{"git", "-C", b, "push"}})

	h := &handler{}

	result, _, err := h.handlePull(t.Context(), nil, PullInput{Repo: a})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Pulled from origin")

	content, readErr := os.ReadFile(filepath.Join(a, "new.txt"))
	require.NoError(t, readErr, "pull should update the working tree")
	assert.Equal(t, "fresh", string(content))
}

func TestHandlePullFFOnlyRejectsDivergence(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)
	b := initWorkClone(t, bare)

	commitFile(t, a, "local.txt", "local", "local commit")
	commitFile(t, b, "remote.txt", "remote", "remote commit")
	runGitCmds(t, [][]string{{"git", "-C", b, "push"}})

	h := &handler{}

	result, _, err := h.handlePull(t.Context(), nil, PullInput{Repo: a})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "git pull failed")
}

func TestHandlePullRebase(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)
	b := initWorkClone(t, bare)

	commitFile(t, a, "local.txt", "local", "local commit")
	commitFile(t, b, "remote.txt", "remote", "remote commit")
	runGitCmds(t, [][]string{{"git", "-C", b, "push"}})

	h := &handler{}

	result, _, err := h.handlePull(t.Context(), nil, PullInput{
		Repo:   a,
		Rebase: true,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	_, statErr := os.Stat(filepath.Join(a, "local.txt"))
	require.NoError(t, statErr)

	_, statErr = os.Stat(filepath.Join(a, "remote.txt"))
	require.NoError(t, statErr)

	// The local commit is replayed on top of the remote one.
	assert.Equal(t, "local commit",
		gitOut(t, "-C", a, "log", "-1", "--format=%s"))
	assert.Equal(t, "remote commit",
		gitOut(t, "-C", a, "log", "-1", "--format=%s", "HEAD~1"))
}

func TestHandlePullRebaseConflictAborts(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)
	b := initWorkClone(t, bare)

	// Both sides edit README (created by initBareRepo), so the
	// rebase pull conflicts.
	commitFile(t, a, "README", "local edit", "local edit")
	commitFile(t, b, "README", "remote edit", "remote edit")
	runGitCmds(t, [][]string{{"git", "-C", b, "push"}})

	h := &handler{}

	result, _, err := h.handlePull(t.Context(), nil, PullInput{
		Repo:   a,
		Rebase: true,
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "git pull failed")

	// The cleanup must have aborted the rebase: no rebase state on
	// disk and a clean working tree at the local commit.
	rebaseDir := gitOut(t, "-C", a, "rev-parse", "--git-path", "rebase-merge")

	_, statErr := os.Stat(rebaseDir)
	require.ErrorIs(t, statErr, os.ErrNotExist,
		"no in-progress rebase may remain after a conflicting pull")

	assert.Empty(t, gitOut(t, "-C", a, "status", "--porcelain"))
	assert.Equal(t, "local edit",
		gitOut(t, "-C", a, "log", "-1", "--format=%s"))
}

func TestHandlePullSkipsHooks(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)
	b := initWorkClone(t, bare)

	commitFile(t, b, "new.txt", "fresh", "add new file")
	runGitCmds(t, [][]string{{"git", "-C", b, "push"}})

	// Hooks execute outside the caller's sandbox, so the pull tool
	// must never run them. post-merge fires on a plain fast-forward
	// pull, which makes it the canary.
	writeHook(t, a, "post-merge", "#!/bin/sh\ntouch hook-ran\n")

	h := &handler{}

	result, _, err := h.handlePull(t.Context(), nil, PullInput{Repo: a})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	_, statErr := os.Stat(filepath.Join(a, "hook-ran"))
	require.ErrorIs(t, statErr, os.ErrNotExist,
		"local hooks must not run during git_pull")
}

func TestHandlePullTimeout(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)

	h := &handler{timeout: time.Nanosecond}

	result, _, err := h.handlePull(t.Context(), nil, PullInput{Repo: a})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "timed out")
}
