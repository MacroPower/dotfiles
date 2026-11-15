package promptguard_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/promptguard"
)

// touch creates an empty file at path, with the directories above it.
func touch(t *testing.T, path string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, nil, 0o644))
}

// TestUnattended pins the payloads that [promptguard.Unattended] reads
// as a call of a Workflow agent. The transcripts follow the layout of
// Claude Code 2.1.319.
func TestUnattended(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	session := filepath.Join(root, "session.jsonl")
	subagents := filepath.Join(root, "session", "subagents")

	// The transcript of an Agent tool agent, and the transcripts of
	// the agents of two Workflow runs.
	touch(t, filepath.Join(subagents, "agent-plain.jsonl"))
	touch(t, filepath.Join(subagents, "workflows", "wf_1", "agent-first.jsonl"))
	touch(t, filepath.Join(subagents, "workflows", "wf_1", "journal.jsonl"))
	touch(t, filepath.Join(subagents, "workflows", "wf_2", "agent-second.jsonl"))

	// A session that ran no agent.
	bare := filepath.Join(root, "bare.jsonl")

	tcs := map[string]struct {
		transcript string
		agentID    string
		agentType  string
		want       bool
	}{
		"main conversation": {
			transcript: session,
		},
		"main conversation of a session with the workflow agent type": {
			transcript: session,
			agentType:  promptguard.WorkflowAgentType,
		},
		"workflow agent type": {
			transcript: session,
			agentID:    "first",
			agentType:  promptguard.WorkflowAgentType,
			want:       true,
		},
		"workflow agent type without a transcript": {
			agentID:   "unwritten",
			agentType: promptguard.WorkflowAgentType,
			want:      true,
		},
		"workflow agent of another type": {
			transcript: session,
			agentID:    "second",
			agentType:  "Explore",
			want:       true,
		},
		"workflow agent without a type": {
			transcript: session,
			agentID:    "first",
			want:       true,
		},
		"agent of the Agent tool": {
			transcript: session,
			agentID:    "plain",
			agentType:  "Explore",
		},
		"agent without a transcript": {
			transcript: session,
			agentID:    "unwritten",
			agentType:  "general-purpose",
		},
		"file of a run that is not a transcript of the agent": {
			transcript: session,
			agentID:    "journal",
			agentType:  "general-purpose",
		},
		"session without agents": {
			transcript: bare,
			agentID:    "first",
			agentType:  "general-purpose",
		},
		"no transcript path": {
			agentID:   "first",
			agentType: "general-purpose",
		},
		// Joined to the directory of a run, this ID names the journal
		// of the run.
		"agent ID that names a path": {
			transcript: session,
			agentID:    "x/../journal",
			agentType:  "general-purpose",
		},
		"payload names the transcript of a workflow agent": {
			transcript: filepath.Join(subagents, "workflows", "wf_2", "agent-second.jsonl"),
			agentID:    "second",
			agentType:  "Explore",
			want:       true,
		},
		"payload names the transcript of an Agent tool agent": {
			transcript: filepath.Join(subagents, "agent-plain.jsonl"),
			agentID:    "plain",
			agentType:  "Explore",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := promptguard.Unattended(tc.transcript, tc.agentID, tc.agentType)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestReason(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		ask  string
		want string
	}{
		"prompt with a reason": {
			ask: "This gh subcommand can mutate GitHub state. Confirm before running.",
			want: "No one answers the prompts of a Workflow agent, so hook-router denied this call in place of a prompt. " +
				"Do the work another way, or continue without the call and name the call in your result. " +
				"Reason for the prompt: This gh subcommand can mutate GitHub state. Confirm before running.",
		},
		"prompt without a reason": {
			want: "No one answers the prompts of a Workflow agent, so hook-router denied this call in place of a prompt. " +
				"Do the work another way, or continue without the call and name the call in your result.",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, promptguard.Reason(tc.ask))
		})
	}
}
