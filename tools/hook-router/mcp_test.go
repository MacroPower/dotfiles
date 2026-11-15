package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/git"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/mcprules"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/msglint"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// TestHandleMCP exercises the decision paths in [handleMCP].
func TestHandleMCP(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	hookInput := func(t *testing.T, toolName string) []byte {
		t.Helper()

		payload := map[string]any{}
		if toolName != "" {
			payload["tool_name"] = toolName
		}

		b, err := json.Marshal(payload)
		require.NoError(t, err)

		return b
	}

	rules := mcprules.New(
		[]string{"mcp__github__search_code", "mcp__nixos", "mcp__kagi__*"},
		[]string{"mcp__spacelift__trigger_stack_run"},
		[]string{"mcp__spacelift__list_api_keys", "mcp__github__search_code"},
	)

	decisionOf := func(t *testing.T, stdout *bytes.Buffer) map[string]any {
		t.Helper()

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])

		return hso
	}

	t.Run("allow match emits allow", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: mcprules.New([]string{"mcp__github__search_code"}, nil, nil)}

		var stdout bytes.Buffer

		err := handleMCP(t.Context(), hookInput(t, "mcp__github__search_code"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "mcp__github__search_code")
	})

	t.Run("ask match emits ask", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(t.Context(), hookInput(t, "mcp__spacelift__trigger_stack_run"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "ask", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "mcp__spacelift__trigger_stack_run")
	})

	t.Run("deny match emits deny", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(t.Context(), hookInput(t, "mcp__spacelift__list_api_keys"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

	t.Run("deny wins when tool is also allow-listed", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(t.Context(), hookInput(t, "mcp__github__search_code"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

	t.Run("bare server pattern allows its tools", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(t.Context(), hookInput(t, "mcp__nixos__nix_versions"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "allow", hso["permissionDecision"])
	})

	t.Run("glob pattern allows its tools", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(t.Context(), hookInput(t, "mcp__kagi__kagi_search_fetch"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "allow", hso["permissionDecision"])
	})

	t.Run("unmatched tool: stdout empty", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(t.Context(), hookInput(t, "mcp__leanspec__view"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("missing tool_name: stdout empty", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(t.Context(), hookInput(t, ""), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("invalid JSON: stdout empty", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(t.Context(), []byte("not json"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("nil ruleset: stdout empty", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		err := handleMCP(t.Context(), hookInput(t, "mcp__github__search_code"), &stdout, config{}, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})
}

// mcpCall returns a PreToolUse payload for an MCP tool call.
func mcpCall(t *testing.T, tool string, input map[string]any) []byte {
	t.Helper()

	b, err := json.Marshal(map[string]any{
		"session_id": "s1",
		"tool_name":  tool,
		"tool_input": input,
	})
	require.NoError(t, err)

	return b
}

// TestHandleMCPProseLint exercises the message lint that [handleMCP]
// runs over the tool fields of an MCP call.
func TestHandleMCPProseLint(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	rules := mcprules.New(
		[]string{"mcp__slack__post_message"},
		[]string{"mcp__github__create_pull_request"},
		[]string{"mcp__github__add_issue_comment"},
	)

	toolFields := []msglint.ToolField{
		{Tool: "mcp__slack__post_message", Fields: []string{"text"}},
		{Tool: "mcp__github__create_pull_request", Fields: []string{"title", "body"}},
		{Tool: "mcp__github__add_issue_comment", Fields: []string{"body"}},
		{Tool: "mcp__chat__send", Fields: []string{"text"}},
	}

	flagDeliberate := []string{"sh", "-c", `grep -q deliberate && { echo '<stdin>:1:1: Style::ProseReassurance: reassurance'; exit 1; }; exit 0`}
	flagAll := []string{"sh", "-c", `echo '<stdin>:1:1: Style::ProseReassurance: reassurance'; exit 1`}
	crash := []string{"sh", "-c", `exit 2`}

	cases := map[string]struct {
		tool   string
		input  map[string]any
		lint   []string
		want   string
		reason string
	}{
		"Required finding denies an allow-listed tool": {
			tool:   "mcp__slack__post_message",
			input:  map[string]any{"channel": "C1", "text": "This is deliberate."},
			lint:   flagDeliberate,
			want:   "deny",
			reason: "prose-lint: 1 finding in the `text` field of mcp__slack__post_message.",
		},
		"Required finding denies an ask-listed tool": {
			tool:   "mcp__github__create_pull_request",
			input:  map[string]any{"title": "Add a flag", "body": "This is deliberate."},
			lint:   flagDeliberate,
			want:   "deny",
			reason: "in the `body` field of mcp__github__create_pull_request.",
		},
		"deny-listed tool keeps its deny-list reason": {
			tool:   "mcp__github__add_issue_comment",
			input:  map[string]any{"body": "Any text."},
			lint:   flagAll,
			want:   "deny",
			reason: "This MCP tool is denied (mcp__github__add_issue_comment).",
		},
		"clean text falls through to the ask decision": {
			tool:  "mcp__github__create_pull_request",
			input: map[string]any{"title": "Add a flag", "body": "Adds a flag."},
			lint:  flagDeliberate,
			want:  "ask",
		},
		"field missing from toolFields is not linted": {
			tool:  "mcp__github__create_pull_request",
			input: map[string]any{"title": "Add a flag", "head": "deliberate"},
			lint:  flagDeliberate,
			want:  "ask",
		},
		"linter crash falls through to the normal decision": {
			tool:  "mcp__github__create_pull_request",
			input: map[string]any{"body": "This is deliberate."},
			lint:  crash,
			want:  "ask",
		},
		"field rule without an mcp rule denies on a Required finding": {
			tool:   "mcp__chat__send",
			input:  map[string]any{"text": "This is deliberate."},
			lint:   flagDeliberate,
			want:   "deny",
			reason: "in the `text` field of mcp__chat__send.",
		},
		"field rule without an mcp rule passes clean text": {
			tool:  "mcp__chat__send",
			input: map[string]any{"text": "Hello."},
			lint:  flagDeliberate,
		},
		"disabled linter keeps the rule decision": {
			tool:  "mcp__slack__post_message",
			input: map[string]any{"text": "This is deliberate."},
			want:  "allow",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := config{
				mcpRules:    rules,
				messageLint: msglint.Config{Command: tc.lint, ToolFields: toolFields},
			}

			var stdout bytes.Buffer

			require.NoError(t, handleMCP(t.Context(), mcpCall(t, tc.tool, tc.input), &stdout, cfg, logger))

			if tc.want == "" {
				assert.Empty(t, stdout.Bytes())

				return
			}

			hso := preToolDecision(t, &stdout)
			assert.Equal(t, tc.want, hso["permissionDecision"])

			if tc.reason != "" {
				assert.Contains(t, hso["permissionDecisionReason"], tc.reason)
			}
		})
	}
}

// TestHandleMCPConditional exercises the conditional rules in
// [handleMCP]. The ruleset comes from the --mcp-rules JSON, so the test
// covers the format that home/claude.nix serializes.
func TestHandleMCPConditional(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	rules, err := mcprules.Parse(`{
		"allow": ["mcp__git__git_clone", "mcp__github__push_files"],
		"ask": ["mcp__github__merge_pull_request"],
		"deny": ["mcp__github__push_files"],
		"conditional": [
			{
				"tool": "mcp__git__git_push",
				"conditions": [
					{"field": "ref", "test": "matches", "pattern": "^(feat|fix)/.+$"},
					{"field": "tags", "test": "absentOrZero", "pattern": ""}
				]
			},
			{
				"tool": "mcp__github__create_pull_request",
				"conditions": [
					{"field": "draft", "test": "true"},
					{"field": "reviewers", "test": "absentOrZero"}
				]
			},
			{
				"tool": "mcp__github__update_pull_request",
				"conditions": [{"field": "draft", "test": "absentOrTrue"}]
			},
			{
				"tool": "mcp__github__push_files",
				"conditions": [{"field": "branch", "test": "matches", "pattern": "feat/.+"}]
			}
		]
	}`)
	require.NoError(t, err)

	toolFields := []msglint.ToolField{
		{Tool: "mcp__github__create_pull_request", Fields: []string{"title", "body"}},
	}

	flagDeliberate := []string{
		"sh",
		"-c",
		`grep -q deliberate && { echo '<stdin>:1:1: Style::ProseReassurance: reassurance'; exit 1; }; exit 0`,
	}

	const (
		pushConditions   = "`ref` matches `^(feat|fix)/.+$` and `tags` is absent, false, or empty"
		createConditions = "`draft` is true and `reviewers` is absent, false, or empty"
	)

	cases := map[string]struct {
		input  map[string]any
		tool   string
		want   string
		reason string
	}{
		"string field matches the pattern: allow": {
			tool:   "mcp__git__git_push",
			input:  map[string]any{"repo": "/r", "ref": "feat/x", "force_with_lease": true},
			want:   "allow",
			reason: "mcp conditional rule (" + pushConditions + ")",
		},
		"string field does not match the pattern: ask": {
			tool:   "mcp__git__git_push",
			input:  map[string]any{"repo": "/r", "ref": "main"},
			want:   "ask",
			reason: "This call needs confirmation. mcp__git__git_push runs without a prompt when " + pushConditions + ".",
		},
		"string field is absent: ask": {
			tool:  "mcp__git__git_push",
			input: map[string]any{"repo": "/r"},
			want:  "ask",
		},
		"field that must be zero is true: ask": {
			tool:  "mcp__git__git_push",
			input: map[string]any{"repo": "/r", "ref": "feat/x", "tags": true},
			want:  "ask",
		},
		"field that must be zero is false: allow": {
			tool:  "mcp__git__git_push",
			input: map[string]any{"repo": "/r", "ref": "fix/y", "tags": false},
			want:  "allow",
		},
		"boolean field is true: allow": {
			tool:   "mcp__github__create_pull_request",
			input:  map[string]any{"title": "Add a flag", "draft": true, "reviewers": []any{}},
			want:   "allow",
			reason: "mcp conditional rule (" + createConditions + ")",
		},
		"boolean field is absent: ask": {
			tool:   "mcp__github__create_pull_request",
			input:  map[string]any{"title": "Add a flag"},
			want:   "ask",
			reason: "mcp__github__create_pull_request runs without a prompt when " + createConditions + ".",
		},
		"boolean field is false: ask": {
			tool:  "mcp__github__create_pull_request",
			input: map[string]any{"title": "Add a flag", "draft": false},
			want:  "ask",
		},
		"list field holds an item: ask": {
			tool:  "mcp__github__create_pull_request",
			input: map[string]any{"title": "Add a flag", "draft": true, "reviewers": []any{"octocat"}},
			want:  "ask",
		},
		"boolean field that may be absent is absent: allow": {
			tool:  "mcp__github__update_pull_request",
			input: map[string]any{"title": "Add a flag"},
			want:  "allow",
		},
		"boolean field that may be absent is true: allow": {
			tool:  "mcp__github__update_pull_request",
			input: map[string]any{"draft": true},
			want:  "allow",
		},
		"boolean field that may be absent is false: ask": {
			tool:   "mcp__github__update_pull_request",
			input:  map[string]any{"draft": false},
			want:   "ask",
			reason: "mcp__github__update_pull_request runs without a prompt when `draft` is absent or true.",
		},
		"missing tool_input: ask": {
			tool: "mcp__git__git_push",
			want: "ask",
		},
		"deny pattern wins over a rule that holds": {
			tool:   "mcp__github__push_files",
			input:  map[string]any{"branch": "feat/x"},
			want:   "deny",
			reason: "This MCP tool is denied (mcp__github__push_files).",
		},
		"lint denies a call that a rule allows": {
			tool:   "mcp__github__create_pull_request",
			input:  map[string]any{"title": "Add a flag", "body": "This is deliberate.", "draft": true},
			want:   "deny",
			reason: "prose-lint: 1 finding in the `body` field of mcp__github__create_pull_request.",
		},
		"lint denies a call that no rule allows": {
			tool:   "mcp__github__create_pull_request",
			input:  map[string]any{"title": "Add a flag", "body": "This is deliberate."},
			want:   "deny",
			reason: "prose-lint: 1 finding in the `body` field of mcp__github__create_pull_request.",
		},
		"clean text keeps the allow of a rule": {
			tool:  "mcp__github__create_pull_request",
			input: map[string]any{"title": "Add a flag", "body": "Adds a flag.", "draft": true},
			want:  "allow",
		},
		"tool with no rule keeps its allow-list decision": {
			tool:   "mcp__git__git_clone",
			input:  map[string]any{"url": "https://example.com/r.git", "ref": "main"},
			want:   "allow",
			reason: "mcp allow-list (mcp__git__git_clone)",
		},
		"tool with no rule keeps its ask-list decision": {
			tool:   "mcp__github__merge_pull_request",
			input:  map[string]any{"draft": true},
			want:   "ask",
			reason: "This MCP tool is on the ask list (mcp__github__merge_pull_request). Confirm before running.",
		},
		"tool with no rule and no pattern gets no decision": {
			tool:  "mcp__git__git_fetch",
			input: map[string]any{"ref": "feat/x"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := config{
				mcpRules:    rules,
				messageLint: msglint.Config{Command: flagDeliberate, ToolFields: toolFields},
			}

			payload := map[string]any{"session_id": "s1", "tool_name": tc.tool}
			if tc.input != nil {
				payload["tool_input"] = tc.input
			}

			input, err := json.Marshal(payload)
			require.NoError(t, err)

			var stdout bytes.Buffer

			require.NoError(t, handleMCP(t.Context(), input, &stdout, cfg, logger))

			if tc.want == "" {
				assert.Empty(t, stdout.Bytes())

				return
			}

			hso := preToolDecision(t, &stdout)
			assert.Equal(t, tc.want, hso["permissionDecision"])

			if tc.reason != "" {
				assert.Contains(t, hso["permissionDecisionReason"], tc.reason)
			}
		})
	}
}

// pushRules is the --mcp-rules JSON that home/claude.nix serializes for
// mcp__git__git_push. It mirrors conventionalBranchPattern and the
// conditionalRules of the git bundle there, so update both together.
const pushRules = `{
	"allow": ["mcp__git__git_clone", "mcp__git__git_fetch", "mcp__git__git_pull"],
	"ask": [],
	"deny": [],
	"conditional": [
		{
			"tool": "mcp__git__git_push",
			"conditions": [
				{
					"field": "ref",
					"test": "branchMatches",
					"pattern": "^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)/.+$",
					"repoField": "repo"
				},
				{"field": "tags", "test": "absentOrZero", "pattern": "", "repoField": ""}
			]
		}
	]
}`

// checkoutRepo creates a git repository with one commit, checks out
// the branch named branch in it, and returns its path. It uses -B
// because git init might already have created a branch of that name.
func checkoutRepo(t *testing.T, branch string) string {
	t.Helper()

	dir := initTestRepo(t)

	cmd := exec.CommandContext(t.Context(), "git", "checkout", "-B", branch)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git checkout -B %s: %s", branch, out)

	return dir
}

// TestHandleMCPPush runs mcp__git__git_push calls through [handleMCP]
// with the rule that home/claude.nix sets for the tool. [parseMCPRules]
// builds the ruleset the way [mainErr] does. A call that names no ref
// pushes the current branch, so those cases read the branch of a real
// repository.
func TestHandleMCPPush(t *testing.T) {
	t.Parallel()

	const tool = "mcp__git__git_push"

	logger := slog.New(slog.DiscardHandler)

	rules, err := parseMCPRules(pushRules, logger)
	require.NoError(t, err)

	cfg := config{mcpRules: rules}

	fixRepo := checkoutRepo(t, "fix/y")
	mainRepo := checkoutRepo(t, "main")

	detachedRepo := checkoutRepo(t, "feat/x")
	cmd := exec.CommandContext(t.Context(), "git", "checkout", "--detach")
	cmd.Dir = detachedRepo

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git checkout --detach: %s", out)

	subdir := filepath.Join(fixRepo, "sub")
	require.NoError(t, os.Mkdir(subdir, 0o755))

	const conditions = "`ref`, or the current branch of `repo` when `ref` is empty, matches " +
		"`^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)/.+$`" +
		" and `tags` is absent, false, or empty"

	cases := map[string]struct {
		input  map[string]any
		want   string
		reason string
	}{
		"ref with a conventional type: allow": {
			input:  map[string]any{"repo": mainRepo, "ref": "feat/x"},
			want:   "allow",
			reason: "mcp conditional rule (" + conditions + ")",
		},
		"ref with a conventional type and force_with_lease: allow": {
			input: map[string]any{"repo": mainRepo, "ref": "feat/x", "force_with_lease": true},
			want:  "allow",
		},
		"ref with a conventional type, set_upstream, and remote: allow": {
			input: map[string]any{"repo": mainRepo, "ref": "feat/x", "set_upstream": true, "remote": "fork"},
			want:  "allow",
		},
		"ref with a conventional type and tags false: allow": {
			input: map[string]any{"repo": mainRepo, "ref": "feat/x", "tags": false},
			want:  "allow",
		},
		"ref main: ask": {
			input:  map[string]any{"repo": fixRepo, "ref": "main"},
			want:   "ask",
			reason: "This call needs confirmation. " + tool + " runs without a prompt when " + conditions + ".",
		},
		"ref with a conventional type and tags: ask": {
			input: map[string]any{"repo": fixRepo, "ref": "feat/x", "tags": true},
			want:  "ask",
		},
		"ref with another prefix: ask": {
			input: map[string]any{"repo": fixRepo, "ref": "feature/x"},
			want:  "ask",
		},
		"ref of a type alone: ask": {
			input: map[string]any{"repo": fixRepo, "ref": "feat"},
			want:  "ask",
		},
		"ref of a type and a slash: ask": {
			input: map[string]any{"repo": fixRepo, "ref": "feat/"},
			want:  "ask",
		},
		"ref with the type after another segment: ask": {
			input: map[string]any{"repo": fixRepo, "ref": "release/feat/x"},
			want:  "ask",
		},
		"full ref name: ask": {
			input: map[string]any{"repo": fixRepo, "ref": "refs/heads/feat/x"},
			want:  "ask",
		},
		"no ref on a branch with a conventional type: allow": {
			input:  map[string]any{"repo": fixRepo},
			want:   "allow",
			reason: "mcp conditional rule (" + conditions + ")",
		},
		"empty ref on a branch with a conventional type: allow": {
			input: map[string]any{"repo": fixRepo, "ref": ""},
			want:  "allow",
		},
		"no ref and force_with_lease on a branch with a conventional type: allow": {
			input: map[string]any{"repo": fixRepo, "force_with_lease": true, "set_upstream": true},
			want:  "allow",
		},
		"no ref in a subdirectory of the repository: allow": {
			input: map[string]any{"repo": subdir},
			want:  "allow",
		},
		"no ref on main: ask": {
			input:  map[string]any{"repo": mainRepo},
			want:   "ask",
			reason: "This call needs confirmation. " + tool + " runs without a prompt when " + conditions + ".",
		},
		"no ref and tags on a branch with a conventional type: ask": {
			input: map[string]any{"repo": fixRepo, "tags": true},
			want:  "ask",
		},
		"no ref on a detached HEAD: ask": {
			input: map[string]any{"repo": detachedRepo},
			want:  "ask",
		},
		"no ref and a path that holds no repository: ask": {
			input: map[string]any{"repo": t.TempDir()},
			want:  "ask",
		},
		"no ref and a path that does not exist: ask": {
			input: map[string]any{"repo": filepath.Join(t.TempDir(), "missing")},
			want:  "ask",
		},
		"no ref and a relative repo: ask": {
			input: map[string]any{"repo": "."},
			want:  "ask",
		},
		"no ref and no repo: ask": {
			input: map[string]any{"remote": "origin"},
			want:  "ask",
		},
		"missing tool_input: ask": {
			want: "ask",
		},
	}

	for _, kind := range []string{
		"build", "chore", "ci", "docs", "feat", "fix", "perf", "refactor", "revert", "style", "test",
	} {
		cases["ref of type "+kind+": allow"] = struct {
			input  map[string]any
			want   string
			reason string
		}{
			input: map[string]any{"repo": mainRepo, "ref": kind + "/retry-budget"},
			want:  "allow",
		}
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			payload := map[string]any{"session_id": "s1", "tool_name": tool}
			if tc.input != nil {
				payload["tool_input"] = tc.input
			}

			input, err := json.Marshal(payload)
			require.NoError(t, err)

			var stdout bytes.Buffer

			require.NoError(t, handleMCP(t.Context(), input, &stdout, cfg, logger))

			hso := preToolDecision(t, &stdout)
			assert.Equal(t, tc.want, hso["permissionDecision"])

			if tc.reason != "" {
				assert.Equal(t, tc.reason, hso["permissionDecisionReason"])
			}
		})
	}

	t.Run("Bash git push keeps the deny that names the tool", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		cfg := config{commandRules: canonicalRules(), autoAllow: true}

		require.NoError(t, handleBash(bashInput(t, "git push origin feat/x", nil), &stdout, cfg, logger))

		hso := preToolDecision(t, &stdout)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Equal(t, pushDeniedReason, hso["permissionDecisionReason"])
		assert.Contains(t, hso["permissionDecisionReason"], tool)
	})
}

// TestRepoBranch checks the paths that [repoBranch] accepts.
func TestRepoBranch(t *testing.T) {
	t.Parallel()

	repo := checkoutRepo(t, "docs/readme")

	got, err := repoBranch(t.Context(), repo)
	require.NoError(t, err)
	assert.Equal(t, "docs/readme", got)

	// A relative path stays unresolved even when the working directory
	// of the test is inside a repository.
	for _, relative := range []string{".", "", "repo", "../repo"} {
		_, err = repoBranch(t.Context(), relative)
		require.ErrorIs(t, err, errRelativeRepo, "path %q", relative)
	}

	_, err = repoBranch(t.Context(), t.TempDir())
	require.Error(t, err)
	require.NotErrorIs(t, err, errRelativeRepo)

	cmd := exec.CommandContext(t.Context(), "git", "checkout", "--detach")
	cmd.Dir = repo

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git checkout --detach: %s", out)

	_, err = repoBranch(t.Context(), repo)
	require.ErrorIs(t, err, git.ErrNoBranch)
}

// TestHandleMCPProseLintWaiver runs the same Recommended-only tool call
// three times against one store. The first run gets the deny and
// claims the waiver, the identical rerun gets the ask decision, and a
// changed body gets the deny again as a new claim.
func TestHandleMCPProseLintWaiver(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "state.db")

	cfg := config{
		mcpRules: mcprules.New(nil, []string{"mcp__github__create_pull_request"}, nil),
		messageLint: msglint.Config{
			Command:    []string{"sh", "-c", `grep -q passive && { echo '<stdin>:1:1: [Recommended] Style::ProsePassive: passive'; exit 1; }; exit 0`},
			ToolFields: []msglint.ToolField{{Tool: "mcp__github__create_pull_request", Fields: []string{"body"}}},
		},
		openStore: func(ctx context.Context) (*state.Store, error) {
			return state.Open(ctx, dbPath)
		},
	}

	call := func(body string) []byte {
		return mcpCall(t, "mcp__github__create_pull_request", map[string]any{"body": body})
	}

	var first bytes.Buffer

	require.NoError(t, handleMCP(t.Context(), call("This is passive."), &first, cfg, logger))

	hso := preToolDecision(t, &first)
	assert.Equal(t, "deny", hso["permissionDecision"])

	reason, ok := hso["permissionDecisionReason"].(string)
	require.True(t, ok)
	assert.Contains(t, reason, "Recommended: Fix each unless")
	assert.Contains(t, reason, "runs again unchanged")

	var rerun bytes.Buffer

	require.NoError(t, handleMCP(t.Context(), call("This is passive."), &rerun, cfg, logger))
	assert.Equal(t, "ask", preToolDecision(t, &rerun)["permissionDecision"], "the identical rerun reaches the ask decision")

	var changed bytes.Buffer

	require.NoError(t, handleMCP(t.Context(), call("This is passive too."), &changed, cfg, logger))
	assert.Equal(t, "deny", preToolDecision(t, &changed)["permissionDecision"], "a changed body claims a new waiver")

	var broken bytes.Buffer

	cfg.openStore = func(context.Context) (*state.Store, error) {
		return nil, errors.New("disk on fire")
	}

	require.NoError(t, handleMCP(t.Context(), call("This is passive again."), &broken, cfg, logger))
	assert.Equal(t, "ask", preToolDecision(t, &broken)["permissionDecision"], "a store error fails open")
}
