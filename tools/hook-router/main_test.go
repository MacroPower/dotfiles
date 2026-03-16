package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewriteClones(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"simple clone": {
			input: "git clone URL dest",
			want:  "git-idempotent clone URL dest",
		},
		"compound &&": {
			input: "cd /tmp/git && git clone URL dest",
			want:  "cd /tmp/git && git-idempotent clone URL dest",
		},
		"empty prefix &&": {
			input: `echo "" && git clone URL dest`,
			want:  `echo "" && git-idempotent clone URL dest`,
		},
		"semicolon chain": {
			input: "echo hi; git clone URL dest",
			want:  "echo hi\ngit-idempotent clone URL dest",
		},
		"subshell": {
			input: "(git clone URL dest)",
			want:  "(git-idempotent clone URL dest)",
		},
		"nested subshell": {
			input: "(cd /tmp && git clone URL dest)",
			want:  "(cd /tmp && git-idempotent clone URL dest)",
		},
		"multiple clones": {
			input: "git clone A B && git clone C D",
			want:  "git-idempotent clone A B && git-idempotent clone C D",
		},
		"clone with flags": {
			input: "git clone --depth 1 URL dest",
			want:  "git-idempotent clone --depth 1 URL dest",
		},
		"no match: git pull": {
			input: "git pull origin main",
		},
		"no match: plain cmd": {
			input: "ls -la",
		},
		"no match: empty": {
			input: "",
		},
		"git only, no subcommand": {
			input: "git",
		},
		"or list": {
			input: "git clone URL dest || echo fail",
			want:  "git-idempotent clone URL dest || echo fail",
		},
		"if block": {
			input: "if true; then git clone URL dest; fi",
			want:  "if true; then git-idempotent clone URL dest; fi",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, rewrote, err := rewriteClones(tt.input, "git-idempotent")
			require.NoError(t, err)
			assert.Equal(t, tt.want != "", rewrote)

			if rewrote {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	cfg := config{
		gitIdempotent: "git-idempotent",
		rtkRewrite:    "",
	}

	makeInput := func(toolInput map[string]any) string {
		hook := map[string]any{"tool_input": toolInput}
		b, err := json.Marshal(hook)
		require.NoError(t, err)

		return string(b)
	}

	t.Run("matching command", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git clone URL dest",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "allow", hso["permissionDecision"])

		updatedInput, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "git-idempotent clone URL dest", updatedInput["command"])
	})

	t.Run("extra fields preserved", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command":     "git clone URL dest",
			"description": "cloning repo",
		})

		var stdout bytes.Buffer

		err := run(strings.NewReader(input), &stdout, cfg)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)

		updatedInput, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "cloning repo", updatedInput["description"])
	})

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
}
