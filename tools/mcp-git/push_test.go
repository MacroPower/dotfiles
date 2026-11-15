package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pushArgsPrefix holds the options that every push takes. A push
// skips the pre-push hooks, signs nothing, follows no tag, and names
// the program that serves it.
func pushArgsPrefix() []string {
	return []string{
		"push", "--no-verify", "--no-signed", "--no-follow-tags", "--receive-pack=git-receive-pack",
	}
}

func TestBuildPushArgs(t *testing.T) {
	t.Parallel()

	push := pushArgsPrefix()

	tests := map[string]struct {
		input   PushInput
		refspec string
		want    []string
	}{
		"no refspec": {
			input: PushInput{Repo: "/r"},
			want:  slices.Concat(push, []string{"--", "origin"}),
		},
		"with refspec": {
			input:   PushInput{Repo: "/r", Ref: "feature"},
			refspec: "refs/heads/feature:refs/heads/feature",
			want:    slices.Concat(push, []string{"--", "origin", "refs/heads/feature:refs/heads/feature"}),
		},
		"with set upstream": {
			input:   PushInput{Repo: "/r", Ref: "feature", SetUpstream: true},
			refspec: "refs/heads/feature:refs/heads/feature",
			want: slices.Concat(push, []string{
				"--set-upstream", "--", "origin", "refs/heads/feature:refs/heads/feature",
			}),
		},
		"with force with lease": {
			input:   PushInput{Repo: "/r", Ref: "main", ForceWithLease: true},
			refspec: "refs/heads/main:refs/heads/main",
			want: slices.Concat(push, []string{
				"--force-with-lease", "--", "origin", "refs/heads/main:refs/heads/main",
			}),
		},
		"with tags": {
			input: PushInput{Repo: "/r", Tags: true},
			want:  slices.Concat(push, []string{"--tags", "--", "origin"}),
		},
		"all options": {
			input: PushInput{
				Repo: "/r", Remote: "up", Ref: "x",
				SetUpstream: true, ForceWithLease: true, Tags: true,
			},
			refspec: "refs/heads/x:refs/heads/x",
			want: slices.Concat(push, []string{
				"--set-upstream", "--force-with-lease", "--tags", "--", "up", "refs/heads/x:refs/heads/x",
			}),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := buildPushArgs(remoteOrDefault(tt.input.Remote), tt.input, tt.refspec)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPushArgs(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)
	branch := currentBranch(t, work)

	runGitCmds(t, [][]string{
		{"git", "-C", work, "branch", "feat/x"},
		{"git", "-C", work, "branch", "both"},
		{"git", "-C", work, "tag", "both"},
		{"git", "-C", work, "tag", "v1.0.0"},
	})

	tests := map[string]struct {
		input PushInput

		// want holds the arguments that follow the options of every
		// push.
		want []string
		err  error
	}{
		"current branch": {
			want: []string{"--", "origin", "refs/heads/" + branch + ":refs/heads/" + branch},
		},
		"branch": {
			input: PushInput{Ref: "feat/x"},
			want:  []string{"--", "origin", "refs/heads/feat/x:refs/heads/feat/x"},
		},
		"tag": {
			input: PushInput{Ref: "v1.0.0"},
			want:  []string{"--", "origin", "refs/tags/v1.0.0:refs/tags/v1.0.0"},
		},
		"full name of a branch": {
			input: PushInput{Ref: "refs/heads/both"},
			want:  []string{"--", "origin", "refs/heads/both:refs/heads/both"},
		},
		"full name of a tag": {
			input: PushInput{Ref: "refs/tags/both"},
			want:  []string{"--", "origin", "refs/tags/both:refs/tags/both"},
		},
		"branch and tags": {
			input: PushInput{Ref: "feat/x", Tags: true},
			want:  []string{"--tags", "--", "origin", "refs/heads/feat/x:refs/heads/feat/x"},
		},
		"branch with options": {
			input: PushInput{Remote: "up", Ref: "feat/x", SetUpstream: true, ForceWithLease: true},
			want: []string{
				"--set-upstream", "--force-with-lease", "--", "up", "refs/heads/feat/x:refs/heads/feat/x",
			},
		},
		// git push --tags with no refspec pushes the tags alone.
		"tags alone": {
			input: PushInput{Tags: true},
			want:  []string{"--tags", "--", "origin"},
		},
		"name of a branch and a tag": {
			input: PushInput{Ref: "both"},
			err:   ErrAmbiguousRef,
		},
		// git lists refs/heads/feat/x for refs/heads/feat.
		"directory of branches": {
			input: PushInput{Ref: "feat"},
			err:   ErrUnknownRef,
		},
		"unknown name": {
			input: PushInput{Ref: "nosuch"},
			err:   ErrUnknownRef,
		},
		"full name of an unknown branch": {
			input: PushInput{Ref: "refs/heads/nosuch"},
			err:   ErrUnknownRef,
		},
		"remote-tracking branch": {
			input: PushInput{Ref: "origin/" + branch},
			err:   ErrUnknownRef,
		},
		"full name outside the branches and the tags": {
			input: PushInput{Ref: "refs/remotes/origin/" + branch},
			err:   ErrUnknownRef,
		},
		"HEAD": {
			input: PushInput{Ref: "HEAD"},
			err:   ErrUnknownRef,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := pushArgs(t.Context(), work, remoteOrDefault(tt.input.Remote), tt.input)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, slices.Concat(pushArgsPrefix(), tt.want), got)
		})
	}
}

func TestPushArgsDetachedHead(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	runGitCmds(t, [][]string{
		{"git", "-C", work, "switch", "-q", "--detach"},
		{"git", "-C", work, "tag", "v1.0.0"},
	})

	_, err := pushArgs(t.Context(), work, defaultRemote, PushInput{})
	require.ErrorIs(t, err, ErrNoBranch)

	got, err := pushArgs(t.Context(), work, defaultRemote, PushInput{Ref: "v1.0.0"})
	require.NoError(t, err)
	assert.Equal(t,
		slices.Concat(pushArgsPrefix(), []string{"--", "origin", "refs/tags/v1.0.0:refs/tags/v1.0.0"}),
		got,
	)

	got, err = pushArgs(t.Context(), work, defaultRemote, PushInput{Tags: true})
	require.NoError(t, err)
	assert.Equal(t, slices.Concat(pushArgsPrefix(), []string{"--tags", "--", "origin"}), got)

	_, err = pushArgs(t.Context(), t.TempDir(), defaultRemote, PushInput{})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoBranch)
}

// git applies push.autoSetupRemote only to a push that names no
// refspec, and the server always names one. The server therefore
// records the upstream itself for a push of the current branch.
func TestPushArgsAutoSetupRemote(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// config holds the arguments of each git config call that the
		// test runs in the clone.
		config [][]string
		input  PushInput

		// want is set when the push records the upstream.
		want bool
		err  bool
	}{
		"key is true": {
			config: [][]string{{"push.autoSetupRemote", "true"}},
			want:   true,
		},
		"key is yes": {
			config: [][]string{{"push.autoSetupRemote", "yes"}},
			want:   true,
		},
		"key is false": {
			config: [][]string{{"push.autoSetupRemote", "false"}},
		},
		"key is no boolean": {
			config: [][]string{{"push.autoSetupRemote", "sometimes"}},
			err:    true,
		},
		"branch has an upstream": {
			config: [][]string{
				{"push.autoSetupRemote", "true"},
				{"branch.feat/x.remote", "origin"},
				{"branch.feat/x.merge", "refs/heads/other"},
			},
		},
		"branch names a remote and no branch of it": {
			config: [][]string{
				{"push.autoSetupRemote", "true"},
				{"branch.feat/x.remote", "origin"},
			},
			want: true,
		},
		// git applies the key to a push that names no refspec.
		"call names the branch": {
			config: [][]string{{"push.autoSetupRemote", "true"}},
			input:  PushInput{Ref: "feat/x"},
		},
		"call sets the upstream": {
			input: PushInput{SetUpstream: true},
			want:  true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bare := initBareRepo(t)
			work := initWorkClone(t, bare)

			runGitCmds(t, [][]string{{"git", "-C", work, "switch", "-q", "-c", "feat/x"}})

			for _, args := range tt.config {
				runGitCmds(t, [][]string{slices.Concat([]string{"git", "-C", work, "config"}, args)})
			}

			got, err := pushArgs(t.Context(), work, defaultRemote, tt.input)
			if tt.err {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, slices.Contains(got, "--set-upstream"))
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

	h := &handler{allowFileURLs: true}

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

	h := &handler{allowFileURLs: true}

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

// A push of the current branch names its refspec, and it still
// records the upstream of a new branch when push.autoSetupRemote asks
// for that.
func TestHandlePushAutoSetupRemote(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	runGitCmds(t, [][]string{
		{"git", "-C", work, "switch", "-c", "feature"},
		{"git", "-C", work, "config", "push.autoSetupRemote", "true"},
	})
	commitFile(t, work, "feature.txt", "x", "feature commit")

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Equal(t, "origin",
		gitOut(t, "-C", work, "config", "branch.feature.remote"))
	assert.Equal(t, "refs/heads/feature",
		gitOut(t, "-C", work, "config", "branch.feature.merge"))
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

	h := &handler{allowFileURLs: true}

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

	h := &handler{allowFileURLs: true}

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

	h := &handler{allowFileURLs: true}

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

	h := &handler{allowFileURLs: true}

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

func TestHandlePushSkipsHooks(t *testing.T) {
	t.Parallel()

	// configHookLine is the line of the note that names the hook check
	// from git config.
	const configHookLine = "- the hook check in git config (hook.check.command)"

	tests := map[string]struct {
		// install adds hooks to the clone at work. Each hook runs
		// script, which leaves a marker file and then rejects the
		// push. install returns the lines that the note of the result
		// must hold, one for each skipped pre-push hook, or nil when
		// the result must hold no note.
		install func(t *testing.T, work, script string) []string
	}{
		"hook file in the git directory": {
			install: func(t *testing.T, work, script string) []string {
				t.Helper()

				return []string{"- the hook file " + writeHook(t, work, "pre-push", script)}
			},
		},
		// husky keeps its hooks in the working tree, where every
		// sandboxed command can edit them.
		"hook file in the working tree": {
			install: func(t *testing.T, work, script string) []string {
				t.Helper()

				runGitCmds(t, [][]string{
					{"git", "-C", work, "config", "core.hooksPath", ".husky"},
				})

				return []string{
					"- the hook file " + writeHookIn(t, filepath.Join(work, ".husky"), "pre-push", script),
				}
			},
		},
		// git push also fires reference-transaction when it updates
		// the remote-tracking ref.
		"hook file for another event": {
			install: func(t *testing.T, work, script string) []string {
				t.Helper()

				writeHook(t, work, "reference-transaction", script)

				return nil
			},
		},
		// Git 2.54 and later run a hook that git config defines.
		"hook in git config": {
			install: func(t *testing.T, work, script string) []string {
				t.Helper()

				writeConfigHook(t, work, script)

				return []string{configHookLine}
			},
		},
		"hook file and hook in git config": {
			install: func(t *testing.T, work, script string) []string {
				t.Helper()

				writeConfigHook(t, work, script)

				return []string{
					"- the hook file " + writeHook(t, work, "pre-push", script),
					configHookLine,
				}
			},
		},
		"disabled hook in git config": {
			install: func(t *testing.T, work, script string) []string {
				t.Helper()

				writeConfigHook(t, work, script)
				runGitCmds(t, [][]string{
					{"git", "-C", work, "config", "hook.check.enabled", "false"},
				})

				return nil
			},
		},
		"hook file that is not executable": {
			install: func(t *testing.T, work, script string) []string {
				t.Helper()

				hook := writeHook(t, work, "pre-push", script)
				require.NoError(t, os.Chmod(hook, 0o644))

				return nil
			},
		},
		"no hook": {
			install: func(*testing.T, string, string) []string { return nil },
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bare := initBareRepo(t)

			// The note names a hook file below the resolved repository
			// path, so the lines that install builds must use it too.
			work, err := filepath.EvalSymlinks(initWorkClone(t, bare))
			require.NoError(t, err)

			commitFile(t, work, "pushed.txt", "x", "pushed commit")

			marker := filepath.Join(t.TempDir(), "hook-ran")
			want := tt.install(t, work, "#!/bin/sh\ntouch '"+marker+"'\nexit 1\n")

			h := &handler{allowFileURLs: true}

			// Hooks execute outside the caller's sandbox, so the push
			// tool must never run them. A hook that runs also rejects
			// the push.
			result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			_, statErr := os.Stat(marker)
			require.ErrorIs(t, statErr, os.ErrNotExist,
				"hooks must not run during git_push")

			assert.Equal(t,
				gitOut(t, "-C", work, "rev-parse", "HEAD"),
				gitOut(t, "-C", bare, "rev-parse", currentBranch(t, work)),
			)

			// The caller must learn about each pre-push hook that the
			// push skipped, so that it can run the hook itself.
			text := resultText(t, result)
			if want == nil {
				assert.NotContains(t, text, "so it skipped these")

				return
			}

			assert.Contains(t, text, "so it skipped these pre-push hooks:")

			for _, line := range want {
				assert.Contains(t, text, "\n"+line+"\n")
			}
		})
	}
}

// writeConfigHook defines the pre-push hook check in the git config
// of repo. The hook runs script.
func writeConfigHook(t *testing.T, repo, script string) {
	t.Helper()

	hook := writeHookIn(t, t.TempDir(), "check", script)

	runGitCmds(t, [][]string{
		{"git", "-C", repo, "config", "hook.check.command", hook},
		{"git", "-C", repo, "config", "hook.check.event", "pre-push"},
	})
}

// writeReceiveHooks installs in the bare repository each hook that
// receive-pack runs during a push. Each hook appends its path to a
// marker file, and writeReceiveHooks returns the path of the marker.
func writeReceiveHooks(t *testing.T, bare string) string {
	t.Helper()

	marker := filepath.Join(t.TempDir(), "hook-ran")
	script := "#!/bin/sh\necho \"$0\" >> '" + marker + "'\n"

	for _, event := range []string{
		"pre-receive", "update", "reference-transaction", "post-receive", "post-update",
	} {
		writeHookIn(t, filepath.Join(bare, "hooks"), event, script)
	}

	return marker
}

func TestHandlePushRefusesLocalRemote(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// remote points the origin remote of the clone at work at the
		// bare repository, in the form that the case names.
		remote func(t *testing.T, work, bare string)

		// reachable is set when every push URL of the remote names
		// the bare repository. A push with file URLs allowed then
		// stays off the network.
		reachable bool
	}{
		// initWorkClone leaves origin at the path of the bare
		// repository.
		"path": {
			remote:    func(*testing.T, string, string) {},
			reachable: true,
		},
		"file URL": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				runGitCmds(t, [][]string{
					{"git", "-C", work, "remote", "set-url", "origin", "file://" + bare},
				})
			},
			reachable: true,
		},
		"pushInsteadOf rewrite": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				runGitCmds(t, [][]string{
					{
						"git",
						"-C",
						work,
						"remote",
						"set-url",
						"origin",
						"https://example.invalid/" + filepath.Base(bare),
					},
					{
						"git",
						"-C",
						work,
						"config",
						"url." + filepath.Dir(bare) + "/.pushInsteadOf",
						"https://example.invalid/",
					},
				})
			},
			reachable: true,
		},
		// git pushes to every push URL of the remote.
		"second push URL": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				runGitCmds(t, [][]string{
					{"git", "-C", work, "remote", "set-url", "--push", "origin", "https://example.invalid/repo.git"},
					{"git", "-C", work, "remote", "set-url", "--add", "--push", "origin", bare},
				})
			},
		},
		// git also reads a remote from a file in the remotes directory
		// of the git directory, outside the config file.
		"remotes directory": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				branch := currentBranch(t, work)
				dir := filepath.Join(work, ".git", "remotes")
				def := "URL: " + bare + "\nPush: refs/heads/" + branch + ":refs/heads/" + branch + "\n"

				runGitCmds(t, [][]string{
					{"git", "-C", work, "remote", "remove", "origin"},
				})
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "origin"), []byte(def), 0o644))

				//nolint:gosec // test command
				cmd := exec.CommandContext(t.Context(), "git", "-C", work, "remote", "get-url", "origin")
				if cmd.Run() != nil {
					t.Skip("git does not read remotes from the remotes directory")
				}
			},
			reachable: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bare := initBareRepo(t)
			work := initWorkClone(t, bare)

			commitFile(t, work, "pushed.txt", "x", "pushed commit")
			tt.remote(t, work, bare)

			marker := writeReceiveHooks(t, bare)
			branch := currentBranch(t, work)
			before := gitOut(t, "-C", bare, "rev-parse", branch)

			result, _, err := (&handler{}).handlePush(t.Context(), nil, PushInput{Repo: work})
			require.NoError(t, err)
			require.True(t, result.IsError, "a push to a local repository must fail")

			text := resultText(t, result)
			assert.Contains(t, text, ErrDeniedURL.Error()+": remote origin")
			assert.NotContains(t, text, filepath.Dir(bare),
				"error must name the remote, never its URL")

			_, statErr := os.Stat(marker)
			require.ErrorIs(t, statErr, os.ErrNotExist,
				"hooks of the receiving repository must not run")
			assert.Equal(t, before, gitOut(t, "-C", bare, "rev-parse", branch))

			if !tt.reachable {
				return
			}

			// A push that reaches the bare repository runs its hooks,
			// so the refusal is what kept them from running.
			result, _, err = (&handler{allowFileURLs: true}).handlePush(
				t.Context(), nil, PushInput{Repo: work},
			)
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))
			assert.FileExists(t, marker)
		})
	}
}

// TestPushRefusesLocalTransport covers a remote whose definition
// changes after runRemote checks its URLs. git resolves the remote
// again for the push, so the git command itself must refuse a remote
// that names a repository on the local file system.
func TestPushRefusesLocalTransport(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// url returns the URL that the remote names when git pushes.
		url func(bare string) string
	}{
		"path":     {url: func(bare string) string { return bare }},
		"file URL": {url: func(bare string) string { return "file://" + bare }},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bare := initBareRepo(t)
			work := initWorkClone(t, bare)

			commitFile(t, work, "pushed.txt", "x", "pushed commit")
			runGitCmds(t, [][]string{
				{"git", "-C", work, "remote", "set-url", "origin", tt.url(bare)},
			})

			marker := writeReceiveHooks(t, bare)
			branch := currentBranch(t, work)
			before := gitOut(t, "-C", bare, "rev-parse", branch)

			op := remoteOp{
				remote: defaultRemote,
				push:   true,
				args: buildPushArgs(
					defaultRemote, PushInput{Repo: work, Ref: branch}, "refs/heads/"+branch+":refs/heads/"+branch,
				),
			}

			// The check saw an https URL, and the remote names the
			// bare repository by the time git pushes.
			const checked = "https://example.invalid/repo.git"

			// push runs the git command of the push as h runs it.
			push := func(h *handler) (string, error) {
				return h.runGit(t.Context(), slices.Concat(h.gitOptions(op, work, checked, work, nil), op.args)...)
			}

			_, err := push(&handler{})
			require.Error(t, err, "git must refuse a push to a local repository")

			_, statErr := os.Stat(marker)
			require.ErrorIs(t, statErr, os.ErrNotExist,
				"hooks of the receiving repository must not run")
			assert.Equal(t, before, gitOut(t, "-C", bare, "rev-parse", branch))

			// Without the refusal, the same command pushes and runs
			// the hooks of the bare repository.
			out, err := push(&handler{allowFileURLs: true})
			require.NoError(t, err, out)
			assert.FileExists(t, marker)
		})
	}
}

// submoduleRepos holds a parent repository with a submodule, and the
// bare repository that each of them pushes to.
type submoduleRepos struct {
	work    string // the parent repository
	sub     string // the submodule, inside work
	bare    string // the upstream of work
	subBare string // the upstream of sub

	// marker is the file that the pre-push hook of the submodule
	// creates when it runs.
	marker string
}

// initSubmoduleRepos creates a parent repository with a submodule,
// and pushes the parent. The upstream of the submodule holds a commit
// that only the tag v1 reaches, as a release tag of a third-party
// project does. The git config of the submodule defines a pre-push
// hook. Git 2.54 and later run such a hook, and a git that does not
// know the keys ignores them.
func initSubmoduleRepos(t *testing.T) submoduleRepos {
	t.Helper()

	subBare := initBareRepo(t)

	release := initWorkClone(t, subBare)
	runGitCmds(t, [][]string{{"git", "-C", release, "switch", "-q", "--detach"}})
	commitFile(t, release, "release.txt", "x", "release v1")
	runGitCmds(t, [][]string{
		{"git", "-C", release, "tag", "v1"},
		{"git", "-C", release, "push", "-q", "origin", "v1"},
	})

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)
	sub := filepath.Join(work, "sub")

	marker := filepath.Join(t.TempDir(), "hook-ran")
	hook := writeHookIn(t, t.TempDir(), "check", "#!/bin/sh\ntouch '"+marker+"'\n")

	runGitCmds(t, [][]string{
		{"git", "-C", work, "-c", "protocol.file.allow=always", "submodule", "add", subBare, "sub"},
		{"git", "-C", work, "commit", "-m", "add submodule"},
		{"git", "-C", work, "push"},
		{"git", "-C", sub, "config", "user.email", "test@test.com"},
		{"git", "-C", sub, "config", "user.name", "Test"},
		{"git", "-C", sub, "config", "hook.check.command", hook},
		{"git", "-C", sub, "config", "hook.check.event", "pre-push"},
	})

	return submoduleRepos{work: work, sub: sub, bare: bare, subBare: subBare, marker: marker}
}

// advanceSubmodule pushes a new commit to the upstream of the
// submodule, and a commit to the upstream of the parent that moves
// the submodule to the new commit. A fetch in the parent that recurses
// into submodules then fetches the new commit into the submodule. It
// returns the branch of the submodule upstream that holds the new
// commit.
func advanceSubmodule(t *testing.T, s submoduleRepos) string {
	t.Helper()

	other := initWorkClone(t, s.bare)
	sub := filepath.Join(other, "sub")
	branch := gitOut(t, "-C", s.subBare, "symbolic-ref", "--short", "HEAD")

	runGitCmds(t, [][]string{
		{"git", "-C", other, "-c", "protocol.file.allow=always", "submodule", "update", "-q", "--init"},
		{"git", "-C", sub, "config", "user.email", "test@test.com"},
		{"git", "-C", sub, "config", "user.name", "Test"},
	})
	commitFile(t, sub, "next.txt", "x", "next submodule commit")
	runGitCmds(t, [][]string{
		{"git", "-C", sub, "push", "-q", "origin", "HEAD:refs/heads/" + branch},
	})
	commitFile(t, other, "bump.txt", "x", "bump submodule")
	runGitCmds(t, [][]string{{"git", "-C", other, "push", "-q"}})

	return branch
}

// The git config can ask git push to recurse into submodules. git
// then runs commands in each submodule, under the git config of the
// submodule. A push therefore never pushes a submodule and never
// checks one, whatever the config asks for.
func TestHandlePushSubmodule(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// config is a key and a value that the test sets in the git
		// config of the parent.
		config []string

		// tag bumps the submodule to the commit of the tag v1, which
		// no remote-tracking ref of the submodule reaches. Otherwise
		// the test bumps the submodule to a new commit that is on no
		// remote.
		tag bool
	}{
		"default config with a tag commit": {tag: true},
		"default config with a new commit": {},
		"push.recurseSubmodules no": {
			config: []string{"push.recurseSubmodules", "no"},
		},
		"push.recurseSubmodules check": {
			config: []string{"push.recurseSubmodules", "check"},
		},
		// With on-demand, git push starts a push in each submodule
		// that holds a commit that the push references.
		"push.recurseSubmodules on-demand": {
			config: []string{"push.recurseSubmodules", "on-demand"},
		},
		"push.recurseSubmodules only": {
			config: []string{"push.recurseSubmodules", "only"},
		},
		"submodule.recurse": {
			config: []string{"submodule.recurse", "true"},
		},
		"push.recurseSubmodules on-demand with a tag commit": {
			config: []string{"push.recurseSubmodules", "on-demand"},
			tag:    true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s := initSubmoduleRepos(t)

			if tt.config != nil {
				runGitCmds(t, [][]string{slices.Concat([]string{"git", "-C", s.work, "config"}, tt.config)})
			}

			if tt.tag {
				runGitCmds(t, [][]string{{"git", "-C", s.sub, "checkout", "-q", "v1"}})
			} else {
				commitFile(t, s.sub, "sub.txt", "x", "submodule commit")
			}

			commitFile(t, s.work, "parent.txt", "x", "bump submodule")

			subBefore := gitOut(t, "-C", s.subBare, "for-each-ref")

			h := &handler{allowFileURLs: true}

			result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: s.work})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			assert.Equal(t,
				gitOut(t, "-C", s.work, "rev-parse", "HEAD"),
				gitOut(t, "-C", s.bare, "rev-parse", currentBranch(t, s.work)),
			)
			assert.Equal(t, subBefore, gitOut(t, "-C", s.subBare, "for-each-ref"),
				"git_push must not push a submodule")

			_, statErr := os.Stat(s.marker)
			require.ErrorIs(t, statErr, os.ErrNotExist,
				"the pre-push hook of the submodule must not run")
		})
	}
}

// A submodule can be a partial clone. git then fetches each commit
// that the submodule lacks from the promisor remote of the submodule,
// and the fetch reads the git config of the submodule. With
// push.recurseSubmodules=check, git looks for each submodule commit
// that a push references, so the caller can make the fetch run a
// command. The test uses the uploadpack key, which names a command for
// a remote on the local file system. A credential helper runs the
// same way for an https remote.
func TestHandlePushRunsNoSubmoduleFetch(t *testing.T) {
	t.Parallel()

	configs := map[string][]string{
		"push.recurseSubmodules check":     {"push.recurseSubmodules", "check"},
		"push.recurseSubmodules on-demand": {"push.recurseSubmodules", "on-demand"},
		"submodule.recurse":                {"submodule.recurse", "true"},
	}

	// missing names a commit that no repository of the test holds.
	const missing = "1111111111111111111111111111111111111111"

	for name, config := range configs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bare := initBareRepo(t)
			work := initWorkClone(t, bare)
			sub := filepath.Join(work, "sub")
			promisor := initBareRepo(t)
			script, marker := markerScript(t)

			runGitCmds(t, [][]string{
				{"git", "init", "-q", sub},
				{"git", "-C", sub, "config", "user.email", "test@test.com"},
				{"git", "-C", sub, "config", "user.name", "Test"},
				{"git", "-C", sub, "commit", "-q", "--allow-empty", "-m", "submodule commit"},
				{"git", "-C", sub, "update-ref", "refs/remotes/promisor/main", "HEAD"},
				{"git", "-C", sub, "config", "extensions.partialClone", "promisor"},
				{"git", "-C", sub, "config", "remote.promisor.promisor", "true"},
				{"git", "-C", sub, "config", "remote.promisor.url", promisor},
				{"git", "-C", sub, "config", "remote.promisor.uploadpack", script},
				{"git", "-C", work, "update-index", "--add", "--cacheinfo", "160000," + missing + ",sub"},
				{"git", "-C", work, "commit", "-q", "-m", "reference a submodule commit"},
				slices.Concat([]string{"git", "-C", work, "config"}, config),
			})

			h := &handler{allowFileURLs: true}

			result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			requireNotRun(t, marker)
			assert.Equal(t,
				gitOut(t, "-C", work, "rev-parse", "HEAD"),
				gitOut(t, "-C", bare, "rev-parse", currentBranch(t, work)),
			)
		})
	}
}

// pushConfigs maps a name to the git config keys of a repository that
// make a plain git push update more than the current branch, or
// another ref. Each entry holds the arguments of one git config call.
// The keys name the branch of the remote as base, and the pushed
// branch is feat/x.
func pushConfigs(base string) map[string][][]string {
	return map[string][][]string{
		"no config": nil,
		"remote.origin.push to another branch": {
			{"remote.origin.push", "+refs/heads/feat/x:refs/heads/" + base},
		},
		"remote.origin.push of every branch": {
			{"remote.origin.push", "+refs/heads/*:refs/heads/*"},
		},
		"remote.origin.mirror": {
			{"remote.origin.mirror", "true"},
		},
		"push.default matching": {
			{"push.default", "matching"},
		},
		"push.default upstream": {
			{"push.default", "upstream"},
			{"branch.feat/x.remote", "origin"},
			{"branch.feat/x.merge", "refs/heads/" + base},
		},
		"push.followTags": {
			{"push.followTags", "true"},
		},
	}
}

// refObjects returns the object of each ref of the repository at
// repo, by the name of the ref.
func refObjects(t *testing.T, repo string) map[string]string {
	t.Helper()

	refs := map[string]string{}

	for line := range strings.SplitSeq(gitOut(t, "-C", repo, "for-each-ref", "--format=%(refname) %(objectname)"), "\n") {
		name, object, ok := strings.Cut(line, " ")
		require.True(t, ok, "ref listing must pair a name with an object: %q", line)

		refs[name] = object
	}

	return refs
}

// The caller can write the git config of the repository, and that
// config names the refs that a plain git push updates. A push must
// update only the ref of the call, so that a push of a feature branch
// never moves another branch of the remote.
func TestHandlePushIgnoresPushConfig(t *testing.T) {
	t.Parallel()

	calls := map[string]struct {
		input PushInput

		// want names the ref that the push adds to the remote.
		want string
	}{
		"current branch":          {want: "refs/heads/feat/x"},
		"named branch":            {input: PushInput{Ref: "feat/x"}, want: "refs/heads/feat/x"},
		"named branch with force": {input: PushInput{Ref: "feat/x", ForceWithLease: true}, want: "refs/heads/feat/x"},
		"tags alone":              {input: PushInput{Tags: true}, want: "refs/tags/v1"},
	}

	for callName, call := range calls {
		for configName, config := range pushConfigs("base") {
			t.Run(callName+"/"+configName, func(t *testing.T) {
				t.Parallel()

				bare := initBareRepo(t)
				work := initWorkClone(t, bare)

				// The remote holds the branch base, and the clone is
				// ahead of it on base and on feat/x. The clone also
				// holds an annotated tag on the pushed commit.
				runGitCmds(t, [][]string{
					{"git", "-C", work, "switch", "-q", "-c", "base"},
					{"git", "-C", work, "push", "-q", "origin", "base"},
				})
				commitFile(t, work, "base.txt", "x", "local commit on base")
				runGitCmds(t, [][]string{{"git", "-C", work, "switch", "-q", "-c", "feat/x"}})
				commitFile(t, work, "feature.txt", "x", "feature commit")
				runGitCmds(t, [][]string{{"git", "-C", work, "tag", "-a", "-m", "release", "v1"}})

				for _, args := range config {
					runGitCmds(t, [][]string{slices.Concat([]string{"git", "-C", work, "config"}, args)})
				}

				want := refObjects(t, bare)
				want[call.want] = gitOut(t, "-C", work, "rev-parse", call.want)

				input := call.input
				input.Repo = work

				result, _, err := (&handler{allowFileURLs: true}).handlePush(t.Context(), nil, input)
				require.NoError(t, err)
				require.False(t, result.IsError, resultText(t, result))

				assert.Equal(t, want, refObjects(t, bare))
			})
		}
	}
}

// The server of the remote acts on the push options that it receives.
// A GitLab server can merge the pushed branch into the default branch.
// The caller can write the git config of the repository, so a push
// sends no push option that push.pushOption sets.
func TestHandlePushSendsNoPushOption(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	commitFile(t, work, "pushed.txt", "x", "pushed commit")

	// receive-pack runs the pre-receive hook with the number of push
	// options that the push sent.
	count := filepath.Join(t.TempDir(), "push-option-count")
	writeHookIn(t, filepath.Join(bare, "hooks"), "pre-receive",
		"#!/bin/sh\necho \"$GIT_PUSH_OPTION_COUNT\" > '"+count+"'\n")

	runGitCmds(t, [][]string{
		{"git", "-C", bare, "config", "receive.advertisePushOptions", "true"},
		{"git", "-C", work, "config", "push.pushOption", "merge_request.create"},
		{"git", "-C", work, "config", "--add", "push.pushOption", "merge_request.target=main"},
	})

	result, _, err := (&handler{allowFileURLs: true}).handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	got, err := os.ReadFile(count)
	require.NoError(t, err, "the pre-receive hook of the remote must run")
	assert.Equal(t, "0\n", string(got), "the push must send no push option")
	assert.Equal(t,
		gitOut(t, "-C", work, "rev-parse", "HEAD"),
		gitOut(t, "-C", bare, "rev-parse", currentBranch(t, work)),
	)
}

// A push that names no ref needs a current branch.
func TestHandlePushDetachedHead(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	commitFile(t, work, "pushed.txt", "x", "pushed commit")
	runGitCmds(t, [][]string{{"git", "-C", work, "switch", "-q", "--detach"}})

	before := refObjects(t, bare)

	result, _, err := (&handler{allowFileURLs: true}).handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), ErrNoBranch.Error())
	assert.Equal(t, before, refObjects(t, bare))
}

// push.gpgSign makes git sign a push with the program of gpg.program
// when the remote accepts signed pushes. The program runs outside the
// caller's sandbox, so the push tool never signs a push.
func TestHandlePushNeverSigns(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	commitFile(t, work, "pushed.txt", "x", "pushed commit")

	script, marker := markerScript(t)

	runGitCmds(t, [][]string{
		{"git", "-C", bare, "config", "receive.certNonceSeed", "seed"},
		{"git", "-C", work, "config", "push.gpgSign", "true"},
		{"git", "-C", work, "config", "gpg.program", script},
	})

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	requireNotRun(t, marker)
	assert.Equal(t,
		gitOut(t, "-C", work, "rev-parse", "HEAD"),
		gitOut(t, "-C", bare, "rev-parse", currentBranch(t, work)),
	)
}

func TestHandlePushTimeout(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	h := &handler{allowFileURLs: true, timeout: time.Nanosecond}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "timed out")
}
