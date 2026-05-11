package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventNeedsStore(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		event string
		tool  string
		input string
		want  bool
	}{
		"Stop always needs store":             {event: "Stop", want: true},
		"SessionStart always needs store":     {event: "SessionStart", want: true},
		"UserPromptSubmit always needs store": {event: "UserPromptSubmit", want: true},
		"PreToolUse ExitPlanMode":             {event: "PreToolUse", tool: "ExitPlanMode", want: true},
		"PreToolUse EnterPlanMode":            {event: "PreToolUse", tool: "EnterPlanMode", want: true},
		"PreToolUse Bash skips store":         {event: "PreToolUse", tool: "Bash", want: false},
		"PreToolUse unknown skips store":      {event: "PreToolUse", tool: "Read", want: false},
		"PostToolUse AskUserQuestion via --tool": {
			event: "PostToolUse", tool: "AskUserQuestion", want: true,
		},
		"PostToolUse AskUserQuestion via stdin fallback": {
			event: "PostToolUse",
			input: `{"tool_name":"AskUserQuestion"}`,
			want:  true,
		},
		"PostToolUse Write skips store": {
			event: "PostToolUse",
			input: `{"tool_name":"Write"}`,
			want:  false,
		},
		"PostToolUse Read (default no-op) skips store": {
			event: "PostToolUse",
			input: `{"tool_name":"Read"}`,
			want:  false,
		},
		"PostToolUse malformed stdin skips store": {
			event: "PostToolUse",
			input: `not json`,
			want:  false,
		},
		"unknown event skips store": {event: "Foo", want: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := eventNeedsStore(tc.event, tc.tool, []byte(tc.input))
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	cfg := config{commandRules: canonicalRules(), claudePID: testPID}
	logger := slog.New(slog.DiscardHandler)

	makeInput := func(toolInput map[string]any) string {
		hook := map[string]any{"tool_input": toolInput}
		b, err := json.Marshal(hook)
		require.NoError(t, err)

		return string(b)
	}

	t.Run("backward compat: no event flag", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git pull origin main",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "", "", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PreToolUse Bash: non-matching command", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git pull origin main",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Bash", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PreToolUse Bash: invalid JSON", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader("not json"), &stdout, "", "", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PreToolUse Bash: missing tool_input", func(t *testing.T) {
		t.Parallel()

		input, err := json.Marshal(map[string]any{"other": "field"})
		require.NoError(t, err)

		var stdout bytes.Buffer

		err = run(t.Context(), strings.NewReader(string(input)), &stdout, "PreToolUse", "Bash", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PreToolUse Bash: missing command key", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"description": "no command here",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Bash", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PreToolUse Bash: empty command string", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Bash", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PreToolUse Bash: denied git stash", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git stash",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Bash", nil, cfg, logger)
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

	t.Run("PreToolUse Bash: denied kubectl without kubeconfig", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "kubectl get pods",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Bash", nil, cfg, logger)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "mcp__kubectx__select")
	})

	t.Run("PreToolUse Bash: rewrite kubectl with kubeconfig", func(t *testing.T) {
		t.Parallel()

		kubeconfigCfg := config{
			kubeconfigPath: "/tmp/claude-kubectx/12345/kubeconfig",
			commandRules:   canonicalRules(),
		}

		input := makeInput(map[string]any{
			"command": "kubectl get pods",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Bash", nil, kubeconfigCfg, logger)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)

		assert.Equal(t, "PreToolUse", hso["hookEventName"],
			"Claude Code rejects hookSpecificOutput without hookEventName")

		updated, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "KUBECONFIG=/tmp/claude-kubectx/12345/kubeconfig kubectl get pods", updated["command"])
	})

	t.Run("PreToolUse Bash: autoAllow flows through run() to handleBash", func(t *testing.T) {
		t.Parallel()

		autoCfg := config{
			commandRules: canonicalRules(),
			autoAllow:    true,
		}

		input := makeInput(map[string]any{
			"command": "echo $USER",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Bash", nil, autoCfg, logger)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Equal(t, "sandbox auto-allow", hso["permissionDecisionReason"])
	})

	t.Run("PreToolUse Bash: denied kubectx", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "kubectx my-context",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Bash", nil, cfg, logger)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "kubectx")
	})

	t.Run("PreToolUse Bash: denied git stash with git clone", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git stash && git clone URL dest",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Bash", nil, cfg, logger)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

	t.Run("PreToolUse ExitPlanMode: no store is noop", func(t *testing.T) {
		t.Parallel()

		input := `{"session_id":"test","tool_input":{}}`

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "ExitPlanMode", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PreToolUse EnterPlanMode: no store is noop", func(t *testing.T) {
		t.Parallel()

		input := `{"session_id":"test"}`

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "EnterPlanMode", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PostToolUse unknown tool is noop", func(t *testing.T) {
		t.Parallel()

		input := `{"session_id":"test","tool_input":{}}`

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PostToolUse", "ExitPlanMode", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PostToolUse AskUserQuestion: no store is noop", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"questions": []any{
				map[string]any{
					"options": []any{
						map[string]any{"label": "/review-implementation"},
					},
				},
			},
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PostToolUse", "AskUserQuestion", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PostToolUse stdin fallback routes AskUserQuestion when --tool empty", func(t *testing.T) {
		t.Parallel()

		input, err := json.Marshal(map[string]any{
			"session_id": "",
			"tool_name":  "AskUserQuestion",
			"tool_input": map[string]any{"questions": []any{}},
		})
		require.NoError(t, err)

		var stdout bytes.Buffer

		// No store: handler returns nil early. The point is that
		// dispatch reaches handlePostAskUserQuestion via the stdin
		// fallback, not the legacy --tool flag.
		err = run(t.Context(), strings.NewReader(string(input)), &stdout, "PostToolUse", "", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PostToolUse stdin fallback routes Write/Edit/MultiEdit through formatter", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		target := filepath.Join(dir, "doc.md")
		const before = "# t\n\n\n\nbar\n"

		rule := FormatterRule{
			PathGlob: filepath.Join(dir, "*.md"),
			Command:  []string{"sh", "-c", `tr -s '\n' < "$1" > "$1.tmp" && mv "$1.tmp" "$1"`, "sh"},
		}

		formatCfg := config{
			commandRules:   canonicalRules(),
			formatterRules: NewFormatterRules([]FormatterRule{rule}),
		}

		for _, toolName := range []string{"Write", "Edit", "MultiEdit"} {
			t.Run(toolName, func(t *testing.T) {
				require.NoError(t, os.WriteFile(target, []byte(before), 0o644))

				input, err := json.Marshal(map[string]any{
					"tool_name":  toolName,
					"tool_input": map[string]any{"file_path": target},
				})
				require.NoError(t, err)

				var stdout bytes.Buffer

				err = run(t.Context(), strings.NewReader(string(input)), &stdout, "PostToolUse", "", nil, formatCfg, logger)
				require.NoError(t, err)
				assert.Empty(t, stdout.Bytes())

				got, err := os.ReadFile(target)
				require.NoError(t, err)
				assert.NotContains(t, string(got), "\n\n\n",
					"%s with --tool empty should reach handlePostFileWrite via stdin fallback", toolName)
			})
		}
	})

	t.Run("Stop: no store is noop", func(t *testing.T) {
		t.Parallel()

		input := `{"session_id":"test"}`

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "Stop", "", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("SessionStart: no store is noop", func(t *testing.T) {
		t.Parallel()

		input := `{"session_id":"new","cwd":"/tmp/x","source":"clear"}`

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "SessionStart", "", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("SessionStart routes to handler and migrates pending plan", func(t *testing.T) {
		t.Parallel()

		store := newTestStore(t)
		ctx := t.Context()

		cwd := t.TempDir()

		resolved, err := filepath.EvalSymlinks(cwd)
		require.NoError(t, err)

		_, err = store.SetPendingPlan(ctx, resolved, testPID, "/plan.md", "sha1")
		require.NoError(t, err)

		input := fmt.Sprintf(`{"session_id":"new","cwd":%q,"source":"clear"}`, cwd)

		var stdout bytes.Buffer

		err = run(ctx, strings.NewReader(input), &stdout, "SessionStart", "", store, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())

		_, planPath, baseSHA, err := store.Session(ctx, "new")
		require.NoError(t, err)
		assert.Equal(t, "/plan.md", planPath)
		assert.Equal(t, "sha1", baseSHA)
	})

	t.Run("UserPromptSubmit: no store is noop", func(t *testing.T) {
		t.Parallel()

		input := `{"session_id":"test","prompt":"/commit"}`

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "UserPromptSubmit", "", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("UserPromptSubmit /commit prompt routes through handler", func(t *testing.T) {
		t.Parallel()

		store := newTestStore(t)
		ctx := t.Context()

		require.NoError(t, store.SetPlanPath(ctx, "s1", "/plan.md", "sha1"))

		routedCfg := config{
			postImpl:     testCatalog(),
			commitSkills: []string{"commit", "commit-push-pr", "merge"},
			commandRules: canonicalRules(),
		}

		input := `{"session_id":"s1","prompt":"/commit"}`

		var stdout bytes.Buffer

		err := run(ctx, strings.NewReader(input), &stdout, "UserPromptSubmit", "", store, routedCfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())

		_, planPath, _, err := store.Session(ctx, "s1")
		require.NoError(t, err)
		assert.Equal(t, "", planPath, "session must be cleared after /commit")
	})

	t.Run("PreToolUse unknown tool is noop", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{"foo": "bar"})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Agent", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("unknown event falls back to Bash handler", func(t *testing.T) {
		t.Parallel()

		input := makeInput(map[string]any{
			"command": "git stash",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "Unknown", "", nil, cfg, logger)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})
}
