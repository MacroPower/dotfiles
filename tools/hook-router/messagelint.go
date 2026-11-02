package main

import (
	"context"
	"log/slog"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/msglint"
)

// lintMessages runs the message lint over msgs and reports whether to
// deny the command or MCP tool call that carries them, and why.
// subject identifies that call in log lines. A Required finding denies
// every time. A Recommended finding with no Required one denies once
// per message. The first run claims the message's waiver key in the
// store and gets the deny. A rerun of the identical call finds the key
// claimed and passes. The store opens lazily through cfg.openStore, so
// a PreToolUse hot path pays for SQLite only when a waiver is in play.
// No opener, a store error, and a linter crash all fail open with a log
// line, because a broken hook must never block a commit or a message.
func lintMessages(ctx context.Context, msgs []msglint.Message, subject slog.Attr, sessionID string, cfg config, logger *slog.Logger) (string, bool) {
	res, err := msglint.Check(ctx, msgs, cfg.messageLint)
	if err != nil {
		logger.Warn("message lint failed",
			subject,
			slog.Any("error", err),
		)

		return "", false
	}

	if res.Deny {
		return res.Reason, true
	}

	if res.WaiverKey == "" {
		return "", false
	}

	if cfg.openStore == nil {
		logger.Warn("message lint waiver skipped: no store configured", subject)

		return "", false
	}

	store, err := cfg.openStore(ctx)
	if err != nil {
		logger.Warn("message lint waiver skipped: opening store",
			subject,
			slog.Any("error", err),
		)

		return "", false
	}
	defer store.Close()

	claimed, err := store.MarkMessageLintWarned(ctx, sessionID, res.WaiverKey)
	if err != nil {
		logger.Warn("message lint waiver skipped: claiming waiver",
			subject,
			slog.Any("error", err),
		)

		return "", false
	}

	if !claimed {
		logger.Info("message lint waived on rerun",
			subject,
			slog.String("message_hash", res.WaiverKey),
		)

		return "", false
	}

	return res.Reason, true
}
