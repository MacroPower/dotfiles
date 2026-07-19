package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// preFileWriteInput marshals a PreToolUse payload for toolName.
func preFileWriteInput(t *testing.T, toolName string, toolInput map[string]any) []byte {
	t.Helper()

	input, err := json.Marshal(map[string]any{
		"tool_name":  toolName,
		"tool_input": toolInput,
	})
	require.NoError(t, err)

	return input
}

// decodeDeny asserts stdout carries a PreToolUse deny decision and
// returns its reason.
func decodeDeny(t *testing.T, stdout []byte) string {
	t.Helper()

	var result map[string]any
	require.NoError(t, json.Unmarshal(stdout, &result))

	hso, ok := result["hookSpecificOutput"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "PreToolUse", hso["hookEventName"])
	assert.Equal(t, "deny", hso["permissionDecision"])

	reason, ok := hso["permissionDecisionReason"].(string)
	require.True(t, ok)

	return reason
}

func TestHandlePreFileWrite(t *testing.T) {
	t.Parallel()

	cfg := config{enforceTypography: true}
	logger := slog.New(slog.DiscardHandler)

	cases := map[string]struct {
		tool      string
		toolInput map[string]any
		wantDeny  string // "" means no output expected
	}{
		"write new file with em dash denies": {
			tool: "Write",
			toolInput: map[string]any{
				"file_path": "/nonexistent/hook-router-test/new.md",
				"content":   "a — b",
			},
			wantDeny: "U+2014",
		},
		"write clean content silent": {
			tool: "Write",
			toolInput: map[string]any{
				"file_path": "/nonexistent/hook-router-test/new.md",
				"content":   "a -- b",
			},
		},
		"edit introducing curly quote denies": {
			tool: "Edit",
			toolInput: map[string]any{
				"file_path":  "/tmp/x.md",
				"old_string": "dont",
				"new_string": "don’t",
			},
			wantDeny: "U+2019",
		},
		"edit preserving dash silent": {
			tool: "Edit",
			toolInput: map[string]any{
				"file_path":  "/tmp/x.md",
				"old_string": "a — b",
				"new_string": "a — b!",
			},
		},
		"edit ascii only silent": {
			tool: "Edit",
			toolInput: map[string]any{
				"file_path":  "/tmp/x.md",
				"old_string": "x",
				"new_string": "y -- z",
			},
		},
		"multiedit one net-introducing edit denies": {
			tool: "MultiEdit",
			toolInput: map[string]any{
				"file_path": "/tmp/x.md",
				"edits": []any{
					map[string]any{"old_string": "a", "new_string": "b"},
					map[string]any{"old_string": "c", "new_string": "wait…"},
				},
			},
			wantDeny: "U+2026",
		},
		"multiedit cross-edit move silent": {
			tool: "MultiEdit",
			toolInput: map[string]any{
				"file_path": "/tmp/x.md",
				"edits": []any{
					map[string]any{"old_string": "a — b", "new_string": "a b"},
					map[string]any{"old_string": "c d", "new_string": "c — d"},
				},
			},
		},
		"multiedit all clean silent": {
			tool: "MultiEdit",
			toolInput: map[string]any{
				"file_path": "/tmp/x.md",
				"edits": []any{
					map[string]any{"old_string": "a", "new_string": "b"},
					map[string]any{"old_string": "c", "new_string": "d"},
				},
			},
		},
		"unknown tool silent": {
			tool:      "Read",
			toolInput: map[string]any{"file_path": "/tmp/x.md"},
		},
		"missing content field silent": {
			tool:      "Write",
			toolInput: map[string]any{"file_path": "/nonexistent/hook-router-test/new.md"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var stdout bytes.Buffer

			err := handlePreFileWrite(preFileWriteInput(t, tc.tool, tc.toolInput), &stdout, cfg, logger)
			require.NoError(t, err)

			if tc.wantDeny == "" {
				assert.Empty(t, stdout.Bytes())
			} else {
				assert.Contains(t, decodeDeny(t, stdout.Bytes()), tc.wantDeny)
			}
		})
	}
}

func TestHandlePreFileWriteOnDisk(t *testing.T) {
	t.Parallel()

	cfg := config{enforceTypography: true}
	logger := slog.New(slog.DiscardHandler)

	t.Run("write preserving on-disk dash silent", func(t *testing.T) {
		t.Parallel()

		target := filepath.Join(t.TempDir(), "doc.md")
		require.NoError(t, os.WriteFile(target, []byte("a — b\n"), 0o644))

		input := preFileWriteInput(t, "Write", map[string]any{
			"file_path": target,
			"content":   "a — b\nmore\n",
		})

		var stdout bytes.Buffer

		require.NoError(t, handlePreFileWrite(input, &stdout, cfg, logger))
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("write adding beyond on-disk count denies", func(t *testing.T) {
		t.Parallel()

		target := filepath.Join(t.TempDir(), "doc.md")
		require.NoError(t, os.WriteFile(target, []byte("a — b\n"), 0o644))

		input := preFileWriteInput(t, "Write", map[string]any{
			"file_path": target,
			"content":   "a — b — c\n",
		})

		var stdout bytes.Buffer

		require.NoError(t, handlePreFileWrite(input, &stdout, cfg, logger))
		assert.Contains(t, decodeDeny(t, stdout.Bytes()), "U+2014")
	})

	t.Run("unreadable existing target skips check", func(t *testing.T) {
		t.Parallel()

		// A directory at file_path makes os.ReadFile return a
		// non-ErrNotExist error, exercising the skip path.
		target := t.TempDir()

		input := preFileWriteInput(t, "Write", map[string]any{
			"file_path": target,
			"content":   "a — b\n",
		})

		var stdout bytes.Buffer

		require.NoError(t, handlePreFileWrite(input, &stdout, cfg, logger))
		assert.Empty(t, stdout.Bytes())
	})
}

func TestHandlePreFileWriteDisabled(t *testing.T) {
	t.Parallel()

	cfg := config{enforceTypography: false}
	logger := slog.New(slog.DiscardHandler)

	input := preFileWriteInput(t, "Write", map[string]any{
		"file_path": "/nonexistent/hook-router-test/new.md",
		"content":   "a — b",
	})

	var stdout bytes.Buffer

	require.NoError(t, handlePreFileWrite(input, &stdout, cfg, logger))
	assert.Empty(t, stdout.Bytes())
}

func TestHandlePreFileWriteMalformedInput(t *testing.T) {
	t.Parallel()

	cfg := config{enforceTypography: true}
	logger := slog.New(slog.DiscardHandler)

	var stdout bytes.Buffer

	require.NoError(t, handlePreFileWrite([]byte("not json"), &stdout, cfg, logger))
	assert.Empty(t, stdout.Bytes())
}
