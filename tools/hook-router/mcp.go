package main

import (
	"context"
	"io"
	"log/slog"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/mcprules"
)

// handleMCP resolves an MCP tool call against the --mcp-rules
// allow/ask/deny lists, lints the prose arguments the call carries, and
// emits the PreToolUse decision.
//
// The lists mirror the MCP entries of settings.json permissions. The
// hook enforces them as well because plan mode ignores settings allow
// rules for subagent-originated MCP calls
// (anthropics/claude-code#73633). Claude Code 2.1.299 and later honor
// an allow from this hook in plan mode only for tools whose server sets
// readOnlyHint. A subagent's call to any other tool reaches the
// permission prompt. None of the branches are gated on cfg.autoAllow:
// the rules encode user-declared permissions, not sandbox containment,
// so they hold on every host.
//
// A deny-listed tool gets its deny without a lint. For every other
// tool, the string arguments that the message lint's ToolFields name go
// through [lintMessages] before the allow or ask decision. Findings
// that deny the call replace that decision, so Claude rewrites the text
// before a permission prompt shows it to the user. Unmatched tools with
// clean text and malformed input fall through to the normal permission
// flow.
func handleMCP(ctx context.Context, input []byte, stdout io.Writer, cfg config, logger *slog.Logger) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Info("invalid JSON, falling through", slog.Any("error", err))
		return nil
	}

	tool := h.ToolName
	if tool == "" {
		return nil
	}

	decision, pattern, matched := cfg.mcpRules.Match(tool)

	if !matched || decision != mcprules.DecisionDeny {
		msgs := cfg.messageLint.ToolMessages(tool, h.ToolInput)
		if len(msgs) > 0 {
			reason, deny := lintMessages(ctx, msgs, slog.String("tool", tool), h.SessionID, cfg, logger)
			if deny {
				logger.Info(
					"denied",
					slog.String("rule", "message-lint"),
					slog.String("tool", tool),
					slog.String("reason", reason),
				)

				return writeDecision(stdout, hook.Deny(reason))
			}
		}
	}

	if !matched {
		return nil
	}

	logger.Info(
		decision,
		slog.String("rule", "mcp-"+decision),
		slog.String("tool", tool),
		slog.String("pattern", pattern),
	)

	var response map[string]any

	switch decision {
	case mcprules.DecisionAllow:
		response = hook.Allow("mcp allow-list (" + pattern + ")")
	case mcprules.DecisionAsk:
		response = hook.Ask("This MCP tool is on the ask list (" + pattern + "). Confirm before running.")
	default:
		response = hook.Deny("This MCP tool is denied (" + pattern + ").")
	}

	return writeDecision(stdout, response)
}
