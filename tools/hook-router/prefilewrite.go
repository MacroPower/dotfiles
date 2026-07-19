package main

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/typography"
)

// handlePreFileWrite denies a Write/Edit/MultiEdit that introduces a
// non-ASCII typographic character (a curly quote, an ellipsis, or a
// dash other than ASCII '-'); the deny message from
// [typography.Reason] suggests ASCII equivalents. Only net-new
// characters are blocked: an Edit compares old_string to new_string,
// a MultiEdit nets deltas across all its edits, and a Write compares
// against the current on-disk file, so characters already present in
// a file survive editing.
//
// "FileWrite" is the routing sentinel from the Write|Edit|MultiEdit
// hook matcher; the real tool name comes from the payload, mirroring
// the MCP handler. Emits nothing when clean, when disabled, or on any
// parse error; a Write whose target exists but cannot be read skips
// the check rather than guessing at the before text.
func handlePreFileWrite(input []byte, stdout io.Writer, cfg config, logger *slog.Logger) error {
	if !cfg.enforceTypography {
		return nil
	}

	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	filePath, _ := h.ToolInput["file_path"].(string)

	var changes []typography.Change

	switch h.ToolName {
	case "Write":
		content, _ := h.ToolInput["content"].(string)

		// A rune absent from the new content can never net positive,
		// so clean content skips the on-disk read entirely.
		if !strings.ContainsFunc(content, typography.Disallowed) {
			return nil
		}

		before, err := os.ReadFile(filePath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			logger.Warn("reading write target",
				slog.String("file_path", filePath),
				slog.Any("error", err),
			)

			return nil
		}

		changes = []typography.Change{{Before: string(before), After: content}}

	case "Edit":
		oldStr, _ := h.ToolInput["old_string"].(string)
		newStr, _ := h.ToolInput["new_string"].(string)
		changes = []typography.Change{{Before: oldStr, After: newStr}}

	case "MultiEdit":
		edits, _ := h.ToolInput["edits"].([]any)
		for _, e := range edits {
			m, ok := e.(map[string]any)
			if !ok {
				continue
			}

			oldStr, _ := m["old_string"].(string)
			newStr, _ := m["new_string"].(string)
			changes = append(changes, typography.Change{Before: oldStr, After: newStr})
		}

	default:
		return nil
	}

	findings := typography.Introduced(changes...)
	if len(findings) == 0 {
		return nil
	}

	logger.Info("denying introduced typographic characters",
		slog.String("tool", h.ToolName),
		slog.String("file_path", filePath),
		slog.Int("findings", len(findings)),
	)

	return writeDecision(stdout, hook.Deny(typography.Reason(filePath, findings)))
}
