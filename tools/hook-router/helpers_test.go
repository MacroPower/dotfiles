package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/cmdrules"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// testPID is the claude_pid used by handler tests that exercise the
// pending-plan handoff.
const testPID = "12345"

// Reason strings mirrored from the rule bundles in home/claude.nix.
// The cmdrules package owns the matcher-level coverage of these rules;
// the copies here exist so the handler-level integration tests can
// assert against the same decisions production emits.
const (
	stashDeniedReason   = "Do not use git stash to shelve changes. All issues in the working tree are your responsibility to fix, regardless of origin."
	cloneDeniedReason   = "Direct git clone usage is blocked. Use mcp__git__git_clone instead."
	kubectxReason       = "Do not use kubectx or kubens directly. Use mcp__kubectx__list to list contexts and mcp__kubectx__select to switch contexts."
	ghGroupAskReason    = "This gh subcommand can mutate GitHub state. Confirm before running."
	ghFallbackAskReason = "This gh subcommand is not on the read-only allowlist. Confirm before running; prefer mcp__github__* tools for reads."
)

// canonicalRules mirrors the rules wired into home/claude.nix for the
// git and kubectx bundles, matching the same-named fixture in the
// cmdrules package tests. Update both when home/claude.nix gains or
// drops rules.
func canonicalRules() *cmdrules.Engine {
	return cmdrules.New([]cmdrules.Rule{
		{
			Command: "git",
			Args:    []string{"clone"},
			Reason:  cloneDeniedReason,
		},
		{
			Command: "git",
			Args:    []string{"stash"},
			Except:  []string{"pop", "apply", "list", "show", "branch", "drop", "clear"},
			Reason:  stashDeniedReason,
		},
		{
			Command: "kubectx",
			Reason:  kubectxReason,
		},
		{
			Command: "kubens",
			Reason:  kubectxReason,
		},
	})
}

// ghAskRules mirrors the gh ask-rule bundle in home/claude.nix:
// subcommand-scoped rules first, top-level fallback last. Matches the
// same-named fixture in the cmdrules package tests.
func ghAskRules() *cmdrules.Engine {
	group := func(name string, except ...string) cmdrules.Rule {
		return cmdrules.Rule{
			Command: "gh",
			Args:    []string{name},
			Except:  except,
			Action:  "ask",
			Reason:  ghGroupAskReason,
		}
	}

	return cmdrules.New([]cmdrules.Rule{
		group("pr", "view", "list", "diff", "checks", "status"),
		group("issue", "view", "list"),
		group("run", "view", "list", "watch"),
		group("repo", "view", "list"),
		group("release", "view", "list"),
		group("workflow", "view", "list"),
		{
			Command: "gh",
			Except: []string{
				"pr", "issue", "run", "repo", "release", "workflow",
				"search", "status", "help", "version", "--version",
			},
			Action: "ask",
			Reason: ghFallbackAskReason,
		},
	})
}

// newTestStore opens a fresh [*state.Store] in a per-test temp dir and
// closes it on cleanup.
func newTestStore(t *testing.T) *state.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")

	store, err := state.Open(t.Context(), dbPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	return store
}

// initTestRepo creates a git repository with one commit and returns
// its path.
func initTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v: %s", args, out)
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644))

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v: %s", args, out)
	}

	return dir
}
