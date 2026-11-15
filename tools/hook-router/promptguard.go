package main

import (
	"io"
	"log/slog"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/promptguard"
)

// writeAsk writes the PreToolUse decision for a call that needs a
// permission prompt. reason is the reason that the prompt shows.
//
// The decision is an ask. Under cfg.promptGuard, a call that a Workflow
// agent made gets a deny instead. No one answers the prompts of that
// agent (see the promptguard package), so an ask holds the agent until
// someone stops its run. The deny keeps reason, so the agent reads what
// the prompt was for.
func writeAsk(stdout io.Writer, h hook.Input, reason string, cfg config, logger *slog.Logger) error {
	if !unattended(h, cfg) {
		return writeDecision(stdout, hook.Ask(reason))
	}

	reason = promptguard.Reason(reason)

	logPromptDeny(h, reason, logger)

	return writeDecision(stdout, hook.Deny(reason))
}

// handlePermissionRequest answers the prompt of a Workflow agent's
// tool call with a deny. It acts only under cfg.promptGuard.
//
// Claude Code fires PermissionRequest for a prompt that a hook can
// answer, and takes the prompt down when a hook answers it. A prompt
// that reaches this event comes from Claude Code itself, such as the
// prompt of a settings ask rule. [writeAsk] already denies the calls
// that hook-router's own rules stop on a prompt. A payload from any
// other caller gets no answer, so a person still decides it. Invalid
// JSON falls through.
//
// Claude Code 2.1.319 skips the event for a prompt that only a person
// can answer, such as the prompt before an MCP tool sends the files of
// the session elsewhere. Such a prompt still holds a Workflow agent
// until someone stops its run.
func handlePermissionRequest(input []byte, stdout io.Writer, cfg config, logger *slog.Logger) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Info("invalid JSON, falling through", slog.Any("error", err))
		return nil
	}

	if !unattended(h, cfg) {
		return nil
	}

	reason := promptguard.Reason("")

	logPromptDeny(h, reason, logger)

	return writeDecision(stdout, hook.DenyPermissionRequest(reason))
}

// unattended reports whether cfg.promptGuard is on and the payload h
// comes from an agent whose prompts no one answers.
func unattended(h hook.Input, cfg config) bool {
	return cfg.promptGuard && promptguard.Unattended(h.TranscriptPath, h.AgentID, h.AgentType)
}

// logPromptDeny records a deny that took the place of a prompt.
func logPromptDeny(h hook.Input, reason string, logger *slog.Logger) {
	logger.Info(
		"denied",
		slog.String("rule", "unattended-prompt"),
		slog.String("tool", h.ToolName),
		slog.String("agent_id", h.AgentID),
		slog.String("agent_type", h.AgentType),
		slog.String("reason", reason),
	)
}
