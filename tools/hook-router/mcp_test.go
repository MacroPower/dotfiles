package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
