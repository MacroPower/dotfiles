package main

import (
	"context"
	"log/slog"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// handlePreCompact stamps the session row with the compaction trigger
// and the time it fired.
//
// The handler takes no io.Writer on purpose. PreCompact can block
// compaction, and it parses stdout that opens with "{" as a decision
// document, so a stray write either cancels the compaction or raises a
// hook error. Having no writer makes that unreachable.
//
// The marker is observational and nothing reads it yet. Plan-guard
// state does not need it: that state lives in SQLite rather than in the
// transcript, so compaction cannot lose it.
//
// Store failures are fail-open, matching [handleSubagentStart]: a
// missing marker costs nothing, while a returned error would exit 1.
func handlePreCompact(
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

	err = store.RecordCompaction(ctx, h.SessionID, h.Trigger)
	if err != nil {
		logger.ErrorContext(ctx, "recording compaction", slog.Any("error", err))
		return nil
	}

	logger.InfoContext(ctx, "recorded compaction",
		slog.String("session", h.SessionID),
		slog.String("trigger", h.Trigger),
	)

	return nil
}
