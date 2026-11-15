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

func TestBuildPushArgs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input PushInput
		want  []string
	}{
		"minimal": {
			input: PushInput{Repo: "/r"},
			want:  []string{"push", "--no-verify", "--", "origin"},
		},
		"with ref": {
			input: PushInput{Repo: "/r", Ref: "feature"},
			want:  []string{"push", "--no-verify", "--", "origin", "feature"},
		},
		"with set upstream": {
			input: PushInput{Repo: "/r", Ref: "feature", SetUpstream: true},
			want: []string{
				"push",
				"--no-verify",
				"--set-upstream",
				"--",
				"origin",
				"feature",
			},
		},
		"with force with lease": {
			input: PushInput{Repo: "/r", Ref: "main", ForceWithLease: true},
			want: []string{
				"push",
				"--no-verify",
				"--force-with-lease",
				"--",
				"origin",
				"main",
			},
		},
		"with tags": {
			input: PushInput{Repo: "/r", Tags: true},
			want:  []string{"push", "--no-verify", "--tags", "--", "origin"},
		},
		"tag ref": {
			input: PushInput{Repo: "/r", Ref: "v1.0.0"},
			want:  []string{"push", "--no-verify", "--", "origin", "v1.0.0"},
		},
		"all options": {
			input: PushInput{
				Repo: "/r", Remote: "up", Ref: "x",
				SetUpstream: true, ForceWithLease: true, Tags: true,
			},
			want: []string{
				"push", "--no-verify", "--set-upstream", "--force-with-lease", "--tags",
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

// TestPushArgsRefuseLocalTransport covers a remote whose definition
// changes after runRemote checks its URLs. git resolves the remote
// again for the push, so the git command itself must refuse a remote
// that names a repository on the local file system.
func TestPushArgsRefuseLocalTransport(t *testing.T) {
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
				args:   buildPushArgs(defaultRemote, PushInput{Repo: work, Ref: branch}),
			}

			// The check saw an https URL, and the remote names the
			// bare repository by the time git pushes.
			const checked = "https://example.invalid/repo.git"

			_, err := runGit(t.Context(), (&handler{}).gitArgs(op, work, checked, false)...)
			require.Error(t, err, "git must refuse a push to a local repository")

			_, statErr := os.Stat(marker)
			require.ErrorIs(t, statErr, os.ErrNotExist,
				"hooks of the receiving repository must not run")
			assert.Equal(t, before, gitOut(t, "-C", bare, "rev-parse", branch))

			// Without the refusal, the same command pushes and runs
			// the hooks of the bare repository.
			out, err := runGit(t.Context(), (&handler{allowFileURLs: true}).gitArgs(op, work, checked, false)...)
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

		// err means that git checks the submodule commit that the
		// push of the parent references, so the push fails.
		err bool
	}{
		"default config with a tag commit": {tag: true},
		"default config with a new commit": {},
		"push.recurseSubmodules no": {
			config: []string{"push.recurseSubmodules", "no"},
		},
		"push.recurseSubmodules check": {
			config: []string{"push.recurseSubmodules", "check"},
			err:    true,
		},
		// With on-demand, git push starts a push in each submodule
		// that holds a commit that the push references.
		"push.recurseSubmodules on-demand": {
			config: []string{"push.recurseSubmodules", "on-demand"},
			err:    true,
		},
		"push.recurseSubmodules only": {
			config: []string{"push.recurseSubmodules", "only"},
			err:    true,
		},
		"submodule.recurse": {
			config: []string{"submodule.recurse", "true"},
			err:    true,
		},
		"push.recurseSubmodules on-demand with a tag commit": {
			config: []string{"push.recurseSubmodules", "on-demand"},
			tag:    true,
			err:    true,
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

			branch := currentBranch(t, s.work)
			before := gitOut(t, "-C", s.bare, "rev-parse", branch)
			subBefore := gitOut(t, "-C", s.subBare, "for-each-ref")

			h := &handler{allowFileURLs: true}

			result, _, err := h.handlePush(t.Context(), nil, PushInput{Repo: s.work})
			require.NoError(t, err)
			assert.Equal(t, subBefore, gitOut(t, "-C", s.subBare, "for-each-ref"),
				"git_push must not push a submodule")

			if tt.err {
				require.True(t, result.IsError, resultText(t, result))
				assert.Equal(t, before, gitOut(t, "-C", s.bare, "rev-parse", branch))
			} else {
				require.False(t, result.IsError, resultText(t, result))
				assert.Equal(t,
					gitOut(t, "-C", s.work, "rev-parse", "HEAD"),
					gitOut(t, "-C", s.bare, "rev-parse", branch),
				)
			}

			// After the caller pushes a new submodule commit, the push
			// of the parent succeeds.
			if tt.err && !tt.tag {
				result, _, err = h.handlePush(t.Context(), nil, PushInput{Repo: s.sub})
				require.NoError(t, err)
				require.False(t, result.IsError, resultText(t, result))

				result, _, err = h.handlePush(t.Context(), nil, PushInput{Repo: s.work})
				require.NoError(t, err)
				require.False(t, result.IsError, resultText(t, result))
				assert.Equal(t,
					gitOut(t, "-C", s.work, "rev-parse", "HEAD"),
					gitOut(t, "-C", s.bare, "rev-parse", branch),
				)
			}

			_, statErr := os.Stat(s.marker)
			require.ErrorIs(t, statErr, os.ErrNotExist,
				"the pre-push hook of the submodule must not run")
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
