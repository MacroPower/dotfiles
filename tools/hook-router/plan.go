package main

import (
	"context"
	"io"
	"log/slog"
)

func handleExitPlanModePre(
	ctx context.Context,
	input []byte,
	stdout io.Writer,
	store *Store,
	workDir string,
	logger *slog.Logger,
) error {
	hook, err := parseHookInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if hook.SessionID == "" {
		return nil
	}

	planPath := ""
	if hook.ToolInput != nil {
		if p, ok := hook.ToolInput["planFilePath"].(string); ok {
			planPath = p
		}
	}

	count, err := store.IncrementExitPlanCount(ctx, hook.SessionID)
	if err != nil {
		// Fail-closed: silently swallowing this error would let the first
		// ExitPlanMode call through without ever invoking plan-reviewer.
		logger.Error("failed to increment exit plan count", slog.Any("error", err))
		return encodeJSON(stdout, denyResponse("plan-guard store unavailable, please retry"))
	}

	if count == 1 {
		reason := "Before exiting plan mode, run the plan-reviewer agent to review the plan"
		if planPath != "" {
			reason += " at " + planPath + ". Pass it the plan file path."
		} else {
			reason += "."
		}

		reason += " After review is complete and any feedback has been addressed, call ExitPlanMode again."

		logger.Info("denied ExitPlanMode (first call)", slog.String("session", hook.SessionID))

		return encodeJSON(stdout, denyResponse(reason))
	}

	// Record plan path and baseline SHA when allowing ExitPlanMode through.
	// PostToolUse does not fire for plan-mode control tools, so we record
	// here on the second (approved) call instead.
	git := &GitRunner{Dir: workDir}

	baseSHA, err := git.HeadSHA(ctx)
	if err != nil {
		logger.Warn("failed to get HEAD SHA", slog.Any("error", err))
	}

	err = store.SetPlanPath(ctx, hook.SessionID, planPath, baseSHA)
	if err != nil {
		// Fail-closed: if we don't record the plan path, the Stop hook
		// won't know a plan is pending and won't ask for implementation
		// review. The counter is already bumped; the user's retry will
		// reach count == 3 and re-attempt SetPlanPath.
		logger.Error("failed to set plan path", slog.Any("error", err))
		return encodeJSON(stdout, denyResponse("plan-guard store unavailable, please retry"))
	}

	logger.Info("allowed ExitPlanMode, recorded plan path",
		slog.String("session", hook.SessionID),
		slog.Int("count", count),
		slog.String("plan_path", planPath),
		slog.String("base_sha", baseSHA),
	)

	return nil
}

// reviewerAgentTypes lists subagent_type values whose spawn should
// capture a git fingerprint so the Stop hook can skip redundant blocks.
var reviewerAgentTypes = map[string]bool{
	"implementation-reviewer": true,
	"plan-reviewer":           true,
}

func handleAgentPre(
	ctx context.Context,
	input []byte,
	store *Store,
	workDir string,
	logger *slog.Logger,
) error {
	hook, err := parseHookInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if hook.SessionID == "" {
		return nil
	}

	agentType, _ := hook.ToolInput["subagent_type"].(string)
	if !reviewerAgentTypes[agentType] {
		return nil
	}

	git := &GitRunner{Dir: workDir}

	headSHA, wtHash, err := git.Fingerprint(ctx)
	if err != nil {
		logger.Warn("failed to get fingerprint for reviewer", slog.Any("error", err))
		return nil
	}

	// Fail-open: this is a pure optimization (Stop short-circuit when the
	// reviewer already ran). On failure Stop still blocks normally and
	// prompts for another reviewer run. Fail-closed here would deny a
	// legitimate reviewer spawn and cascade into Stop blocking anyway.
	err = store.SetReviewFingerprint(ctx, hook.SessionID, headSHA, wtHash)
	if err != nil {
		logger.Error("failed to set review fingerprint", slog.Any("error", err))
	}

	logger.Info("recorded review fingerprint",
		slog.String("session", hook.SessionID),
		slog.String("agent_type", agentType),
		slog.String("head_sha", headSHA),
	)

	return nil
}

func handleEnterPlanMode(ctx context.Context, input []byte, store *Store, logger *slog.Logger) error {
	hook, err := parseHookInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if hook.SessionID == "" {
		return nil
	}

	// Fail-open: stale counter means at most one extra ExitPlanMode deny,
	// which the user already recovers from via the normal deny-then-allow
	// flow. Fail-closed would prevent the user from entering plan mode.
	err = store.ResetSession(ctx, hook.SessionID)
	if err != nil {
		logger.Error("failed to reset session", slog.Any("error", err))
	}

	logger.Info("reset session for plan mode", slog.String("session", hook.SessionID))

	return nil
}

func handleStop(
	ctx context.Context,
	input []byte,
	stdout io.Writer,
	store *Store,
	workDir string,
	logger *slog.Logger,
) error {
	hook, err := parseHookInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if hook.SessionID == "" {
		return nil
	}

	// Escape hatch: if already retrying after a block, clear and allow.
	// ClearSession is fail-open so the escape hatch works even when the
	// store is unavailable — the user has explicitly overridden the guard.
	if hook.StopHookActive {
		logger.Info("stop_hook_active, clearing session", slog.String("session", hook.SessionID))

		err := store.ClearSession(ctx, hook.SessionID)
		if err != nil {
			logger.WarnContext(ctx, "failed to clear session", slog.Any("error", err))
		}

		return nil
	}

	_, planPath, baseSHA, err := store.Session(ctx, hook.SessionID)
	if err != nil {
		// Fail-closed: today this silently allows Stop through, bypassing
		// the implementation-reviewer guard. Block instead so the user
		// retries. The stop_hook_active escape hatch above is preserved.
		logger.Error("failed to read session", slog.Any("error", err))
		return encodeJSON(stdout, blockResponse("plan-guard store unavailable, please retry"))
	}

	if planPath == "" {
		logger.Info("no plan path, allowing through", slog.String("session", hook.SessionID))
		return nil
	}

	git := &GitRunner{Dir: workDir}

	changed, err := git.HasChanges(ctx, baseSHA)
	if err != nil {
		logger.Warn("failed to check for changes", slog.Any("error", err))
		return nil
	}

	if !changed {
		logger.Info("no code changes, allowing through",
			slog.String("session", hook.SessionID),
			slog.String("plan_path", planPath),
		)

		return nil
	}

	// Fail-open on the fingerprint read: the fallback is the normal block
	// response below, which is more useful than a "store unavailable"
	// message. A failed read just means we miss the short-circuit.
	reviewHead, reviewWT, fpErr := store.ReviewFingerprint(ctx, hook.SessionID)
	if fpErr != nil {
		logger.Warn("failed to read review fingerprint", slog.Any("error", fpErr))
	}

	if reviewHead != "" {
		currentHead, currentWT, fpErr := git.Fingerprint(ctx)
		if fpErr != nil {
			logger.Warn("failed to get current fingerprint", slog.Any("error", fpErr))
		} else if currentHead == reviewHead && currentWT == reviewWT {
			logger.Info("reviewer already ran against current state, allowing",
				slog.String("session", hook.SessionID),
				slog.String("head_sha", currentHead),
			)

			return nil
		}
	}

	reason := "Before finishing, run the implementation-reviewer agent to review" +
		" your code changes against the plan at " + planPath + "." +
		" The pre-implementation baseline commit is " + baseSHA + "." +
		" Pass it both the plan file path and the base SHA." +
		" If your implementation deviated from the original plan," +
		" explain your reasoning to the reviewer." +
		" If the reviewer finds issues, fix them and re-run the reviewer." +
		" Repeat until you get LGTM, then you may stop."

	logger.Info("blocking stop for implementation review",
		slog.String("session", hook.SessionID),
		slog.String("plan_path", planPath),
		slog.String("base_sha", baseSHA),
	)

	return encodeJSON(stdout, blockResponse(reason))
}
