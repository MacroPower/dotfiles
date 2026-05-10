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
