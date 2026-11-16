package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// remoteTool describes a call of one tool that contacts a remote of an
// existing repository, and the repositories that the call needs.
type remoteTool struct {
	// upstream is set when the remote must hold a commit that the
	// clone lacks.
	upstream bool

	// local is set when the clone must hold a commit that the remote
	// lacks.
	local bool

	// failure is the error that wraps each failure of the tool.
	failure error

	// op holds the fields of the [remoteOp] that the tool runs for
	// the remote origin. A test that calls git itself sets the
	// repository.
	op remoteOp

	// call runs the tool on the repository at repo.
	call func(ctx context.Context, h *handler, repo string) (*mcp.CallToolResult, any, error)

	// landed returns the two commits that are equal after the call
	// did its work.
	landed func(t *testing.T, bare, work, branch string) (got, want string)
}

// remoteTools returns the calls that the tests of this file run.
func remoteTools() map[string]remoteTool {
	return map[string]remoteTool{
		"fetch": {
			upstream: true,
			failure:  ErrFetch,
			op:       remoteOp{remote: defaultRemote, args: buildFetchArgs(defaultRemote, FetchInput{})},
			call: func(ctx context.Context, h *handler, repo string) (*mcp.CallToolResult, any, error) {
				return h.handleFetch(ctx, nil, FetchInput{Repo: repo})
			},
			landed: func(t *testing.T, bare, work, branch string) (string, string) {
				t.Helper()

				return gitOut(t, "-C", work, "rev-parse", "origin/"+branch), gitOut(t, "-C", bare, "rev-parse", branch)
			},
		},
		"pull": {
			upstream: true,
			failure:  ErrPull,
			op: remoteOp{
				remote:   defaultRemote,
				worktree: true,
				args:     buildPullArgs(defaultRemote, PullInput{}),
			},
			call: func(ctx context.Context, h *handler, repo string) (*mcp.CallToolResult, any, error) {
				return h.handlePull(ctx, nil, PullInput{Repo: repo})
			},
			landed: func(t *testing.T, bare, work, branch string) (string, string) {
				t.Helper()

				return gitOut(t, "-C", work, "rev-parse", "HEAD"), gitOut(t, "-C", bare, "rev-parse", branch)
			},
		},
		"pull with rebase": {
			upstream: true,
			local:    true,
			failure:  ErrPull,
			op: remoteOp{
				remote:      defaultRemote,
				worktree:    true,
				args:        buildPullArgs(defaultRemote, PullInput{Rebase: true}),
				cleanupArgs: []string{"rebase", "--abort"},
			},
			call: func(ctx context.Context, h *handler, repo string) (*mcp.CallToolResult, any, error) {
				return h.handlePull(ctx, nil, PullInput{Repo: repo, Rebase: true})
			},
			// The rebase puts the local commit on top of the commit of
			// the remote.
			landed: func(t *testing.T, bare, work, branch string) (string, string) {
				t.Helper()

				return gitOut(t, "-C", work, "rev-parse", "HEAD~1"), gitOut(t, "-C", bare, "rev-parse", branch)
			},
		},
		"push": {
			local:   true,
			failure: ErrPush,
			op: remoteOp{
				remote: defaultRemote,
				push:   true,
				buildArgs: func(ctx context.Context, repo string) ([]string, error) {
					return pushArgs(ctx, repo, defaultRemote, PushInput{})
				},
			},
			call: func(ctx context.Context, h *handler, repo string) (*mcp.CallToolResult, any, error) {
				return h.handlePush(ctx, nil, PushInput{Repo: repo})
			},
			landed: func(t *testing.T, bare, work, branch string) (string, string) {
				t.Helper()

				return gitOut(t, "-C", bare, "rev-parse", branch), gitOut(t, "-C", work, "rev-parse", "HEAD")
			},
		},
	}
}

// repos creates a bare repository and a clone of it, with the commits
// that the tool needs. It returns the path of each.
func (rt remoteTool) repos(t *testing.T) (string, string) {
	t.Helper()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	if rt.upstream {
		other := initWorkClone(t, bare)

		commitFile(t, other, "remote.txt", "remote", "remote commit")
		runGitCmds(t, [][]string{{"git", "-C", other, "push", "-q"}})
	}

	if rt.local {
		commitFile(t, work, "local.txt", "local", "local commit")
	}

	return bare, work
}

// requireLanded fails the test when the call of the tool did not do
// its work.
func (rt remoteTool) requireLanded(t *testing.T, bare, work string) {
	t.Helper()

	got, want := rt.landed(t, bare, work, currentBranch(t, work))
	require.Equal(t, want, got, "the tool must do its work")
}

// requireNotLanded fails the test when the call of the tool did its
// work.
func (rt remoteTool) requireNotLanded(t *testing.T, bare, work string) {
	t.Helper()

	got, want := rt.landed(t, bare, work, currentBranch(t, work))
	require.NotEqual(t, want, got, "the tool must not do its work")
}

// runGit runs the git command of the tool in the repository at work,
// as [handler.runRemote] runs it after its checks pass. A test calls
// it for a repository whose config changes after those checks.
func (rt remoteTool) runGit(t *testing.T, h *handler, work string) (string, error) {
	t.Helper()

	args := rt.op.args

	if rt.op.buildArgs != nil {
		var err error

		args, err = rt.op.buildArgs(t.Context(), work)
		require.NoError(t, err)
	}

	// The checks saw the working tree at work and no hook.
	opts := h.gitOptions(rt.op, work, work, nil)

	return h.runGit(t.Context(), slices.Concat(opts, args)...)
}

// requireEmptyDir fails the test when the directory at dir holds an
// entry.
func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "git must write nothing to %s", dir)
}

// hookEvents holds the events that git fires in a fetch, a pull, or a
// push, other than pre-push. A push reports the pre-push hooks that it
// skips, and TestHandlePushSkipsHooks covers that report.
var hookEvents = []string{
	"reference-transaction",
	"pre-rebase",
	"post-checkout",
	"post-commit",
	"post-index-change",
	"post-merge",
	"post-rewrite",
	"pre-auto-gc",
}

// hookConfig returns git config text that defines a hook with each
// name for every event of hookEvents. Each hook runs script.
func hookConfig(script string, names ...string) string {
	var text strings.Builder

	for _, name := range names {
		text.WriteString("[hook \"" + name + "\"]\n\tcommand = " + script + "\n")

		for _, event := range hookEvents {
			text.WriteString("\tevent = " + event + "\n")
		}
	}

	return text.String()
}

// Hooks execute outside the caller's sandbox, so no tool runs a hook
// that the git config defines, whatever the name of the hook is.
func TestRemoteToolsSkipConfigHooks(t *testing.T) {
	t.Parallel()

	// configs maps the name of a case to a function that returns the
	// git config text of the case. Each hook runs script.
	configs := map[string]func(script string) string{
		"plain name":          func(script string) string { return hookConfig(script, "check") },
		"name with a dot":     func(script string) string { return hookConfig(script, "dotted.name") },
		"name with a space":   func(script string) string { return hookConfig(script, "spaced name") },
		"name with an equals": func(script string) string { return hookConfig(script, "equals=sign") },
		"name in upper case":  func(script string) string { return hookConfig(script, "UPPER") },
		"empty name":          func(script string) string { return hookConfig(script, "") },
		"more than one hook":  func(script string) string { return hookConfig(script, "check", "lint") },
		// git has a switch that disables every hook of one event. The
		// switch stops working when the config gives a command to a
		// hook with the name of the event. The tools therefore do not
		// use the switch.
		"hook with the name of an event": func(script string) string {
			return "[hook \"reference-transaction\"]\n\tcommand = " + script + "\n" + hookConfig(script, "check")
		},
	}

	for toolName, rt := range remoteTools() {
		for name, config := range configs {
			t.Run(toolName+"/"+name, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)
				script, marker := markerScript(t)

				appendGitConfig(t, work, config(script))

				result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
				require.NoError(t, err)
				require.False(t, result.IsError, resultText(t, result))

				requireNotRun(t, marker)
				rt.requireLanded(t, bare, work)
			})
		}

		t.Run(toolName+"/hook files", func(t *testing.T) {
			t.Parallel()

			bare, work := rt.repos(t)
			script, marker := markerScript(t)

			for _, event := range hookEvents {
				writeHook(t, work, event, "#!/bin/sh\nexec '"+script+"'\n")
			}

			result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			requireNotRun(t, marker)
			rt.requireLanded(t, bare, work)
		})
	}
}

// git options turn off each command that the git config names under a
// fixed key. A tool therefore does its work and never runs the command.
func TestRemoteToolsIgnoreConfigCommands(t *testing.T) {
	t.Parallel()

	configs := map[string]func(t *testing.T, work, script string){
		// git runs the hook when it reads the index.
		"core.fsmonitor": func(t *testing.T, work, script string) {
			t.Helper()

			runGitCmds(t, [][]string{{"git", "-C", work, "config", "core.fsmonitor", script}})
		},
		// A fetch runs the command for each alternate object store.
		"core.alternateRefsCommand": func(t *testing.T, work, script string) {
			t.Helper()

			alternate := filepath.Join(t.TempDir(), "alternate.git")
			runGitCmds(t, [][]string{
				{"git", "init", "-q", "--bare", alternate},
				{"git", "-C", work, "config", "core.alternateRefsCommand", script},
			})
			require.NoError(t, os.WriteFile(
				filepath.Join(work, ".git", "objects", "info", "alternates"),
				[]byte(filepath.Join(alternate, "objects")+"\n"),
				0o644,
			))
		},
	}

	for toolName, rt := range remoteTools() {
		for name, setup := range configs {
			t.Run(toolName+"/"+name, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)
				script, marker := markerScript(t)

				setup(t, work, script)

				result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
				require.NoError(t, err)
				require.False(t, result.IsError, resultText(t, result))

				requireNotRun(t, marker)
				rt.requireLanded(t, bare, work)
			})
		}
	}
}

// git options cannot override these keys, so a tool refuses the
// repository when its config sets one that the tool reads. The keys
// name a command, or they change how git connects to a remote.
func TestRemoteToolsRefuseConfigKeys(t *testing.T) {
	t.Parallel()

	keys := map[string]struct {
		// worktree is set for a key that only a tool that updates
		// the working tree reads.
		worktree bool
	}{
		"credential.helper":                         {},
		"credential.https://example.invalid.helper": {},
		"remote.origin.uploadpack":                  {},
		"remote.other.receivepack":                  {},
		"http.proxy":                                {},
		"http.https://example.invalid/.proxy":       {},
		"http.sslverify":                            {},
		"http.https://example.invalid/.sslverify":   {},
		"http.sslcainfo":                            {},
		"http.curloptresolve":                       {},
		"http.cookiefile":                           {},
		"remote.origin.proxy":                       {},
		"remote.other.proxyauthmethod":              {},
		"filter.x.clean":                            {worktree: true},
		"filter.x.smudge":                           {worktree: true},
		"filter.x.process":                          {worktree: true},
		"merge.x.driver":                            {worktree: true},
		"gpg.program":                               {worktree: true},
		"gpg.ssh.defaultkeycommand":                 {worktree: true},
		"lfs.extension.x.clean":                     {worktree: true},
		"lfs.extension.x.smudge":                    {worktree: true},
		"lfs.customtransfer.x.path":                 {worktree: true},
		"lfs.customtransfer.x.args":                 {worktree: true},
		"lfs.storage":                               {worktree: true},
	}

	for toolName, rt := range remoteTools() {
		for key, tt := range keys {
			t.Run(toolName+"/"+key, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)
				script, marker := markerScript(t)

				runGitCmds(t, [][]string{{"git", "-C", work, "config", key, script}})

				result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
				require.NoError(t, err)

				requireNotRun(t, marker)

				if tt.worktree && !rt.op.worktree {
					require.False(t, result.IsError, resultText(t, result))
					rt.requireLanded(t, bare, work)

					return
				}

				require.True(t, result.IsError, "the tool must refuse the repository")

				text := resultText(t, result)
				assert.Contains(t, text, ErrDeniedConfig.Error()+": the repository sets "+key+".")
				assert.NotContains(t, text, script, "the message must name the key, never its value")
				rt.requireNotLanded(t, bare, work)
			})
		}
	}
}

// The environment of the server can set GIT_CONFIG. A git config
// command then reads only the file that the variable names. A fetch,
// a pull, and a push still read the config of the repository. Every
// tool must therefore still refuse a key that the config of the
// repository sets. It must also still skip a hook that this config
// defines.
func TestRemoteToolsSkipConfigFileVariable(t *testing.T) { //nolint:paralleltest // t.Setenv
	for toolName, rt := range remoteTools() { //nolint:paralleltest // t.Setenv
		t.Run(toolName+"/refused key", func(t *testing.T) {
			bare, work := rt.repos(t)
			script, marker := markerScript(t)

			runGitCmds(t, [][]string{{"git", "-C", work, "config", "credential.helper", script}})
			setConfigFileEnv(t, "")

			result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
			require.NoError(t, err)
			require.True(t, result.IsError, "the tool must refuse the repository")

			assert.Contains(t,
				resultText(t, result),
				ErrDeniedConfig.Error()+": the repository sets credential.helper.",
			)
			requireNotRun(t, marker)
			rt.requireNotLanded(t, bare, work)
		})

		t.Run(toolName+"/config hook", func(t *testing.T) {
			bare, work := rt.repos(t)
			script, marker := markerScript(t)

			appendGitConfig(t, work, hookConfig(script, "check"))
			setConfigFileEnv(t, "")

			result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			requireNotRun(t, marker)
			rt.requireLanded(t, bare, work)
		})
	}
}

// core.worktree in the config of a repository names its working tree,
// and git writes there. Every tool therefore refuses a repository
// whose working tree lies outside the allowed directories.
func TestRemoteToolsRefuseWorkTreeOutsideAllowDir(t *testing.T) {
	t.Parallel()

	for toolName, rt := range remoteTools() {
		t.Run(toolName, func(t *testing.T) {
			t.Parallel()

			bare, work := rt.repos(t)
			outside := t.TempDir()

			runGitCmds(t, [][]string{{"git", "-C", work, "config", "core.worktree", outside}})

			h := &handler{
				allowDirs:     mustResolveAllowDirs(t, filepath.Dir(work)),
				allowFileURLs: true,
			}

			result, _, err := rt.call(t.Context(), h, work)
			require.NoError(t, err)
			require.True(t, result.IsError, "the tool must refuse the working tree")
			assert.Contains(t, resultText(t, result), ErrDeniedDest.Error())

			requireEmptyDir(t, outside)
			rt.requireNotLanded(t, bare, work)
		})
	}
}

// The allowed directories can hold paths that the sandbox of the
// caller cannot write, such as another checkout and the hooks
// directory of its git directory. Every tool therefore refuses a
// working tree that does not hold the git directory of the
// repository, or that lies inside a git directory.
func TestRemoteToolsRefuseForeignWorkTree(t *testing.T) {
	t.Parallel()

	// Each setup prepares a directory under other, the second allowed
	// directory, for the clone at work. It returns the path of the
	// call and the directory that git must not write.
	trees := map[string]func(t *testing.T, other, work string) (string, string){
		"hooks directory of another repository": func(t *testing.T, other, work string) (string, string) {
			t.Helper()

			hooks := makeHooksDir(t, filepath.Join(initRepo(t, other), ".git"))
			setWorkTree(t, work, hooks)

			return work, hooks
		},
		"another checkout": func(t *testing.T, other, work string) (string, string) {
			t.Helper()

			checkout := initRepo(t, other)
			setWorkTree(t, work, checkout)

			return work, checkout
		},
		"plain directory": func(t *testing.T, other, work string) (string, string) {
			t.Helper()

			dir := filepath.Join(other, "plain")
			require.NoError(t, os.Mkdir(dir, 0o755))
			setWorkTree(t, work, dir)

			return work, dir
		},
	}

	// A .git file in the git directory of another repository points
	// git at the git directory of the clone. git then finds the git
	// directory of the clone from that directory and from each
	// directory inside it. git reads the .git file before HEAD, so the
	// caller can also change HEAD.
	for headName, changeHead := range headChanges() {
		trees["git directory of another repository, "+headName] = func(t *testing.T, other, work string) (string, string) {
			t.Helper()

			gitDir := plantGitFile(t, initRepo(t, other), work)
			changeHead(t, gitDir)
			setWorkTree(t, work, gitDir)

			return work, gitDir
		}

		trees["hooks directory of another repository with a .git file, "+headName] = func(t *testing.T, other, work string) (string, string) {
			t.Helper()

			gitDir := plantGitFile(t, initRepo(t, other), work)
			changeHead(t, gitDir)

			hooks := makeHooksDir(t, gitDir)
			setWorkTree(t, work, hooks)

			return work, hooks
		}

		// git takes the directory that holds a .git file as the
		// working tree, so the call names no core.worktree.
		trees["call in the git directory of another repository, "+headName] = func(t *testing.T, other, work string) (string, string) {
			t.Helper()

			gitDir := plantGitFile(t, initRepo(t, other), work)
			changeHead(t, gitDir)

			return gitDir, gitDir
		}
	}

	for toolName, rt := range remoteTools() {
		for treeName, setup := range trees {
			t.Run(toolName+"/"+treeName, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)
				other := t.TempDir()
				repo, dir := setup(t, other, work)
				before := dirEntries(t, dir)

				h := &handler{
					allowDirs:     mustResolveAllowDirs(t, filepath.Dir(work), other),
					allowFileURLs: true,
				}

				result, _, err := rt.call(t.Context(), h, repo)
				require.NoError(t, err)
				require.True(t, result.IsError, "the tool must refuse the working tree")
				assert.Contains(t, resultText(t, result), ErrDeniedWorkTree.Error())

				assert.Equal(t, before, dirEntries(t, dir), "git must write nothing to %s", dir)
				rt.requireNotLanded(t, bare, work)
			})
		}
	}
}

// initRepo creates a repository in a new directory under parent, and
// returns the path of its working tree.
func initRepo(t *testing.T, parent string) string {
	t.Helper()

	repo := filepath.Join(parent, "repo")
	runGitCmds(t, [][]string{{"git", "init", "-q", repo}})

	return repo
}

// makeHooksDir creates the hooks directory of the git directory at
// gitDir, and it returns the path of the hooks directory. git init and
// git clone copy the hooks directory from the template directory, and
// [TestMain] gives git an empty template directory.
func makeHooksDir(t *testing.T, gitDir string) string {
	t.Helper()

	hooks := filepath.Join(gitDir, "hooks")
	require.NoError(t, os.MkdirAll(hooks, 0o755))

	return hooks
}

// setWorkTree sets core.worktree in the config of the repository at
// work to dir.
func setWorkTree(t *testing.T, work, dir string) {
	t.Helper()

	runGitCmds(t, [][]string{{"git", "-C", work, "config", "core.worktree", dir}})
}

// plantGitFile writes a .git file into the git directory of the
// repository at repo. The file points git at the git directory of the
// repository at work. plantGitFile returns the git directory of repo.
func plantGitFile(t *testing.T, repo, work string) string {
	t.Helper()

	gitDir := filepath.Join(repo, ".git")
	target := gitOut(t, "-C", work, "rev-parse", "--absolute-git-dir")

	require.NoError(t, os.WriteFile(filepath.Join(gitDir, ".git"), []byte("gitdir: "+target+"\n"), 0o644))

	return gitDir
}

// headChanges maps a name to a function that changes the HEAD of the
// git directory at gitDir, as a caller that can write that git
// directory can.
func headChanges() map[string]func(t *testing.T, gitDir string) {
	return map[string]func(t *testing.T, gitDir string){
		"HEAD in place": func(*testing.T, string) {},
		"HEAD moved aside": func(t *testing.T, gitDir string) {
			t.Helper()

			head := filepath.Join(gitDir, "HEAD")
			require.NoError(t, os.Rename(head, head+".aside"))
		},
		"HEAD linked to a missing ref": func(t *testing.T, gitDir string) {
			t.Helper()

			linkHead(t, gitDir, "refs/heads/nope")
		},
	}
}

// linkHead replaces the HEAD of the git directory at gitDir with a
// symbolic link to target. git accepts a link into refs/ as HEAD.
func linkHead(t *testing.T, gitDir, target string) {
	t.Helper()

	head := filepath.Join(gitDir, "HEAD")
	require.NoError(t, os.Remove(head))
	require.NoError(t, os.Symlink(target, head))
}

// dirEntries returns the names of the entries of the directory at
// dir.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// TestGitIgnoresCoreWorktree covers a repository whose core.worktree
// changes after runRemote checks the working tree. The git command
// itself must name the working tree that the server checked.
func TestGitIgnoresCoreWorktree(t *testing.T) {
	t.Parallel()

	for toolName, rt := range remoteTools() {
		if !rt.op.worktree {
			continue
		}

		t.Run(toolName, func(t *testing.T) {
			t.Parallel()

			bare, work := rt.repos(t)
			outside := t.TempDir()

			runGitCmds(t, [][]string{{"git", "-C", work, "config", "core.worktree", outside}})

			out, err := rt.runGit(t, &handler{allowFileURLs: true}, work)
			require.NoError(t, err, out)

			requireEmptyDir(t, outside)
			assert.FileExists(t, filepath.Join(work, "remote.txt"))
			rt.requireLanded(t, bare, work)
		})
	}
}

// deniedRemotes maps a name to a function that points the remote
// origin of the clone at work at a URL that the server denies. script
// is the command that git runs when it follows the URL.
func deniedRemotes() map[string]func(t *testing.T, work, bare, script string) {
	return map[string]func(t *testing.T, work, bare, script string){
		// initWorkClone leaves origin at the path of the bare
		// repository.
		"path": func(*testing.T, string, string, string) {},
		"file URL": func(t *testing.T, work, bare, _ string) {
			t.Helper()

			runGitCmds(t, [][]string{
				{"git", "-C", work, "remote", "set-url", "origin", "file://" + bare},
			})
		},
		// The ext transport runs the command that the URL holds, and
		// git allows it only when the config says so.
		"ext URL": func(t *testing.T, work, _, script string) {
			t.Helper()

			runGitCmds(t, [][]string{
				{"git", "-C", work, "remote", "set-url", "origin", "ext::" + script},
				{"git", "-C", work, "config", "protocol.ext.allow", "always"},
			})
		},
	}
}

// git reaches a repository on the local file system by running a
// command in it. The ext transport runs a command from the URL. Every
// tool therefore refuses a remote with such a URL.
func TestRemoteToolsRefuseDeniedURL(t *testing.T) {
	t.Parallel()

	for toolName, rt := range remoteTools() {
		for name, remote := range deniedRemotes() {
			t.Run(toolName+"/"+name, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)
				script, marker := markerScript(t)

				remote(t, work, bare, script)

				result, _, err := rt.call(t.Context(), &handler{}, work)
				require.NoError(t, err)
				require.True(t, result.IsError, "the tool must refuse the remote")

				text := resultText(t, result)
				assert.Contains(t, text, ErrDeniedURL.Error()+": remote origin")
				assert.NotContains(t, text, filepath.Dir(bare), "error must name the remote, never its URL")
				assert.NotContains(t, text, script, "error must name the remote, never its URL")

				requireNotRun(t, marker)
				rt.requireNotLanded(t, bare, work)
			})
		}
	}
}

// The git config can define a remote without a URL. git then uses the
// name of the remote as its URL, and the server refuses that name as a
// URL. The error must say that the remote has no URL. It must not say
// that the remote has a URL of a scheme that the server refuses.
func TestRemoteToolsRefuseRemoteWithoutURL(t *testing.T) {
	t.Parallel()

	const (
		noURL     = "remote has no URL: origin: the git config stores no URL in remote.origin.url"
		noPushURL = noURL + " and no push URL in remote.origin.pushurl"
	)

	// An empty value of remote.<name>.url or remote.<name>.pushurl
	// clears the URLs that the git config stores before it, in git
	// 2.46 and later.
	tcs := map[string]struct {
		// remote removes the URL of the origin remote of the clone at
		// work. bare is the path of the bare repository.
		remote func(t *testing.T, work, bare string)

		// pushURL is set when the remote keeps the path of the bare
		// repository as its push URL.
		pushURL bool
	}{
		"remote that lost its URL": {
			remote: func(t *testing.T, work, _ string) {
				t.Helper()

				runGitCmds(t, [][]string{{"git", "-C", work, "config", "--unset", "remote.origin.url"}})
			},
		},
		"empty value clears the URL": {
			remote: func(t *testing.T, work, _ string) {
				t.Helper()

				appendGitConfig(t, work, remoteNamed("origin", ""))
			},
		},
		"push URL and no URL": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				runGitCmds(t, [][]string{
					{"git", "-C", work, "config", "--unset", "remote.origin.url"},
					{"git", "-C", work, "config", "remote.origin.pushurl", bare},
				})
			},
			pushURL: true,
		},
		"empty values clear the URL and the push URL": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				runGitCmds(t, [][]string{{"git", "-C", work, "config", "remote.origin.pushurl", bare}})
				appendGitConfig(t, work, "[remote \"origin\"]\n\turl =\n\tpushurl =\n")
			},
		},
	}

	for toolName, rt := range remoteTools() {
		for name, tc := range tcs {
			t.Run(toolName+"/"+name, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)

				tc.remote(t, work, bare)

				// git pushes to the push URLs of a remote, so a remote
				// with a push URL and no URL still takes a push.
				if rt.op.push && tc.pushURL {
					result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
					require.NoError(t, err)
					require.False(t, result.IsError, resultText(t, result))

					rt.requireLanded(t, bare, work)

					return
				}

				want := rt.failure.Error() + ": " + noURL
				if rt.op.push {
					want = rt.failure.Error() + ": " + noPushURL
				}

				// A server that allows every URL has no URL to connect
				// to either.
				for _, h := range []*handler{{}, {allowFileURLs: true}} {
					result, _, err := rt.call(t.Context(), h, work)
					require.NoError(t, err)
					require.True(t, result.IsError, "the tool must refuse the remote")

					assert.Equal(t, want, resultText(t, result))
				}

				rt.requireNotLanded(t, bare, work)
			})
		}
	}
}

// The git config can store the name of a remote as its URL. git reads
// the same URL for that remote as for a remote without a URL. The
// remote has a URL, so the error must still say that the server
// refuses the URL.
func TestRemoteToolsRefuseStoredRemoteName(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		// cmds stores the name origin as a URL of the origin remote of
		// the clone at work.
		cmds func(work string) [][]string

		// push is set when only a push reads the URL.
		push bool
	}{
		"URL": {
			cmds: func(work string) [][]string {
				return [][]string{{"git", "-C", work, "config", "remote.origin.url", "origin"}}
			},
		},
		"push URL and no URL": {
			cmds: func(work string) [][]string {
				return [][]string{
					{"git", "-C", work, "config", "--unset", "remote.origin.url"},
					{"git", "-C", work, "config", "remote.origin.pushurl", "origin"},
				}
			},
			push: true,
		},
	}

	for toolName, rt := range remoteTools() {
		for name, tc := range tcs {
			if tc.push && !rt.op.push {
				continue
			}

			t.Run(toolName+"/"+name, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)

				runGitCmds(t, tc.cmds(work))

				result, _, err := rt.call(t.Context(), &handler{}, work)
				require.NoError(t, err)
				require.True(t, result.IsError, "the tool must refuse the remote")

				assert.Equal(t,
					rt.failure.Error()+": "+ErrDeniedURL.Error()+
						": remote origin has a URL that is not https, ssh, or user@host:path",
					resultText(t, result),
				)

				rt.requireNotLanded(t, bare, work)
			})
		}
	}
}

// git can open a repository whose config gives the URL of a remote no
// value, but git remote get-url rejects that config. The config
// defines the remote, so the error must say that the server could not
// read the remote. It must not say that the remote is unknown.
func TestRemoteToolsReportUnreadableRemote(t *testing.T) {
	t.Parallel()

	for toolName, rt := range remoteTools() {
		t.Run(toolName, func(t *testing.T) {
			t.Parallel()

			bare, work := rt.repos(t)

			appendGitConfig(t, work, valuelessOriginURL)

			result, _, err := rt.call(t.Context(), &handler{}, work)
			require.NoError(t, err)
			require.True(t, result.IsError, "the tool must not contact the remote")

			text := resultText(t, result)
			assert.Contains(t, text, rt.failure.Error()+": reading the origin remote of "+work+": ")
			assert.NotContains(t, text, "unknown remote")

			rt.requireNotLanded(t, bare, work)
		})
	}
}

// git finds a repository whose config holds a line that it cannot
// parse, and it cannot open that repository. The path of the call lies
// in a repository. The error must therefore say that the server could
// not open the repository, and it must hold the message of git. It
// must not say that the path is not a git repository.
func TestRemoteToolsReportUnparseableConfig(t *testing.T) {
	t.Parallel()

	for toolName, rt := range remoteTools() {
		t.Run(toolName, func(t *testing.T) {
			t.Parallel()

			bare, work := rt.repos(t)
			config := filepath.Join(work, ".git", "config")

			parseable, err := os.ReadFile(config)
			require.NoError(t, err)

			appendGitConfig(t, work, unparseableConfig)

			result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
			require.NoError(t, err)
			require.True(t, result.IsError, "the tool must not contact the remote")

			text := resultText(t, result)
			assert.Contains(t, text, rt.failure.Error()+": opening the repository at "+work+": ")
			assert.Contains(t, text, "fatal: bad config line")
			assert.NotContains(t, text, ErrNotRepo.Error())

			// git reads the commits of the clone only when it can parse
			// the config.
			require.NoError(t, os.WriteFile(config, parseable, 0o644))
			rt.requireNotLanded(t, bare, work)
		})
	}
}

// TestGitRefusesTransports covers a remote whose definition changes
// after runRemote checks its URLs. git resolves the remote again when
// it connects, so the git command itself must refuse each transport
// that the server denies.
func TestGitRefusesTransports(t *testing.T) {
	t.Parallel()

	remotes := deniedRemotes()

	// The config can name the transport of a remote, whatever its URL
	// is.
	remotes["transport that the config names"] = func(t *testing.T, work, _, script string) {
		t.Helper()

		runGitCmds(t, [][]string{
			{"git", "-C", work, "remote", "set-url", "origin", script},
			{"git", "-C", work, "config", "remote.origin.vcs", "ext"},
			{"git", "-C", work, "config", "protocol.ext.allow", "always"},
		})
	}

	for toolName, rt := range remoteTools() {
		for name, remote := range remotes {
			t.Run(toolName+"/"+name, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)
				script, marker := markerScript(t)

				remote(t, work, bare, script)

				_, err := rt.runGit(t, &handler{}, work)
				require.ErrorContains(t, err, "not allowed")

				requireNotRun(t, marker)
				rt.requireNotLanded(t, bare, work)

				// A server that allows the file transport still denies
				// the ext transport.
				_, err = rt.runGit(t, &handler{allowFileURLs: true}, work)

				if name == "path" || name == "file URL" {
					require.NoError(t, err)
					rt.requireLanded(t, bare, work)

					return
				}

				require.ErrorContains(t, err, "not allowed")
				requireNotRun(t, marker)
			})
		}
	}
}

// TestGitIgnoresServePrograms covers a remote whose definition changes
// after runRemote reads the config. git runs the program that serves a
// fetch or a push on the host of the remote. The git command itself
// must therefore name the program.
func TestGitIgnoresServePrograms(t *testing.T) {
	t.Parallel()

	for toolName, rt := range remoteTools() {
		for _, key := range []string{"remote.origin.uploadpack", "remote.origin.receivepack"} {
			t.Run(toolName+"/"+key, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)
				script, marker := markerScript(t)

				runGitCmds(t, [][]string{{"git", "-C", work, "config", key, script}})

				out, err := rt.runGit(t, &handler{allowFileURLs: true}, work)
				require.NoError(t, err, out)

				requireNotRun(t, marker)
				rt.requireLanded(t, bare, work)
			})
		}
	}
}

// Each tool gives the token to an https connection to github.com.
// The credential helper of the git config answers for every other
// host, also when the token is set.
func TestRemoteToolsCredential(t *testing.T) { //nolint:paralleltest // t.Setenv
	const token = "ghp_test123"

	tests := map[string]struct {
		host string
		cred string
	}{
		"GitHub":       {host: "github.com", cred: "x-access-token:" + token},
		"another host": {host: "gitlab.com", cred: "global:secret"},
	}

	for toolName, rt := range remoteTools() { //nolint:paralleltest // setProxyEnv calls t.Setenv.
		for name, tt := range tests {
			t.Run(toolName+"/"+name, func(t *testing.T) {
				bare, work := rt.repos(t)
				url := "https://" + tt.host + "/" + filepath.Base(bare)

				runGitCmds(t, [][]string{
					{"git", "-C", bare, "config", "http.receivepack", "true"},
					{"git", "-C", work, "remote", "set-url", "origin", url},
				})

				backend := requireHostCredential(
					map[string]string{tt.host: tt.cred},
					gitBackend(t, filepath.Dir(bare)),
				)
				proxy := startAuthProxy(t, backend)
				setProxyEnv(t, token, proxy.config()+globalHelper)

				result, _, err := rt.call(t.Context(), &handler{token: token}, work)
				require.NoError(t, err)
				require.False(t, result.IsError, resultText(t, result))

				rt.requireLanded(t, bare, work)

				hosts, creds := proxy.requests()
				assert.Equal(t, []string{tt.host + ":443"}, hosts)
				assert.Equal(t, []string{tt.cred}, slices.Compact(creds))
			})
		}
	}
}
