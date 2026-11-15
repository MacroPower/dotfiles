package promptguard

import (
	"os"
	"path/filepath"
	"strings"
)

// WorkflowAgentType is the agent type of an agent that a Workflow
// script starts without naming an agent type.
const WorkflowAgentType = "workflow-subagent"

const (
	// transcriptExt is the extension of a transcript file.
	transcriptExt = ".jsonl"

	// subagentsDir holds the transcripts of the agents of a session.
	// It sits in the directory that carries the name of the session
	// transcript without its extension.
	subagentsDir = "subagents"

	// workflowsDir sits in [subagentsDir]. It holds one directory of
	// agent transcripts for each Workflow run.
	workflowsDir = "workflows"
)

// Unattended reports whether no one answers a prompt for a tool call,
// because an agent of the Workflow tool made the call.
//
// transcriptPath, agentID, and agentType come from the hook payload of
// the call. A payload without an agent ID comes from the main
// conversation, where a person answers the prompts.
func Unattended(transcriptPath, agentID, agentType string) bool {
	if agentID == "" {
		return false
	}

	if agentType == WorkflowAgentType {
		return true
	}

	return workflowTranscript(transcriptPath, agentID)
}

// Reason returns the reason of the deny that takes the place of a
// prompt. ask is the reason that the prompt gives, and an empty ask
// leaves it out.
func Reason(ask string) string {
	reason := "No one answers the prompts of a Workflow agent, so hook-router denied this call in place of a prompt. " +
		"Do the work another way, or continue without the call and name the call in your result."

	if ask == "" {
		return reason
	}

	return reason + " Reason for the prompt: " + ask
}

// workflowTranscript reports whether Claude Code keeps the transcript
// of the agent agentID with the transcripts of a Workflow run.
//
// transcriptPath names the transcript of the session, or the transcript
// of the agent itself. Claude Code creates the transcript of an agent
// when it starts the agent, so the file exists before the first tool
// call of the agent. An agentID that is not a plain file name matches
// no transcript.
func workflowTranscript(transcriptPath, agentID string) bool {
	if transcriptPath == "" || agentID != filepath.Base(agentID) {
		return false
	}

	name := "agent-" + agentID + transcriptExt

	if filepath.Base(transcriptPath) == name {
		runs := filepath.Dir(filepath.Dir(transcriptPath))

		return filepath.Base(runs) == workflowsDir && filepath.Base(filepath.Dir(runs)) == subagentsDir
	}

	runs := filepath.Join(strings.TrimSuffix(transcriptPath, transcriptExt), subagentsDir, workflowsDir)

	entries, err := os.ReadDir(runs)
	if err != nil {
		// A session that ran no Workflow has no such directory.
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		_, err := os.Stat(filepath.Join(runs, entry.Name(), name))
		if err == nil {
			return true
		}
	}

	return false
}
