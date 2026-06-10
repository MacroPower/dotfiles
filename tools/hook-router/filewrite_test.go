package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/formatter"
)

func TestHandlePostFileWriteTools(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	const before = "# t\n\n\n\nbar\n"

	rule := formatter.Rule{
		PathGlob: filepath.Join(tmp, "*.md"),
		// `tr -s '\n'` collapses runs of newlines, proving the formatter
		// actually ran without relying on mdformat being on PATH.
		Command: []string{"sh", "-c", `tr -s '\n' < "$1" > "$1.tmp" && mv "$1.tmp" "$1"`, "sh"},
	}

	cfg := config{formatterRules: formatter.New([]formatter.Rule{rule})}
	logger := slog.New(slog.DiscardHandler)

	for _, toolName := range []string{"Write", "Edit", "MultiEdit"} {
		t.Run(toolName, func(t *testing.T) {
			require.NoError(t, os.WriteFile(target, []byte(before), 0o644))

			input, err := json.Marshal(map[string]any{
				"tool_name":  toolName,
				"tool_input": map[string]any{"file_path": target},
			})
			require.NoError(t, err)

			err = handlePostFileWrite(t.Context(), input, cfg, logger)
			require.NoError(t, err)

			got, err := os.ReadFile(target)
			require.NoError(t, err)
			assert.NotContains(t, string(got), "\n\n\n", "blank-line run should be collapsed for "+toolName)
		})
	}
}

func TestHandlePostFileWriteToolsDoublestar(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	subDir := filepath.Join(tmp, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	target := filepath.Join(subDir, "doc.md")
	const before = "# t\n\n\n\nbar\n"

	rule := formatter.Rule{
		PathGlob: filepath.Join(tmp, "**/*.md"),
		Command:  []string{"sh", "-c", `tr -s '\n' < "$1" > "$1.tmp" && mv "$1.tmp" "$1"`, "sh"},
	}

	cfg := config{formatterRules: formatter.New([]formatter.Rule{rule})}
	logger := slog.New(slog.DiscardHandler)

	for _, toolName := range []string{"Write", "Edit", "MultiEdit"} {
		t.Run(toolName, func(t *testing.T) {
			require.NoError(t, os.WriteFile(target, []byte(before), 0o644))

			input, err := json.Marshal(map[string]any{
				"tool_name":  toolName,
				"tool_input": map[string]any{"file_path": target},
			})
			require.NoError(t, err)

			err = handlePostFileWrite(t.Context(), input, cfg, logger)
			require.NoError(t, err)

			got, err := os.ReadFile(target)
			require.NoError(t, err)
			assert.NotContains(t, string(got), "\n\n\n", "blank-line run should be collapsed for "+toolName)
		})
	}
}

func TestHandlePostFileWriteNoMatch(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	const before = "# t\n\n\n\nbar\n"
	require.NoError(t, os.WriteFile(target, []byte(before), 0o644))

	// Glob in an unrelated directory — rule must not fire.
	rule := formatter.Rule{
		PathGlob: "/var/empty/should-never-match/*.md",
		Command:  []string{"sh", "-c", `echo bad > "$1"`, "sh"},
	}

	cfg := config{formatterRules: formatter.New([]formatter.Rule{rule})}
	logger := slog.New(slog.DiscardHandler)

	input, err := json.Marshal(map[string]any{
		"tool_name":  "Write",
		"tool_input": map[string]any{"file_path": target},
	})
	require.NoError(t, err)

	err = handlePostFileWrite(t.Context(), input, cfg, logger)
	require.NoError(t, err)

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, before, string(got))
}

func TestHandlePostFileWriteEmptyEngine(t *testing.T) {
	t.Parallel()

	cfg := config{formatterRules: formatter.New(nil)}
	logger := slog.New(slog.DiscardHandler)

	input := []byte(`{"tool_name":"Write","tool_input":{"file_path":"/tmp/x.md"}}`)

	err := handlePostFileWrite(t.Context(), input, cfg, logger)
	assert.NoError(t, err)
}

func TestHandlePostFileWriteMissingFilePath(t *testing.T) {
	t.Parallel()

	rule := formatter.Rule{
		PathGlob: "/tmp/*.md",
		Command:  []string{"true"},
	}

	cfg := config{formatterRules: formatter.New([]formatter.Rule{rule})}
	logger := slog.New(slog.DiscardHandler)

	cases := map[string]string{
		"no tool_input":         `{"tool_name":"Write"}`,
		"tool_input has no key": `{"tool_name":"Write","tool_input":{}}`,
		"file_path empty":       `{"tool_name":"Write","tool_input":{"file_path":""}}`,
		"file_path wrong type":  `{"tool_name":"Write","tool_input":{"file_path":42}}`,
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := handlePostFileWrite(t.Context(), []byte(in), cfg, logger)
			assert.NoError(t, err)
		})
	}
}

func TestHandlePostFileWriteFormatterFailureSwallowed(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	require.NoError(t, os.WriteFile(target, []byte("# t\n"), 0o644))

	rule := formatter.Rule{
		PathGlob: filepath.Join(tmp, "*.md"),
		Command:  []string{"sh", "-c", "exit 7", "sh"},
	}

	cfg := config{formatterRules: formatter.New([]formatter.Rule{rule})}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	input, err := json.Marshal(map[string]any{
		"tool_name":  "Write",
		"tool_input": map[string]any{"file_path": target},
	})
	require.NoError(t, err)

	err = handlePostFileWrite(t.Context(), input, cfg, logger)
	require.NoError(t, err, "formatter exit codes must not propagate")
	assert.Contains(t, strings.ToLower(buf.String()), "formatter run failed",
		"non-zero exit should be logged at warn")
}
