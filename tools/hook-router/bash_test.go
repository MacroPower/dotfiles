package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
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

// writeRTKScript writes a fake rtk-rewrite.sh shell script and returns
// its absolute path. Call this from a single goroutine (typically
// before [*testing.T.Parallel] subtests fire) so the file is fully
// written and closed before any parallel goroutine execs it.
// Concurrent write+exec across goroutines triggers Linux ETXTBSY,
// see golang/go#22220.
func writeRTKScript(t *testing.T, name, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, name)

	err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755)
	require.NoError(t, err)

	return path
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

// TestHandleBashAutoAllow exercises the --auto-allow paths in
// [handleBash]. RTK fakes are written serially in the parent test
// before any parallel subtest runs, so each script file is closed
// before exec. This avoids the Linux ETXTBSY write+exec race
// (golang/go#22220).
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

	// All RTK fakes used by parallel subtests below. Written once,
	// from this single goroutine, before any t.Parallel() fires.
	rtkEmpty := writeRTKScript(t, "rtk-empty.sh", "exit 0")
	rtkAllowJSON := writeRTKScript(t, "rtk-allow.sh", `cat <<'EOF'
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{"command":"rtk-rewritten"}}}
EOF`)
	rtkAllowWithReason := writeRTKScript(t, "rtk-allow-reason.sh", `cat <<'EOF'
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":"RTK auto-rewrite","updatedInput":{"command":"rtk-rewritten"}}}
EOF`)
	rtkRewriteOnly := writeRTKScript(t, "rtk-rewrite-only.sh", `cat <<'EOF'
{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"rtk ls /tmp"}}}
EOF`)
	rtkInvalidJSON := writeRTKScript(t, "rtk-invalid.sh", `printf 'not json\n'`)
	rtkMissingHSO := writeRTKScript(t, "rtk-missing-hso.sh", `printf '%s\n' '{"unrelated":"shape"}'`)
	rtkExit5 := writeRTKScript(t, "rtk-exit5.sh", "exit 5")
	rtkPartialFail := writeRTKScript(t, "rtk-partial.sh", `printf '%s\n' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"partial"}}}'
exit 7`)

	t.Run("autoAllow=false, simple command, RTK empty: stdout empty", func(t *testing.T) {
		t.Parallel()

		// RTK exit 0 + empty stdout is its "no rewrite" signal. Without
		// auto-allow, forward nothing.
		cfg := config{
			commandRules: canonicalRules(),
			rtkRewrite:   rtkEmpty,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "echo $USER"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("autoAllow=true, simple command, RTK empty: emits auto-allow JSON", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules: canonicalRules(),
			rtkRewrite:   rtkEmpty,
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "echo $USER"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Equal(t, "sandbox auto-allow", hso["permissionDecisionReason"])
	})

	t.Run("autoAllow=true, RTK provides own decision: preserved (bytes may be re-encoded)", func(t *testing.T) {
		t.Parallel()

		// Field order is not guaranteed across the merge path, so this
		// test decodes JSON rather than asserting raw byte equality.
		cfg := config{
			commandRules: canonicalRules(),
			rtkRewrite:   rtkAllowJSON,
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "echo $USER"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "allow", hso["permissionDecision"])

		updated, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "rtk-rewritten", updated["command"])
		// Reason field is absent because RTK's response did not include
		// one. The merge helper must not synthesize a reason on this
		// path.
		_, hasReason := hso["permissionDecisionReason"]
		assert.False(t, hasReason, "decision-present short-circuit must not add fields")
	})

	t.Run("autoAllow=true, RTK rewrite without decision: allow merged in", func(t *testing.T) {
		t.Parallel()

		// Real RTK exit-3 ask path emits updatedInput without a
		// permissionDecision. Under --auto-allow, hook-router must
		// attach allow so Claude Code does not re-prompt on the
		// rewritten command.
		cfg := config{
			commandRules: canonicalRules(),
			rtkRewrite:   rtkRewriteOnly,
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "ls /tmp"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"])
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Equal(t, "sandbox auto-allow (rtk rewrite)", hso["permissionDecisionReason"])

		updated, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "rtk ls /tmp", updated["command"], "RTK rewrite must be preserved")
	})

	t.Run("autoAllow=true, RTK exit-0 with reason: not overwritten", func(t *testing.T) {
		t.Parallel()

		// Real RTK exit-0 path includes both permissionDecision: allow
		// AND permissionDecisionReason: "RTK auto-rewrite". The
		// decision-present short-circuit must preserve RTK's reason
		// rather than restamping it with the sandbox auto-allow reason.
		cfg := config{
			commandRules: canonicalRules(),
			rtkRewrite:   rtkAllowWithReason,
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "echo hi"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Equal(t, "RTK auto-rewrite", hso["permissionDecisionReason"],
			"RTK's own reason must not be overwritten")
	})

	t.Run("autoAllow=true, RTK invalid JSON: forwarded verbatim, no error", func(t *testing.T) {
		t.Parallel()

		// Malformed RTK output is RTK's bug, not ours to rewrite. The
		// merge helper returns an error; the caller logs at warn and
		// forwards the captured bytes so Claude Code surfaces whatever
		// is actually broken.
		cfg := config{
			commandRules: canonicalRules(),
			rtkRewrite:   rtkInvalidJSON,
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "echo hi"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Equal(t, "not json\n", stdout.String())
	})

	t.Run("autoAllow=true, RTK valid JSON missing hookSpecificOutput: forwarded verbatim, no error", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules: canonicalRules(),
			rtkRewrite:   rtkMissingHSO,
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "echo hi"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Equal(t, "{\"unrelated\":\"shape\"}\n", stdout.String())
	})

	t.Run("autoAllow=true, deny match: deny precedence holds", func(t *testing.T) {
		t.Parallel()

		// No rtkRewrite, so the deny check fires before delegate.
		// Sanity-check that auto-allow does not weaken existing denies.
		cfg := config{
			commandRules: canonicalRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "git stash"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
	})

	t.Run("autoAllow=true, kubectl rewrite preserves hookEventName + adds allow", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules:   canonicalRules(),
			kubeconfigPath: "/tmp/claude-kubectx/12345/kubeconfig",
			autoAllow:      true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "kubectl get pods"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "PreToolUse", hso["hookEventName"],
			"merge must not drop hookEventName")
		assert.Equal(t, "allow", hso["permissionDecision"])
		assert.Equal(t, "sandbox auto-allow (kubectl rewrite)", hso["permissionDecisionReason"])

		updated, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "KUBECONFIG=/tmp/claude-kubectx/12345/kubeconfig kubectl get pods", updated["command"])
	})

	t.Run("autoAllow=true, kubectl no kubeconfig: deny only, no allow merge", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules: canonicalRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "kubectl get pods"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "deny", hso["permissionDecision"])
		assert.Contains(t, hso["permissionDecisionReason"], "mcp__kubectx__select")
	})

	t.Run("autoAllow=true, malformed JSON: bypasses auto-allow", func(t *testing.T) {
		t.Parallel()

		// Malformed JSON keeps the existing delegate path instead of
		// branching into auto-allow. delegate() streams to os.Stdout,
		// so the stdout io.Writer argument stays empty. That proves
		// handleBash did NOT take the auto-allow JSON encoder path,
		// which writes to the argument.
		cfg := config{
			commandRules: canonicalRules(),
			rtkRewrite:   rtkEmpty,
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), []byte("not json"), &stdout, cfg, logger)
		require.NoError(t, err)
		assert.Empty(t, stdout.Bytes(),
			"malformed JSON must hit delegate(), not the auto-allow encoder")
	})

	t.Run("autoAllow=true, RTK exits non-zero with empty stdout: error propagates", func(t *testing.T) {
		t.Parallel()

		cfg := config{
			commandRules: canonicalRules(),
			rtkRewrite:   rtkExit5,
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "echo hi"), &stdout, cfg, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delegating to "+rtkExit5)
		// No auto-allow injected; RTK failure must surface to the user.
		assert.Empty(t, stdout.Bytes())
	})

	t.Run("autoAllow=true, RTK writes partial stdout then errors: forwarded + error", func(t *testing.T) {
		t.Parallel()

		// RTK writes a complete JSON object before failing on a
		// downstream step. Policy: forward what was captured (the
		// rewrite may already be valid), then propagate the error.
		cfg := config{
			commandRules: canonicalRules(),
			rtkRewrite:   rtkPartialFail,
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "echo hi"), &stdout, cfg, logger)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delegating to "+rtkPartialFail)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)

		updated, ok := hso["updatedInput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "partial", updated["command"])
	})

	t.Run("autoAllow=true, no rtkRewrite configured: emits auto-allow", func(t *testing.T) {
		t.Parallel()

		// rtkRewrite="" makes delegateCapture return (nil, nil) so the
		// caller still sees "RTK produced no rewrite" and emits the
		// auto-allow response.
		cfg := config{
			commandRules: canonicalRules(),
			autoAllow:    true,
		}

		var stdout bytes.Buffer

		err := handleBash(context.Background(), hookInput(t, "echo hi"), &stdout, cfg, logger)
		require.NoError(t, err)

		var result map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

		hso, ok := result["hookSpecificOutput"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "allow", hso["permissionDecision"])
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
