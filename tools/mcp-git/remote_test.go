package main

import (
	"context"
	"maps"
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

	// landed returns a repository and a revision of it. The revision
	// names the commit that the call brings only after the call did
	// its work. That commit is the remote commit for a tool with
	// upstream set, and the local commit for every other tool.
	landed func(bare, work, branch string) (repo, rev string)
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
			landed: func(_, work, branch string) (string, string) { return work, "origin/" + branch },
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
			landed: func(_, work, _ string) (string, string) { return work, "HEAD" },
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
			landed: func(_, work, _ string) (string, string) { return work, "HEAD~1" },
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
			landed: func(bare, _, branch string) (string, string) { return bare, branch },
		},
	}
}

// repos returns a bare repository and a clone of it, with the commits
// that the tool needs, as [initRepos] creates them.
func (rt remoteTool) repos(t *testing.T) (string, string) {
	t.Helper()

	return initRepos(t, rt.upstream, rt.local)
}

// landedCommits returns the commit that the call of the tool brings,
// and the commit that the revision of landed names.
func (rt remoteTool) landedCommits(t *testing.T, bare, work string) (string, string) {
	t.Helper()

	tm := repos(t)

	want := tm.localCommit
	if rt.upstream {
		want = tm.remoteCommit
	}

	repo, rev := rt.landed(bare, work, tm.branch)

	return want, gitOut(t, "-C", repo, "rev-parse", rev)
}

// requireLanded fails the test when the call of the tool did not do
// its work.
func (rt remoteTool) requireLanded(t *testing.T, bare, work string) {
	t.Helper()

	want, got := rt.landedCommits(t, bare, work)
	require.Equal(t, want, got, "the tool must do its work")
}

// requireNotLanded fails the test when the call of the tool did its
// work.
func (rt remoteTool) requireNotLanded(t *testing.T, bare, work string) {
	t.Helper()

	want, got := rt.landedCommits(t, bare, work)
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
		text.WriteString("[hook " + configQuote(name) + "]\n\tcommand = " + configQuote(script) + "\n")

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

	// names holds a hook name of each form that the git config allows.
	// One config defines them all, and each hook has a marker of its
	// own, so a failure names the hook that ran.
	names := []string{"check", "dotted.name", "spaced name", "equals=sign", "UPPER", "", "lint"}

	// git has a switch that disables every hook of one event. The
	// switch stops working when the config gives a command to a hook
	// with the name of the event. The tools therefore do not use the
	// switch.
	const eventName = "reference-transaction"

	for toolName, rt := range remoteTools() {
		t.Run(toolName+"/hooks in the git config", func(t *testing.T) {
			t.Parallel()

			bare, work := rt.repos(t)
			markers := map[string]string{}

			var config strings.Builder

			for _, name := range names {
				script, marker := markerScript(t)

				markers[name] = marker

				config.WriteString(hookConfig(script, name))
			}

			script, marker := markerScript(t)

			markers[eventName] = marker

			config.WriteString("[hook " + configQuote(eventName) + "]\n\tcommand = " + configQuote(script) + "\n")
			appendGitConfig(t, work, config.String())

			result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
			require.NoError(t, err)

			for name, marker := range markers {
				assert.NoFileExists(t, marker, "git must not run the hook %q of the git config", name)
			}

			require.False(t, result.IsError, resultText(t, result))
			rt.requireLanded(t, bare, work)
		})

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
// One config sets every key, and each command has a marker of its own.
func TestRemoteToolsIgnoreConfigCommands(t *testing.T) {
	t.Parallel()

	for toolName, rt := range remoteTools() {
		t.Run(toolName, func(t *testing.T) {
			t.Parallel()

			bare, work := rt.repos(t)

			// git runs the hook of core.fsmonitor when it reads the
			// index. A fetch runs core.alternateRefsCommand for each
			// alternate object store.
			fsmonitor, fsmonitorRan := markerScript(t)
			alternateRefs, alternateRefsRan := markerScript(t)

			alternate := filepath.Join(t.TempDir(), "alternate.git")
			initEmptyBare(t, alternate)

			setConfig(t, work,
				"core.fsmonitor", fsmonitor,
				"core.alternateRefsCommand", alternateRefs,
			)
			require.NoError(t, os.WriteFile(
				filepath.Join(work, ".git", "objects", "info", "alternates"),
				[]byte(filepath.Join(alternate, "objects")+"\n"),
				0o644,
			))

			result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
			require.NoError(t, err)

			assert.NoFileExists(t, fsmonitorRan, "git must not run the command of core.fsmonitor")
			assert.NoFileExists(t, alternateRefsRan, "git must not run the command of core.alternateRefsCommand")

			require.False(t, result.IsError, resultText(t, result))
			rt.requireLanded(t, bare, work)
		})
	}
}

// git options cannot override these keys, so a tool refuses the
// repository when its config sets one that the tool reads. The keys
// name a command, or they change how git connects to a remote.
//
// The error of a refusal names every key that the tool refuses. One
// config therefore sets all the keys of a case, and the test compares
// the keys that the error names.
func TestRemoteToolsRefuseConfigKeys(t *testing.T) {
	t.Parallel()

	// remoteKeys holds the keys that every tool reads.
	remoteKeys := []string{
		"credential.helper",
		"credential.https://example.invalid.helper",
		"remote.origin.uploadpack",
		"remote.other.receivepack",
		"http.proxy",
		"http.https://example.invalid/.proxy",
		"http.sslverify",
		"http.https://example.invalid/.sslverify",
		"http.sslcainfo",
		"http.curloptresolve",
		"http.cookiefile",
		"remote.origin.proxy",
		"remote.other.proxyauthmethod",
	}

	// worktreeKeys holds the keys that only a tool that updates the
	// working tree reads.
	worktreeKeys := []string{
		"filter.x.clean",
		"filter.x.smudge",
		"filter.x.process",
		"merge.x.driver",
		"gpg.program",
		"gpg.ssh.defaultkeycommand",
		"lfs.extension.x.clean",
		"lfs.extension.x.smudge",
		"lfs.customtransfer.x.path",
		"lfs.customtransfer.x.args",
		"lfs.storage",
	}

	tcs := map[string]struct {
		// keys holds the keys that the config of the clone sets, in
		// the order of the config file.
		keys []string

		// worktreeOnly is set when only a tool that updates the
		// working tree refuses the config.
		worktreeOnly bool
	}{
		"every key":                     {keys: slices.Concat(remoteKeys, worktreeKeys)},
		"keys of a working tree update": {keys: worktreeKeys, worktreeOnly: true},
	}

	for toolName, rt := range remoteTools() {
		for name, tc := range tcs {
			t.Run(toolName+"/"+name, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)
				script, marker := markerScript(t)

				for _, key := range tc.keys {
					setConfig(t, work, key, script)
				}

				before := readTrees(t, bare, work)

				result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
				require.NoError(t, err)

				requireNotRun(t, marker)

				if tc.worktreeOnly && !rt.op.worktree {
					require.False(t, result.IsError, resultText(t, result))
					rt.requireLanded(t, bare, work)

					return
				}

				require.True(t, result.IsError, "the tool must refuse the repository")

				// The error names the keys that every tool reads
				// first.
				want := tc.keys
				if !rt.op.worktree {
					want = remoteKeys
				}

				text := resultText(t, result)
				assert.Contains(t, text, ErrDeniedConfig.Error()+": the repository sets "+strings.Join(want, ", ")+".")
				assert.NotContains(t, text, script, "the message must name the keys, never their values")
				requireUnchanged(t, before, bare, work)
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
//
// Every tool reads the config of the repository with the same code,
// and [TestReadRepoConfigSkipsConfigFileVariable] covers that code.
// Each tool therefore runs one of the two cases, and the tools take
// the cases in turn.
func TestRemoteToolsSkipConfigFileVariable(t *testing.T) { //nolint:paralleltest // t.Setenv
	cases := map[string]func(t *testing.T, rt remoteTool){
		"refused key": func(t *testing.T, rt remoteTool) {
			t.Helper()

			bare, work := rt.repos(t)
			script, marker := markerScript(t)

			setConfig(t, work, "credential.helper", script)
			setConfigFileEnv(t, "")

			before := readTrees(t, bare, work)

			result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
			require.NoError(t, err)
			require.True(t, result.IsError, "the tool must refuse the repository")

			assert.Contains(t,
				resultText(t, result),
				ErrDeniedConfig.Error()+": the repository sets credential.helper.",
			)
			requireNotRun(t, marker)
			requireUnchanged(t, before, bare, work)
		},
		"config hook": func(t *testing.T, rt remoteTool) {
			t.Helper()

			bare, work := rt.repos(t)
			script, marker := markerScript(t)

			appendGitConfig(t, work, hookConfig(script, "check"))
			setConfigFileEnv(t, "")

			result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			requireNotRun(t, marker)
			rt.requireLanded(t, bare, work)
		},
	}

	tools := remoteTools()
	caseNames := slices.Sorted(maps.Keys(cases))

	for i, toolName := range slices.Sorted(maps.Keys(tools)) { //nolint:paralleltest // t.Setenv
		caseName := caseNames[i%len(caseNames)]

		t.Run(toolName+"/"+caseName, func(t *testing.T) {
			cases[caseName](t, tools[toolName])
		})
	}
}

// The environment of the server can set variables that point git at a
// repository, such as GIT_DIR. git sets them for the hooks of a
// commit, and a server that a hook starts inherits them. With GIT_DIR
// set, git -C <repo> acts on the repository that the variable names.
// Every tool must still act on the repository of the call, and it must
// leave the repository that the variables name as it was.
//
// The environment of each call sets every variable that the server
// must remove, as [serverRepositoryPaths] lists them.
func TestRemoteToolsSkipRepositoryVariables(t *testing.T) { //nolint:paralleltest // t.Setenv
	// other stands for the repository that the variables name. Its
	// setup does not run git commit in other. A commit starts git
	// maintenance in the background, and maintenance writes under the
	// git directory.
	other := initWorkClone(t, initBareRepo(t))

	for toolName, rt := range remoteTools() { //nolint:paralleltest // t.Setenv
		t.Run(toolName, func(t *testing.T) {
			bare, work := rt.repos(t)
			before := readTree(t, other)

			unset := setRepositoryEnv(t, other, serverRepositoryPaths(t))
			result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)

			unset()

			require.NoError(t, err)
			assert.Equal(t, before, readTree(t, other), "the tool must not write the other repository")
			require.False(t, result.IsError, resultText(t, result))
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

			setWorkTree(t, work, outside)

			before := readTrees(t, bare, work)

			h := &handler{
				allowDirs:     mustResolveAllowDirs(t, filepath.Dir(work)),
				allowFileURLs: true,
			}

			result, _, err := rt.call(t.Context(), h, work)
			require.NoError(t, err)
			require.True(t, result.IsError, "the tool must refuse the working tree")
			assert.Contains(t, resultText(t, result), ErrDeniedDest.Error())

			requireEmptyDir(t, outside)
			requireUnchanged(t, before, bare, work)
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

	// Every tool takes the check of the working tree before it does
	// anything of its own. Each tree therefore runs with one tool, and
	// the trees take the tools in turn, so that each tool refuses more
	// than one tree.
	tools := remoteTools()
	toolNames := slices.Sorted(maps.Keys(tools))

	for i, treeName := range slices.Sorted(maps.Keys(trees)) {
		toolName := toolNames[i%len(toolNames)]
		rt := tools[toolName]
		setup := trees[treeName]

		t.Run(toolName+"/"+treeName, func(t *testing.T) {
			t.Parallel()

			bare, work := rt.repos(t)
			other := t.TempDir()
			repo, dir := setup(t, other, work)
			entries := dirEntries(t, dir)
			before := readTrees(t, bare, work)

			h := &handler{
				allowDirs:     mustResolveAllowDirs(t, filepath.Dir(work), other),
				allowFileURLs: true,
			}

			result, _, err := rt.call(t.Context(), h, repo)
			require.NoError(t, err)
			require.True(t, result.IsError, "the tool must refuse the working tree")
			assert.Contains(t, resultText(t, result), ErrDeniedWorkTree.Error())

			assert.Equal(t, entries, dirEntries(t, dir), "git must write nothing to %s", dir)
			requireUnchanged(t, before, bare, work)
		})
	}
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

	setConfig(t, work, "core.worktree", dir)
}

// plantGitFile writes a .git file into the git directory of the
// repository at repo. The file points git at the git directory of the
// repository at work. plantGitFile returns the git directory of repo.
func plantGitFile(t *testing.T, repo, work string) string {
	t.Helper()

	gitDir := filepath.Join(repo, ".git")

	// git names a git directory by its path without a symbolic link.
	target, err := filepath.EvalSymlinks(filepath.Join(work, ".git"))
	require.NoError(t, err)

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

			setWorkTree(t, work, outside)

			out, err := rt.runGit(t, &handler{allowFileURLs: true}, work)
			require.NoError(t, err, out)

			requireEmptyDir(t, outside)
			assert.FileExists(t, filepath.Join(work, remoteFile))
			rt.requireLanded(t, bare, work)
		})
	}
}

// deniedRemotes maps a name to a function that points the remote
// origin of the clone at work at a URL that the server denies. script
// is the command that git runs when it follows the URL.
func deniedRemotes() map[string]func(t *testing.T, work, bare, script string) {
	return map[string]func(t *testing.T, work, bare, script string){
		// The clone names its origin by the path of the bare
		// repository.
		"path": func(*testing.T, string, string, string) {},
		"file URL": func(t *testing.T, work, bare, _ string) {
			t.Helper()

			setOriginURL(t, work, bare, "file://"+bare)
		},
		// The ext transport runs the command that the URL holds, and
		// git allows it only when the config says so.
		"ext URL": func(t *testing.T, work, bare, script string) {
			t.Helper()

			setOriginURL(t, work, bare, "ext::"+script)
			setConfig(t, work, "protocol.ext.allow", "always")
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

				before := readTrees(t, bare, work)

				result, _, err := rt.call(t.Context(), &handler{}, work)
				require.NoError(t, err)
				require.True(t, result.IsError, "the tool must refuse the remote")

				text := resultText(t, result)
				assert.Contains(t, text, ErrDeniedURL.Error()+": remote origin")
				assert.NotContains(t, text, filepath.Dir(bare), "error must name the remote, never its URL")
				assert.NotContains(t, text, script, "error must name the remote, never its URL")

				requireNotRun(t, marker)
				requireUnchanged(t, before, bare, work)
			})
		}
	}
}

// The git config can define a remote without a URL. git then uses the
// name of the remote as its URL, and the server refuses that name as a
// URL. The error must say that the remote has no URL. It must not say
// that the remote has a URL of a scheme that the server refuses.
//
// [TestRemoteLacksURL] covers each form of a remote without a URL,
// such as a URL that an empty value clears.
func TestRemoteToolsRefuseRemoteWithoutURL(t *testing.T) {
	t.Parallel()

	const (
		noURL     = "remote has no URL: origin: the git config stores no URL in remote.origin.url"
		noPushURL = noURL + " and no push URL in remote.origin.pushurl"
	)

	tcs := map[string]struct {
		// pushURL is set when the remote keeps the path of the bare
		// repository as its push URL.
		pushURL bool
	}{
		"remote that lost its URL": {},
		"push URL and no URL":      {pushURL: true},
	}

	for toolName, rt := range remoteTools() {
		for name, tc := range tcs {
			t.Run(toolName+"/"+name, func(t *testing.T) {
				t.Parallel()

				bare, work := rt.repos(t)

				unsetOriginURL(t, work, bare)

				if tc.pushURL {
					setConfig(t, work, "remote.origin.pushurl", bare)
				}

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

				before := readTrees(t, bare, work)

				// A server that allows every URL has no URL to connect
				// to either.
				for _, h := range []*handler{{}, {allowFileURLs: true}} {
					result, _, err := rt.call(t.Context(), h, work)
					require.NoError(t, err)
					require.True(t, result.IsError, "the tool must refuse the remote")

					assert.Equal(t, want, resultText(t, result))
				}

				requireUnchanged(t, before, bare, work)
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
		// remote stores the name origin as a URL of the origin remote
		// of the clone at work. bare is the path of the bare
		// repository.
		remote func(t *testing.T, work, bare string)

		// push is set when only a push reads the URL.
		push bool
	}{
		"URL": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				setOriginURL(t, work, bare, "origin")
			},
		},
		"push URL and no URL": {
			remote: func(t *testing.T, work, bare string) {
				t.Helper()

				unsetOriginURL(t, work, bare)
				setConfig(t, work, "remote.origin.pushurl", "origin")
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

				tc.remote(t, work, bare)

				before := readTrees(t, bare, work)

				result, _, err := rt.call(t.Context(), &handler{}, work)
				require.NoError(t, err)
				require.True(t, result.IsError, "the tool must refuse the remote")

				assert.Equal(t,
					rt.failure.Error()+": "+ErrDeniedURL.Error()+
						": remote origin has a URL that is not https, ssh, or user@host:path",
					resultText(t, result),
				)

				requireUnchanged(t, before, bare, work)
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

			before := readTrees(t, bare, work)

			result, _, err := rt.call(t.Context(), &handler{}, work)
			require.NoError(t, err)
			require.True(t, result.IsError, "the tool must not contact the remote")

			text := resultText(t, result)
			assert.Contains(t, text, rt.failure.Error()+": reading the origin remote of "+work+": ")
			assert.NotContains(t, text, "unknown remote")

			requireUnchanged(t, before, bare, work)
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

			appendGitConfig(t, work, unparseableConfig)

			before := readTrees(t, bare, work)

			result, _, err := rt.call(t.Context(), &handler{allowFileURLs: true}, work)
			require.NoError(t, err)
			require.True(t, result.IsError, "the tool must not contact the remote")

			text := resultText(t, result)
			assert.Contains(t, text, rt.failure.Error()+": opening the repository at "+work+": ")
			assert.Contains(t, text, "fatal: bad config line")
			assert.NotContains(t, text, ErrNotRepo.Error())

			requireUnchanged(t, before, bare, work)
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
	remotes["transport that the config names"] = func(t *testing.T, work, bare, script string) {
		t.Helper()

		setOriginURL(t, work, bare, script)
		setConfig(t, work,
			"remote.origin.vcs", "ext",
			"protocol.ext.allow", "always",
		)
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
// must therefore name the program. One config names a program for a
// fetch and a program for a push, and each program has a marker of its
// own.
func TestGitIgnoresServePrograms(t *testing.T) {
	t.Parallel()

	for toolName, rt := range remoteTools() {
		t.Run(toolName, func(t *testing.T) {
			t.Parallel()

			bare, work := rt.repos(t)
			uploadPack, uploadPackRan := markerScript(t)
			receivePack, receivePackRan := markerScript(t)

			setConfig(t, work,
				"remote.origin.uploadpack", uploadPack,
				"remote.origin.receivepack", receivePack,
			)

			out, err := rt.runGit(t, &handler{allowFileURLs: true}, work)

			assert.NoFileExists(t, uploadPackRan, "git must not run the program of remote.origin.uploadpack")
			assert.NoFileExists(t, receivePackRan, "git must not run the program of remote.origin.receivepack")

			require.NoError(t, err, out)
			rt.requireLanded(t, bare, work)
		})
	}
}

// Each tool gives the token to an https connection to github.com.
// The credential helper of the git config answers for every other
// host, also when the token is set.
//
// The credential options of a call do not depend on the tool, and
// [TestGitOptions] checks that every command takes them. Each tool
// therefore connects to one host, and the tools take the hosts in
// turn. [TestHandlePushCredentialOfEachURL] covers a push to both
// hosts.
func TestRemoteToolsCredential(t *testing.T) { //nolint:paralleltest // t.Setenv
	const token = "ghp_test123"

	hosts := []struct {
		host string
		cred string
	}{
		{host: "github.com", cred: "x-access-token:" + token},
		{host: "gitlab.com", cred: "global:secret"},
	}

	tools := remoteTools()

	for i, toolName := range slices.Sorted(maps.Keys(tools)) { //nolint:paralleltest // setProxyEnv calls t.Setenv.
		rt := tools[toolName]
		tt := hosts[i%len(hosts)]

		t.Run(toolName+"/"+tt.host, func(t *testing.T) {
			bare, work := rt.repos(t)

			setConfig(t, bare, "http.receivepack", "true")
			setOriginURL(t, work, bare, "https://"+tt.host+"/"+filepath.Base(bare))

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

			connected, creds := proxy.requests()
			assert.Equal(t, []string{tt.host + ":443"}, connected)
			assert.Equal(t, []string{tt.cred}, slices.Compact(creds))
		})
	}
}
