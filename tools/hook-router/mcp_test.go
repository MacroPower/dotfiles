package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/mcprules"
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

		err := handleMCP(hookInput(t, "mcp__github__search_code"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "mcp__github__search_code")
	})

	t.Run("ask match emits ask", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(hookInput(t, "mcp__spacelift__trigger_stack_run"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "ask", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "mcp__spacelift__trigger_stack_run")
	})

	t.Run("deny match emits deny", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(hookInput(t, "mcp__spacelift__list_api_keys"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

	t.Run("deny wins when tool is also allow-listed", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(hookInput(t, "mcp__github__search_code"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

	t.Run("bare server pattern allows its tools", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(hookInput(t, "mcp__nixos__nix_versions"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "allow", hso["permissionDecision"])
	})

	t.Run("glob pattern allows its tools", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(hookInput(t, "mcp__kagi__kagi_search_fetch"), &stdout, cfg, logger)
		require.NoError(t, err)

		hso := decisionOf(t, &stdout)
		assert.Equal(t, "allow", hso["permissionDecision"])
	})

	t.Run("unmatched tool: stdout empty", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(hookInput(t, "mcp__leanspec__view"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("missing tool_name: stdout empty", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP(hookInput(t, ""), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("invalid JSON: stdout empty", func(t *testing.T) {
		t.Parallel()

		cfg := config{mcpRules: rules}

		var stdout bytes.Buffer

		err := handleMCP([]byte("not json"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("nil ruleset: stdout empty", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		err := handleMCP(hookInput(t, "mcp__github__search_code"), &stdout, config{}, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})
}
