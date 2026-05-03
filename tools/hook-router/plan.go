package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
)

// pendingPlanTTLSeconds bounds how long a cwd-keyed pending plan
// handoff is honored after [handleExitPlanModePre] records it.
//
// In the Claude Code TUI, "Yes, clear context (...) and bypass
// permissions" is a single button. The sequence
//
//  1. PreToolUse:ExitPlanMode (count==2) writes the pending row
//  2. Claude Code clears context and creates the new session
//  3. SessionStart fires and consumes the row
//
// is atomic from the user's perspective — there is no separate "clear
// later" affordance — so the gap between (1) and (3) is sub-second
// system latency. A short TTL is therefore deterministic in practice
// while still preventing a stuck pending row (e.g. SessionStart hook
// crash, DB busy at the wrong moment) from attaching to an unrelated
// future session in the same cwd.
const pendingPlanTTLSeconds = 30

// resolveCwd returns [filepath.EvalSymlinks] of raw when the path
// resolves, falling back to raw with a warn-log when it does not.
// Callers use the resolved form as the pending_plans primary key so
// symlinked and real invocations of the same directory hit the same
// row.
func resolveCwd(raw string, logger *slog.Logger) string {
	if raw == "" {
		return ""
	}

	resolved, err := filepath.EvalSymlinks(raw)
	if err != nil {
		logger.Warn("failed to resolve cwd symlinks, using raw path",
			slog.String("cwd", raw),
			slog.Any("error", err),
		)

		return raw
	}

	return resolved
}

// dropPendingPlan is the fail-open cleanup used at lifecycle boundaries
// (EnterPlanMode, wrap-up skill, stop_hook_active escape, fingerprint
// short-circuit, post-impl AUQ recorded). It resolves the cwd, deletes
// the pending_plans row if any, and logs at Error on failure with the
// caller-supplied site tag. A no-op when cwd is empty.
func dropPendingPlan(ctx context.Context, store *Store, rawCwd, site string, logger *slog.Logger) {
	cwd := resolveCwd(rawCwd, logger)
	if cwd == "" {
		return
	}

	err := store.DeletePendingPlan(ctx, cwd)
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

	// Fail-open: at worst Stop will keep blocking with the plan-mode
	// message after ExitPlanMode succeeded; the user's recovery path
	// is the stop_hook_active escape hatch in handleStop.
	err = store.SetInPlanMode(ctx, hook.SessionID, false)
	if err != nil {
		logger.Error("failed to clear in_plan_mode", slog.Any("error", err))
	}

	// Fail-open: pending_plans is the cwd-keyed handoff that bridges a
	// `/clear` plan-accept. A failure here only degrades option-1
	// (clear-context) plan accepts; the session-keyed flow above still
	// works for option-2. Fail-closing would cause spurious denials.
	cwd := resolveCwd(hook.Cwd, logger)
	if cwd != "" {
		overwroteFresh, err := store.SetPendingPlan(ctx, cwd, planPath, baseSHA)
		if err != nil {
			logger.ErrorContext(ctx, "failed to set pending plan", slog.Any("error", err))
		} else if overwroteFresh {
			logger.WarnContext(ctx, "overwrote fresh pending plan; concurrent CC sessions in same cwd",
				slog.String("session", hook.SessionID),
				slog.String("cwd", cwd),
				slog.String("plan_path", planPath),
			)
		}
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

// BuildAskReason returns the unified Stop block-message used while a
// session is mid-implementation. The message has two branches:
//
//   - "If you have completed the implementation": instructs Claude to
//     call AskUserQuestion with the catalog's canonical option labels.
//   - "If you are not done": directs Claude to keep working, with
//     AskUserQuestion as the path for clarifying questions.
//
// Bullets render in catalog order (Nix list order, preserved through
// [builtins.toJSON]). When the catalog is empty the bullet section is
// suppressed and the "completed" branch falls back to a single sentence
// — production should never hit this path (mainErr logs a warning) but
// the invariant that callers can render without a nil-guard holds.
//
// Wording note: the message describes what Claude should do, not what
// the gate is checking. Disclosing the precise unlock condition (e.g.
// "Stop unlocks when a post-impl AUQ is answered against the current
// git state") tends to make the model optimize for the unlock rather
// than for the work the unlock is meant to gate.
func (c *PostImplCatalog) BuildAskReason(planPath, baseSHA string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are implementing the plan at %s (baseline: %s).\n\n",
		planPath, baseSHA)

	if len(c.agents) > 0 {
		b.WriteString("If you have completed the implementation, call AskUserQuestion" +
			" with the post-implementation review options below. Each option's" +
			" `label` MUST be exactly one of:\n")

		for _, a := range c.agents {
			fmt.Fprintf(&b, "  - %s: %s\n", a.Label, a.Description)
		}

		b.WriteString("After the user answers, run the chosen agents in an" +
			" appropriate order.\n\n")
	} else {
		b.WriteString("If you have completed the implementation, call AskUserQuestion" +
			" with the post-implementation review options provided by your" +
			" environment.\n\n")
	}

	b.WriteString("If you are not done, keep working. Call AskUserQuestion if you" +
		" need input from the user.")

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

	// The post-impl question being answered means the migration handoff
	// for this cwd is no longer needed.
	dropPendingPlan(ctx, store, hook.Cwd, "post-impl AUQ", logger)

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

	// Fail-open: if the bit doesn't get set, Stop falls through to the
	// existing post-impl path. The user loses the plan-mode block but
	// still gets the deny-on-EnterPlanMode workflow.
	err = store.SetInPlanMode(ctx, hook.SessionID, true)
	if err != nil {
		logger.Error("failed to set in_plan_mode", slog.Any("error", err))
	}

	// EnterPlanMode signals a fresh plan; abandon any stale handoff for
	// this cwd. Fail-open — see SetPendingPlan rationale.
	dropPendingPlan(ctx, store, hook.Cwd, "EnterPlanMode", logger)

	logger.Info("reset session for plan mode", slog.String("session", hook.SessionID))

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
	store *Store,
	cfg config,
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

	skill, ok := matchCommitPrompt(hook.Prompt, cfg.commitSkills)
	if !ok {
		return nil
	}

	err = store.ClearSession(ctx, hook.SessionID)
	if err != nil {
		// Fail-open: leaves the user behind a still-active gate, but
		// stop_hook_active in handleStop is the documented recovery.
		logger.Error("failed to clear session for wrap-up skill",
			slog.String("session", hook.SessionID),
			slog.String("skill", skill),
			slog.Any("error", err),
		)

		return nil
	}

	// Wrap-up skill ends the implementation cycle; drop any cwd-keyed
	// handoff so it cannot leak onto the next session in this directory.
	dropPendingPlan(ctx, store, hook.Cwd, "wrap-up skill", logger)

	logger.Info("cleared session for wrap-up skill",
		slog.String("session", hook.SessionID),
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

		// Drop any cwd-keyed handoff alongside the session clear.
		dropPendingPlan(ctx, store, hook.Cwd, "stop_hook_active", logger)

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

	// Fail-closed: same posture as the Session() error above. A stale
	// store should produce a block + retry, not a silent allow.
	inPlanMode, err := store.InPlanMode(ctx, hook.SessionID)
	if err != nil {
		logger.Error("failed to read in_plan_mode", slog.Any("error", err))
		return encodeJSON(stdout, blockResponse("plan-guard store unavailable, please retry"))
	}

	// Plan-mode block must run BEFORE the empty-plan-path allow:
	// EnterPlanMode sets in_plan_mode=1 with plan_path="" (only set on
	// the second/approved ExitPlanMode call), so falling through here
	// would silently allow Stop in plan mode.
	if inPlanMode {
		logger.Info("blocking stop in plan mode", slog.String("session", hook.SessionID))
		return encodeJSON(stdout, blockResponse(planModeBlockReason))
	}

	if planPath == "" {
		logger.Info("no plan path, allowing through", slog.String("session", hook.SessionID))
		return nil
	}

	git := &GitRunner{Dir: workDir}

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

			// Implementation review is done for this state; the cwd
			// handoff is no longer needed.
			dropPendingPlan(ctx, store, hook.Cwd, "fingerprint match", logger)

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

// handleSessionStart migrates a cwd-keyed pending plan onto the new
// session_id when a session starts (typically after `/clear` accepted
// from the plan-accept dialog).
//
// The hook input does not link the new session_id to the pre-clear
// session — see anthropics/claude-code#29094 (closed not_planned). The
// only available join key is cwd, with a TTL bound to prevent stale
// rows from attaching to unrelated future sessions in the same
// directory. hook.Source is intentionally ignored: the TTL alone is
// sufficient, and the plan-accept-clear path may emit either
// `source=clear` or `source=startup` depending on Claude Code version.
//
// All store operations are fail-open. A migration failure leaves the
// new session without a plan_path, so Stop allows through (the original
// bug behavior). Option-2 (no clear) is unaffected.
func handleSessionStart(
	ctx context.Context,
	input []byte,
	store *Store,
	logger *slog.Logger,
) error {
	hook, err := parseHookInput(input)
	if err != nil {
		logger.WarnContext(ctx, "failed to parse hook input", slog.Any("error", err))
		return nil
	}

	if hook.SessionID == "" || hook.Cwd == "" {
		return nil
	}

	cwd := resolveCwd(hook.Cwd, logger)

	planPath, baseSHA, found, err := store.ConsumePendingPlan(ctx, cwd, pendingPlanTTLSeconds)
	if err != nil {
		logger.ErrorContext(ctx, "failed to consume pending plan", slog.Any("error", err))
		return nil
	}

	if !found {
		return nil
	}

	err = store.SetPlanPath(ctx, hook.SessionID, planPath, baseSHA)
	if err != nil {
		logger.ErrorContext(ctx, "failed to migrate plan path to new session", slog.Any("error", err))
		return nil
	}

	logger.InfoContext(ctx, "migrated pending plan to new session",
		slog.String("session", hook.SessionID),
		slog.String("source", hook.Source),
		slog.String("cwd", cwd),
		slog.String("plan_path", planPath),
	)

	return nil
}
