package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
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

// PostImplAgent describes one post-implementation agent Claude may run
// when a plan's Stop gate fires. The shape mirrors the JSON emitted by
// the Nix-side `postImplAgents` list: field order and tag casing must
// match or [builtins.toJSON] output will silently unmarshal to zero
// values.
type PostImplAgent struct {
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases,omitempty"`
}

// PostImplCatalog bundles a list of [PostImplAgent] entries (used for
// block-message rendering) with the derived label set (used for O(1)
// validation of AskUserQuestion option labels). Construct with
// [NewPostImplCatalog] so the two stay in sync.
type PostImplCatalog struct {
	labels map[string]bool
	agents []PostImplAgent
}

// NewPostImplCatalog builds a [*PostImplCatalog] from the given agents,
// folding each agent's canonical label and aliases into the validation
// set. Duplicates across entries are not deduped: the Nix list is the
// source of truth and is expected to stay clean.
func NewPostImplCatalog(agents []PostImplAgent) *PostImplCatalog {
	labels := make(map[string]bool, len(agents))

	for _, a := range agents {
		labels[a.Label] = true
		for _, alias := range a.Aliases {
			labels[alias] = true
		}
	}

	return &PostImplCatalog{agents: agents, labels: labels}
}

// HasLabel reports whether label matches any canonical label or alias
// in the catalog.
func (c *PostImplCatalog) HasLabel(label string) bool { return c.labels[label] }

// Empty reports whether the catalog has no agents.
func (c *PostImplCatalog) Empty() bool { return len(c.agents) == 0 }

// BuildAskReason returns the Stop block-message that instructs Claude
// to call AskUserQuestion with the catalog's canonical option labels.
// Bullets are rendered in catalog order (Nix list order, preserved
// through [builtins.toJSON]).
func (c *PostImplCatalog) BuildAskReason(planPath, baseSHA string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Before finishing, call AskUserQuestion to decide which post-implementation"+
		" agents should run against the plan at %s. Pre-implementation"+
		" baseline commit: %s.\n\n"+
		"Ask one multi-select question. Each option's `label` MUST be exactly one"+
		" of (free-form labels will not satisfy the gate):\n", planPath, baseSHA)

	for _, a := range c.agents {
		fmt.Fprintf(&b, "  - %s: %s\n", a.Label, a.Description)
	}

	b.WriteString("\nAfter the user answers, choose an appropriate order and concurrency for" +
		" the selected agents (e.g. review before committing; independent passes" +
		" can run in parallel). Run them, then attempt to stop. If a selected" +
		" agent modified code, Stop will re-block; call AskUserQuestion again" +
		" (offering the same options) so the user can decide whether to run any" +
		" agent against the new state (e.g. re-run implementation-reviewer after" +
		" simplify).")

	return b.String()
}

// parsePostImplAgents decodes the JSON payload passed via
// --post-impl-agents into a [*PostImplCatalog]. An empty input yields
// an empty catalog (valid for tests and early-startup paths);
// malformed JSON returns an error so wrapper misconfiguration is loud.
func parsePostImplAgents(s string) (*PostImplCatalog, error) {
	if s == "" {
		return NewPostImplCatalog(nil), nil
	}

	var agents []PostImplAgent

	err := json.Unmarshal([]byte(s), &agents)
	if err != nil {
		return nil, fmt.Errorf("decoding post-impl agents JSON: %w", err)
	}

	return NewPostImplCatalog(agents), nil
}

// handlePostAskUserQuestion runs on PostToolUse:AskUserQuestion.
// When the question's option labels identify it as the Stop-gate
// question, it captures the current git fingerprint so the Stop
// hook can short-circuit. Fail-open throughout: parse errors,
// missing session, fingerprint errors, and store write errors all
// return nil; Stop will re-block on the next attempt, which is the
// recovery path.
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
	store *Store,
	cfg config,
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

	// Belt-and-suspenders against a misconfigured matcher that routes a
	// non-AskUserQuestion event here.
	if hook.ToolName != "" && hook.ToolName != "AskUserQuestion" {
		return nil
	}

	if !hasPostImplLabel(hook.ToolInput, cfg.postImpl) {
		return nil
	}

	git := &GitRunner{Dir: workDir}

	headSHA, wtHash, err := git.Fingerprint(ctx)
	if err != nil {
		logger.Warn("failed to get fingerprint for ask", slog.Any("error", err))
		return nil
	}

	// Fail-open: this is a pure optimization (Stop short-circuit when the
	// user already answered the post-impl question against current state).
	// On failure Stop still blocks normally and prompts again.
	err = store.SetAskFingerprint(ctx, hook.SessionID, headSHA, wtHash)
	if err != nil {
		logger.Error("failed to set ask fingerprint", slog.Any("error", err))
		return nil
	}

	logger.Info("recorded ask fingerprint",
		slog.String("session", hook.SessionID),
		slog.String("head_sha", headSHA),
	)

	return nil
}

// hasPostImplLabel walks tool_input["questions"][].options[].label and
// reports whether any label matches a [*PostImplCatalog] label or
// alias. All type assertions use comma-ok; malformed shapes are
// treated as "no match". An empty catalog matches nothing.
func hasPostImplLabel(toolInput map[string]any, cat *PostImplCatalog) bool {
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
	cfg config,
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
	askHead, askWT, fpErr := store.AskFingerprint(ctx, hook.SessionID)
	if fpErr != nil {
		logger.Warn("failed to read ask fingerprint", slog.Any("error", fpErr))
	}

	if askHead != "" {
		currentHead, currentWT, fpErr := git.Fingerprint(ctx)
		if fpErr != nil {
			logger.Warn("failed to get current fingerprint", slog.Any("error", fpErr))
		} else if currentHead == askHead && currentWT == askWT {
			logger.Info("post-impl question already answered for current state, allowing",
				slog.String("session", hook.SessionID),
				slog.String("head_sha", currentHead),
			)

			return nil
		}
	}

	reason := cfg.postImpl.BuildAskReason(planPath, baseSHA)

	logger.Info("blocking stop for post-impl question",
		slog.String("session", hook.SessionID),
		slog.String("plan_path", planPath),
		slog.String("base_sha", baseSHA),
	)

	return encodeJSON(stdout, blockResponse(reason))
}
