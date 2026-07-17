package main

import (
	"io"
	"log/slog"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/mcprules"
)

// handleMCP resolves an MCP tool call against the --mcp-rules
// allow/ask/deny lists and emits the matching PreToolUse decision.
// The lists mirror the MCP entries of settings.json permissions, but
// are enforced here because plan mode ignores settings allow rules for
// subagent-originated MCP calls (anthropics/claude-code#73633) while
// hook decisions still apply. None of the branches are gated on
// cfg.autoAllow: the rules encode user-declared permissions, not
// sandbox containment, so they hold on every host. Unmatched tools and
// malformed input fall through to the normal permission flow.
func handleMCP(input []byte, stdout io.Writer, cfg config, logger *slog.Logger) error {
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
