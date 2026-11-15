package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/git"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/mcprules"
)

// errRelativeRepo reports a repository path that is not absolute.
var errRelativeRepo = errors.New("repository path is not absolute")

// handleMCP resolves an MCP tool call against the lists and the
// conditional rules of --mcp-rules. It lints the prose arguments the
// call carries, and emits the PreToolUse decision.
//
// The allow, ask, and deny lists mirror the MCP entries of
// settings.json permissions. The hook enforces them as well because
// plan mode ignores settings allow rules for subagent-originated MCP
// calls (anthropics/claude-code#73633). Claude Code 2.1.299 and later
// honor an allow from this hook in plan mode only for tools whose
// server sets readOnlyHint. A subagent's call to any other tool reaches
// the permission prompt. None of the branches are gated on
// cfg.autoAllow: the rules encode user-declared permissions, not
// sandbox containment, so they hold on every host.
//
// A tool with a conditional rule has no entry in the settings ask and
// allow lists. Claude Code prompts on a settings ask rule even when a
// hook allows the call, so this hook emits every decision for such a
// tool. A call whose arguments satisfy one of the tool's rules gets an
// allow. Every other call gets an ask, and its reason states the
// conditions that a call must meet to run without a prompt.
//
// A rule can test the branch that a call acts on. When the call names
// no branch, the rule reads the current branch of the repository that
// the call names. [parseMCPRules] gives the ruleset the resolver of
// [currentBranch] for that read. A branch that does not resolve fails
// the rule, so the call gets an ask.
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

	res, matched := cfg.mcpRules.Match(ctx, tool, h.ToolInput)
	decision, pattern := res.Decision, res.Pattern

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

	if res.Conditions != "" {
		return writeDecision(stdout, conditionalDecision(ctx, tool, res, logger))
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

// conditionalDecision logs and returns the PreToolUse decision for a
// call that the conditional rules of tool decided. res.Conditions
// states the conditions of the rule that allowed the call, or of every
// rule of the tool when the call satisfied none.
func conditionalDecision(ctx context.Context, tool string, res mcprules.Result, logger *slog.Logger) map[string]any {
	attrs := []any{
		slog.String("rule", "mcp-conditional-"+res.Decision),
		slog.String("tool", tool),
		slog.String("conditions", res.Conditions),
	}

	if res.Decision == mcprules.DecisionAllow {
		logger.InfoContext(ctx, "allow", attrs...)

		return hook.Allow("mcp conditional rule (" + res.Conditions + ")")
	}

	logger.InfoContext(ctx, "ask", attrs...)

	return hook.Ask("This call needs confirmation. " + tool + " runs without a prompt when " + res.Conditions + ".")
}

// parseMCPRules builds the ruleset of [handleMCP] from the --mcp-rules
// JSON. The ruleset reads the current branch of a repository through
// [currentBranch].
func parseMCPRules(rulesJSON string, logger *slog.Logger) (*mcprules.Ruleset, error) {
	rules, err := mcprules.Parse(rulesJSON, mcprules.WithBranchResolver(currentBranch(logger)))
	if err != nil {
		return nil, fmt.Errorf("parsing --mcp-rules: %w", err)
	}

	return rules, nil
}

// currentBranch returns the [mcprules.BranchResolver] of the hook. The
// resolver reads the branch through [repoBranch] and logs each answer,
// because the answer decides whether a call prompts.
func currentBranch(logger *slog.Logger) mcprules.BranchResolver {
	return func(ctx context.Context, repo string) (string, error) {
		branch, err := repoBranch(ctx, repo)
		if err != nil {
			logger.InfoContext(ctx, "current branch unresolved",
				slog.String("repo", repo),
				slog.Any("error", err),
			)

			return "", err
		}

		logger.InfoContext(ctx, "current branch resolved",
			slog.String("repo", repo),
			slog.String("branch", branch),
		)

		return branch, nil
	}
}

// repoBranch asks git for the current branch of the repository at the
// absolute path repo.
//
// It rejects a relative path with [errRelativeRepo]. An MCP server
// resolves a relative path against its own working directory, and that
// directory can differ from the directory of this process. The two
// processes then read different repositories.
func repoBranch(ctx context.Context, repo string) (string, error) {
	if !filepath.IsAbs(repo) {
		return "", fmt.Errorf("%w: %q", errRelativeRepo, repo)
	}

	branch, err := (&git.Runner{Dir: repo}).CurrentBranch(ctx)
	if err != nil {
		return "", fmt.Errorf("resolving the current branch of %s: %w", repo, err)
	}

	return branch, nil
}
