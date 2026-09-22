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
	"go.jacobcolvin.com/dotfiles/tools/hook-router/linter"
)

// lintLines is a linter script that reports every line of its input,
// so a handler test can check which lines survive the touched-lines
// filter. Exits 1 with one `path:N:1: Style::Test: <text>` finding per
// line.
var lintLines = []string{"sh", "-c", `n=0; while IFS= read -r line; do n=$((n+1)); printf '%s:%d:1: Style::Test: %s\n' "$1" "$n" "$line"; done < "$1"; exit 1`, "sh"}

func TestHandlePostFileWriteLint(t *testing.T) {
	t.Parallel()

	const content = "one\ntwo\nthree\n"

	cases := map[string]struct {
		toolName  string
		toolInput map[string]any
		wantLines []string
	}{
		"Write reports every finding": {
			toolName:  "Write",
			toolInput: map[string]any{"content": content},
			wantLines: []string{":1:1:", ":2:1:", ":3:1:"},
		},
		"Edit reports only the lines it wrote": {
			toolName:  "Edit",
			toolInput: map[string]any{"old_string": "2", "new_string": "two"},
			wantLines: []string{":2:1:"},
		},
		"Edit spanning lines reports the span": {
			toolName:  "Edit",
			toolInput: map[string]any{"old_string": "x", "new_string": "two\nthree"},
			wantLines: []string{":2:1:", ":3:1:"},
		},
		"Edit with an empty new_string reports nothing": {
			toolName:  "Edit",
			toolInput: map[string]any{"old_string": "gone", "new_string": ""},
			wantLines: nil,
		},
		"Edit whose new_string is missing from the file reports every finding": {
			toolName:  "Edit",
			toolInput: map[string]any{"old_string": "x", "new_string": "rewritten by the formatter"},
			wantLines: []string{":1:1:", ":2:1:", ":3:1:"},
		},
		"MultiEdit unions its edits": {
			toolName: "MultiEdit",
			toolInput: map[string]any{
				"edits": []any{
					map[string]any{"old_string": "a", "new_string": "one"},
					map[string]any{"old_string": "b", "new_string": "three"},
				},
			},
			wantLines: []string{":1:1:", ":3:1:"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tmp := t.TempDir()
			target := filepath.Join(tmp, "doc.md")
			require.NoError(t, os.WriteFile(target, []byte(content), 0o644))

			rule := linter.Rule{PathGlob: filepath.Join(tmp, "*.md"), Command: lintLines}
			cfg := config{formatterRules: formatter.New(nil), linterRules: linter.New([]linter.Rule{rule})}
			logger := slog.New(slog.DiscardHandler)

			toolInput := map[string]any{"file_path": target}
			for k, v := range tc.toolInput {
				toolInput[k] = v
			}

			input, err := json.Marshal(map[string]any{"tool_name": tc.toolName, "tool_input": toolInput})
			require.NoError(t, err)

			err = handlePostFileWrite(t.Context(), input, cfg, logger)
			if tc.wantLines == nil {
				require.NoError(t, err)
				return
			}

			var blocked *blockError
			require.ErrorAs(t, err, &blocked)
			assert.Contains(t, blocked.Reason, "prose-lint: ")
			assert.Contains(t, blocked.Reason, target)

			for _, want := range tc.wantLines {
				assert.Contains(t, blocked.Reason, target+want)
			}

			assert.Equal(t, len(tc.wantLines), strings.Count(blocked.Reason, "Style::Test"),
				"only the touched lines should be reported")
		})
	}
}

func TestHandlePostFileWriteFormatsBeforeLinting(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	require.NoError(t, os.WriteFile(target, []byte("# t\n\n\n\nbar\n"), 0o644))

	collapse := formatter.Rule{
		PathGlob: filepath.Join(tmp, "*.md"),
		Command:  []string{"sh", "-c", `tr -s '\n' < "$1" > "$1.tmp" && mv "$1.tmp" "$1"`, "sh"},
	}
	countLines := linter.Rule{
		PathGlob: filepath.Join(tmp, "*.md"),
		Command:  []string{"sh", "-c", `printf '%s:%s:1: Style::Test: lines\n' "$1" "$(wc -l < "$1" | tr -d ' ')"; exit 1`, "sh"},
	}

	cfg := config{
		formatterRules: formatter.New([]formatter.Rule{collapse}),
		linterRules:    linter.New([]linter.Rule{countLines}),
	}
	logger := slog.New(slog.DiscardHandler)

	input, err := json.Marshal(map[string]any{
		"tool_name":  "Write",
		"tool_input": map[string]any{"file_path": target},
	})
	require.NoError(t, err)

	err = handlePostFileWrite(t.Context(), input, cfg, logger)

	var blocked *blockError
	require.ErrorAs(t, err, &blocked)
	assert.Contains(t, blocked.Reason, target+":2:1:", "the linter should see the formatted two-line file")
}

func TestHandlePostFileWriteLinterFailureSwallowed(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	require.NoError(t, os.WriteFile(target, []byte("# t\n"), 0o644))

	rule := linter.Rule{
		PathGlob: filepath.Join(tmp, "*.md"),
		Command:  []string{"sh", "-c", "echo crashed >&2; exit 2", "sh"},
	}

	cfg := config{formatterRules: formatter.New(nil), linterRules: linter.New([]linter.Rule{rule})}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	input, err := json.Marshal(map[string]any{
		"tool_name":  "Write",
		"tool_input": map[string]any{"file_path": target},
	})
	require.NoError(t, err)

	err = handlePostFileWrite(t.Context(), input, cfg, logger)
	require.NoError(t, err, "a linter crash must not block the write")
	assert.Contains(t, buf.String(), "linter run failed")
	assert.Contains(t, buf.String(), "crashed")
}

func TestHandlePostFileWriteLinterNoMatch(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.yaml")
	require.NoError(t, os.WriteFile(target, []byte("a: b\n"), 0o644))

	rule := linter.Rule{PathGlob: filepath.Join(tmp, "*.md"), Command: lintLines}
	cfg := config{formatterRules: formatter.New(nil), linterRules: linter.New([]linter.Rule{rule})}
	logger := slog.New(slog.DiscardHandler)

	input, err := json.Marshal(map[string]any{
		"tool_name":  "Write",
		"tool_input": map[string]any{"file_path": target},
	})
	require.NoError(t, err)

	assert.NoError(t, handlePostFileWrite(t.Context(), input, cfg, logger))
}

func TestHandlePostBashEditsNeverLints(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	target := filepath.Join(tmp, "doc.md")
	require.NoError(t, os.WriteFile(target, []byte("one\n"), 0o644))

	rule := linter.Rule{PathGlob: filepath.Join(tmp, "*.md"), Command: lintLines}
	cfg := config{formatterRules: formatter.New(nil), linterRules: linter.New([]linter.Rule{rule})}
	logger := slog.New(slog.DiscardHandler)

	input, err := json.Marshal(map[string]any{
		"tool_name":     "Bash",
		"tool_input":    map[string]any{"command": "sed -i s/a/b/ doc.md"},
		"tool_response": map[string]any{"bashEditDiff": map[string]any{"changedFiles": []any{target}}},
	})
	require.NoError(t, err)

	assert.NoError(t, handlePostBashEdits(t.Context(), input, cfg, logger),
		"Bash edits are formatter-only; a block here would discard the Bash output")
}

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

func TestHandlePostBashEdits(t *testing.T) {
	t.Parallel()

	const before = "# t\n\n\n\nbar\n"

	// Collapses newline runs; formatted files lose the "\n\n\n" run.
	collapse := []string{"sh", "-c", `tr -s '\n' < "$1" > "$1.tmp" && mv "$1.tmp" "$1"`, "sh"}

	tests := map[string]struct {
		// files maps a file name to whether the formatter must have
		// touched it. Every file is created under a temp dir with the
		// same starting content; the rule glob covers *.md only.
		files map[string]bool
		// response builds tool_response from the resolved paths.
		response func(paths map[string]string) map[string]any
	}{
		"formats matching files and skips the rest": {
			files: map[string]bool{"a.md": true, "b.md": true, "c.txt": false},
			response: func(paths map[string]string) map[string]any {
				return map[string]any{
					"bashEditDiff": map[string]any{
						"changedFiles": []any{paths["a.md"], paths["b.md"], paths["c.txt"]},
					},
				}
			},
		},
		"ignores non-string entries": {
			files: map[string]bool{"a.md": true},
			response: func(paths map[string]string) map[string]any {
				return map[string]any{
					"bashEditDiff": map[string]any{
						"changedFiles": []any{42, "", nil, paths["a.md"]},
					},
				}
			},
		},
		"no bashEditDiff field": {
			files: map[string]bool{"a.md": false},
			response: func(map[string]string) map[string]any {
				return map[string]any{"stdout": "ok"}
			},
		},
		"bashEditDiff without changedFiles": {
			files: map[string]bool{"a.md": false},
			response: func(map[string]string) map[string]any {
				return map[string]any{
					"bashEditDiff": map[string]any{"unavailable": true},
				}
			},
		},
		"changedFiles wrong type": {
			files: map[string]bool{"a.md": false},
			response: func(paths map[string]string) map[string]any {
				return map[string]any{
					"bashEditDiff": map[string]any{"changedFiles": paths["a.md"]},
				}
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tmp := t.TempDir()
			paths := make(map[string]string, len(tc.files))

			for file := range tc.files {
				p := filepath.Join(tmp, file)
				require.NoError(t, os.WriteFile(p, []byte(before), 0o644))
				paths[file] = p
			}

			rule := formatter.Rule{PathGlob: filepath.Join(tmp, "*.md"), Command: collapse}
			cfg := config{formatterRules: formatter.New([]formatter.Rule{rule})}
			logger := slog.New(slog.DiscardHandler)

			input, err := json.Marshal(map[string]any{
				"tool_name":     "Bash",
				"tool_input":    map[string]any{"command": "sed -i s/a/b/ *"},
				"tool_response": tc.response(paths),
			})
			require.NoError(t, err)

			require.NoError(t, handlePostBashEdits(t.Context(), input, cfg, logger))

			for file, want := range tc.files {
				got, err := os.ReadFile(paths[file])
				require.NoError(t, err)

				if want {
					assert.NotContains(t, string(got), "\n\n\n", file+" should be formatted")
				} else {
					assert.Equal(t, before, string(got), file+" should be untouched")
				}
			}
		})
	}
}

func TestHandlePostBashEditsEmptyEngine(t *testing.T) {
	t.Parallel()

	cfg := config{formatterRules: formatter.New(nil)}
	logger := slog.New(slog.DiscardHandler)

	input := []byte(`{"tool_name":"Bash","tool_response":{"bashEditDiff":{"changedFiles":["/tmp/x.md"]}}}`)

	assert.NoError(t, handlePostBashEdits(t.Context(), input, cfg, logger))
}

func TestHandlePostBashEditsNoToolResponse(t *testing.T) {
	t.Parallel()

	rule := formatter.Rule{PathGlob: "/tmp/*.md", Command: []string{"true"}}
	cfg := config{formatterRules: formatter.New([]formatter.Rule{rule})}
	logger := slog.New(slog.DiscardHandler)

	input := []byte(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`)

	assert.NoError(t, handlePostBashEdits(t.Context(), input, cfg, logger))
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
