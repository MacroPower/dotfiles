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
			local: true,
			op:    remoteOp{remote: defaultRemote, push: true, args: buildPushArgs(defaultRemote, PushInput{})},
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

	// The checks saw an https URL and no hook.
	opts := h.gitOptions(rt.op, work, "https://example.invalid/repo.git", false, nil)

	return h.runGit(t.Context(), slices.Concat(opts, rt.op.args)...)
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

// git options cannot turn off these commands, so a tool refuses the
// repository when its config names one that the tool reads.
func TestRemoteToolsRefuseConfigCommands(t *testing.T) {
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
		"filter.x.clean":                            {worktree: true},
		"filter.x.smudge":                           {worktree: true},
		"filter.x.process":                          {worktree: true},
		"merge.x.driver":                            {worktree: true},
		"gpg.program":                               {worktree: true},
		"gpg.ssh.defaultkeycommand":                 {worktree: true},
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
