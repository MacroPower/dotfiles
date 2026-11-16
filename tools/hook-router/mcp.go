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
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

var (
	// errRelativeRepo reports a repository path that is not absolute.
	errRelativeRepo = errors.New("repository path is not absolute")

	// errNoStore reports a read of the state store on a hook that runs
	// without one.
	errNoStore = errors.New("no state store configured")
)

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
// A rule can also test whether an agent opened the pull request that a
// call names. [handlePostCreatePullRequest] records those pull requests
// in the state store, and [parseMCPRules] gives the ruleset the lookup
// of [recordedPullRequest] to read them. A record that the lookup
// cannot read fails the rule, so the call gets an ask.
//
// A deny-listed tool gets its deny without a lint. For every other
// tool, the string arguments that the message lint's ToolFields name go
// through [lintMessages] before the allow or ask decision. Findings
// that deny the call replace that decision, so Claude rewrites the text
// before a permission prompt shows it to the user. Unmatched tools with
// clean text and malformed input fall through to the normal permission
// flow.
//
// Every ask goes through [writeAsk]. Under cfg.promptGuard, a Workflow
// agent's call therefore gets a deny that carries the reason of the
// ask.
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
		return writeConditionalDecision(ctx, stdout, h, res, cfg, logger)
	}

	logger.Info(
		decision,
		slog.String("rule", "mcp-"+decision),
		slog.String("tool", tool),
		slog.String("pattern", pattern),
	)

	switch decision {
	case mcprules.DecisionAllow:
		return writeDecision(stdout, hook.Allow("mcp allow-list ("+pattern+")"))
	case mcprules.DecisionAsk:
		reason := "This MCP tool is on the ask list (" + pattern + "). Confirm before running."

		return writeAsk(stdout, h, reason, cfg, logger)

	default:
		return writeDecision(stdout, hook.Deny("This MCP tool is denied ("+pattern+")."))
	}
}

// writeConditionalDecision logs and writes the PreToolUse decision for
// a call that the conditional rules of its tool decided. res.Conditions
// states the conditions of the rule that allowed the call, or of every
// rule of the tool when the call satisfied none. An ask goes through
// [writeAsk], so under cfg.promptGuard a Workflow agent reads its
// reason in a deny.
func writeConditionalDecision(
	ctx context.Context,
	stdout io.Writer,
	h hook.Input,
	res mcprules.Result,
	cfg config,
	logger *slog.Logger,
) error {
	tool := h.ToolName

	attrs := []any{
		slog.String("rule", "mcp-conditional-"+res.Decision),
		slog.String("tool", tool),
		slog.String("conditions", res.Conditions),
	}

	if res.Decision == mcprules.DecisionAllow {
		logger.InfoContext(ctx, "allow", attrs...)

		return writeDecision(stdout, hook.Allow("mcp conditional rule ("+res.Conditions+")"))
	}

	logger.InfoContext(ctx, "ask", attrs...)

	reason := "This call needs confirmation. " + tool + " runs without a prompt when " + res.Conditions + "."

	return writeAsk(stdout, h, reason, cfg, logger)
}

// parseMCPRules builds the ruleset of [handleMCP] from the --mcp-rules
// JSON. The ruleset reads the current branch of a repository through
// [currentBranch]. It reads the record of the pull requests that agents
// opened through [recordedPullRequest], which opens the state store
// with openStore. A nil openStore fails every condition that reads the
// record.
func parseMCPRules(
	rulesJSON string,
	openStore func(context.Context) (*state.Store, error),
	logger *slog.Logger,
) (*mcprules.Ruleset, error) {
	rules, err := mcprules.Parse(
		rulesJSON,
		mcprules.WithBranchResolver(currentBranch(logger)),
		mcprules.WithPullRequestLookup(recordedPullRequest(openStore, logger)),
	)
	if err != nil {
		return nil, fmt.Errorf("parsing --mcp-rules: %w", err)
	}

	return rules, nil
}

// recordedPullRequest returns the [mcprules.PullRequestLookup] of the
// hook. The lookup reports whether [handlePostCreatePullRequest]
// recorded a pull request, in this session or in an earlier one. It
// logs each answer, because the answer decides whether a call prompts.
//
// [eventNeedsStore] keeps the store closed on PreToolUse:MCP, so the
// lookup opens it through open for the one read and closes it again.
// A call whose rule fails before the lookup runs never opens the
// store. The lookup returns [errNoStore] when open is nil.
func recordedPullRequest(
	open func(context.Context) (*state.Store, error),
	logger *slog.Logger,
) mcprules.PullRequestLookup {
	return func(ctx context.Context, owner, repo string, number int) (bool, error) {
		attrs := []any{
			slog.String("owner", owner),
			slog.String("repo", repo),
			slog.Int("number", number),
		}

		found, err := hasPullRequest(ctx, open, state.PullRequest{Owner: owner, Repo: repo, Number: number})
		if err != nil {
			logger.WarnContext(ctx, "pull request record unread", append(attrs, slog.Any("error", err))...)

			return false, err
		}

		logger.InfoContext(ctx, "pull request record read", append(attrs, slog.Bool("recorded", found))...)

		return found, nil
	}
}

// hasPullRequest opens the state store through open and reports
// whether it holds a record of pr. It closes the store before it
// returns.
func hasPullRequest(
	ctx context.Context,
	open func(context.Context) (*state.Store, error),
	pr state.PullRequest,
) (bool, error) {
	if open == nil {
		return false, errNoStore
	}

	store, err := open(ctx)
	if err != nil {
		return false, fmt.Errorf("opening store: %w", err)
	}

	found, err := store.HasPullRequest(ctx, pr)
	if err != nil {
		return false, errors.Join(fmt.Errorf("reading the pull request record: %w", err), store.Close())
	}

	err = store.Close()
	if err != nil {
		return false, fmt.Errorf("closing store: %w", err)
	}

	return found, nil
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
// absolute path repo. A rule reads that branch for a call that names no
// branch, so repoBranch must read the repository that the MCP server
// acts on.
//
// It rejects a relative path with [errRelativeRepo]. An MCP server
// resolves a relative path against its own working directory, and that
// directory can differ from the directory of this process. The two
// processes then read different repositories.
//
// An MCP server starts with the environment of Claude Code, as this
// process does. That environment can name another repository with one
// of these variables:
//
//   - With GIT_DIR set, a git command reads the HEAD, the refs, and the
//     remotes of the repository that GIT_DIR names.
//   - With GIT_REFERENCE_BACKEND set, the command reads the HEAD and
//     the refs of the ref store that the variable names, such as the
//     git directory of another repository. It reads the remotes at
//     repo.
//   - With GIT_COMMON_DIR set, the command reads the HEAD and the refs
//     at repo. It reads the remotes and the objects of the repository
//     that GIT_COMMON_DIR names.
//
// A [git.Runner] starts its git commands without these variables, and
// the mcp-git server removes the same variables from its git commands.
// Both processes then read the current branch of the repository at
// repo, and the server pushes that branch to a remote of the same
// repository.
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
