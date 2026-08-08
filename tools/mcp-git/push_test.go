package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPushArgs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input PushInput
		want  []string
	}{
		"minimal": {
			input: PushInput{Repo: "/r"},
			want:  []string{"push", "--", "origin"},
		},
		"with ref": {
			input: PushInput{Repo: "/r", Ref: "feature"},
			want:  []string{"push", "--", "origin", "feature"},
		},
		"with set upstream": {
			input: PushInput{Repo: "/r", Ref: "feature", SetUpstream: true},
			want:  []string{"push", "--set-upstream", "--", "origin", "feature"},
		},
		"with force with lease": {
			input: PushInput{Repo: "/r", Ref: "main", ForceWithLease: true},
			want:  []string{"push", "--force-with-lease", "--", "origin", "main"},
		},
		"with tags": {
			input: PushInput{Repo: "/r", Tags: true},
			want:  []string{"push", "--tags", "--", "origin"},
		},
		"tag ref": {
			input: PushInput{Repo: "/r", Ref: "v1.0.0"},
			want:  []string{"push", "--", "origin", "v1.0.0"},
		},
		"all options": {
			input: PushInput{
				Repo: "/r", Remote: "up", Ref: "x",
				SetUpstream: true, ForceWithLease: true, Tags: true,
			},
			want: []string{
				"push", "--set-upstream", "--force-with-lease", "--tags",
				"--", "up", "x",
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := buildPushArgs(remoteOrDefault(tt.input.Remote), tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandlePushValidation(t *testing.T) {
	t.Parallel()

	h := &handler{}

	tests := map[string]struct {
		input PushInput
		want  string
	}{
		"missing repo": {
			input: PushInput{},
			want:  ErrMissingRepo.Error(),
		},
		"dash repo": {
			input: PushInput{Repo: "--upload-pack=evil"},
			want:  ErrDeniedRepoPrefix.Error(),
		},
		"remote url": {
			input: PushInput{Repo: "/tmp/x", Remote: "https://evil/x"},
			want:  "remote name is invalid",
		},
		"delete refspec": {
			input: PushInput{Repo: "/tmp/x", Ref: "main:evil"},
			want:  "ref name is invalid",
		},
		"force refspec": {
			input: PushInput{Repo: "/tmp/x", Ref: "+main"},
			want:  "ref name is invalid",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, _, err := h.handlePush(t.Context(), nil, tt.input)
			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.Contains(t, resultText(t, result), tt.want)
		})
	}
}

func TestHandlePush(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	commitFile(t, work, "pushed.txt", "x", "pushed commit")

	h := &handler{}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Pushed to origin")

	branch := currentBranch(t, work)
	assert.Equal(t,
		gitOut(t, "-C", work, "rev-parse", "HEAD"),
		gitOut(t, "-C", bare, "rev-parse", branch),
	)
}

func TestHandlePushSetUpstream(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	runGitCmds(t, [][]string{
		{"git", "-C", work, "switch", "-c", "feature"},
	})
	commitFile(t, work, "feature.txt", "x", "feature commit")

	h := &handler{}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{
		Repo:        work,
		Ref:         "feature",
		SetUpstream: true,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Equal(t, "origin",
		gitOut(t, "-C", work, "config", "branch.feature.remote"))
	assert.Equal(t,
		gitOut(t, "-C", work, "rev-parse", "HEAD"),
		gitOut(t, "-C", bare, "rev-parse", "feature"),
	)
}

func TestHandlePushTag(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	runGitCmds(t, [][]string{
		{"git", "-C", work, "tag", "v1.2.3"},
	})

	h := &handler{}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{
		Repo: work,
		Ref:  "v1.2.3",
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Equal(t,
		gitOut(t, "-C", work, "rev-parse", "v1.2.3"),
		gitOut(t, "-C", bare, "rev-parse", "v1.2.3"),
	)
}

func TestHandlePushTags(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	runGitCmds(t, [][]string{
		{"git", "-C", work, "tag", "v2.0.0"},
	})

	h := &handler{}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{
		Repo: work,
		Tags: true,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Equal(t,
		gitOut(t, "-C", work, "rev-parse", "v2.0.0"),
		gitOut(t, "-C", bare, "rev-parse", "v2.0.0"),
	)
}

func TestHandlePushForceWithLease(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	commitFile(t, work, "history.txt", "v1", "original commit")

	h := &handler{}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	// Rewrite history; a plain push is now non-fast-forward.
	runGitCmds(t, [][]string{
		{"git", "-C", work, "commit", "--amend", "-m", "amended commit"},
	})

	result, _, err = h.handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.True(t, result.IsError, "plain push after a rewrite must fail")

	result, _, err = h.handlePush(t.Context(), nil, PushInput{
		Repo:           work,
		ForceWithLease: true,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	branch := currentBranch(t, work)
	assert.Equal(t,
		gitOut(t, "-C", work, "rev-parse", "HEAD"),
		gitOut(t, "-C", bare, "rev-parse", branch),
	)
}

func TestHandlePushOutputCaptured(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	h := &handler{}

	// Nothing to push: git reports it on stderr, which must land in
	// the result text.
	result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Everything up-to-date")
}

func TestHandlePushUnknownRemote(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	h := &handler{}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{
		Repo:   work,
		Remote: "nosuch",
	})
	require.NoError(t, err)
	require.True(t, result.IsError)

	text := resultText(t, result)
	assert.Contains(t, text, "unknown remote: nosuch")
	assert.NotContains(t, text, bare,
		"error must name the remote, never its URL")
}

func TestHandlePushTimeout(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	h := &handler{timeout: time.Nanosecond}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "timed out")
}
