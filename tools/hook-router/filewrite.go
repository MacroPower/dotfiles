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

	formatPath(ctx, cfg, filePath, logger)

	return nil
}

// handlePostBashEdits runs the matching formatter rule against every
// file a Bash command changed, so a `sed -i` or `tee` edit gets the
// same formatting a Write or Edit does. Stateless: takes no store.
//
// The changed-file list comes from tool_response.bashEditDiff.changedFiles,
// which Claude Code fills when the bashEditDiffEnabled setting is on:
// absolute paths of files changed under the command's Git repository,
// capped at 200 upstream. A payload without the field (setting off,
// background command, read-only command, or a command outside a
// repository) is a no-op. Claude Code documents the list as best
// effort, which suits formatting: a missed file stays unformatted
// until its next edit, and a spurious entry is formatted in place.
//
// Files are formatted in list order and the loop stops once ctx is
// done, so a long list cannot outlive hook-router's own deadline.
// Emits no hookSpecificOutput.
func handlePostBashEdits(
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

	diff, ok := h.ToolResponse["bashEditDiff"].(map[string]any)
	if !ok {
		return nil
	}

	changed, ok := diff["changedFiles"].([]any)
	if !ok {
		return nil
	}

	for _, entry := range changed {
		if ctx.Err() != nil {
			logger.Warn("stopped formatting bash edits", slog.Any("error", ctx.Err()))
			return nil
		}

		filePath, ok := entry.(string)
		if !ok || filePath == "" {
			continue
		}

		formatPath(ctx, cfg, filePath, logger)
	}

	return nil
}

// formatPath runs the first formatter rule whose glob accepts filePath.
// A path no rule matches is left alone. A failing formatter logs at
// warn and is otherwise swallowed, so formatter breakage never reaches
// Claude.
func formatPath(ctx context.Context, cfg config, filePath string, logger *slog.Logger) {
	rule, ok := cfg.formatterRules.Match(filePath)
	if !ok {
		return
	}

	err := rule.Run(ctx, filePath)
	if err != nil {
		logger.Warn("formatter run failed",
			slog.String("file_path", filePath),
			slog.String("formatter", rule.Command[0]),
			slog.Any("error", err),
		)

		return
	}

	logger.Info("formatted file",
		slog.String("file_path", filePath),
		slog.String("formatter", rule.Command[0]),
	)
}
