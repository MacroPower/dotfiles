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

	"go.jacobcolvin.com/dotfiles/tools/hook-router/archive"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/cmdrules"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/compact"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/searchrewrite"
)

// TestHandleBashAutoAllow exercises the --auto-allow paths in
// [handleBash].
func TestHandleBashAutoAllow(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	hookInput := func(t *testing.T, command string) []byte {
		t.Helper()

		b, err := json.Marshal(map[string]any{
			"tool_input": map[string]any{"command": command},
		})
		require.NoError(t, err)

		return b
	}

	t.Run("autoAllow=false, simple command: stdout empty", func(t *testing.T) {
		t.Parallel()

		// Without auto-allow, the fall-through emits nothing and the
		// command proceeds through Claude Code's normal permission flow.
		cfg := config{
			commandRules: canonicalRules(),
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "echo $USER"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("autoAllow=true, simple command: emits auto-allow JSON", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules: canonicalRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "echo $USER"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Equal(t, "sandbox auto-allow", hso["permissionDecisionReason"])
	})

	t.Run("autoAllow=true, deny match: deny precedence holds", func(t *testing.T) {
		t.Parallel()

		// Sanity-check that auto-allow does not weaken existing denies.
		cfg := config{
			commandRules: canonicalRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "git stash"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

	t.Run("autoAllow=true, ask match: ask emitted instead of auto-allow", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules: ghAskRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "gh pr merge 1"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "ask", hso["permissionDecision"])
		assert.Equal(t, ghGroupAskReason, hso["permissionDecisionReason"])
	})

	t.Run("autoAllow=false, ask match: ask emitted", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules: ghAskRules(),
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "gh api /user"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "ask", hso["permissionDecision"])
		assert.Equal(t, ghFallbackAskReason, hso["permissionDecisionReason"])
	})

	t.Run("autoAllow=true, ask-exempt command: falls through to auto-allow", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules: ghRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "gh pr checks 1"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Equal(t, "sandbox auto-allow", hso["permissionDecisionReason"])
	})

	t.Run("autoAllow=true, redirect match: deny emitted instead of auto-allow", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules: ghRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "gh pr view 1"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Equal(t, ghRedirectReason("mcp__github__pull_request_read"), hso["permissionDecisionReason"])
	})

	t.Run("autoAllow=true, kubectl with kubeconfig: allow without updatedInput", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules:   canonicalRules(),
			kubeconfigPath: "/tmp/claude-kubectx.12345/kubeconfig",
			autoAllow:      true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "kubectl get pods"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Equal(t, "sandbox auto-allow (kubectl)", hso["permissionDecisionReason"])

		_, hasUpdated := hso["updatedInput"]
		assert.False(t, hasUpdated,
			"kubectl handler must not rewrite the command; KUBECONFIG comes from the process env")
	})

	t.Run("autoAllow=false, kubectl with kubeconfig: no output (normal permission flow)", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules:   canonicalRules(),
			kubeconfigPath: "/tmp/claude-kubectx.12345/kubeconfig",
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "kubectl get pods"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes(),
			"without auto-allow, kubectl handler falls through to the normal permission flow")
	})

	t.Run("autoAllow=true, kubectl no kubeconfig: deny only, no allow merge", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules: canonicalRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "kubectl get pods"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "mcp__kubectx__select")
		assert.Contains(t, hso["permissionDecisionReason"], "No kubeconfig selected")
	})

	t.Run("autoAllow=true, kubectl with kubeconfig override: denies", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules:   canonicalRules(),
			kubeconfigPath: "/tmp/claude-kubectx.12345/kubeconfig",
			autoAllow:      true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "kubectl --kubeconfig /etc/other get pods"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "overrides the session kubeconfig")
	})

	t.Run("autoAllow=true, inline KUBECONFIG override: denies", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules:   canonicalRules(),
			kubeconfigPath: "/tmp/claude-kubectx.12345/kubeconfig",
			autoAllow:      true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "KUBECONFIG=/etc/other kubectl get pods"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "overrides the session kubeconfig")
	})

	t.Run("autoAllow=true, no kubeconfig, override command: no-kubeconfig deny wins", func(t *testing.T) {
		t.Parallel()

		// With no context selected, the no-kubeconfig deny fires first and
		// the override check is never reached.
		cfg := config{
			commandRules: canonicalRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "kubectl --kubeconfig /etc/other get pods"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "No kubeconfig selected")
	})

	t.Run("autoAllow=true, missing command: bypasses auto-allow", func(t *testing.T) {
		t.Parallel()

		// Valid JSON without a tool_input.command takes the early-out
		// path: nothing is written, so the call proceeds through Claude
		// Code's normal permission flow instead of being auto-allowed.
		cfg := config{
			commandRules: canonicalRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash([]byte(`{"tool_input":{}}`), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("autoAllow=true, malformed JSON: bypasses auto-allow", func(t *testing.T) {
		t.Parallel()

		// Malformed JSON takes the early-out path instead of branching
		// into auto-allow: nothing is written, so the command proceeds
		// through Claude Code's normal permission flow.
		cfg := config{
			commandRules: canonicalRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash([]byte("not json"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes(),
			"malformed JSON must hit the early-out, not the auto-allow encoder")
	})
}

// TestHandleBashSearchRewrite covers the search-rewrite branch in
// [handleBash]: read-only rewrites emit allow + updatedInput, non-read-only
// commands are left untouched, and deny rules still win.
func TestHandleBashSearchRewrite(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	searchCfg := searchrewrite.Config{
		Grep:         true,
		Find:         true,
		FindExcludes: []string{".git", ".worktrees", ".claude/worktrees"},
	}

	// inputWith builds a Bash payload carrying extra tool_input fields so
	// the updatedInput carry-over can be asserted.
	inputWith := func(t *testing.T, command string, extra map[string]any) []byte {
		t.Helper()

		ti := map[string]any{"command": command}
		for k, v := range extra {
			ti[k] = v
		}

		b, err := json.Marshal(map[string]any{"tool_input": ti})
		require.NoError(t, err)

		return b
	}

	t.Run("read-only find: allow with rewritten updatedInput", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules:  canonicalRules(),
			searchRewrite: searchCfg,
		}

		var stdout bytes.Buffer

		input := inputWith(t, `find . -name "*.go"`, map[string]any{
			"description": "find go files",
			"timeout":     float64(5000),
		})

		err := handleBash(input, &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "allow", hso["permissionDecision"])

		updated, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok, "missing updatedInput")
		assert.Equal(t,
			`bfs -exclude \( -name .git -o -name .worktrees -o -path '*.claude/worktrees' \) . -name "*.go"`,
			updated["command"],
		)
		// Sibling tool_input fields must carry over: updatedInput replaces
		// the entire input object.
		assert.Equal(t, "find go files", updated["description"])
		assert.Equal(t, float64(5000), updated["timeout"])
	})

	t.Run("read-only grep: allow with rewritten updatedInput", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules:  canonicalRules(),
			searchRewrite: searchCfg,
		}

		var stdout bytes.Buffer

		err := handleBash([]byte(`{"tool_input":{"command":"grep -rn foo ."}}`), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "allow", hso["permissionDecision"])

		updated, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok, "missing updatedInput")
		assert.Equal(t,
			`rg -n foo . -g '!.git' -g '!.worktrees' -g '!.claude/worktrees' -g '!**/.claude/worktrees'`,
			updated["command"],
		)
	})

	t.Run("non-read-only find -delete: no rewrite emitted", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules:  canonicalRules(),
			searchRewrite: searchCfg,
		}

		var stdout bytes.Buffer

		err := handleBash([]byte(`{"tool_input":{"command":"find . -delete"}}`), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes(),
			"a non-read-only command must fall through unrewritten")
	})

	t.Run("non-read-only redirection: no rewrite emitted", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules:  canonicalRules(),
			searchRewrite: searchCfg,
		}

		var stdout bytes.Buffer

		err := handleBash([]byte(`{"tool_input":{"command":"grep -rn foo . > out"}}`), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("deny rule beats a rewritable command", func(t *testing.T) {
		t.Parallel()

		// A deny rule on a search command must win over the rewrite: the
		// rewrite runs after commandRules.Check, so deny precedence holds.
		cfg := config{
			commandRules: cmdrules.New([]cmdrules.Rule{
				{Command: "find", Reason: "no find allowed"},
			}),
			searchRewrite: searchCfg,
		}

		var stdout bytes.Buffer

		err := handleBash([]byte(`{"tool_input":{"command":"find . -name x"}}`), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

	t.Run("autoAllow=true + read-only search: allow with updatedInput", func(t *testing.T) {
		t.Parallel()

		// The rewrite runs before the autoAllow fall-through, so a
		// read-only search still gets its rewritten updatedInput rather
		// than a plain auto-allow.
		cfg := config{
			commandRules:  canonicalRules(),
			searchRewrite: searchCfg,
			autoAllow:     true,
		}

		var stdout bytes.Buffer

		err := handleBash([]byte(`{"tool_input":{"command":"find . -name x"}}`), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "allow", hso["permissionDecision"])

		updated, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok, "read-only search must rewrite even under autoAllow")
		assert.Contains(t, updated["command"], "bfs -exclude")
	})

	t.Run("non-read-only under autoAllow: plain auto-allow, no updatedInput", func(t *testing.T) {
		t.Parallel()

		// A non-read-only command is left unrewritten and falls through to
		// the existing autoAllow branch, so no prompt-behavior regression.
		cfg := config{
			commandRules:  canonicalRules(),
			searchRewrite: searchCfg,
			autoAllow:     true,
		}

		var stdout bytes.Buffer

		err := handleBash([]byte(`{"tool_input":{"command":"find . -delete"}}`), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Equal(t, "sandbox auto-allow", hso["permissionDecisionReason"])

		_, hasUpdated := hso["updatedInput"]
		assert.False(t, hasUpdated, "non-read-only command must not carry updatedInput")
	})
}

// postBashPayload builds a PostToolUse:Bash JSON payload with the
// given tool_input.command and tool_response keys. Pass response=nil
// to omit tool_response entirely (which exercises the missing-response
// no-op path).
func postBashPayload(t *testing.T, command string, response map[string]any) []byte {
	t.Helper()

	hook := map[string]any{
		"session_id":      "s1",
		"hook_event_name": "PostToolUse",
		"tool_name":       "Bash",
		"transcript_path": "/tmp/t.jsonl",
		"cwd":             "/tmp",
		"tool_input":      map[string]any{"command": command},
	}
	if response != nil {
		hook["tool_response"] = response
	}

	b, err := json.Marshal(hook)
	require.NoError(t, err)

	return b
}

// TestHandlePostBash covers the failure-detection precedence and
// type-coercion guards documented on [handlePostBash]. Each case
// either records exactly one row or leaves the table empty;
// per-row field assertions live in dedicated tests below.
func TestHandlePostBash(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	tests := map[string]struct {
		input    []byte
		wantRows int
	}{
		"exit_code=0 no-op": {
			input: postBashPayload(t, "true", map[string]any{
				"exit_code": float64(0),
			}),
			wantRows: 0,
		},
		"exit_code=1 insert": {
			input: postBashPayload(t, "false", map[string]any{
				"exit_code": float64(1),
			}),
			wantRows: 1,
		},
		"interrupted=true insert (exit_code absent)": {
			input: postBashPayload(t, "sleep 999", map[string]any{
				"interrupted": true,
			}),
			wantRows: 1,
		},
		"is_error=true insert (exit_code absent)": {
			input: postBashPayload(t, "weird", map[string]any{
				"is_error": true,
			}),
			wantRows: 1,
		},
		"is_error=false with exit_code=0 no-op even with stderr noise": {
			input: postBashPayload(t, "kubectl get pods", map[string]any{
				"exit_code": float64(0),
				"is_error":  false,
				"stderr":    strings.Repeat("warning: x\n", 20),
			}),
			wantRows: 0,
		},
		"missing tool_response no-op": {
			input:    postBashPayload(t, "true", nil),
			wantRows: 0,
		},
		"malformed JSON no-op (warn only)": {
			input:    []byte("not json"),
			wantRows: 0,
		},
		"exit_code as JSON string is treated as absent (no-op)": {
			input: postBashPayload(t, "false", map[string]any{
				"exit_code": "1",
			}),
			wantRows: 0,
		},
		"empty command no-op": {
			input: postBashPayload(t, "", map[string]any{
				"exit_code": float64(1),
			}),
			wantRows: 0,
		},
		"is_error wins over exit_code=0": {
			input: postBashPayload(t, "weird", map[string]any{
				"exit_code": float64(0),
				"is_error":  true,
			}),
			wantRows: 1,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newTestStore(t)

			require.NoError(t, handlePostBash(t.Context(), tt.input, store, logger))

			var count int

			require.NoError(t, store.DB().QueryRowContext(t.Context(),
				`SELECT COUNT(*) FROM bash_failures`).Scan(&count))
			assert.Equal(t, tt.wantRows, count)
		})
	}
}

// TestHandlePostBash_RecordsFailureFields checks that an inserted row
// carries the structured signals (is_error, interrupted, exit_code)
// alongside the transcript and event metadata, so later analysis has
// enough to replay.
func TestHandlePostBash_RecordsFailureFields(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	input := postBashPayload(t, "false", map[string]any{
		"exit_code":   float64(2),
		"is_error":    true,
		"interrupted": false,
		"stdout":      "out",
		"stderr":      "err",
	})

	require.NoError(t, handlePostBash(t.Context(), input, store, logger))

	var (
		session, transcript, event, cwd, command, stdout, stderr string
		isError, interrupted                                     int
		exitCode                                                 int
	)

	err := store.DB().QueryRowContext(t.Context(), `
		SELECT session_id, transcript_path, hook_event_name, cwd, command,
		       stdout, stderr, is_error, interrupted, exit_code
		FROM bash_failures`).Scan(
		&session, &transcript, &event, &cwd, &command,
		&stdout, &stderr, &isError, &interrupted, &exitCode,
	)
	require.NoError(t, err)
	assert.Equal(t, "s1", session)
	assert.Equal(t, "/tmp/t.jsonl", transcript)
	assert.Equal(t, "PostToolUse", event)
	assert.Equal(t, "/tmp", cwd)
	assert.Equal(t, "false", command)
	assert.Equal(t, "out", stdout)
	assert.Equal(t, "err", stderr)
	assert.Equal(t, 1, isError)
	assert.Equal(t, 0, interrupted)
	assert.Equal(t, 2, exitCode)
}

// TestHandlePostBash_Truncation pins the stdout/stderr capture limits:
// stderr keeps the last 16 KiB, stdout keeps a 2 KiB head and 2 KiB
// tail joined by the sentinel.
func TestHandlePostBash_Truncation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	// stderr: 20 KiB so the head is dropped, last 16 KiB kept.
	const stderrLen = 20 * 1024

	stderr := strings.Repeat("E", stderrLen)

	// stdout: 20 KiB so head+tail truncation fires.
	const stdoutLen = 20 * 1024

	stdout := strings.Repeat("H", bashStdoutHeadBytes) +
		strings.Repeat("M", stdoutLen-bashStdoutHeadBytes-bashStdoutTailBytes) +
		strings.Repeat("T", bashStdoutTailBytes)

	input := postBashPayload(t, "noisy", map[string]any{
		"exit_code": float64(1),
		"stdout":    stdout,
		"stderr":    stderr,
	})

	require.NoError(t, handlePostBash(t.Context(), input, store, logger))

	var gotStdout, gotStderr string

	require.NoError(t, store.DB().QueryRowContext(t.Context(),
		`SELECT stdout, stderr FROM bash_failures`).Scan(&gotStdout, &gotStderr))

	assert.Len(t, gotStderr, bashStderrTailBytes, "stderr must be tail-truncated to 16 KiB")
	assert.Equal(t, strings.Repeat("E", bashStderrTailBytes), gotStderr,
		"tail bytes must be preserved verbatim")

	assert.Contains(t, gotStdout, bashTruncSentinel, "stdout must carry the truncation sentinel")
	assert.True(t, strings.HasPrefix(gotStdout, strings.Repeat("H", bashStdoutHeadBytes)),
		"stdout head 2 KiB must be preserved")
	assert.True(t, strings.HasSuffix(gotStdout, strings.Repeat("T", bashStdoutTailBytes)),
		"stdout tail 2 KiB must be preserved")
}

// decodeUpdatedOutput extracts the updatedToolOutput map from a
// PostToolUse compaction response written to buf. ok is false when buf
// is empty (no decision emitted).
func decodeUpdatedOutput(t *testing.T, buf []byte) (map[string]any, bool) {
	t.Helper()

	if len(buf) == 0 {
		return nil, false
	}

	var result map[string]any

	require.NoError(t, json.Unmarshal(buf, &result))

	hso, ok := result["hookSpecificOutput"].(map[string]any)
	require.True(t, ok, "missing hookSpecificOutput")
	assert.Equal(t, "PostToolUse", hso["hookEventName"])

	updated, ok := hso["updatedToolOutput"].(map[string]any)
	require.True(t, ok, "missing updatedToolOutput")

	return updated, true
}

func TestHandlePostBashCompact(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	// enabledCompactor clears the size gate (MinBytes 1) so the small
	// test fixtures exercise the transforms rather than the byte gate. The
	// variadic streams pick which tool_response fields are eligible.
	enabledCompactor := func(streams ...string) config {
		return config{compactor: compact.New(compact.Config{
			Enable:       true,
			StripAnsi:    true,
			MinRunLength: 3,
			MinBytes:     1,
			Streams:      streams,
		})}
	}

	// enabledArchiveCompactor is enabledCompactor wired to archive into
	// dir, so a stream that compacts is written out and pointed at rather
	// than lossily collapsed. Each case passes its own t.TempDir() so the
	// files it writes can be inspected in isolation.
	enabledArchiveCompactor := func(dir string, streams ...string) config {
		cfg := enabledCompactor(streams...)
		cfg.outputArchive = archive.New(dir)

		return cfg
	}

	const (
		wideStdout = "a-wide-repeated-stdout-line"
		wideStderr = "a-wide-repeated-stderr-line"
	)

	t.Run("collapses stdout and preserves sibling fields", func(t *testing.T) {
		t.Parallel()

		in := strings.Repeat(wideStdout+"\n", 50)
		input := postBashPayload(t, "noisy", map[string]any{
			"stdout":      in,
			"stderr":      "",
			"interrupted": false,
			"isImage":     false,
			"exit_code":   float64(0),
		})

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, enabledCompactor("stdout", "stderr"), logger))

		updated, ok := decodeUpdatedOutput(t, stdout.Bytes())
		require.True(t, ok, "a collapsible stdout must emit updatedToolOutput")

		gotStdout, ok := updated["stdout"].(string)
		require.True(t, ok)
		assert.Less(t, len(gotStdout), len(in), "stdout must be shorter")
		assert.Contains(t, gotStdout, compact.Marker(49))

		// Sibling fields survive the shallow copy.
		assert.Equal(t, false, updated["interrupted"])
		assert.Equal(t, false, updated["isImage"])
		assert.Equal(t, float64(0), updated["exit_code"])
	})

	t.Run("disabled compactor emits nothing", func(t *testing.T) {
		t.Parallel()

		input := postBashPayload(t, "noisy", map[string]any{
			"stdout": strings.Repeat(wideStdout+"\n", 50),
		})

		cfg := config{compactor: compact.New(compact.Config{})}

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, cfg, logger))
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("nil compactor emits nothing", func(t *testing.T) {
		t.Parallel()

		input := postBashPayload(t, "noisy", map[string]any{
			"stdout": strings.Repeat(wideStdout+"\n", 50),
		})

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, config{}, logger))
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("nothing repeats emits nothing", func(t *testing.T) {
		t.Parallel()

		input := postBashPayload(t, "ls", map[string]any{
			"stdout": "line one\nline two\nline three\n",
		})

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, enabledCompactor("stdout", "stderr"), logger))
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("below minBytes emits nothing", func(t *testing.T) {
		t.Parallel()

		// Default MinBytes (2048); the repeating fixture is far smaller.
		cfg := config{compactor: compact.New(compact.Config{
			Enable:    true,
			StripAnsi: true,
			Streams:   []string{"stdout", "stderr"},
		})}

		input := postBashPayload(t, "noisy", map[string]any{
			"stdout": strings.Repeat("x\n", 10),
		})

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, cfg, logger))
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("missing tool_response is a no-op", func(t *testing.T) {
		t.Parallel()

		input := postBashPayload(t, "noisy", nil)

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, enabledCompactor("stdout", "stderr"), logger))
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("malformed JSON warns and emits nothing", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		warnLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		var stdout bytes.Buffer

		cfg := enabledCompactor("stdout", "stderr")
		require.NoError(t, handlePostBashCompact([]byte("not json"), &stdout, cfg, warnLogger))
		assert.Empty(t, stdout.Bytes())
		assert.Contains(t, strings.ToLower(buf.String()), "parse hook input")
	})

	t.Run("clean stdout preserved verbatim while stderr collapses", func(t *testing.T) {
		t.Parallel()

		cleanStdout := "the one and only stdout line\n"
		dirtyStderr := strings.Repeat(wideStderr+"\n", 50)

		input := postBashPayload(t, "noisy", map[string]any{
			"stdout": cleanStdout,
			"stderr": dirtyStderr,
		})

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, enabledCompactor("stdout", "stderr"), logger))

		updated, ok := decodeUpdatedOutput(t, stdout.Bytes())
		require.True(t, ok, "a collapsible stderr must emit updatedToolOutput")

		assert.Equal(t, cleanStdout, updated["stdout"], "untouched stdout must be preserved verbatim")

		gotStderr, ok := updated["stderr"].(string)
		require.True(t, ok)
		assert.Less(t, len(gotStderr), len(dirtyStderr), "stderr must be shorter")
		assert.Contains(t, gotStderr, compact.Marker(49))
	})

	t.Run("clean stderr preserved verbatim while stdout collapses", func(t *testing.T) {
		t.Parallel()

		dirtyStdout := strings.Repeat(wideStdout+"\n", 50)
		cleanStderr := "the one and only stderr line\n"

		input := postBashPayload(t, "noisy", map[string]any{
			"stdout": dirtyStdout,
			"stderr": cleanStderr,
		})

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, enabledCompactor("stdout", "stderr"), logger))

		updated, ok := decodeUpdatedOutput(t, stdout.Bytes())
		require.True(t, ok)

		assert.Equal(t, cleanStderr, updated["stderr"], "untouched stderr must be preserved verbatim")

		gotStdout, ok := updated["stdout"].(string)
		require.True(t, ok)
		assert.Less(t, len(gotStdout), len(dirtyStdout))
		assert.Contains(t, gotStdout, compact.Marker(49))
	})

	t.Run("stderr omitted from streams leaves a collapsible stderr alone", func(t *testing.T) {
		t.Parallel()

		input := postBashPayload(t, "noisy", map[string]any{
			"stdout": "the one and only stdout line\n",
			"stderr": strings.Repeat(wideStderr+"\n", 50),
		})

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, enabledCompactor("stdout"), logger))
		assert.Empty(t, stdout.Bytes(),
			"with stderr out of streams and clean stdout, no updatedToolOutput is emitted")
	})

	t.Run("archive enabled: stdout gets a pointer and the file holds the raw original", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		raw := strings.Repeat(wideStdout+"\n", 50)
		input := postBashPayload(t, "noisy", map[string]any{"stdout": raw})

		var stdout bytes.Buffer

		cfg := enabledArchiveCompactor(dir, "stdout", "stderr")
		require.NoError(t, handlePostBashCompact(input, &stdout, cfg, logger))

		updated, ok := decodeUpdatedOutput(t, stdout.Bytes())
		require.True(t, ok, "a compressible+archivable stdout must emit updatedToolOutput")

		gotStdout, ok := updated["stdout"].(string)
		require.True(t, ok)
		assert.Less(t, len(gotStdout), len(raw), "annotated stdout must be shorter than raw")
		assert.Contains(t, gotStdout, "[hook-router: uncompacted stdout saved to ")
		assert.True(t, strings.HasSuffix(gotStdout, fmt.Sprintf("(%d bytes)]", len(raw))),
			"annotated stdout must end with the pointer line reporting the raw byte count")

		// The named file holds the raw original verbatim.
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, entries, 1)

		archived, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
		require.NoError(t, err)
		assert.Equal(t, raw, string(archived), "the archived file must hold the raw original stream")
	})

	t.Run("archive save failure reverts to the full original, warns, emits no response", func(t *testing.T) {
		t.Parallel()

		// The archive dir lives under a regular file, so MkdirAll fails
		// for every stream. With no stream able to archive, each reverts
		// to its full original and nothing changes, so no updatedToolOutput
		// is emitted -- Claude keeps the unmodified original rather than a
		// lossy compaction with no recovery path.
		base := t.TempDir()
		notDir := filepath.Join(base, "f")
		require.NoError(t, os.WriteFile(notDir, []byte("x"), 0o644))

		raw := strings.Repeat(wideStdout+"\n", 50)
		input := postBashPayload(t, "noisy", map[string]any{"stdout": raw})

		cfg := enabledArchiveCompactor(filepath.Join(notDir, "outputs"), "stdout", "stderr")

		var buf bytes.Buffer

		warnLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, cfg, warnLogger))

		assert.Empty(t, stdout.Bytes(), "a pure archive failure reverts every stream, so nothing is emitted")
		assert.Contains(t, strings.ToLower(buf.String()), "compaction output dir", "the save failure must warn")

		// The blocking regular file is untouched: MkdirAll never created
		// an outputs tree under it.
		info, err := os.Stat(notDir)
		require.NoError(t, err)
		assert.False(t, info.IsDir(), "the blocking regular file must remain a regular file")
	})

	t.Run("pointer would not net-shorten: revert to raw, no file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		// 5 identical lines compact (saving ~74 bytes) but the pointer
		// marker is longer than the saving, so the gate declines: the
		// stream reverts to raw and writes no file. With no other stream,
		// nothing changes, so no response is emitted.
		raw := strings.Repeat(wideStdout+"\n", 5)
		input := postBashPayload(t, "noisy", map[string]any{"stdout": raw})

		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, enabledArchiveCompactor(dir, "stdout"), logger))

		assert.Empty(t, stdout.Bytes(), "a gate-miss reverts to raw; with no other change, nothing is emitted")

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Empty(t, entries, "a gate-miss must not write a file")
	})

	t.Run("both streams: stdout pointers while stderr reverts, independent outcomes", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		// stdout is large -> compacts and archives (pointer). stderr is
		// small -> compacts but its pointer would not net-shorten, so it
		// reverts to its full original. The response still emits because
		// stdout changed.
		rawStdout := strings.Repeat(wideStdout+"\n", 50)
		rawStderr := strings.Repeat(wideStderr+"\n", 5)
		input := postBashPayload(t, "noisy", map[string]any{
			"stdout": rawStdout,
			"stderr": rawStderr,
		})

		var stdout bytes.Buffer

		cfg := enabledArchiveCompactor(dir, "stdout", "stderr")
		require.NoError(t, handlePostBashCompact(input, &stdout, cfg, logger))

		updated, ok := decodeUpdatedOutput(t, stdout.Bytes())
		require.True(t, ok, "a changed stdout must emit updatedToolOutput even when stderr reverts")

		gotStdout, ok := updated["stdout"].(string)
		require.True(t, ok)
		assert.Contains(t, gotStdout, "[hook-router: uncompacted stdout saved to ")

		// The reverted stream keeps its full original, with no pointer.
		gotStderr, ok := updated["stderr"].(string)
		require.True(t, ok)
		assert.Equal(t, rawStderr, gotStderr, "the reverted stderr must keep its full original")
		assert.NotContains(t, gotStderr, "uncompacted", "the reverted stream must carry no pointer")

		// Only stdout's file is written.
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Len(t, entries, 1, "only the archived stream writes a file")

		// Per-stream outcomes never grow the surfaced output.
		assert.LessOrEqual(t, len(gotStdout)+len(gotStderr), len(rawStdout)+len(rawStderr),
			"the combined surfaced output must not exceed the original")
	})

	t.Run("disabled archive: byte-identical lossy compaction, no pointer", func(t *testing.T) {
		t.Parallel()

		raw := strings.Repeat(wideStdout+"\n", 50)
		input := postBashPayload(t, "noisy", map[string]any{"stdout": raw})

		// enabledCompactor leaves outputArchive nil (archiving off).
		var stdout bytes.Buffer
		require.NoError(t, handlePostBashCompact(input, &stdout, enabledCompactor("stdout", "stderr"), logger))

		updated, ok := decodeUpdatedOutput(t, stdout.Bytes())
		require.True(t, ok)

		got, ok := updated["stdout"].(string)
		require.True(t, ok)
		assert.NotContains(t, got, "uncompacted", "a disabled archive must not append a pointer")

		// Byte-identical to the pure compactor transform.
		want, did := compact.New(compact.Config{
			Enable:       true,
			StripAnsi:    true,
			MinRunLength: 3,
			MinBytes:     1,
			Streams:      []string{"stdout"},
		}).Compact(raw)
		require.True(t, did)
		assert.Equal(t, want, got, "disabled-archive output must equal the pure compactor output")
	})
}

func TestTruncateTail(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   string
		n    int
		want string
	}{
		"short string unchanged":  {in: "hello", n: 16, want: "hello"},
		"exact length unchanged":  {in: "abcd", n: 4, want: "abcd"},
		"keeps last n bytes":      {in: "abcdef", n: 3, want: "def"},
		"multi-byte rune aligned": {in: "abcédef", n: 4, want: "def"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, truncateTail(tt.in, tt.n))
		})
	}
}

func TestTruncateHeadTail(t *testing.T) {
	t.Parallel()

	t.Run("under threshold returns input verbatim", func(t *testing.T) {
		t.Parallel()

		in := strings.Repeat("x", 100)
		assert.Equal(t, in, truncateHeadTail(in, 50, 50))
	})

	t.Run("over threshold splits head + sentinel + tail", func(t *testing.T) {
		t.Parallel()

		in := strings.Repeat("H", 50) + strings.Repeat("M", 1000) + strings.Repeat("T", 50)
		out := truncateHeadTail(in, 50, 50)
		assert.Contains(t, out, bashTruncSentinel)
		assert.True(t, strings.HasPrefix(out, strings.Repeat("H", 50)))
		assert.True(t, strings.HasSuffix(out, strings.Repeat("T", 50)))
		assert.NotContains(t, out, "M", "middle must be dropped")
	})
}
