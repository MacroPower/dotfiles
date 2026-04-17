package main

import (
	"context"
	"io"
	"log/slog"
)

func handleExitPlanModePre(input []byte, stdout io.Writer, store *Store, workDir string, logger *slog.Logger) error {
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

	count, err := store.IncrementExitPlanCount(context.Background(), hook.SessionID)
	if err != nil {
		logger.Error("failed to increment exit plan count", slog.Any("error", err))
		return nil
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

	baseSHA, err := git.HeadSHA(context.Background())
	if err != nil {
		logger.Warn("failed to get HEAD SHA", slog.Any("error", err))
	}

	if err := store.SetPlanPath(context.Background(), hook.SessionID, planPath, baseSHA); err != nil {
		logger.Error("failed to set plan path", slog.Any("error", err))
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

func handleAgentPre(input []byte, stdout io.Writer, store *Store, workDir string, logger *slog.Logger) error {
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

	headSHA, wtHash, err := git.Fingerprint(context.Background())
	if err != nil {
		logger.Warn("failed to get fingerprint for reviewer", slog.Any("error", err))
		return nil
	}

	if err := store.SetReviewFingerprint(context.Background(), hook.SessionID, headSHA, wtHash); err != nil {
		logger.Error("failed to set review fingerprint", slog.Any("error", err))
	}

	logger.Info("recorded review fingerprint",
		slog.String("session", hook.SessionID),
		slog.String("agent_type", agentType),
		slog.String("head_sha", headSHA),
	)

	return nil
}

func handleEnterPlanMode(input []byte, store *Store, logger *slog.Logger) error {
	hook, err := parseHookInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if hook.SessionID == "" {
		return nil
	}

	if err := store.ResetSession(context.Background(), hook.SessionID); err != nil {
		logger.Error("failed to reset session", slog.Any("error", err))
	}

	logger.Info("reset session for plan mode", slog.String("session", hook.SessionID))

	return nil
}

func handleStop(input []byte, stdout io.Writer, store *Store, workDir string, logger *slog.Logger) error {
	hook, err := parseHookInput(input)
	if err != nil {
		logger.Warn("failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if hook.SessionID == "" {
		return nil
	}

	// Escape hatch: if already retrying after a block, clear and allow.
	if hook.StopHookActive {
		logger.Info("stop_hook_active, clearing session", slog.String("session", hook.SessionID))
		_ = store.ClearSession(context.Background(), hook.SessionID)

		return nil
	}

	_, planPath, baseSHA, err := store.Session(context.Background(), hook.SessionID)
	if err != nil {
		logger.Error("failed to read session", slog.Any("error", err))
		return nil
	}

	if planPath == "" {
		logger.Info("no plan path, allowing through", slog.String("session", hook.SessionID))
		return nil
	}

	git := &GitRunner{Dir: workDir}

	changed, err := git.HasChanges(context.Background(), baseSHA)
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

	// Check whether a reviewer already ran against the current state.
	reviewHead, reviewWT, fpErr := store.ReviewFingerprint(context.Background(), hook.SessionID)
	if fpErr != nil {
		logger.Warn("failed to read review fingerprint", slog.Any("error", fpErr))
	}

	if reviewHead != "" {
		currentHead, currentWT, fpErr := git.Fingerprint(context.Background())
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
