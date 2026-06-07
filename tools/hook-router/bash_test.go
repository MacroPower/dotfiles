package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
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

func TestHasKubectl(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  bool
	}{
		"simple command": {
			input: "kubectl get pods",
			want:  true,
		},
		"bare kubectl": {
			input: "kubectl",
			want:  true,
		},
		"pipeline": {
			input: "kubectl get pods | grep foo",
			want:  true,
		},
		"subshell": {
			input: "(kubectl get ns)",
			want:  true,
		},
		"chained": {
			input: "kubectl get pods && kubectl get svc",
			want:  true,
		},
		"no match: already wrapped": {
			input: "kubectl-claude get pods",
		},
		"no match: echo": {
			input: "echo kubectl",
		},
		"no match: sh -c": {
			input: `sh -c "kubectl get pods"`,
		},
		"no match: unrelated": {
			input: "git status",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			assert.Equal(t, tt.want, hasKubectl(prog))
		})
	}
}

func TestKubectlKubeconfigOverride(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  bool
	}{
		"inline KUBECONFIG assignment": {
			input: "KUBECONFIG=/x kubectl get pods",
			want:  true,
		},
		"flag separate value": {
			input: "kubectl --kubeconfig /x get pods",
			want:  true,
		},
		"flag inline value": {
			input: "kubectl --kubeconfig=/x get pods",
			want:  true,
		},
		"flag in later position": {
			input: "kubectl get pods --kubeconfig /x",
			want:  true,
		},
		"inline KUBECONFIG expansion": {
			input: "KUBECONFIG=$OTHER kubectl get pods",
			want:  true,
		},
		"flag inline expansion value": {
			input: "kubectl --kubeconfig=$VAR get pods",
			want:  true,
		},
		"flag separate expansion value": {
			input: "kubectl --kubeconfig $VAR get pods",
			want:  true,
		},
		"no match: plain kubectl": {
			input: "kubectl get pods",
		},
		"no match: KUBECONFIG on non-kubectl": {
			input: "KUBECONFIG=/x helm list",
		},
		"no match: env wrapper out of scope": {
			input: "env -i kubectl get pods",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			prog := mustParse(t, tt.input)
			_, got := kubectlKubeconfigOverride(prog)
			assert.Equal(t, tt.want, got)
		})
	}
}

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
			commandRules: ghAskRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(hookInput(t, "gh pr view 1"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Equal(t, "sandbox auto-allow", hso["permissionDecisionReason"])
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

			require.NoError(t, store.db.QueryRowContext(t.Context(),
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

	err := store.db.QueryRowContext(t.Context(), `
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

	require.NoError(t, store.db.QueryRowContext(t.Context(),
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
