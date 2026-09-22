package main

import (
	"context"
	"log/slog"
	"os"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/linter"
)

// handlePostFileWrite formats and then lints the file Claude Code just
// wrote. Stateless: takes no store. Reads file_path from tool_input
// (shared by Write, Edit, and MultiEdit), runs the matching formatter
// rule first so linter line numbers refer to the formatted file, then
// the matching linter rule. Linter findings come back as a
// [*blockError]; [main] prints its Reason to stderr and exits 2, which
// is what the asyncRewake hook entry in home/claude.nix wakes Claude
// on. Formatter and linter crashes log at warn and are otherwise
// swallowed, so a wedged or missing tool never reaches the hook JSON
// channel.
//
// Emits no hookSpecificOutput.
func handlePostFileWrite(
	ctx context.Context,
	input []byte,
	cfg config,
	logger *slog.Logger,
) error {
	if cfg.formatterRules.Empty() && cfg.linterRules.Empty() {
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

	return lintPath(ctx, cfg, h, filePath, logger)
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
// effort, which suits formatting, since a missed file stays unformatted
// until its next edit and formatting a spurious entry in place does no
// harm.
//
// Linting stays off this path on purpose. PostToolUse:Bash is
// synchronous and emits at most one decision, the compactor's
// updatedToolOutput, so an exit-2 block here would discard the Bash
// output Claude needs. Lint a file written through Bash by hand with
// `prose-lint <file>` instead.
//
// The loop formats files in list order and stops when ctx expires, so
// a long list cannot outlive hook-router's own deadline. Emits no
// hookSpecificOutput.
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
// A path no rule matches stays untouched. A failing formatter logs at
// warn and nothing more, so formatter breakage never reaches Claude.
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

// lintPath runs the first linter rule whose glob accepts filePath and
// returns its findings as a [*blockError]. A path no rule matches, a
// clean file, and a linter that fails (logged at warn) all return nil.
// A Write reports every finding; an Edit or MultiEdit reports only the
// findings on lines it wrote, see [touchedFindings].
func lintPath(ctx context.Context, cfg config, h hook.Input, filePath string, logger *slog.Logger) error {
	rule, ok := cfg.linterRules.Match(filePath)
	if !ok {
		return nil
	}

	findings, err := rule.Run(ctx, filePath)
	if err != nil {
		logger.Warn("linter run failed",
			slog.String("file_path", filePath),
			slog.String("linter", rule.Command[0]),
			slog.Any("error", err),
		)

		return nil
	}

	total := len(findings)
	findings = touchedFindings(h, filePath, findings, logger)

	logger.Info("linted file",
		slog.String("file_path", filePath),
		slog.String("linter", rule.Command[0]),
		slog.String("tool", h.ToolName),
		slog.Int("findings", total),
		slog.Int("reported", len(findings)),
	)

	if len(findings) == 0 {
		return nil
	}

	return &blockError{Reason: linter.Format(filePath, findings, linter.MaxFindings)}
}

// touchedFindings narrows findings to the lines an Edit or MultiEdit
// wrote, so a file whose older findings Claude is not touching does
// not rewake it on every edit. Every occurrence of each new_string
// counts, since the Edit tool only requires old_string to be unique.
// A Write keeps every finding. An edit whose new_string values are all
// empty (a deletion) reports nothing, because no written line can
// carry a finding. When the file lacks a new_string (the formatter
// rewrote it), the function reports every finding, with an info log.
func touchedFindings(h hook.Input, filePath string, findings []linter.Finding, logger *slog.Logger) []linter.Finding {
	if len(findings) == 0 {
		return nil
	}

	var needles []string

	switch h.ToolName {
	case "Edit":
		needles = append(needles, stringField(h.ToolInput, "new_string"))
	case "MultiEdit":
		edits, _ := h.ToolInput["edits"].([]any)
		for _, edit := range edits {
			fields, ok := edit.(map[string]any)
			if !ok {
				continue
			}

			needles = append(needles, stringField(fields, "new_string"))
		}

	default:
		return findings
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		logger.Warn("reading linted file",
			slog.String("file_path", filePath),
			slog.Any("error", err),
		)

		return findings
	}

	var ranges []linter.LineRange

	for _, needle := range needles {
		if needle == "" {
			continue
		}

		hits := linter.Ranges(string(content), needle)
		if len(hits) == 0 {
			logger.Info("edit text not found after formatting, reporting every finding",
				slog.String("file_path", filePath),
			)

			return findings
		}

		ranges = append(ranges, hits...)
	}

	return linter.Filter(findings, ranges)
}

// stringField returns fields[key] when it is a string, else "".
func stringField(fields map[string]any, key string) string {
	s, _ := fields[key].(string)

	return s
}
