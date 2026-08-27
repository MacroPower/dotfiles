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
	gitRemoteAskReason  = "This git remote subcommand rewrites where the repository pushes and fetches. Confirm before running."
	kubectxReason       = "Do not use kubectx or kubens directly. Use mcp__kubectx__list to list contexts and mcp__kubectx__select to switch contexts."
	ghGroupAskReason    = "This gh subcommand can mutate GitHub state. Confirm before running."
	ghFallbackAskReason = "This gh subcommand is not on the read-only allowlist. Confirm before running; prefer mcp__github__* tools for reads."
	ghAPIDeniedReason   = "`gh api` reaches api.github.com, which is denied. Clone with mcp__git__git_clone to read repository files locally, or use the mcp__github__* tools for issues, PRs, releases, and search."
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
		{
			Command:    "git",
			Args:       []string{"remote"},
			Except:     []string{"-v", "--verbose", "show", "get-url", "-h", "--help"},
			ExceptBare: true,
			Action:     "ask",
			Reason:     gitRemoteAskReason,
		},
	})
}

// ghAskRules mirrors the gh ask-rule bundle in home/claude.nix:
// subcommand-scoped rules first, top-level fallback last. Each group's
// except set is the union of its allowed and redirected read-only
// leaves. Matches the same-named fixture in the cmdrules package tests.
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
		group("cache", "list"),
		group("issue", "list", "view"),
		group("label", "list"),
		group("pr", "checks", "status", "diff", "list", "view"),
		group("release", "list", "view"),
		group("repo", "view", "list"),
		group("run", "watch", "view", "list"),
		group("workflow", "view", "list"),
		{
			Command: "gh",
			Except: []string{
				"cache", "issue", "label", "pr", "release", "repo",
				"run", "workflow", "status", "help", "version",
				"--version",
			},
			Action: "ask",
			Reason: ghFallbackAskReason,
		},
	})
}

// ghRedirectReason mirrors the redirect deny reason produced in
// home/claude.nix for a gh read subcommand with a github MCP equivalent.
func ghRedirectReason(tool string) string {
	return "Read via " + tool + " instead of the gh CLI."
}

// ghRedirectRules mirrors the gh redirect deny-rule bundle in
// home/claude.nix, including the blanket `gh api` deny that leads it.
// Matches the same-named fixture in the cmdrules package tests.
func ghRedirectRules() *cmdrules.Engine {
	redirect := func(tool string, args ...string) cmdrules.Rule {
		return cmdrules.Rule{
			Command: "gh",
			Args:    args,
			Reason:  ghRedirectReason(tool),
		}
	}

	return cmdrules.New([]cmdrules.Rule{
		{
			Command: "gh",
			Args:    []string{"api"},
			Reason:  ghAPIDeniedReason,
		},
		redirect("mcp__github__issue_read", "issue", "view"),
		redirect("mcp__github__list_issues", "issue", "list"),
		redirect("mcp__github__list_label", "label", "list"),
		redirect("mcp__github__pull_request_read", "pr", "view"),
		redirect("mcp__github__list_pull_requests", "pr", "list"),
		redirect("mcp__github__pull_request_read (diff method)", "pr", "diff"),
		redirect("mcp__github__get_release_by_tag / mcp__github__get_latest_release", "release", "view"),
		redirect("mcp__github__list_releases", "release", "list"),
		redirect("mcp__github__actions_get (get_workflow_run) / mcp__github__get_job_logs for logs", "run", "view"),
		redirect("mcp__github__actions_list (list_workflow_runs)", "run", "list"),
		redirect("mcp__github__actions_get (get_workflow)", "workflow", "view"),
		redirect("mcp__github__actions_list (list_workflows)", "workflow", "list"),
		redirect("mcp__github__search_code / search_issues / search_pull_requests / search_repositories", "search"),
	})
}

// ghWriteRedirectReason mirrors the redirect deny reason produced in
// home/claude.nix for a gh write subcommand that has a github MCP
// equivalent.
func ghWriteRedirectReason(tool string) string {
	return "Write via " + tool + " instead of the gh CLI."
}

// ghWriteRedirectRules mirrors the gh write-redirect deny-rule bundle
// in home/claude.nix: mutating gh subcommands whose mutation an
// ask-gated github MCP tool covers, denied and pointed at the MCP
// tool. Matches the same-named fixture in the cmdrules package tests.
func ghWriteRedirectRules() *cmdrules.Engine {
	redirect := func(tool string, args ...string) cmdrules.Rule {
		return cmdrules.Rule{
			Command: "gh",
			Args:    args,
			Reason:  ghWriteRedirectReason(tool),
		}
	}

	return cmdrules.New([]cmdrules.Rule{
		redirect("mcp__github__create_pull_request", "pr", "create"),
		redirect("mcp__github__update_pull_request", "pr", "edit"),
		redirect("mcp__github__update_pull_request (state: closed)", "pr", "close"),
		redirect("mcp__github__update_pull_request (state: open)", "pr", "reopen"),
		redirect("mcp__github__update_pull_request (draft: false)", "pr", "ready"),
		redirect("mcp__github__merge_pull_request", "pr", "merge"),
		redirect("mcp__github__pull_request_review_write (+ add_comment_to_pending_review for inline comments)", "pr", "review"),
		redirect("mcp__github__update_pull_request_branch", "pr", "update-branch"),
		redirect("mcp__github__actions_run_trigger (rerun_workflow_run / rerun_failed_jobs)", "run", "rerun"),
		redirect("mcp__github__actions_run_trigger (cancel_workflow_run)", "run", "cancel"),
		redirect("mcp__github__actions_run_trigger (run_workflow)", "workflow", "run"),
	})
}

// ghRules mirrors the full production gh engine: read and write
// redirect deny rules before ask rules, matching the hook-router
// wrapper's serialization order (all deny rules precede any ask rule).
func ghRules() *cmdrules.Engine {
	rules := append(ghRedirectRules().Rules(), ghWriteRedirectRules().Rules()...)
	return cmdrules.New(append(rules, ghAskRules().Rules()...))
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
