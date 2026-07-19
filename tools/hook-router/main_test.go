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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/archive"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/formatter"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/mcprules"
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
		"PreToolUse MCP skips store":          {event: "PreToolUse", tool: "MCP", want: false},
		"PreToolUse FileWrite skips store":    {event: "PreToolUse", tool: "FileWrite", want: false},
		"PreToolUse unknown skips store":      {event: "PreToolUse", tool: "Read", want: false},
		"PostToolUse AskUserQuestion via --tool": {
			event: "PostToolUse", tool: "AskUserQuestion", want: true,
		},
		"PostToolUse AskUserQuestion via stdin fallback": {
			event: "PostToolUse",
			input: `{"tool_name":"AskUserQuestion"}`,
			want:  true,
		},
		"PostToolUse Bash via --tool needs store": {
			event: "PostToolUse", tool: "Bash", want: true,
		},
		"PostToolUse Bash via stdin fallback needs store": {
			event: "PostToolUse",
			input: `{"tool_name":"Bash"}`,
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
		assert.Contains(t, hso["permissionDecisionReason"], "No kubeconfig selected")
	})

	t.Run("PreToolUse Bash: kubectl with kubeconfig: no rewrite, no output", func(t *testing.T) {
		t.Parallel()

		// With KUBECONFIG inherited from the launcher wrapper, the
		// kubectl subprocess uses the right kubeconfig without any
		// hook-router rewrite. Without auto-allow, hook-router emits
		// no JSON and Claude Code's normal permission flow handles
		// the kubectl invocation.
		kubeconfigCfg := config{
			kubeconfigPath: "/tmp/claude-kubectx.12345/kubeconfig",
			commandRules:   canonicalRules(),
		}

		input := makeInput(map[string]any{
			"command": "kubectl get pods",
		})

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "Bash", nil, kubeconfigCfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
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

	t.Run("PreToolUse MCP: allow match flows through run() to handleMCP", func(t *testing.T) {
		t.Parallel()

		mcpCfg := config{
			mcpRules: mcprules.New([]string{"mcp__github__search_code"}, nil, nil),
		}

		input := `{"tool_name":"mcp__github__search_code"}`

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "MCP", nil, mcpCfg, logger)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "mcp__github__search_code")
	})

	t.Run("PreToolUse MCP: unmatched tool emits nothing", func(t *testing.T) {
		t.Parallel()

		mcpCfg := config{
			mcpRules: mcprules.New([]string{"mcp__github__search_code"}, nil, nil),
		}

		input := `{"tool_name":"mcp__leanspec__view"}`

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "MCP", nil, mcpCfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("PreToolUse FileWrite: introduced dash flows through run() to deny", func(t *testing.T) {
		t.Parallel()

		fwCfg := config{enforceTypography: true}

		input := `{"tool_name":"Write","tool_input":{"file_path":"/nonexistent/hook-router-test/new.md","content":"a — b"}}`

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "PreToolUse", "FileWrite", nil, fwCfg, logger)
		require.NoError(t, err)

		var result map[string]any

		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "U+2014")
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

		in := strings.NewReader(input)
		err := run(t.Context(), in, &stdout, "PostToolUse", "AskUserQuestion", nil, cfg, logger)
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

		rule := formatter.Rule{
			PathGlob: filepath.Join(dir, "*.md"),
			Command:  []string{"sh", "-c", `tr -s '\n' < "$1" > "$1.tmp" && mv "$1.tmp" "$1"`, "sh"},
		}

		formatCfg := config{
			commandRules:   canonicalRules(),
			formatterRules: formatter.New([]formatter.Rule{rule}),
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

				in := strings.NewReader(string(input))
				err = run(t.Context(), in, &stdout, "PostToolUse", "", nil, formatCfg, logger)
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

		_, err := store.SetPendingPlan(ctx, testPID, "/plan.md", "sha1")
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

	t.Run("SessionStart sweeps stale compaction outputs", func(t *testing.T) {
		t.Parallel()

		outDir := t.TempDir()

		oldFile := filepath.Join(outDir, "s1-stdout-deadbeefdeadbeef.log")
		require.NoError(t, os.WriteFile(oldFile, []byte("old"), 0o644))

		freshFile := filepath.Join(outDir, "s2-stdout-cafef00dcafef00d.log")
		require.NoError(t, os.WriteFile(freshFile, []byte("fresh"), 0o644))

		// Age the old file past the TTL deterministically (no sleeps).
		past := time.Now().Add(-(archive.DefaultTTL + time.Hour))
		require.NoError(t, os.Chtimes(oldFile, past, past))

		sweepCfg := config{
			commandRules:  canonicalRules(),
			claudePID:     testPID,
			outputArchive: archive.New(outDir),
		}

		input := `{"session_id":"new","cwd":"/tmp/x","source":"clear"}`

		var stdout bytes.Buffer

		// store==nil: the sweep runs before the store gate, so the
		// SessionStart arm still exercises it.
		err := run(t.Context(), strings.NewReader(input), &stdout, "SessionStart", "", nil, sweepCfg, logger)
		require.NoError(t, err)

		_, statErr := os.Stat(oldFile)
		assert.True(t, os.IsNotExist(statErr), "SessionStart must sweep outputs older than the TTL")

		_, statErr = os.Stat(freshFile)
		assert.NoError(t, statErr, "SessionStart must keep fresh outputs")
	})

	t.Run("SessionStart with no archive dir does not sweep", func(t *testing.T) {
		t.Parallel()

		// cfg.outputArchive is nil, so Dir() == "" and the sweep is a
		// nil-safe no-op (it must not panic or error).
		input := `{"session_id":"new","cwd":"/tmp/x","source":"clear"}`

		var stdout bytes.Buffer

		err := run(t.Context(), strings.NewReader(input), &stdout, "SessionStart", "", nil, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
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

// writeLocalKubeconfig writes a local.yaml with the given
// current-context and locally-defined context names, returning its
// path. An empty current produces the bare-stub shape.
func writeLocalKubeconfig(t *testing.T, current string, contexts ...string) string {
	t.Helper()

	var b strings.Builder

	b.WriteString("apiVersion: v1\nkind: Config\n")

	if current != "" {
		fmt.Fprintf(&b, "current-context: %s\n", current)
	}

	if len(contexts) > 0 {
		b.WriteString("contexts:\n")

		for _, name := range contexts {
			fmt.Fprintf(&b, "- name: %s\n  context:\n    cluster: %s\n    user: %s\n", name, name, name)
		}
	}

	path := filepath.Join(t.TempDir(), "local.yaml")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))

	return path
}

func TestConfigFromEnv(t *testing.T) { //nolint:tparallel,paralleltest // subtests use t.Setenv
	t.Run("CLAUDE_KUBECTX_LOCAL unset: kubeconfigPath empty", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

		assert.Empty(t, configFromEnv().kubeconfigPath)
	})

	t.Run("bare stub (empty current-context): denies", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_LOCAL", writeLocalKubeconfig(t, ""))
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

		assert.Empty(t, configFromEnv().kubeconfigPath,
			"the bare stub has no current-context, so no context is selected")
	})

	t.Run("current-context names a local context: selected", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		local := writeLocalKubeconfig(t, "kind-dev", "kind-dev")
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

		assert.Equal(t, local, configFromEnv().kubeconfigPath,
			"a local context with inline creds counts as selected")
	})

	t.Run("external current-context with live sidecar: selected", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		local := writeLocalKubeconfig(t, "prod")
		sidecar := filepath.Join(t.TempDir(), "kubeconfig")
		require.NoError(t, os.Symlink("/scoped/kubeconfig.yaml", sidecar))

		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", sidecar)

		assert.Equal(t, local, configFromEnv().kubeconfigPath,
			"an external selection with a published sidecar counts as selected")
	})

	t.Run("use-context external never MCP-selected: denies", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv. current-context names a
		// context that is neither local nor backed by a sidecar: the
		// gate must still emit the actionable select-first deny.
		t.Setenv("CLAUDE_KUBECTX_LOCAL", writeLocalKubeconfig(t, "prod"))
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", filepath.Join(t.TempDir(), "no-such-sidecar"))

		assert.Empty(t, configFromEnv().kubeconfigPath,
			"current-context set with no usable creds must still deny")
	})
}
