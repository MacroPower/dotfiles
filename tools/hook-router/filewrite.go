package main

import (
	"context"
	"log/slog"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
)

// handlePostFileWrite runs the matching formatter rule against the
// file Claude Code just wrote. Stateless: takes no store. Reads
// file_path from tool_input (shared by Write, Edit, and MultiEdit),
// looks up a rule via [formatter.Engine.Match], and runs it. Non-zero
// formatter exits log at warn and are otherwise swallowed, so a
// wedged or missing formatter never reaches the hook JSON channel.
//
// Emits no hookSpecificOutput; the only side effect of a successful
// run is the formatted file on disk.
func handlePostFileWrite(
	ctx context.Context,
	input []byte,
	cfg config,
	logger *slog.Logger,
) error {
	if cfg.formatterRules.Empty() {
		return nil
	}

	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	filePath, ok := h.ToolInput["file_path"].(string)
	if !ok || filePath == "" {
		return nil
	}

	rule, ok := cfg.formatterRules.Match(filePath)
	if !ok {
		return nil
	}

	err = rule.Run(ctx, filePath)
	if err != nil {
		logger.Warn("formatter run failed",
			slog.String("file_path", filePath),
			slog.String("formatter", rule.Command[0]),
			slog.Any("error", err),
		)

		return nil
	}

	logger.Info("formatted file",
		slog.String("file_path", filePath),
		slog.String("formatter", rule.Command[0]),
	)

	return nil
}
