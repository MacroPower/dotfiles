package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"
)

func mustParse(t *testing.T, command string) *syntax.File {
	t.Helper()

	prog, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	require.NoError(t, err)

	return prog
}

func TestCheckGitStashDenied(t *testing.T) {
	t.Parallel()

	const stashDenied = "Do not use git stash to shelve changes. All issues in the working tree are your responsibility to fix, regardless of origin."

	tests := map[string]struct {
		input string
		want  string
	}{
		"bare git stash": {
			input: "git stash",
			want:  stashDenied,
		},
		"git stash push": {
			input: "git stash push",
			want:  stashDenied,
		},
		"git stash push with path": {
			input: "git stash push -- file.go",
			want:  stashDenied,
		},
		"git stash save": {
			input: `git stash save "wip"`,
			want:  stashDenied,
		},
		"git stash -k": {
			input: "git stash -k",
			want:  stashDenied,
		},
		"git stash --keep-index": {
			input: "git stash --keep-index",
			want:  stashDenied,
		},
		"git stash in pipeline": {
			input: "git stash || echo fail",
			want:  stashDenied,
		},
		"git stash in subshell": {
			input: "(git stash)",
			want:  stashDenied,
		},
		"no match: git stash pop": {
			input: "git stash pop",
		},
		"no match: git stash apply": {
			input: "git stash apply stash@{0}",
		},
		"no match: git stash list": {
			input: "git stash list",
		},
		"no match: git stash show": {
			input: "git stash show -p",
		},
		"no match: git stash drop": {
			input: "git stash drop stash@{1}",
		},
		"no match: git stash branch": {
			input: "git stash branch newbranch",
		},
		"no match: git stash clear": {
			input: "git stash clear",
		},
		"no match: git status": {
			input: "git status",
		},
		"no match: echo git stash": {
			input: "echo git stash",
		},
		"no match: sh -c git stash": {
			input: `sh -c "git stash"`,
		},
		"no match: git help stash": {
			input: "git help stash",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			got, denied := checkGitStashDenied(prog)
			assert.Equal(t, tt.want != "", denied)

			if denied {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	cfg := config{}

	makeInput := func(toolInput map[string]any) string {
		hook := map[string]any{"tool_input": toolInput}
		b, err := json.Marshal(hook)
		require.NoError(t, err)

		return string(b)
	}

	t.Run("non-matching command", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git pull origin main",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("invalid JSON", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		err := run(strings.NewReader("not json"), &stdout, cfg)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("missing tool_input", func(t *testing.T) {
		t.Parallel()

		input, err := json.Marshal(map[string]any{"other": "field"})
		require.NoError(t, err)

		var stdout bytes.Buffer

		err = run(strings.NewReader(string(input)), &stdout, cfg)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("missing command key", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"description": "no command here",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("empty command string", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("denied git stash", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git stash",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "git stash")
	})

	t.Run("denied git stash with git clone", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git stash && git clone URL dest",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

}
