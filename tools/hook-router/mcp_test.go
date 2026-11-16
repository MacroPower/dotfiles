package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/git"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/gittest"
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

// conventionalBranchPattern mirrors conventionalBranchPattern in
// home/claude.nix, so update both together. Every rule there that
// tests a branch name carries this expression.
const conventionalBranchPattern = `^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)/.+$`

// conventionalBranchTypes returns the types that
// [conventionalBranchPattern] accepts before the slash.
func conventionalBranchTypes() []string {
	return []string{
		"build", "chore", "ci", "docs", "feat", "fix", "perf", "refactor", "revert", "style", "test",
	}
}

// pushRules is the --mcp-rules JSON that home/claude.nix serializes for
// mcp__git__git_push. It mirrors the conditionalRules of the git bundle
// there, so update both together.
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
					"pattern": "` + conventionalBranchPattern + `",
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

	rules, err := parseMCPRules(pushRules, nil, logger)
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
		"`" + conventionalBranchPattern + "`" +
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

	for _, kind := range conventionalBranchTypes() {
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

// An mcp__git__git_push call with no ref pushes the current branch of
// the repository that the call names. The MCP server starts with the
// environment of Claude Code, as hook-router does. That environment can
// name another repository with GIT_DIR, GIT_COMMON_DIR, or
// GIT_REFERENCE_BACKEND. The mcp-git server removes these variables
// from its git commands, so it still pushes the branch of the
// repository that the call names. The decision must follow that
// branch, whichever of the two repositories is on a branch with a
// conventional type.
//
// Claude Code inherits config in GIT_CONFIG_COUNT or in
// GIT_CONFIG_PARAMETERS, such as a safe.directory entry. Only git
// config reads GIT_CONFIG, and git sets GIT_PREFIX for a shell alias
// but never reads it. A Claude Code that a commit hook in a main
// worktree starts inherits GIT_INDEX_FILE and GIT_PREFIX from git.
// GIT_WORK_TREE changes the working tree of git, not its repository.
func TestHandleMCPPushReadsBranchOfCallRepository(t *testing.T) { //nolint:paralleltest // t.Setenv
	const tool = "mcp__git__git_push"

	logger := slog.New(slog.DiscardHandler)

	rules, err := parseMCPRules(pushRules, nil, logger)
	require.NoError(t, err)

	cfg := config{mcpRules: rules}

	fixRepo := checkoutRepo(t, "fix/y")
	mainRepo := checkoutRepo(t, "main")

	configFile := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(configFile, []byte("[user]\n\tname = Other\n"), 0o600))

	cases := map[string]struct {
		repo string
		env  map[string]string
		want string
	}{
		"git directory on a branch with a conventional type: ask": {
			repo: mainRepo,
			env:  map[string]string{"GIT_DIR": filepath.Join(fixRepo, ".git")},
			want: "ask",
		},
		"call names a repository on a branch with a conventional type: allow": {
			repo: fixRepo,
			env:  map[string]string{"GIT_DIR": filepath.Join(mainRepo, ".git")},
			want: "allow",
		},
		"common directory on a branch with a conventional type: ask": {
			repo: mainRepo,
			env:  map[string]string{"GIT_COMMON_DIR": filepath.Join(fixRepo, ".git")},
			want: "ask",
		},
		"call names a repository on a branch with a conventional type, common directory: allow": {
			repo: fixRepo,
			env:  map[string]string{"GIT_COMMON_DIR": filepath.Join(mainRepo, ".git")},
			want: "allow",
		},
		"reference backend on a branch with a conventional type: ask": {
			repo: mainRepo,
			env:  map[string]string{"GIT_REFERENCE_BACKEND": "files://" + filepath.Join(fixRepo, ".git")},
			want: "ask",
		},
		"call names a repository on a branch with a conventional type, reference backend: allow": {
			repo: fixRepo,
			env:  map[string]string{"GIT_REFERENCE_BACKEND": "files://" + filepath.Join(mainRepo, ".git")},
			want: "allow",
		},
		"config count: allow": {
			repo: fixRepo,
			env: map[string]string{
				"GIT_CONFIG_COUNT":   "1",
				"GIT_CONFIG_KEY_0":   "safe.directory",
				"GIT_CONFIG_VALUE_0": fixRepo,
			},
			want: "allow",
		},
		"config parameters: allow": {
			repo: fixRepo,
			env:  map[string]string{"GIT_CONFIG_PARAMETERS": "'safe.directory'='" + fixRepo + "'"},
			want: "allow",
		},
		"config file: allow": {
			repo: fixRepo,
			env:  map[string]string{"GIT_CONFIG": configFile},
			want: "allow",
		},
		"alias prefix: allow": {
			repo: fixRepo,
			env:  map[string]string{"GIT_PREFIX": "sub/"},
			want: "allow",
		},
		// git sets these values for the hooks of a commit in a main
		// worktree.
		"commit hook of a main worktree: allow": {
			repo: fixRepo,
			env: map[string]string{
				"GIT_INDEX_FILE": filepath.Join(".git", "index"),
				"GIT_PREFIX":     "",
			},
			want: "allow",
		},
		"working tree: allow": {
			repo: fixRepo,
			env:  map[string]string{"GIT_WORK_TREE": mainRepo},
			want: "allow",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for variable, value := range tc.env {
				t.Setenv(variable, value)
			}

			input, err := json.Marshal(map[string]any{
				"session_id": "s1",
				"tool_name":  tool,
				"tool_input": map[string]any{"repo": tc.repo},
			})
			require.NoError(t, err)

			var stdout bytes.Buffer

			require.NoError(t, handleMCP(t.Context(), input, &stdout, cfg, logger))

			hso := preToolDecision(t, &stdout)
			assert.Equal(t, tc.want, hso["permissionDecision"])
		})
	}
}

// [repoBranch] must answer with the branch of its path, whatever
// repository the environment names. The mcp-git server removes the
// same variables from its git commands, so it pushes that branch. git
// rev-parse --local-env-vars does not print GIT_REFERENCE_BACKEND, so
// the test adds it. Each case sets one variable to the git directory
// of a second repository.
func TestRepoBranchEnvironmentRepository(t *testing.T) { //nolint:paralleltest // t.Setenv
	const referenceBackend = "GIT_REFERENCE_BACKEND"

	names, err := gittest.RepositoryEnv(t.Context())
	require.NoError(t, err)

	for _, name := range []string{"GIT_DIR", "GIT_COMMON_DIR"} {
		require.Contains(t, names, name)
	}

	if !slices.Contains(names, referenceBackend) {
		names = append(names, referenceBackend)
	}

	repo := checkoutRepo(t, "fix/y")
	other := checkoutRepo(t, "main")

	// git rejects a path as the value of the variables that pass
	// config, and a git.Runner keeps both. These cases set a value that
	// git accepts. git reads the URI of a ref store from
	// GIT_REFERENCE_BACKEND.
	values := map[string]string{
		"GIT_CONFIG_COUNT":      "0",
		"GIT_CONFIG_PARAMETERS": "'user.name'='Other'",
		referenceBackend:        "files://" + filepath.Join(other, ".git"),
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			value, ok := values[name]
			if !ok {
				value = filepath.Join(other, ".git")
			}

			t.Setenv(name, value)

			branch, err := repoBranch(t.Context(), repo)
			require.NoError(t, err)
			assert.Equal(t, "fix/y", branch)
		})
	}
}

// pullRequestRules is the --mcp-rules JSON that home/claude.nix
// serializes for the pull request tools of the github bundle. It
// mirrors the conditionalRules of that bundle, and the entries of its
// permissions lists that name the tools here, so update both together.
const pullRequestRules = `{
	"allow": ["mcp__github__pull_request_read"],
	"ask": ["mcp__github__merge_pull_request"],
	"deny": ["mcp__github__create_pull_request_with_copilot"],
	"conditional": [
		{
			"tool": "mcp__github__create_pull_request",
			"conditions": [
				{"field": "draft", "test": "true", "pattern": "", "repoField": ""},
				{
					"field": "head",
					"test": "matches",
					"pattern": "` + conventionalBranchPattern + `",
					"repoField": ""
				},
				{"field": "reviewers", "test": "absentOrZero", "pattern": "", "repoField": ""}
			]
		},
		{
			"tool": "mcp__github__update_pull_request",
			"conditions": [
				{"field": "draft", "test": "absentOrTrue", "pattern": "", "repoField": ""},
				{"field": "reviewers", "test": "absentOrZero", "pattern": "", "repoField": ""},
				{"field": "base", "test": "absent", "pattern": "", "repoField": ""},
				{"field": "maintainer_can_modify", "test": "absent", "pattern": "", "repoField": ""},
				{"field": "pullNumber", "test": "pullRequestRecorded", "pattern": "", "repoField": ""}
			]
		},
		{
			"tool": "mcp__github__update_pull_request_branch",
			"conditions": [
				{"field": "pullNumber", "test": "pullRequestRecorded", "pattern": "", "repoField": ""}
			]
		}
	]
}`

// TestHandleMCPCreatePullRequest runs mcp__github__create_pull_request
// calls through [handleMCP] with the rule that home/claude.nix sets for
// the tool. The message lint is on, because it reads the title and the
// body of a call before the rule allows the call.
func TestHandleMCPCreatePullRequest(t *testing.T) {
	t.Parallel()

	const tool = "mcp__github__create_pull_request"

	logger := slog.New(slog.DiscardHandler)

	rules, err := parseMCPRules(pullRequestRules, nil, logger)
	require.NoError(t, err)

	cfg := config{
		mcpRules: rules,
		messageLint: msglint.Config{
			Command: []string{
				"sh",
				"-c",
				`grep -q deliberate && { echo '<stdin>:1:1: Style::ProseReassurance: reassurance'; exit 1; }; exit 0`,
			},
			ToolFields: []msglint.ToolField{{Tool: tool, Fields: []string{"title", "body"}}},
		},
	}

	const conditions = "`draft` is true and `head` matches `" + conventionalBranchPattern + "`" +
		" and `reviewers` is absent, false, or empty"

	// args returns the arguments of a call with clean text, and sets
	// each entry of fields in them. The rule reads draft, head, and
	// reviewers, so each case names the ones that it sets.
	args := func(fields map[string]any) map[string]any {
		input := map[string]any{
			"owner": "octo-org",
			"repo":  "widgets",
			"title": "Add a retry budget",
			"body":  "Adds a retry budget to the client.",
			"base":  "main",
		}

		maps.Copy(input, fields)

		return input
	}

	cases := map[string]struct {
		input  map[string]any
		want   string
		reason string
	}{
		"draft from a branch with a conventional type: allow": {
			input:  args(map[string]any{"draft": true, "head": "feat/x"}),
			want:   "allow",
			reason: "mcp conditional rule (" + conditions + ")",
		},
		"draft with an empty reviewers list: allow": {
			input: args(map[string]any{"draft": true, "head": "feat/x", "reviewers": []any{}}),
			want:  "allow",
		},
		"draft with null reviewers: allow": {
			input: args(map[string]any{"draft": true, "head": "feat/x", "reviewers": nil}),
			want:  "allow",
		},
		"draft against another base: allow": {
			input: args(map[string]any{"draft": true, "head": "fix/y", "base": "release/1.x"}),
			want:  "allow",
		},
		"draft that maintainers cannot modify: allow": {
			input: args(map[string]any{"draft": true, "head": "fix/y", "maintainer_can_modify": false}),
			want:  "allow",
		},
		"no draft field: ask": {
			input:  args(map[string]any{"head": "feat/x"}),
			want:   "ask",
			reason: "This call needs confirmation. " + tool + " runs without a prompt when " + conditions + ".",
		},
		"draft false: ask": {
			input: args(map[string]any{"draft": false, "head": "feat/x"}),
			want:  "ask",
		},
		"draft as a string: ask": {
			input: args(map[string]any{"draft": "true", "head": "feat/x"}),
			want:  "ask",
		},
		"head with no type: ask": {
			input:  args(map[string]any{"draft": true, "head": "my-branch"}),
			want:   "ask",
			reason: "This call needs confirmation. " + tool + " runs without a prompt when " + conditions + ".",
		},
		"head on a fork: ask": {
			input: args(map[string]any{"draft": true, "head": "someone:feat/x"}),
			want:  "ask",
		},
		"head main: ask": {
			input: args(map[string]any{"draft": true, "head": "main"}),
			want:  "ask",
		},
		"head with another prefix: ask": {
			input: args(map[string]any{"draft": true, "head": "feature/x"}),
			want:  "ask",
		},
		"head of a type alone: ask": {
			input: args(map[string]any{"draft": true, "head": "feat"}),
			want:  "ask",
		},
		"head of a type and a slash: ask": {
			input: args(map[string]any{"draft": true, "head": "feat/"}),
			want:  "ask",
		},
		"head with the type after another segment: ask": {
			input: args(map[string]any{"draft": true, "head": "release/feat/x"}),
			want:  "ask",
		},
		"full ref name as head: ask": {
			input: args(map[string]any{"draft": true, "head": "refs/heads/feat/x"}),
			want:  "ask",
		},
		"no head: ask": {
			input: args(map[string]any{"draft": true}),
			want:  "ask",
		},
		"reviewers list with a user: ask": {
			input:  args(map[string]any{"draft": true, "head": "feat/x", "reviewers": []any{"octocat"}}),
			want:   "ask",
			reason: "This call needs confirmation. " + tool + " runs without a prompt when " + conditions + ".",
		},
		"reviewers list with a team: ask": {
			input: args(map[string]any{"draft": true, "head": "feat/x", "reviewers": []any{"octo-org/platform"}}),
			want:  "ask",
		},
		"missing tool_input: ask": {
			want: "ask",
		},
		"Required finding in the body of a call that the rule allows: deny": {
			input:  args(map[string]any{"draft": true, "head": "feat/x", "body": "This is deliberate."}),
			want:   "deny",
			reason: "prose-lint: 1 finding in the `body` field of " + tool + ".",
		},
		"Required finding in the title of a call that the rule allows: deny": {
			input:  args(map[string]any{"draft": true, "head": "feat/x", "title": "Make a deliberate change"}),
			want:   "deny",
			reason: "prose-lint: 1 finding in the `title` field of " + tool + ".",
		},
		"Required finding in the body of a call that the rule does not allow: deny": {
			input:  args(map[string]any{"head": "my-branch", "body": "This is deliberate."}),
			want:   "deny",
			reason: "prose-lint: 1 finding in the `body` field of " + tool + ".",
		},
		"flagged word in a field that the lint does not read: allow": {
			input: args(map[string]any{"draft": true, "head": "fix/deliberate"}),
			want:  "allow",
		},
	}

	for _, kind := range conventionalBranchTypes() {
		cases["head of type "+kind+": allow"] = struct {
			input  map[string]any
			want   string
			reason string
		}{
			input: args(map[string]any{"draft": true, "head": kind + "/retry-budget"}),
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
				assert.Contains(t, hso["permissionDecisionReason"], tc.reason)
			}
		})
	}

	t.Run("merge of a draft from a conventional branch stays on the ask list", func(t *testing.T) {
		t.Parallel()

		const merge = "mcp__github__merge_pull_request"

		var stdout bytes.Buffer

		input := mcpCall(t, merge, map[string]any{"pullNumber": 7, "draft": true, "head": "feat/x"})

		require.NoError(t, handleMCP(t.Context(), input, &stdout, cfg, logger))

		hso := preToolDecision(t, &stdout)
		assert.Equal(t, "ask", hso["permissionDecision"])
		assert.Equal(
			t,
			"This MCP tool is on the ask list ("+merge+"). Confirm before running.",
			hso["permissionDecisionReason"],
		)
	})

	t.Run("Bash gh pr create keeps the deny that names the tool", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		cfg := config{commandRules: ghRules(), autoAllow: true}

		command := "gh pr create --draft --head feat/x --title 'Add a retry budget' --body 'Adds a retry budget.'"

		require.NoError(t, handleBash(bashInput(t, command, nil), &stdout, cfg, logger))

		hso := preToolDecision(t, &stdout)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Equal(t, ghWriteRedirectReason(tool), hso["permissionDecisionReason"])
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
