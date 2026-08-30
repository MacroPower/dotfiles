package main

import (
	"context"
	"log/slog"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// handleTeammateIdle blocks a teammate that is about to go idle while
// the session still owes the post-impl skills, extending to in-process
// teammates a gate that [handleStop] applies only to the lead.
//
// The block travels as a [blockError], not as a stdout decision:
// TeammateIdle reads a block only from exit code 2 (see blockError).
//
// Two deliberate departures from [handleStop], which enforces the same
// catalog:
//
//   - The gate fires once per teammate. TeammateIdle carries no
//     stop_hook_active field, so an unbounded block would loop a
//     teammate that cannot raise the post-impl AskUserQuestion itself.
//     [*state.Store.MarkTeammateIdleBlocked] claims the single block,
//     and clearing or resetting the session re-arms it.
//   - Reads are fail-open where handleStop is fail-closed. Stop can
//     recover through stop_hook_active; TeammateIdle cannot, so a store
//     outage must not wedge a teammate.
//
// The Info line runs on the allow path too. Whether a teammate's
// payload carries its own session_id or the lead's is not documented,
// and only the lead's row holds plan_path, so an always-empty plan_path
// in the log is the signal that this gate is a silent no-op.
func handleTeammateIdle(
	ctx context.Context,
	input []byte,
	store *state.Store,
	cfg config,
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

	_, planPath, baseSHA, err := store.Session(ctx, h.SessionID)
	if err != nil {
		logger.ErrorContext(ctx, "reading session", slog.Any("error", err))
		return nil
	}

	logger.InfoContext(ctx, "teammate going idle",
		slog.String("session", h.SessionID),
		slog.String("teammate", h.TeammateName),
		slog.String("plan_path", planPath),
	)

	if planPath == "" {
		return nil
	}

	claimed, err := store.MarkTeammateIdleBlocked(ctx, h.SessionID, h.TeammateName)
	if err != nil {
		logger.ErrorContext(ctx, "claiming teammate idle block", slog.Any("error", err))
		return nil
	}

	if !claimed {
		logger.InfoContext(ctx, "teammate already blocked once, allowing idle",
			slog.String("session", h.SessionID),
			slog.String("teammate", h.TeammateName),
		)

		return nil
	}

	logger.InfoContext(ctx, "blocking teammate idle for post-impl question",
		slog.String("session", h.SessionID),
		slog.String("teammate", h.TeammateName),
		slog.String("plan_path", planPath),
		slog.String("base_sha", baseSHA),
	)

	return &blockError{Reason: cfg.postImpl.BuildAskReason(planPath, baseSHA)}
}
