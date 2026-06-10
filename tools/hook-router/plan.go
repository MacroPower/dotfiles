package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/git"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/postimpl"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/state"
)

// pendingPlanTTLSeconds bounds how long a claude_pid-keyed pending plan
// handoff is honored after [handleExitPlanModePre] records it.
//
// The sequence:
//
//  1. PreToolUse:ExitPlanMode (count==2) writes the pending row
//  2. User reviews the plan, then clicks "Yes, clear context (...)
//     and bypass permissions"
//  3. Claude Code clears context and creates the new session
//  4. SessionStart fires and consumes the row
//
// Most of the wall-clock between (1) and (4) is the user reading the
// plan and deciding whether to accept, which can take a while. The TTL
// needs to cover that wait. It also needs to expire stuck rows
// (SessionStart hook crashed, DB was busy, and so on) before an
// unrelated future invocation picks them up.
//
// claude_pid is the OS-process identity of the Claude Code window and
// is preserved across `/clear` (a `/clear` does not fork). The key
// therefore partitions handoffs per window, so a stuck row from a dead
// window cannot leak onto a peer window. The TTL is then only doing
// time-bounding within a window.
const pendingPlanTTLSeconds = 3600

// dropPendingPlan is the fail-open cleanup used at any lifecycle
// boundary where the handoff is no longer needed. It deletes the
// pending_plans row for claudePID if any, and logs at Error on failure
// with the caller-supplied site tag. A no-op when claudePID is empty.
func dropPendingPlan(ctx context.Context, store *state.Store, claudePID, site string, logger *slog.Logger) {
	if claudePID == "" {
		return
	}

	err := store.DeletePendingPlan(ctx, claudePID)
	if err != nil {
		logger.ErrorContext(ctx, "failed to delete pending plan",
			slog.String("site", site),
			slog.Any("error", err),
		)
	}
}

// planModeBlockReason is the Stop block message used while a session
// is between EnterPlanMode and an approved ExitPlanMode. Plan path is
// not yet recorded at this point so the message is a literal string.
//
// The wording deliberately tells Claude what to do (continue, ask, or
// exit plan mode) without disclosing what specifically releases the
// gate. Spelling out the unlock condition tends to make the model
// optimize for that condition rather than for the work it represents.
const planModeBlockReason = "You are in plan mode. Continue working on the plan," +
	" call AskUserQuestion if you need input from the user, or call" +
	" ExitPlanMode when the plan is ready."

func handleExitPlanModePre(
	ctx context.Context,
	input []byte,
	stdout io.Writer,
	store *state.Store,
	cfg config,
	workDir string,
	logger *slog.Logger,
) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if h.SessionID == "" {
		return nil
	}

	planPath := ""
	if h.ToolInput != nil {
		if p, ok := h.ToolInput["planFilePath"].(string); ok {
			planPath = p
		}
	}

	// skipPlanReview disables the deny-once gate entirely; the counter
	// exists only to implement it, so the increment is skipped too and
	// count stays 0 (a meaningful signal in the allow log below).
	var count int

	if !cfg.skipPlanReview {
		count, err = store.IncrementExitPlanCount(ctx, h.SessionID)
		if err != nil {
			// Fail-closed: silently swallowing this error would let the first
			// ExitPlanMode call through without ever invoking plan-reviewer.
			logger.Error("failed to increment exit plan count", slog.Any("error", err))
			return writeDecision(stdout, hook.Deny("plan-guard store unavailable, please retry"))
		}

		if count == 1 {
			reason := "Before exiting plan mode, run the plan-reviewer agent to review the plan"
			if planPath != "" {
				reason += " at " + planPath + ". Pass it the plan file path."
			} else {
				reason += "."
			}

			reason += " After review is complete and any feedback has been addressed, call ExitPlanMode again."

			logger.Info("denied ExitPlanMode (first call)", slog.String("session", h.SessionID))

			return writeDecision(stdout, hook.Deny(reason))
		}
	}

	// Record plan path and baseline SHA when allowing ExitPlanMode through.
	// PostToolUse does not fire for plan-mode control tools, so we record
	// here on the second (approved) call instead.
	g := &git.Runner{Dir: workDir}

	baseSHA, err := g.HeadSHA(ctx)
	if err != nil {
		logger.Warn("failed to get HEAD SHA", slog.Any("error", err))
	}

	err = store.SetPlanPath(ctx, h.SessionID, planPath, baseSHA)
	if err != nil {
		// Fail-closed: if we don't record the plan path, the Stop hook
		// won't know a plan is pending and won't ask for implementation
		// review. The counter is already bumped; the user's retry will
		// reach count == 3 and re-attempt SetPlanPath.
		logger.Error("failed to set plan path", slog.Any("error", err))
		return writeDecision(stdout, hook.Deny("plan-guard store unavailable, please retry"))
	}

	// Fail-open: at worst Stop will keep blocking with the plan-mode
	// message after ExitPlanMode succeeded; the user's recovery path
	// is the stop_hook_active escape hatch in handleStop.
	err = store.SetInPlanMode(ctx, h.SessionID, false)
	if err != nil {
		logger.Error("failed to clear in_plan_mode", slog.Any("error", err))
	}

	// Fail-open: pending_plans is the claude_pid-keyed handoff that
	// bridges a `/clear` plan-accept. A failure here only degrades
	// option-1 (clear-context) plan accepts; option-2 still works via
	// the session-keyed path above. Fail-closing would cause spurious
	// denials. Empty claudePID disables the handoff entirely (see
	// config.claudePID).
	if cfg.claudePID != "" {
		overwroteFresh, err := store.SetPendingPlan(ctx, cfg.claudePID, planPath, baseSHA)
		if err != nil {
			logger.ErrorContext(ctx, "failed to set pending plan", slog.Any("error", err))
		} else if overwroteFresh {
			logger.InfoContext(ctx, "overwrote fresh pending plan; same window re-planned within 60s without consuming previous handoff",
				slog.String("session", h.SessionID),
				slog.String("claude_pid", cfg.claudePID),
				slog.String("plan_path", planPath),
			)
		}
	}

	logger.Info("allowed ExitPlanMode, recorded plan path",
		slog.String("session", h.SessionID),
		slog.Int("count", count),
		slog.String("plan_path", planPath),
		slog.String("base_sha", baseSHA),
	)

	return nil
}

// handlePostAskUserQuestion runs on PostToolUse:AskUserQuestion.
// When the question's option labels identify it as the Stop-gate
// question, it clears the session row, releasing the Stop gate for
// the rest of the plan cycle: the post-impl question fires once per
// cycle, and follow-up edits by the chosen review skills do not
// re-arm it. EnterPlanMode re-arms naturally via ResetSession plus
// the next approved ExitPlanMode recording a fresh plan_path.
// Fail-open throughout: parse errors, missing session, and store
// write errors all return nil; Stop will re-block on the next
// attempt, which is the recovery path.
//
// The signature deliberately omits an io.Writer: this handler must
// never produce updatedInput, because workmux's PostToolUse entry
// runs concurrently and only one hook per tool may mutate input.
// Expected tool_input shape (walk defensively with comma-ok):
//
//	{ "questions": [{ "options": [{"label": "..."}, ...] }, ...] }
func handlePostAskUserQuestion(
	ctx context.Context,
	input []byte,
	store *state.Store,
	cfg config,
	logger *slog.Logger,
) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if h.SessionID == "" {
		return nil
	}

	// Belt-and-suspenders against a misconfigured matcher that routes a
	// non-AskUserQuestion event here.
	if h.ToolName != "" && h.ToolName != "AskUserQuestion" {
		return nil
	}

	if !hasPostImplLabel(h.ToolInput, cfg.postImpl) {
		return nil
	}

	// Fail-open: on failure Stop re-blocks and re-asks the post-impl
	// question, which is the recovery path.
	err = store.ClearSession(ctx, h.SessionID)
	if err != nil {
		logger.Error("failed to clear session for post-impl AUQ", slog.Any("error", err))
		return nil
	}

	// The post-impl question being answered means the migration handoff
	// for this claude_pid is no longer needed.
	dropPendingPlan(ctx, store, cfg.claudePID, "post-impl AUQ", logger)

	logger.Info("cleared session for post-impl AUQ, Stop gate released",
		slog.String("session", h.SessionID),
	)

	return nil
}

// hasPostImplLabel walks tool_input["questions"][].options[].label and
// reports whether any label matches a [*postimpl.Catalog] label. All
// type assertions use comma-ok; malformed shapes are treated as "no
// match". An empty catalog matches nothing.
func hasPostImplLabel(toolInput map[string]any, cat *postimpl.Catalog) bool {
	if toolInput == nil {
		return false
	}

	questions, ok := toolInput["questions"].([]any)
	if !ok {
		return false
	}

	for _, q := range questions {
		qMap, ok := q.(map[string]any)
		if !ok {
			continue
		}

		options, ok := qMap["options"].([]any)
		if !ok {
			continue
		}

		for _, opt := range options {
			optMap, ok := opt.(map[string]any)
			if !ok {
				continue
			}

			label, ok := optMap["label"].(string)
			if !ok {
				continue
			}

			if cat.HasLabel(label) {
				return true
			}
		}
	}

	return false
}

func handleEnterPlanMode(ctx context.Context, input []byte, store *state.Store, claudePID string, logger *slog.Logger) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if h.SessionID == "" {
		return nil
	}

	// Fail-open: stale counter means at most one extra ExitPlanMode deny,
	// which the user already recovers from via the normal deny-then-allow
	// flow. Fail-closed would prevent the user from entering plan mode.
	err = store.ResetSession(ctx, h.SessionID)
	if err != nil {
		logger.Error("failed to reset session", slog.Any("error", err))
	}

	// Fail-open: if the bit doesn't get set, Stop falls through to the
	// existing post-impl path. The user loses the plan-mode block but
	// still gets the deny-on-EnterPlanMode workflow.
	err = store.SetInPlanMode(ctx, h.SessionID, true)
	if err != nil {
		logger.Error("failed to set in_plan_mode", slog.Any("error", err))
	}

	// EnterPlanMode signals a fresh plan; abandon this window's stale
	// handoff. Fail-open — see SetPendingPlan rationale.
	dropPendingPlan(ctx, store, claudePID, "EnterPlanMode", logger)

	logger.Info("reset session for plan mode", slog.String("session", h.SessionID))

	return nil
}

// handleUserPromptSubmit clears the session when the user invokes a
// configured wrap-up skill (e.g. /commit, /merge). After the clear,
// Stop allows through normally because Session() re-INSERTs the row
// with all defaults (in_plan_mode=0, empty plan_path).
//
// Fail-open: if the parse fails or the clear errors, the user's
// recovery path is the stop_hook_active escape hatch — Claude Code
// retries the Stop hook with that flag set after the next block, and
// handleStop's existing escape hatch will clear and allow.
func handleUserPromptSubmit(
	ctx context.Context,
	input []byte,
	store *state.Store,
	cfg config,
	logger *slog.Logger,
) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if h.SessionID == "" {
		return nil
	}

	skill, ok := matchCommitPrompt(h.Prompt, cfg.commitSkills)
	if !ok {
		return nil
	}

	err = store.ClearSession(ctx, h.SessionID)
	if err != nil {
		// Fail-open: leaves the user behind a still-active gate, but
		// stop_hook_active in handleStop is the documented recovery.
		logger.Error("failed to clear session for wrap-up skill",
			slog.String("session", h.SessionID),
			slog.String("skill", skill),
			slog.Any("error", err),
		)

		return nil
	}

	// Wrap-up skill ends the implementation cycle; drop this window's
	// handoff so it cannot leak onto the next session.
	dropPendingPlan(ctx, store, cfg.claudePID, "wrap-up skill", logger)

	logger.Info("cleared session for wrap-up skill",
		slog.String("session", h.SessionID),
		slog.String("skill", skill),
	)

	return nil
}

// matchCommitPrompt reports whether prompt opens with one of the
// configured wrap-up skill invocations (e.g. "/commit", "/merge foo").
// Returns the matched skill name (without the leading slash) on hit.
//
// Matching is anchored on the first whitespace-delimited token of the
// first line, with a required leading slash. Comparison is case
// sensitive — Claude Code slash commands are registered lowercase and
// case-sensitive — so "/Commit" does not match "/commit", and a
// conversational mention like "please /commit this" never matches
// (the first token is "please").
func matchCommitPrompt(prompt string, skills []string) (string, bool) {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return "", false
	}

	if i := strings.IndexByte(trimmed, '\n'); i >= 0 {
		trimmed = trimmed[:i]
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", false
	}

	token := fields[0]
	if !strings.HasPrefix(token, "/") {
		return "", false
	}

	name := token[1:]
	if !slices.Contains(skills, name) {
		return "", false
	}

	return name, true
}

// parseCommitSkills decodes the JSON payload passed via --commit-skills
// into a list of skill names. An empty input yields nil (valid: no
// wrap-up skills configured); malformed JSON returns an error so
// wrapper misconfiguration is loud.
func parseCommitSkills(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}

	var skills []string

	err := json.Unmarshal([]byte(s), &skills)
	if err != nil {
		return nil, fmt.Errorf("decoding commit skills JSON: %w", err)
	}

	return skills, nil
}

func handleStop(
	ctx context.Context,
	input []byte,
	stdout io.Writer,
	store *state.Store,
	cfg config,
	logger *slog.Logger,
) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if h.SessionID == "" {
		return nil
	}

	// Escape hatch: if already retrying after a block, clear and allow.
	// ClearSession is fail-open so the escape hatch works even when the
	// store is unavailable — the user has explicitly overridden the guard.
	if h.StopHookActive {
		logger.Info("stop_hook_active, clearing session", slog.String("session", h.SessionID))

		err := store.ClearSession(ctx, h.SessionID)
		if err != nil {
			logger.WarnContext(ctx, "failed to clear session", slog.Any("error", err))
		}

		// Drop this window's handoff alongside the session clear.
		dropPendingPlan(ctx, store, cfg.claudePID, "stop_hook_active", logger)

		return nil
	}

	_, planPath, baseSHA, err := store.Session(ctx, h.SessionID)
	if err != nil {
		// Fail-closed: today this silently allows Stop through, bypassing
		// the post-impl review guard. Block instead so the user retries.
		// The stop_hook_active escape hatch above is preserved.
		logger.Error("failed to read session", slog.Any("error", err))
		return writeDecision(stdout, hook.Block("plan-guard store unavailable, please retry"))
	}

	// Fail-closed: same posture as the Session() error above. A stale
	// store should produce a block + retry, not a silent allow.
	inPlanMode, err := store.InPlanMode(ctx, h.SessionID)
	if err != nil {
		logger.Error("failed to read in_plan_mode", slog.Any("error", err))
		return writeDecision(stdout, hook.Block("plan-guard store unavailable, please retry"))
	}

	// Plan-mode block must run BEFORE the empty-plan-path allow:
	// EnterPlanMode sets in_plan_mode=1 with plan_path="" (only set on
	// the second/approved ExitPlanMode call), so falling through here
	// would silently allow Stop in plan mode.
	if inPlanMode {
		logger.Info("blocking stop in plan mode", slog.String("session", h.SessionID))
		return writeDecision(stdout, hook.Block(planModeBlockReason))
	}

	// An answered post-impl AskUserQuestion lands here too: the handler
	// clears the session row, so Session() re-INSERTs it with an empty
	// plan_path and Stop allows for the rest of the plan cycle.
	if planPath == "" {
		logger.Info("no plan path, allowing through", slog.String("session", h.SessionID))
		return nil
	}

	reason := cfg.postImpl.BuildAskReason(planPath, baseSHA)

	logger.Info("blocking stop for post-impl question",
		slog.String("session", h.SessionID),
		slog.String("plan_path", planPath),
		slog.String("base_sha", baseSHA),
	)

	return writeDecision(stdout, hook.Block(reason))
}

// handleSessionStart migrates a claude_pid-keyed pending plan onto the
// new session_id when a session starts (typically after `/clear`
// accepted from the plan-accept dialog).
//
// The hook input does not link the new session_id to the pre-clear
// session; see anthropics/claude-code#29094 (closed not_planned). The
// session_id is freshly minted by `/clear`, so it cannot bridge the
// pre- and post-clear processes. PPID, on the other hand, is the
// OS-process identity of the Claude Code window itself, and `/clear`
// does not fork a new window: the same window keeps the same PPID
// through a `/clear`, while two windows carry distinct PPIDs. Joining
// on claude_pid therefore lets each window consume only its own
// handoff. The TTL bounds stuck rows (SessionStart hook crashed, DB
// busy, and so on) so they expire before being mistaken for a fresh
// handoff.
//
// h.Source is intentionally ignored: the TTL alone is sufficient,
// and the plan-accept-clear path may emit either `source=clear` or
// `source=startup` depending on Claude Code version.
//
// All store operations are fail-open. A migration failure leaves the
// new session without a plan_path, so Stop allows through. Option-2
// (no clear) is unaffected. An empty claudePID disables the handoff
// entirely (no Claude parent), matching the kubeconfig empty-guard in
// config.
func handleSessionStart(
	ctx context.Context,
	input []byte,
	store *state.Store,
	claudePID string,
	logger *slog.Logger,
) error {
	h, err := hook.ParseInput(input)
	if err != nil {
		logger.WarnContext(ctx, "failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if h.SessionID == "" || claudePID == "" {
		return nil
	}

	planPath, baseSHA, found, err := store.ConsumePendingPlan(ctx, claudePID, pendingPlanTTLSeconds)
	if err != nil {
		logger.ErrorContext(ctx, "failed to consume pending plan", slog.Any("error", err))
		return nil
	}

	if !found {
		return nil
	}

	err = store.SetPlanPath(ctx, h.SessionID, planPath, baseSHA)
	if err != nil {
		logger.ErrorContext(ctx, "failed to migrate plan path to new session", slog.Any("error", err))
		return nil
	}

	logger.InfoContext(ctx, "migrated pending plan to new session",
		slog.String("session", h.SessionID),
		slog.String("source", h.Source),
		slog.String("plan_path", planPath),
	)

	return nil
}
