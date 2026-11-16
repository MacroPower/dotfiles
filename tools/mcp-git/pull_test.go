package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPullArgs(t *testing.T) {
	t.Parallel()

	// Every pull fetches and updates no submodule, and it names the
	// program that serves its fetch.
	pull := []string{"pull", "--no-recurse-submodules", "--upload-pack=git-upload-pack"}

	tests := map[string]struct {
		input PullInput
		want  []string
	}{
		"minimal": {
			input: PullInput{Repo: "/r"},
			want:  slices.Concat(pull, []string{"--ff-only", "--", "origin"}),
		},
		"with rebase": {
			input: PullInput{Repo: "/r", Rebase: true},
			want:  slices.Concat(pull, []string{"--rebase", "--", "origin"}),
		},
		"with branch": {
			input: PullInput{Repo: "/r", Branch: "main"},
			want:  slices.Concat(pull, []string{"--ff-only", "--", "origin", "main"}),
		},
		"all options": {
			input: PullInput{
				Repo: "/r", Remote: "up", Branch: "dev", Rebase: true,
			},
			want: slices.Concat(pull, []string{"--rebase", "--", "up", "dev"}),
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

	h := &handler{allowFileURLs: true}

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

	h := &handler{allowFileURLs: true}

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

	h := &handler{allowFileURLs: true}

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

	h := &handler{allowFileURLs: true}

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

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePull(t.Context(), nil, PullInput{Repo: a})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	_, statErr := os.Stat(filepath.Join(a, "hook-ran"))
	require.ErrorIs(t, statErr, os.ErrNotExist,
		"local hooks must not run during git_pull")
}

// The cleanup that aborts a conflicting rebase updates refs and the
// working tree, so it fires hooks as the pull itself does.
func TestHandlePullRebaseConflictSkipsHooks(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)
	b := initWorkClone(t, bare)

	// Both sides edit README (created by initBareRepo), so the
	// rebase pull conflicts.
	commitFile(t, a, "README", "local edit", "local edit")
	commitFile(t, b, "README", "remote edit", "remote edit")
	runGitCmds(t, [][]string{{"git", "-C", b, "push"}})

	script, marker := markerScript(t)

	for _, event := range hookEvents {
		writeHook(t, a, event, "#!/bin/sh\nexec '"+script+"'\n")
	}

	appendGitConfig(t, a, hookConfig(script, "check"))

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePull(t.Context(), nil, PullInput{
		Repo:   a,
		Rebase: true,
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "git pull failed")

	requireNotRun(t, marker)

	rebaseDir := gitOut(t, "-C", a, "rev-parse", "--git-path", "rebase-merge")

	_, statErr := os.Stat(rebaseDir)
	require.ErrorIs(t, statErr, os.ErrNotExist,
		"no in-progress rebase may remain after a conflicting pull")

	assert.Empty(t, gitOut(t, "-C", a, "status", "--porcelain"))
	assert.Equal(t, "local edit",
		gitOut(t, "-C", a, "log", "-1", "--format=%s"))
}

// git fetches and updates a submodule with git commands of their own,
// which run the hooks that the git config of the submodule defines.
// The pull tool therefore never fetches or updates a submodule.
func TestHandlePullSkipsSubmodules(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// config is a key and a value that the test sets in the git
		// config of the parent.
		config []string
		rebase bool
	}{
		// git fetches a submodule when the fetch of the parent brings
		// a commit that moves the submodule.
		"default config":          {},
		"fetch.recurseSubmodules": {config: []string{"fetch.recurseSubmodules", "true"}},
		// With submodule.recurse, a pull also checks out the new
		// commit in the submodule.
		"submodule.recurse":             {config: []string{"submodule.recurse", "true"}},
		"submodule.recurse with rebase": {config: []string{"submodule.recurse", "true"}, rebase: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := initSubmoduleRepos(t)
			subBranch := advanceSubmodule(t, s)

			// The hook of the submodule fires when git updates a ref
			// of the submodule or checks out a commit in it.
			runGitCmds(t, [][]string{
				{"git", "-C", s.sub, "config", "--add", "hook.check.event", "reference-transaction"},
				{"git", "-C", s.sub, "config", "--add", "hook.check.event", "post-checkout"},
			})

			if tt.config != nil {
				runGitCmds(t, [][]string{slices.Concat([]string{"git", "-C", s.work, "config"}, tt.config)})
			}

			before := gitOut(t, "-C", s.sub, "rev-parse", "origin/"+subBranch)
			head := gitOut(t, "-C", s.sub, "rev-parse", "HEAD")

			h := &handler{allowFileURLs: true}

			result, _, err := h.handlePull(t.Context(), nil, PullInput{Repo: s.work, Rebase: tt.rebase})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			assert.Equal(t,
				gitOut(t, "-C", s.bare, "rev-parse", currentBranch(t, s.work)),
				gitOut(t, "-C", s.work, "rev-parse", "HEAD"),
			)
			assert.Equal(t, before, gitOut(t, "-C", s.sub, "rev-parse", "origin/"+subBranch),
				"git_pull must not fetch a submodule")
			assert.Equal(t, head, gitOut(t, "-C", s.sub, "rev-parse", "HEAD"),
				"git_pull must not update a submodule")

			_, statErr := os.Stat(s.marker)
			require.ErrorIs(t, statErr, os.ErrNotExist,
				"the hook of the submodule must not run")
		})
	}
}

// The server names the working tree of each git command. A pull must
// therefore update the working tree that git picks for the path of
// the call.
func TestHandlePullWorkTrees(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// setup returns the path of the call, and the root of the
		// working tree that the pull updates, for a clone at work.
		setup func(t *testing.T, work string) (string, string)
	}{
		"root of the working tree": {
			setup: func(_ *testing.T, work string) (string, string) { return work, work },
		},
		"directory of the working tree": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				dir := filepath.Join(work, "dir")
				require.NoError(t, os.Mkdir(dir, 0o755))

				return dir, work
			},
		},
		"linked worktree": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				wt := filepath.Join(filepath.Dir(work), "wt")
				runGitCmds(t, [][]string{{"git", "-C", work, "worktree", "add", "-b", "wt-branch", wt}})

				return wt, wt
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bare := initBareRepo(t)
			work := initWorkClone(t, bare)
			other := initWorkClone(t, bare)
			branch := currentBranch(t, work)

			repo, root := tt.setup(t, work)

			commitFile(t, other, "new.txt", "fresh", "add new file")
			runGitCmds(t, [][]string{{"git", "-C", other, "push"}})

			h := &handler{
				allowDirs:     mustResolveAllowDirs(t, filepath.Dir(work)),
				allowFileURLs: true,
			}

			result, _, err := h.handlePull(t.Context(), nil, PullInput{Repo: repo, Branch: branch})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			assert.FileExists(t, filepath.Join(root, "new.txt"))
		})
	}
}

// git names the working tree of a submodule with core.worktree, in
// the git config of the submodule. A pull in the submodule updates
// that working tree.
func TestHandlePullInSubmodule(t *testing.T) {
	t.Parallel()

	s := initSubmoduleRepos(t)
	branch := advanceSubmodule(t, s)

	require.NotEmpty(t, gitOut(t, "-C", s.sub, "config", "core.worktree"))

	h := &handler{
		allowDirs:     mustResolveAllowDirs(t, filepath.Dir(s.work)),
		allowFileURLs: true,
	}

	result, _, err := h.handlePull(t.Context(), nil, PullInput{Repo: s.sub, Branch: branch})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.FileExists(t, filepath.Join(s.sub, "next.txt"))
}

// A pull updates the working tree, so it refuses a repository that
// has none.
func TestHandlePullRefusesBareRepo(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	mirror := filepath.Join(t.TempDir(), "mirror.git")

	runGitCmds(t, [][]string{{"git", "clone", "--bare", bare, mirror}})

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePull(t.Context(), nil, PullInput{Repo: mirror})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), ErrNoWorkTree.Error())

	// A fetch does not need a working tree.
	result, _, err = h.handleFetch(t.Context(), nil, FetchInput{Repo: mirror})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
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
