package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/mcprules"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/promptguard"
)

// promptCaller names the caller of a tool call in a hook payload.
type promptCaller struct {
	agentID   string
	agentType string
}

var (
	// mainCaller is the main conversation.
	mainCaller = promptCaller{}

	// workflowCaller is an agent that a Workflow script started
	// without an agent type.
	workflowCaller = promptCaller{agentID: "a4393e7f5a77d2308", agentType: promptguard.WorkflowAgentType}

	// typedWorkflowCaller is an agent that a Workflow script started
	// with the Explore agent type. Only the place of its transcript
	// marks it as a Workflow agent (see [promptSession]).
	typedWorkflowCaller = promptCaller{agentID: "aca8277c9e4b00d73", agentType: "Explore"}

	// agentToolCaller is an agent that the Agent tool started.
	agentToolCaller = promptCaller{agentID: "a1c7045d0c631e30e", agentType: "Explore"}
)

// promptSession creates the transcripts of a session that ran
// [typedWorkflowCaller] in a Workflow and [agentToolCaller] through the
// Agent tool. It returns the path of the session transcript.
func promptSession(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	subagents := filepath.Join(root, "session", "subagents")

	for _, path := range []string{
		filepath.Join(root, "session.jsonl"),
		filepath.Join(subagents, "agent-"+agentToolCaller.agentID+".jsonl"),
		filepath.Join(subagents, "workflows", "wf_f6a32162-f53", "agent-"+typedWorkflowCaller.agentID+".jsonl"),
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, nil, 0o644))
	}

	return filepath.Join(root, "session.jsonl")
}

// promptPayload returns a hook payload for a call that caller made in
// the session of transcript.
func promptPayload(t *testing.T, transcript string, caller promptCaller, fields map[string]any) []byte {
	t.Helper()

	payload := map[string]any{
		"session_id":      "s1",
		"transcript_path": transcript,
		"permission_mode": "bypassPermissions",
	}

	if caller.agentID != "" {
		payload["agent_id"] = caller.agentID
	}

	if caller.agentType != "" {
		payload["agent_type"] = caller.agentType
	}

	maps.Copy(payload, fields)

	b, err := json.Marshal(payload)
	require.NoError(t, err)

	return b
}

// TestHandleBashPromptGuard checks the decision of [handleBash] for a
// command that an ask rule matches, by the caller of the command.
func TestHandleBashPromptGuard(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	transcript := promptSession(t)

	// The command of a Workflow agent that waited more than two hours
	// on the prompt of the gh fallback ask rule.
	const stalled = `cd /work && for f in aven chief; do printf '%s ' "$f"; ` +
		`git log --diff-filter=A --format='%ad %h' --date=short -- "pkgs/$f-update.sh" | tail -1; done; ` +
		`which task gh yq; gh auth status 2>&1 | head -5`

	tcs := map[string]struct {
		caller  promptCaller
		command string
		off     bool
		want    string
		reason  string
	}{
		"main conversation: ask": {
			caller:  mainCaller,
			command: stalled,
			want:    "ask",
			reason:  ghFallbackAskReason,
		},
		"agent of the Agent tool: ask": {
			caller:  agentToolCaller,
			command: stalled,
			want:    "ask",
			reason:  ghFallbackAskReason,
		},
		"Workflow agent: deny with the reason of the ask": {
			caller:  workflowCaller,
			command: stalled,
			want:    "deny",
			reason:  promptguard.Reason(ghFallbackAskReason),
		},
		"Workflow agent of another type: deny": {
			caller:  typedWorkflowCaller,
			command: "gh issue create --title x",
			want:    "deny",
			reason:  promptguard.Reason(ghGroupAskReason),
		},
		"Workflow agent with the guard off: ask": {
			caller:  workflowCaller,
			command: stalled,
			off:     true,
			want:    "ask",
			reason:  ghFallbackAskReason,
		},
		"Workflow agent, deny rule: the deny of the rule": {
			caller:  workflowCaller,
			command: "gh api repos/o/r",
			want:    "deny",
			reason:  ghAPIDeniedReason,
		},
		"Workflow agent, auto-allowed command: allow": {
			caller:  workflowCaller,
			command: "gh pr checks 12",
			want:    "allow",
			reason:  "sandbox auto-allow",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := config{commandRules: ghRules(), autoAllow: true, promptGuard: !tc.off}
			input := promptPayload(t, transcript, tc.caller, map[string]any{
				"tool_name":  "Bash",
				"tool_input": map[string]any{"command": tc.command},
			})

			var stdout bytes.Buffer

			require.NoError(t, handleBash(input, &stdout, cfg, logger))

			hso := preToolDecision(t, &stdout)
			assert.Equal(t, "PreToolUse", hso["hookEventName"])
			assert.Equal(t, tc.want, hso["permissionDecision"])
			assert.Equal(t, tc.reason, hso["permissionDecisionReason"])
		})
	}
}

// TestHandleMCPPromptGuard checks the decision of [handleMCP] for a
// Workflow agent's call of a tool on each list and of a tool with a
// conditional rule.
func TestHandleMCPPromptGuard(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	transcript := promptSession(t)

	rules, err := mcprules.Parse(`{
		"allow": ["mcp__git__git_clone"],
		"ask": ["mcp__github__merge_pull_request"],
		"deny": ["mcp__kagi__kagi_extract"],
		"conditional": [
			{
				"tool": "mcp__git__git_push",
				"conditions": [{"field": "ref", "test": "matches", "pattern": "^(feat|fix)/.+$"}]
			}
		]
	}`)
	require.NoError(t, err)

	const (
		listAsk        = "This MCP tool is on the ask list (mcp__github__merge_pull_request). Confirm before running."
		conditionalAsk = "This call needs confirmation. mcp__git__git_push runs without a prompt when `ref` matches `^(feat|fix)/.+$`."
	)

	tcs := map[string]struct {
		caller promptCaller
		tool   string
		input  map[string]any
		off    bool
		want   string
		reason string
	}{
		"main conversation, ask list: ask": {
			caller: mainCaller,
			tool:   "mcp__github__merge_pull_request",
			want:   "ask",
			reason: listAsk,
		},
		"agent of the Agent tool, ask list: ask": {
			caller: agentToolCaller,
			tool:   "mcp__github__merge_pull_request",
			want:   "ask",
			reason: listAsk,
		},
		"Workflow agent, ask list: deny": {
			caller: workflowCaller,
			tool:   "mcp__github__merge_pull_request",
			want:   "deny",
			reason: promptguard.Reason(listAsk),
		},
		"Workflow agent of another type, ask list: deny": {
			caller: typedWorkflowCaller,
			tool:   "mcp__github__merge_pull_request",
			want:   "deny",
			reason: promptguard.Reason(listAsk),
		},
		"Workflow agent with the guard off, ask list: ask": {
			caller: workflowCaller,
			tool:   "mcp__github__merge_pull_request",
			off:    true,
			want:   "ask",
			reason: listAsk,
		},
		"main conversation, conditions not met: ask": {
			caller: mainCaller,
			tool:   "mcp__git__git_push",
			input:  map[string]any{"repo": "/r", "ref": "main"},
			want:   "ask",
			reason: conditionalAsk,
		},
		"Workflow agent, conditions not met: deny with the conditions": {
			caller: workflowCaller,
			tool:   "mcp__git__git_push",
			input:  map[string]any{"repo": "/r", "ref": "main"},
			want:   "deny",
			reason: promptguard.Reason(conditionalAsk),
		},
		"Workflow agent, conditions met: allow": {
			caller: workflowCaller,
			tool:   "mcp__git__git_push",
			input:  map[string]any{"repo": "/r", "ref": "fix/stall"},
			want:   "allow",
			reason: "mcp conditional rule (`ref` matches `^(feat|fix)/.+$`)",
		},
		"Workflow agent, allow list: allow": {
			caller: workflowCaller,
			tool:   "mcp__git__git_clone",
			want:   "allow",
			reason: "mcp allow-list (mcp__git__git_clone)",
		},
		"Workflow agent, deny list: the deny of the list": {
			caller: workflowCaller,
			tool:   "mcp__kagi__kagi_extract",
			want:   "deny",
			reason: "This MCP tool is denied (mcp__kagi__kagi_extract).",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := config{mcpRules: rules, promptGuard: !tc.off}
			input := promptPayload(t, transcript, tc.caller, map[string]any{
				"tool_name":  tc.tool,
				"tool_input": tc.input,
			})

			var stdout bytes.Buffer

			require.NoError(t, handleMCP(t.Context(), input, &stdout, cfg, logger))

			hso := preToolDecision(t, &stdout)
			assert.Equal(t, "PreToolUse", hso["hookEventName"])
			assert.Equal(t, tc.want, hso["permissionDecision"])
			assert.Equal(t, tc.reason, hso["permissionDecisionReason"])
		})
	}
}

// TestHandleFileWritePrePromptGuard checks the decision of
// [handleFileWritePre] for a write that the sandbox denies to
// commands, by the caller of the write.
func TestHandleFileWritePrePromptGuard(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	transcript := promptSession(t)

	guarded, root, _ := guardFixture(t)
	outside := filepath.Join(root, "outside.md")

	// The reason of the prompt, as the write guard words it.
	ask := "The sandbox does not let commands write " + outside +
		", because it is outside every sandbox write path. Confirm before writing."

	tcs := map[string]struct {
		caller promptCaller
		off    bool
		want   string
		reason string
	}{
		"main conversation: ask": {
			caller: mainCaller,
			want:   "ask",
			reason: ask,
		},
		"agent of the Agent tool: ask": {
			caller: agentToolCaller,
			want:   "ask",
			reason: ask,
		},
		"Workflow agent: deny with the reason of the ask": {
			caller: workflowCaller,
			want:   "deny",
			reason: promptguard.Reason(ask),
		},
		"Workflow agent of another type: deny": {
			caller: typedWorkflowCaller,
			want:   "deny",
			reason: promptguard.Reason(ask),
		},
		"Workflow agent with the guard off: ask": {
			caller: workflowCaller,
			off:    true,
			want:   "ask",
			reason: ask,
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := guarded
			cfg.promptGuard = !tc.off

			input := promptPayload(t, transcript, tc.caller, map[string]any{
				"tool_name":  "Write",
				"tool_input": map[string]any{"file_path": outside},
			})

			var stdout bytes.Buffer

			require.NoError(t, handleFileWritePre(input, &stdout, cfg, logger))

			hso := preToolDecision(t, &stdout)
			assert.Equal(t, "PreToolUse", hso["hookEventName"])
			assert.Equal(t, tc.want, hso["permissionDecision"])
			assert.Equal(t, tc.reason, hso["permissionDecisionReason"])
		})
	}
}

// TestHandlePermissionRequest checks which permission prompts
// [handlePermissionRequest] answers.
func TestHandlePermissionRequest(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	transcript := promptSession(t)

	tcs := map[string]struct {
		caller promptCaller
		off    bool
		deny   bool
	}{
		"main conversation":                 {caller: mainCaller},
		"agent of the Agent tool":           {caller: agentToolCaller},
		"Workflow agent":                    {caller: workflowCaller, deny: true},
		"Workflow agent of another type":    {caller: typedWorkflowCaller, deny: true},
		"Workflow agent with the guard off": {caller: workflowCaller, off: true},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := config{promptGuard: !tc.off}
			input := promptPayload(t, transcript, tc.caller, map[string]any{
				"hook_event_name": "PermissionRequest",
				"tool_name":       "Bash",
				"tool_input":      map[string]any{"command": "git send-email 0001.patch"},
			})

			var stdout bytes.Buffer

			require.NoError(t, handlePermissionRequest(input, &stdout, cfg, logger))

			if !tc.deny {
				assert.Empty(t, stdout.String())
				return
			}

			assert.JSONEq(t, `{
				"hookSpecificOutput": {
					"hookEventName": "PermissionRequest",
					"decision": {
						"behavior": "deny",
						"message": "No one answers the prompts of a Workflow agent, so hook-router denied this call in place of a prompt. Do the work another way, or continue without the call and name the call in your result."
					}
				}
			}`, stdout.String())
		})
	}

	t.Run("invalid JSON falls through", func(t *testing.T) {
		t.Parallel()

		var stdout bytes.Buffer

		require.NoError(t, handlePermissionRequest([]byte("not json"), &stdout, config{promptGuard: true}, logger))
		assert.Empty(t, stdout.String())
	})
}

// TestRunPermissionRequest checks that [run] routes the
// PermissionRequest event to [handlePermissionRequest] without a state
// store.
func TestRunPermissionRequest(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	input := promptPayload(t, "", workflowCaller, map[string]any{
		"hook_event_name": "PermissionRequest",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file_path": "/work/.git/config"},
	})

	var stdout bytes.Buffer

	err := run(
		t.Context(), bytes.NewReader(input), &stdout,
		"PermissionRequest", "", nil, config{promptGuard: true}, logger,
	)
	require.NoError(t, err)

	want, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PermissionRequest",
			"decision": map[string]any{
				"behavior": "deny",
				"message":  promptguard.Reason(""),
			},
		},
	})
	require.NoError(t, err)
	assert.JSONEq(t, string(want), stdout.String())
}

// TestConfigFlagsPromptGuard checks that --prompt-guard turns the guard
// on and that the guard stays off without the flag.
func TestConfigFlagsPromptGuard(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	tcs := map[string]struct {
		flags configFlags
		want  bool
	}{
		"flag set":   {flags: configFlags{promptGuard: true}, want: true},
		"flag unset": {flags: configFlags{}},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := tc.flags.parse(config{}, nil, logger)
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg.promptGuard)
		})
	}
}
