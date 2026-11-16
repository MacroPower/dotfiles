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

	work := initWorkClone(t, initBareRepo(t))
	branch := defaultBranch(t)

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

	work := initWorkClone(t, initBareRepo(t))

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
		// config holds each key that the test sets in the git config
		// of the clone, with its value after it.
		config []string
		input  PushInput

		// want is set when the push records the upstream.
		want bool
		err  bool
	}{
		"key is true": {
			config: []string{"push.autoSetupRemote", "true"},
			want:   true,
		},
		"key is yes": {
			config: []string{"push.autoSetupRemote", "yes"},
			want:   true,
		},
		"key is false": {
			config: []string{"push.autoSetupRemote", "false"},
		},
		"key is no boolean": {
			config: []string{"push.autoSetupRemote", "sometimes"},
			err:    true,
		},
		"branch has an upstream": {
			config: []string{
				"push.autoSetupRemote", "true",
				"branch.feat/x.remote", "origin",
				"branch.feat/x.merge", "refs/heads/other",
			},
		},
		"branch names a remote and no branch of it": {
			config: []string{
				"push.autoSetupRemote", "true",
				"branch.feat/x.remote", "origin",
			},
			want: true,
		},
		// git applies the key to a push that names no refspec.
		"call names the branch": {
			config: []string{"push.autoSetupRemote", "true"},
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

			// The clone has the new branch feat/x checked out.
			work := initFeatureRepos(t).work

			setConfig(t, work, tt.config...)

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

	bare, work := initRepos(t, false, true)

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Pushed to origin")

	assert.Equal(t, repos(t).localCommit, gitOut(t, "-C", bare, "rev-parse", defaultBranch(t)))
}

func TestHandlePushSetUpstream(t *testing.T) {
	t.Parallel()

	f := initFeatureRepos(t)

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{
		Repo:        f.work,
		Ref:         "feat/x",
		SetUpstream: true,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Equal(t, "origin",
		gitOut(t, "-C", f.work, "config", "branch.feat/x.remote"))
	assert.Equal(t, f.workRefs["refs/heads/feat/x"], refObjects(t, f.bare)["refs/heads/feat/x"])
}

// A push of the current branch names its refspec, and it still
// records the upstream of a new branch when push.autoSetupRemote asks
// for that.
func TestHandlePushAutoSetupRemote(t *testing.T) {
	t.Parallel()

	f := initFeatureRepos(t)

	setConfig(t, f.work, "push.autoSetupRemote", "true")

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: f.work})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Equal(t,
		"branch.feat/x.remote origin\nbranch.feat/x.merge refs/heads/feat/x",
		gitOut(t, "-C", f.work, "config", "--get-regexp", `^branch\.feat/x\.`),
	)
	assert.Equal(t, f.workRefs["refs/heads/feat/x"], refObjects(t, f.bare)["refs/heads/feat/x"])
}

func TestHandlePushTag(t *testing.T) {
	t.Parallel()

	f := initFeatureRepos(t)

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{
		Repo: f.work,
		Ref:  "v1",
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	want := f.bareRefs
	want["refs/tags/v1"] = f.workRefs["refs/tags/v1"]

	assert.Equal(t, want, refObjects(t, f.bare))
}

func TestHandlePushTags(t *testing.T) {
	t.Parallel()

	f := initFeatureRepos(t)

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{
		Repo: f.work,
		Tags: true,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	want := f.bareRefs
	want["refs/tags/v1"] = f.workRefs["refs/tags/v1"]

	assert.Equal(t, want, refObjects(t, f.bare))
}

func TestHandlePushForceWithLease(t *testing.T) {
	t.Parallel()

	bare, work := initRepos(t, false, true)

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

	assert.Equal(t,
		gitOut(t, "-C", work, "rev-parse", "HEAD"),
		gitOut(t, "-C", bare, "rev-parse", defaultBranch(t)),
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

				setConfig(t, work, "core.hooksPath", ".husky")

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
				setConfig(t, work, "hook.check.enabled", "false")

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

			bare, work := initRepos(t, false, true)

			// The note names a hook file below the resolved repository
			// path, so the lines that install builds must use it too.
			work, err := filepath.EvalSymlinks(work)
			require.NoError(t, err)

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

			assert.Equal(t, repos(t).localCommit, gitOut(t, "-C", bare, "rev-parse", defaultBranch(t)))

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

	setConfig(t, repo,
		"hook.check.command", hook,
		"hook.check.event", "pre-push",
	)
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
		// The clone names its origin by the path of the bare
		// repository.
		"path": {
			remote:    func(*testing.T, string, string) {},
			reachable: true,
		},
		"file URL": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				setOriginURL(t, work, bare, "file://"+bare)
			},
			reachable: true,
		},
		"pushInsteadOf rewrite": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				setOriginURL(t, work, bare, "https://example.invalid/"+filepath.Base(bare))
				setConfig(t, work, "url."+filepath.Dir(bare)+"/.pushInsteadOf", "https://example.invalid/")
			},
			reachable: true,
		},
		// git pushes to every push URL of the remote.
		"second push URL": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				setConfig(t, work,
					"remote.origin.pushurl", "https://example.invalid/repo.git",
					"remote.origin.pushurl", bare,
				)
			},
		},
		// git also reads a remote from a file in the remotes directory
		// of the git directory, outside the config file.
		"remotes directory": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				branch := defaultBranch(t)
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

			bare, work := initRepos(t, false, true)

			tt.remote(t, work, bare)

			marker := writeReceiveHooks(t, bare)
			before := readTree(t, bare)

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
			requireUnchanged(t, before, bare)

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

			bare, work := initRepos(t, false, true)

			setOriginURL(t, work, bare, tt.url(bare))

			marker := writeReceiveHooks(t, bare)
			branch := defaultBranch(t)
			before := readTree(t, bare)

			op := remoteOp{
				remote: defaultRemote,
				push:   true,
				args: buildPushArgs(
					defaultRemote, PushInput{Repo: work, Ref: branch}, "refs/heads/"+branch+":refs/heads/"+branch,
				),
			}

			// push runs the git command of the push as h runs it after
			// its checks pass. The remote names the bare repository by
			// the time git pushes.
			push := func(h *handler) (string, error) {
				return h.runGit(t.Context(), slices.Concat(h.gitOptions(op, work, work, nil), op.args)...)
			}

			_, err := push(&handler{})
			require.Error(t, err, "git must refuse a push to a local repository")

			_, statErr := os.Stat(marker)
			require.ErrorIs(t, statErr, os.ErrNotExist,
				"hooks of the receiving repository must not run")
			requireUnchanged(t, before, bare)

			// Without the refusal, the same command pushes and runs
			// the hooks of the bare repository.
			out, err := push(&handler{allowFileURLs: true})
			require.NoError(t, err, out)
			assert.FileExists(t, marker)
		})
	}
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

			// The git config of the submodule defines a pre-push hook.
			s := initSubmoduleRepos(t, "pre-push")

			setConfig(t, s.work, tt.config...)

			if tt.tag {
				runGitCmds(t, [][]string{{"git", "-C", s.sub, "checkout", "-q", "v1"}})
			} else {
				commitFile(t, s.sub, "sub.txt", "x", "submodule commit")
			}

			commitFile(t, s.work, "parent.txt", "x", "bump submodule")

			h := &handler{allowFileURLs: true}

			result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: s.work})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			assert.Equal(t,
				gitOut(t, "-C", s.work, "rev-parse", "HEAD"),
				gitOut(t, "-C", s.bare, "rev-parse", defaultBranch(t)),
			)
			assert.Equal(t, s.subBareRefs, gitOut(t, "-C", s.subBare, "for-each-ref"),
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

			initRepoAt(t, sub)
			appendGitConfig(t, sub, identityConfig)
			runGitCmds(t, [][]string{
				{"git", "-C", sub, "commit", "-q", "--allow-empty", "-m", "submodule commit"},
				{"git", "-C", sub, "update-ref", "refs/remotes/promisor/main", "HEAD"},
			})
			setConfig(t, sub,
				"extensions.partialClone", "promisor",
				"remote.promisor.promisor", "true",
				"remote.promisor.url", promisor,
				"remote.promisor.uploadpack", script,
			)
			runGitCmds(t, [][]string{
				{"git", "-C", work, "update-index", "--add", "--cacheinfo", "160000," + missing + ",sub"},
				{"git", "-C", work, "commit", "-q", "-m", "reference a submodule commit"},
			})
			setConfig(t, work, config...)

			h := &handler{allowFileURLs: true}

			result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			requireNotRun(t, marker)
			assert.Equal(t,
				gitOut(t, "-C", work, "rev-parse", "HEAD"),
				gitOut(t, "-C", bare, "rev-parse", defaultBranch(t)),
			)
		})
	}
}

// pushConfigs maps a name to git config keys of a repository that
// make a plain git push update more than the current branch, or
// another ref. Each entry holds every key with its value after it. The
// keys name the branch of the remote as base, and the pushed branch is
// feat/x.
//
// remote.origin.push and each value of push.default name the refs of a
// push that names none, and git reads push.default only when the
// remote has no push refspec. Each of the three therefore has a config
// of its own. remote.origin.mirror and push.followTags change a push
// whatever those keys say, so each shares the config of another key.
func pushConfigs(base string) map[string][]string {
	return map[string][]string{
		"remote.origin.push and remote.origin.mirror": {
			"remote.origin.push", "+refs/heads/feat/x:refs/heads/" + base,
			"remote.origin.push", "+refs/heads/*:refs/heads/*",
			"remote.origin.mirror", "true",
		},
		"push.default matching and push.followTags": {
			"push.default", "matching",
			"push.followTags", "true",
		},
		"push.default upstream": {
			"push.default", "upstream",
			"branch.feat/x.remote", "origin",
			"branch.feat/x.merge", "refs/heads/" + base,
		},
	}
}

// refObjects returns the object of each ref of the repository at
// repo, by the name of the ref.
func refObjects(t *testing.T, repo string) map[string]string {
	t.Helper()

	return parseRefs(gitOut(t, "-C", repo, "for-each-ref", refFormat))
}

// The caller can write the git config of the repository, and that
// config names the refs that a plain git push updates. A push must
// update only the ref of the call, so that a push of a feature branch
// never moves another branch of the remote.
//
// The tests of a push without those keys cover each call of this test
// in a repository whose config sets none of them.
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

				// The remote holds the branch base, and the clone is
				// ahead of it on base and on feat/x. The clone also
				// holds an annotated tag on the pushed commit.
				f := initFeatureRepos(t)

				setConfig(t, f.work, config...)

				want := f.bareRefs
				want[call.want] = f.workRefs[call.want]

				input := call.input
				input.Repo = f.work

				result, _, err := (&handler{allowFileURLs: true}).handlePush(t.Context(), nil, input)
				require.NoError(t, err)
				require.False(t, result.IsError, resultText(t, result))

				assert.Equal(t, want, refObjects(t, f.bare))
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

	bare, work := initRepos(t, false, true)

	// receive-pack runs the pre-receive hook with the number of push
	// options that the push sent.
	count := filepath.Join(t.TempDir(), "push-option-count")
	writeHookIn(t, filepath.Join(bare, "hooks"), "pre-receive",
		"#!/bin/sh\necho \"$GIT_PUSH_OPTION_COUNT\" > '"+count+"'\n")

	setConfig(t, bare, "receive.advertisePushOptions", "true")
	setConfig(t, work,
		"push.pushOption", "merge_request.create",
		"push.pushOption", "merge_request.target=main",
	)

	result, _, err := (&handler{allowFileURLs: true}).handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	got, err := os.ReadFile(count)
	require.NoError(t, err, "the pre-receive hook of the remote must run")
	assert.Equal(t, "0\n", string(got), "the push must send no push option")
	assert.Equal(t, repos(t).localCommit, gitOut(t, "-C", bare, "rev-parse", defaultBranch(t)))
}

// A push that names no ref needs a current branch.
func TestHandlePushDetachedHead(t *testing.T) {
	t.Parallel()

	bare, work := initRepos(t, false, true)

	runGitCmds(t, [][]string{{"git", "-C", work, "switch", "-q", "--detach"}})

	before := readTree(t, bare)

	result, _, err := (&handler{allowFileURLs: true}).handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), ErrNoBranch.Error())
	requireUnchanged(t, before, bare)
}

// push.gpgSign makes git sign a push with the program of gpg.program
// when the remote accepts signed pushes. The program runs outside the
// caller's sandbox, so the push tool never signs a push.
func TestHandlePushNeverSigns(t *testing.T) {
	t.Parallel()

	bare, work := initRepos(t, false, true)

	script, marker := markerScript(t)

	setConfig(t, bare, "receive.certNonceSeed", "seed")
	setConfig(t, work,
		"push.gpgSign", "true",
		"gpg.program", script,
	)

	h := &handler{allowFileURLs: true}

	result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: work})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	requireNotRun(t, marker)
	assert.Equal(t, repos(t).localCommit, gitOut(t, "-C", bare, "rev-parse", defaultBranch(t)))
}

// A push goes to every push URL of the remote. Each push URL must
// authenticate as the only URL of a remote does, whatever its place
// among the push URLs is. The token must reach the URL that names
// github.com. The credential helper of the git config must answer for
// the URL of the other host.
func TestHandlePushCredentialOfEachURL(t *testing.T) { //nolint:paralleltest // t.Setenv
	const (
		token      = "ghp_test123"
		tokenCred  = "x-access-token:" + token
		globalCred = "global:secret"
		github     = "https://github.com/a/b.git"
		gitlab     = "https://gitlab.com/c/d.git"
	)

	tests := map[string]struct {
		urls  []string
		hosts []string
		creds []string
	}{
		"GitHub after another host": {
			urls:  []string{gitlab, github},
			hosts: []string{"gitlab.com:443", "github.com:443"},
			creds: []string{globalCred, tokenCred},
		},
		"another host after GitHub": {
			urls:  []string{github, gitlab},
			hosts: []string{"github.com:443", "gitlab.com:443"},
			creds: []string{tokenCred, globalCred},
		},
	}

	for name, tt := range tests { //nolint:paralleltest // setProxyEnv calls t.Setenv.
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			bares := []string{filepath.Join(root, "a", "b.git"), filepath.Join(root, "c", "d.git")}

			for _, bare := range bares {
				initEmptyBare(t, bare)
				setConfig(t, bare, "http.receivepack", "true")
			}

			// The clone holds two commits that neither bare repository
			// holds.
			tm := repos(t)
			work := copyClone(t, tm.local, tt.urls[0])

			for _, url := range tt.urls {
				setConfig(t, work, "remote.origin.pushurl", url)
			}

			backend := requireHostCredential(
				map[string]string{"github.com": tokenCred, "gitlab.com": globalCred},
				gitBackend(t, root),
			)
			proxy := startAuthProxy(t, backend)
			setProxyEnv(t, token, proxy.config()+globalHelper)

			result, _, err := (&handler{token: token}).handlePush(t.Context(), nil, PushInput{Repo: work})
			require.NoError(t, err)

			hosts, creds := proxy.requests()
			assert.Equal(t, tt.hosts, hosts)
			assert.Equal(t, tt.creds, slices.Compact(creds))
			require.False(t, result.IsError, resultText(t, result))

			for _, bare := range bares {
				assert.Equal(t, tm.localCommit, gitOut(t, "-C", bare, "rev-parse", "refs/heads/"+tm.branch), bare)
			}
		})
	}
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
