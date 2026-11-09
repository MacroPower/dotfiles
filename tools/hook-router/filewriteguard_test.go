package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/writeguard"
)

// errNoDirs stands in for a [writeguard.DirsFromEnv] failure.
var errNoDirs = errors.New("no dirs")

// guardFixture creates a home directory and a project for the write
// guard, and returns a config whose guard resolves against them.
func guardFixture(t *testing.T) (config, string, string) {
	t.Helper()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o755))
	require.NoError(t, os.MkdirAll(project, 0o755))

	dirs := writeguard.Dirs{
		Home:       home,
		ConfigDir:  filepath.Join(home, ".claude"),
		ProjectDir: project,
		TempDir:    filepath.Join(root, "tmp"),
	}

	cfg := config{
		writeGuardDirs: func(string) (writeguard.Dirs, error) { return dirs, nil },
	}

	return cfg, root, project
}

func TestHandleFileWritePre(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	guarded, root, project := guardFixture(t)
	outside := filepath.Join(root, "outside.md")

	broken, _, brokenProject := guardFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(brokenProject, ".claude"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(brokenProject, ".claude", "settings.json"), []byte(`{"sandbox":`), 0o644))

	failing := config{
		writeGuardDirs: func(string) (writeguard.Dirs, error) { return writeguard.Dirs{}, errNoDirs },
	}

	tcs := map[string]struct {
		cfg       config
		tool      string
		mode      string
		cwd       string
		toolInput map[string]any
		want      []string
	}{
		"guard off": {
			cfg:       config{},
			tool:      "Write",
			mode:      "plan",
			toolInput: map[string]any{"file_path": outside},
		},
		"default mode": {
			cfg:       guarded,
			tool:      "Write",
			mode:      "default",
			toolInput: map[string]any{"file_path": outside},
		},
		"acceptEdits mode": {
			cfg:       guarded,
			tool:      "Edit",
			mode:      "acceptEdits",
			toolInput: map[string]any{"file_path": outside},
		},
		"plan mode inside the project": {
			cfg:       guarded,
			tool:      "Write",
			mode:      "plan",
			toolInput: map[string]any{"file_path": filepath.Join(project, "main.go")},
		},
		"plan mode outside the sandbox": {
			cfg:       guarded,
			tool:      "Write",
			mode:      "plan",
			toolInput: map[string]any{"file_path": outside},
			want:      []string{outside, "outside every sandbox write path", "Confirm before writing."},
		},
		"bypass mode outside the sandbox": {
			cfg:       guarded,
			tool:      "MultiEdit",
			mode:      "bypassPermissions",
			toolInput: map[string]any{"file_path": outside},
			want:      []string{outside, "outside every sandbox write path"},
		},
		"protected path": {
			cfg:       guarded,
			tool:      "Edit",
			mode:      "bypassPermissions",
			toolInput: map[string]any{"file_path": filepath.Join(project, ".claude", "settings.json")},
			want:      []string{"protected path"},
		},
		"notebook path": {
			cfg:       guarded,
			tool:      "NotebookEdit",
			mode:      "plan",
			toolInput: map[string]any{"notebook_path": filepath.Join(root, "nb.ipynb")},
			want:      []string{filepath.Join(root, "nb.ipynb")},
		},
		"relative path inside the project": {
			cfg:       guarded,
			tool:      "Write",
			mode:      "plan",
			cwd:       project,
			toolInput: map[string]any{"file_path": "notes.md"},
		},
		"relative path that leaves the project": {
			cfg:       guarded,
			tool:      "Write",
			mode:      "plan",
			cwd:       project,
			toolInput: map[string]any{"file_path": "../outside.md"},
			want:      []string{outside},
		},
		"no path": {
			cfg:       guarded,
			tool:      "Write",
			mode:      "plan",
			toolInput: map[string]any{"content": "x"},
		},
		"directories do not resolve": {
			cfg:       failing,
			tool:      "Write",
			mode:      "plan",
			toolInput: map[string]any{"file_path": outside},
			want:      []string{"cannot check", errNoDirs.Error()},
		},
		"malformed settings file": {
			cfg:       broken,
			tool:      "Write",
			mode:      "plan",
			toolInput: map[string]any{"file_path": filepath.Join(brokenProject, "main.go")},
			want:      []string{"cannot check", writeguard.ErrSettings.Error()},
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			input, err := json.Marshal(map[string]any{
				"tool_name":       tc.tool,
				"permission_mode": tc.mode,
				"cwd":             tc.cwd,
				"tool_input":      tc.toolInput,
			})
			require.NoError(t, err)

			var stdout bytes.Buffer

			require.NoError(t, handleFileWritePre(input, &stdout, tc.cfg, logger))

			if tc.want == nil {
				assert.Empty(t, stdout.String())
				return
			}

			var result map[string]any
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

			hso, ok := result["hookSpecificOutput"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "PreToolUse", hso["hookEventName"])
			assert.Equal(t, "ask", hso["permissionDecision"])

			for _, want := range tc.want {
				assert.Contains(t, hso["permissionDecisionReason"], want)
			}
		})
	}

	t.Run("invalid JSON falls through", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		require.NoError(t, handleFileWritePre([]byte("not json"), &stdout, guarded, logger))
		assert.Empty(t, stdout.String())
	})
}
