package main

import (
	"context"
	"log/slog"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// handleSubagentStart records an Agent tool spawn in the state store.
//
// The handler is observational. SubagentStart cannot block a spawn, and
// exit-2 stderr surfaces only as a hook-error notice in the subagent's
// own transcript, so there is nothing useful to emit. It takes no
// io.Writer for that reason, the way [handlePostAskUserQuestion] does.
//
// Every store operation is fail-open: a spawn that goes unrecorded
// costs a row of history, while a returned error would exit 1 and put a
// hook-error notice in front of the user on a purely observational
// event.
func handleSubagentStart(
	ctx context.Context,
	input []byte,
	store *state.Store,
	logger *slog.Logger,
) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.WarnContext(ctx, "unparsable hook input", slog.Any("error", err))
		return nil
	}

	if h.SessionID == "" {
		return nil
	}

	err = store.RecordSubagentStart(ctx, state.SubagentStart{
		SessionID: h.SessionID,
		AgentID:   h.AgentID,
		AgentType: h.AgentType,
	})
	if err != nil {
		logger.ErrorContext(ctx, "recording subagent start", slog.Any("error", err))
		return nil
	}

	logger.InfoContext(ctx, "recorded subagent start",
		slog.String("session", h.SessionID),
		slog.String("agent_type", h.AgentType),
		slog.String("agent_id", h.AgentID),
	)

	return nil
}
