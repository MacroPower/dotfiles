package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFetchArgs(t *testing.T) {
	t.Parallel()

	// Every fetch fetches no submodule, and it names the program that
	// serves it.
	fetch := []string{"fetch", "--no-recurse-submodules", "--upload-pack=git-upload-pack"}

	tests := map[string]struct {
		input FetchInput
		want  []string
	}{
		"minimal": {
			input: FetchInput{Repo: "/r"},
			want:  slices.Concat(fetch, []string{"--", "origin"}),
		},
		"with remote": {
			input: FetchInput{Repo: "/r", Remote: "upstream"},
			want:  slices.Concat(fetch, []string{"--", "upstream"}),
		},
		"with prune": {
			input: FetchInput{Repo: "/r", Prune: true},
			want:  slices.Concat(fetch, []string{"--prune", "--", "origin"}),
		},
		"with tags": {
			input: FetchInput{Repo: "/r", Tags: true},
			want:  slices.Concat(fetch, []string{"--tags", "--", "origin"}),
		},
		"with ref": {
			input: FetchInput{Repo: "/r", Ref: "main"},
			want:  slices.Concat(fetch, []string{"--", "origin", "main"}),
		},
		"all options": {
			input: FetchInput{
				Repo: "/r", Remote: "up", Ref: "v1.0.0",
				Prune: true, Tags: true,
			},
			want: slices.Concat(fetch, []string{"--prune", "--tags", "--", "up", "v1.0.0"}),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := buildFetchArgs(remoteOrDefault(tt.input.Remote), tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandleFetchValidation(t *testing.T) {
	t.Parallel()

	h := &handler{}

	tests := map[string]struct {
		input FetchInput
		want  string
	}{
		"missing repo": {
			input: FetchInput{},
			want:  ErrMissingRepo.Error(),
		},
		"dash repo": {
			input: FetchInput{Repo: "--upload-pack=evil"},
			want:  ErrDeniedRepoPrefix.Error(),
		},
		"remote url": {
			input: FetchInput{Repo: "/tmp/x", Remote: "https://evil/x"},
			want:  "remote name is invalid",
		},
		"dash remote": {
			input: FetchInput{Repo: "/tmp/x", Remote: "-x"},
			want:  "remote name is invalid",
		},
		"refspec ref": {
			input: FetchInput{Repo: "/tmp/x", Ref: "main:evil"},
			want:  "ref name is invalid",
		},
		"dash ref": {
			input: FetchInput{Repo: "/tmp/x", Ref: "-x"},
			want:  "ref name is invalid",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, _, err := h.handleFetch(t.Context(), nil, tt.input)
			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.Contains(t, resultText(t, result), tt.want)
		})
	}
}

func TestHandleFetch(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)
	b := initWorkClone(t, bare)

	commitFile(t, b, "new.txt", "fresh", "add new file")
	runGitCmds(t, [][]string{{"git", "-C", b, "push"}})

	h := &handler{allowFileURLs: true}

	result, _, err := h.handleFetch(t.Context(), nil, FetchInput{Repo: a})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Fetched from origin")

	branch := currentBranch(t, a)
	assert.Equal(t,
		gitOut(t, "-C", b, "rev-parse", "HEAD"),
		gitOut(t, "-C", a, "rev-parse", "origin/"+branch),
		"remote-tracking ref should point at the pushed commit",
	)

	_, statErr := os.Stat(filepath.Join(a, "new.txt"))
	require.ErrorIs(t, statErr, os.ErrNotExist,
		"fetch must not touch the working tree")
}

func TestHandleFetchRef(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)
	b := initWorkClone(t, bare)

	commitFile(t, b, "ref.txt", "x", "ref commit")
	runGitCmds(t, [][]string{{"git", "-C", b, "push"}})

	branch := currentBranch(t, a)
	h := &handler{allowFileURLs: true}

	result, _, err := h.handleFetch(t.Context(), nil, FetchInput{
		Repo: a,
		Ref:  branch,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Equal(t,
		gitOut(t, "-C", b, "rev-parse", "HEAD"),
		gitOut(t, "-C", a, "rev-parse", "origin/"+branch),
	)
}

func TestHandleFetchPrune(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	b := initWorkClone(t, bare)

	runGitCmds(t, [][]string{
		{"git", "-C", b, "branch", "extra"},
		{"git", "-C", b, "push", "origin", "extra"},
	})

	a := initWorkClone(t, bare)
	require.NotEmpty(t, gitOut(t, "-C", a, "rev-parse", "origin/extra"))

	runGitCmds(t, [][]string{
		{"git", "-C", bare, "branch", "-D", "extra"},
	})

	h := &handler{allowFileURLs: true}

	result, _, err := h.handleFetch(t.Context(), nil, FetchInput{
		Repo:  a,
		Prune: true,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	cmd := exec.CommandContext(t.Context(), "git", "-C", a, "rev-parse", "origin/extra")
	require.Error(t, cmd.Run(), "pruned remote-tracking ref should be gone")
}

func TestHandleFetchTags(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)
	b := initWorkClone(t, bare)

	commitFile(t, b, "tagged.txt", "x", "tagged commit")
	runGitCmds(t, [][]string{
		{"git", "-C", b, "tag", "v9.9.9"},
		{"git", "-C", b, "push"},
		{"git", "-C", b, "push", "--tags"},
	})

	h := &handler{allowFileURLs: true}

	result, _, err := h.handleFetch(t.Context(), nil, FetchInput{
		Repo: a,
		Tags: true,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Contains(t, gitOut(t, "-C", a, "tag", "--list"), "v9.9.9")
}

// git fetches a submodule with a git command of its own, which runs
// the hooks that the git config of the submodule defines. The fetch
// tool therefore never fetches a submodule.
func TestHandleFetchSkipsSubmodules(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// config is a key and a value that the test sets in the git
		// config of the parent.
		config []string
	}{
		// git fetches a submodule when the fetch of the parent brings
		// a commit that moves the submodule.
		"default config":          {},
		"fetch.recurseSubmodules": {config: []string{"fetch.recurseSubmodules", "true"}},
		"submodule.recurse":       {config: []string{"submodule.recurse", "true"}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := initSubmoduleRepos(t)
			subBranch := advanceSubmodule(t, s)

			// The hook of the submodule fires when a fetch updates a
			// ref of the submodule.
			runGitCmds(t, [][]string{
				{"git", "-C", s.sub, "config", "--add", "hook.check.event", "reference-transaction"},
			})

			if tt.config != nil {
				runGitCmds(t, [][]string{slices.Concat([]string{"git", "-C", s.work, "config"}, tt.config)})
			}

			before := gitOut(t, "-C", s.sub, "rev-parse", "origin/"+subBranch)

			h := &handler{allowFileURLs: true}

			result, _, err := h.handleFetch(t.Context(), nil, FetchInput{Repo: s.work})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			branch := currentBranch(t, s.work)
			assert.Equal(t,
				gitOut(t, "-C", s.bare, "rev-parse", branch),
				gitOut(t, "-C", s.work, "rev-parse", "origin/"+branch),
			)
			assert.Equal(t, before, gitOut(t, "-C", s.sub, "rev-parse", "origin/"+subBranch),
				"git_fetch must not fetch a submodule")

			_, statErr := os.Stat(s.marker)
			require.ErrorIs(t, statErr, os.ErrNotExist,
				"the hook of the submodule must not run")
		})
	}
}

func TestHandleFetchUnknownRemote(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)

	h := &handler{}

	result, _, err := h.handleFetch(t.Context(), nil, FetchInput{
		Repo:   a,
		Remote: "nosuch",
	})
	require.NoError(t, err)
	require.True(t, result.IsError)

	text := resultText(t, result)
	assert.Contains(t, text, "unknown remote: nosuch")
	assert.NotContains(t, text, bare,
		"error must name the remote, never its URL")
}

func TestHandleFetchNotRepo(t *testing.T) {
	t.Parallel()

	h := &handler{}

	result, _, err := h.handleFetch(t.Context(), nil, FetchInput{
		Repo: t.TempDir(),
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "not a git repository")
}

func TestHandleFetchTimeout(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	a := initWorkClone(t, bare)

	h := &handler{timeout: time.Nanosecond}

	result, _, err := h.handleFetch(t.Context(), nil, FetchInput{Repo: a})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "timed out")
}
