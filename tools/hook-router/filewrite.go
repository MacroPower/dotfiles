package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
// A Write reports every finding. An Edit or MultiEdit reports the
// findings on lines it wrote, see [touchedFindings], plus every finding
// it created elsewhere in the file, see [freshFindings].
//
// The hook runs in the background, so a second edit to the same file
// can land before this one lints it. This hook then reports the
// findings the second edit created, and the second edit's hook reports
// them again. The race repeats findings but never drops one.
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
	reported := findings
	fresh := 0

	if len(findings) > 0 && h.ToolName != "Write" {
		reported, fresh = editFindings(ctx, rule, h, filePath, findings, logger)
	}

	logger.Info("linted file",
		slog.String("file_path", filePath),
		slog.String("linter", rule.Command[0]),
		slog.String("tool", h.ToolName),
		slog.Int("findings", total),
		slog.Int("fresh", fresh),
		slog.Int("reported", len(reported)),
	)

	if len(reported) == 0 {
		return nil
	}

	return &blockError{Reason: linter.Format(filePath, reported, linter.MaxFindings)}
}

// editFindings returns the findings an Edit or MultiEdit reports, in
// the linter's order: the [touchedFindings] and the [freshFindings],
// each finding once. It also returns the number of fresh findings.
// When reading the file fails, the function reports every finding.
func editFindings(
	ctx context.Context,
	rule linter.Rule,
	h hook.Input,
	filePath string,
	findings []linter.Finding,
	logger *slog.Logger,
) ([]linter.Finding, int) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		logger.Warn("reading linted file",
			slog.String("file_path", filePath),
			slog.Any("error", err),
		)

		return findings, 0
	}

	touched := touchedFindings(h, filePath, string(content), findings, logger)
	if len(touched) == len(findings) {
		return touched, 0
	}

	fresh := freshFindings(ctx, rule, h, filePath, string(content), findings, logger)

	return mergeFindings(findings, touched, fresh), len(fresh)
}

// touchedFindings narrows findings to the lines an Edit or MultiEdit
// wrote into content, the file after the edit. A file whose older
// findings Claude is not touching then does not rewake it on every
// edit. Every occurrence of each new_string counts, because the Edit
// tool requires only old_string to be unique. An edit whose new_string
// values are all empty (a deletion) wrote no line, so it touches no
// finding. When the file lacks a new_string (the formatter rewrote
// it), the function reports every finding, with an info log.
func touchedFindings(
	h hook.Input,
	filePath, content string,
	findings []linter.Finding,
	logger *slog.Logger,
) []linter.Finding {
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

	var ranges []linter.LineRange

	for _, needle := range needles {
		if needle == "" {
			continue
		}

		hits := linter.Ranges(content, needle)
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

// freshFindings returns the findings an edit created anywhere in the
// file. [touchedFindings] misses such a finding when it anchors on a
// line the edit did not write. A sentence check, for example, anchors
// on the sentence's first line. An edit to the end of a long sentence
// then leaves its finding outside the written lines.
//
// The function lints tool_response.originalFile, the file before the
// edit, and passes the findings from both lints to [linter.Fresh]. The
// copy keeps the file's base name, because the linter picks its rules
// by extension and by name. A payload without originalFile returns
// nil, and so does a pre-edit lint that fails. The edit then reports
// its touched findings alone.
func freshFindings(
	ctx context.Context,
	rule linter.Rule,
	h hook.Input,
	filePath, content string,
	findings []linter.Finding,
	logger *slog.Logger,
) []linter.Finding {
	original, ok := h.ToolResponse["originalFile"].(string)
	if !ok {
		return nil
	}

	dir, err := os.MkdirTemp("", "hook-router-lint-")
	if err != nil {
		logger.Warn("creating pre-edit lint dir", slog.Any("error", err))
		return nil
	}

	defer func() {
		err := os.RemoveAll(dir)
		if err != nil {
			logger.Warn("removing pre-edit lint dir", slog.String("dir", dir), slog.Any("error", err))
		}
	}()

	copyPath := filepath.Join(dir, filepath.Base(filePath))

	err = os.WriteFile(copyPath, []byte(original), 0o600)
	if err != nil {
		logger.Warn("writing pre-edit copy", slog.String("file_path", filePath), slog.Any("error", err))
		return nil
	}

	before, err := rule.Run(ctx, copyPath)
	if err != nil {
		logger.Warn("pre-edit linter run failed",
			slog.String("file_path", filePath),
			slog.String("linter", rule.Command[0]),
			slog.Any("error", err),
		)

		return nil
	}

	// The linter names the path it got in every line, including a
	// finding with no line number. Left in place, the copy's path
	// keeps that finding from matching its post-edit counterpart.
	for i := range before {
		before[i].Text = strings.ReplaceAll(before[i].Text, copyPath, filePath)
	}

	return linter.Fresh(findings, content, before, original)
}

// mergeFindings returns the findings that appear in a, in b, or in
// both, in the order of all. a and b must each keep the order of all,
// which [linter.Filter] and [linter.Fresh] do.
func mergeFindings(all, a, b []linter.Finding) []linter.Finding {
	var merged []linter.Finding

	i, j := 0, 0

	for _, f := range all {
		inA := i < len(a) && a[i] == f
		if inA {
			i++
		}

		inB := j < len(b) && b[j] == f
		if inB {
			j++
		}

		if inA || inB {
			merged = append(merged, f)
		}
	}

	return merged
}

// stringField returns fields[key] when it is a string, else "".
func stringField(fields map[string]any, key string) string {
	s, _ := fields[key].(string)

	return s
}
