package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/compact"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/hook"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/postimpl"
)

func makeHookJSON(t *testing.T, in hook.Input) []byte {
	t.Helper()

	b, err := json.Marshal(in)
	require.NoError(t, err)

	return b
}

// testCatalog returns a [*postimpl.Catalog] matching the canonical
// skills declared in home/claude.nix. Tests that exercise
// handlePostAskUserQuestion or handleStop pass this via cfg so the
// Nix-driven validation and block-message rendering paths are the
// same shape as production.
func testCatalog() *postimpl.Catalog {
	return postimpl.New([]postimpl.Skill{
		{Label: "/review-implementation", Description: "Review code changes against the plan."},
		{Label: "/simplify", Description: "Review and simplify the implemented code."},
		{Label: "/humanize", Description: "Clean up AI writing patterns in any prose/docs that changed."},
		{Label: "/commit", Description: "Wrap up the cycle by creating a git commit."},
	})
}

// cfg is the shared [config] used by every test in this file that
// exercises handleStop or handlePostAskUserQuestion. Declaring it at
// package scope keeps handler call sites uniform; tests that need a
// different shape (e.g. empty catalog for degraded-mode assertions)
// declare their own local cfg, which shadows this one.
var cfg = config{
	postImpl:     testCatalog(),
	commitSkills: []string{"commit", "commit-push-pr", "merge"},
	claudePID:    testPID,
	compactor:    compact.New(compact.Config{}),
}

func TestHandleExitPlanModePre_FirstCallDenies(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

	hso := result["hookSpecificOutput"].(map[string]any)
	assert.Equal(t, "deny", hso["permissionDecision"])
	assert.Contains(t, hso["permissionDecisionReason"], "plan-reviewer")
	assert.Contains(t, hso["permissionDecisionReason"], "/path/plan.md")

	// plan_path should NOT be set yet (only on allow).
	_, planPath, _, err := store.Session(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "", planPath)
}

func TestHandleExitPlanModePre_SecondCallAllowsAndRecords(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// First call: denied.
	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)
	assert.NotEmpty(t, stdout.Bytes())

	// Second call: allowed (no output), and records plan path.
	stdout.Reset()

	err = handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())

	// Verify plan_path and base_sha are now recorded.
	_, planPath, baseSHA, err := store.Session(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "/path/plan.md", planPath)
	assert.Len(t, baseSHA, 40)
}

func TestHandleExitPlanModePre_SkipPlanReviewAllowsFirstCall(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	cfg := config{
		postImpl:       testCatalog(),
		commitSkills:   []string{"commit", "commit-push-pr", "merge"},
		claudePID:      testPID,
		skipPlanReview: true,
	}

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// First call: allowed (no deny output), and records plan path.
	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(), "skip-plan-review must allow the first ExitPlanMode call")

	// Bookkeeping still ran: plan_path and base_sha are recorded.
	_, planPath, baseSHA, err := store.Session(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "/path/plan.md", planPath)
	assert.Len(t, baseSHA, 40)
}

func TestHandleExitPlanModePre_NoPlanPath(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

	hso := result["hookSpecificOutput"].(map[string]any)
	assert.Contains(t, hso["permissionDecisionReason"], "plan-reviewer")
}

func TestHandleExitPlanModePre_EmptySessionAllows(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, hook.Input{
		SessionID: "",
		ToolInput: map[string]any{"planFilePath": "/plan.md"},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())
}

func TestHandleEnterPlanMode_ResetsSession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	// Set some state.
	_, _ = store.IncrementExitPlanCount(ctx, "s1")
	_ = store.SetPlanPath(ctx, "s1", "/plan.md", "sha1")

	input := makeHookJSON(t, hook.Input{SessionID: "s1"})

	err := handleEnterPlanMode(t.Context(), input, store, testPID, logger)
	require.NoError(t, err)

	count, planPath, baseSHA, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, "", planPath)
	assert.Equal(t, "", baseSHA)
}

func TestEnterPlanMode_SetsInPlanMode(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	input := makeHookJSON(t, hook.Input{SessionID: "s1"})

	err := handleEnterPlanMode(t.Context(), input, store, testPID, logger)
	require.NoError(t, err)

	inPlanMode, err := store.InPlanMode(ctx, "s1")
	require.NoError(t, err)
	assert.True(t, inPlanMode)
}

func TestExitPlanModePre_SecondCall_ClearsInPlanMode(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()
	dir := initTestRepo(t)

	require.NoError(t, store.SetInPlanMode(ctx, "s1", true))

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/plan.md"},
	})

	var stdout bytes.Buffer

	// First call denies.
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger))

	// in_plan_mode still set after deny (only cleared on allow).
	inPlanMode, err := store.InPlanMode(ctx, "s1")
	require.NoError(t, err)
	assert.True(t, inPlanMode, "in_plan_mode must remain set across the deny call")

	// Second call allows and clears in_plan_mode.
	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger))

	inPlanMode, err = store.InPlanMode(ctx, "s1")
	require.NoError(t, err)
	assert.False(t, inPlanMode)
}

func TestBugFix_OnlyDenied_StopAllowsThrough(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// PreToolUse:ExitPlanMode fires once -- denied.
	// The session never reaches the "allow" path (plan review cycle ongoing).
	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger))

	// Stop fires -- should allow through because plan_path is empty
	// (only recorded on allow, not on deny).
	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(), "Stop should allow through when only deny was issued")
}

func TestStopBlocksWithChanges(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// Full flow: deny then allow (which records plan path).
	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger))

	// Get the baseSHA that was recorded.
	_, _, baseSHA, err := store.Session(context.Background(), "s1")
	require.NoError(t, err)

	// Make a code change and commit.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644))

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "implement plan"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})
	stdout.Reset()

	err = handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])
	assert.Contains(t, result["reason"], "AskUserQuestion")
	assert.Contains(t, result["reason"], "/review-implementation")
	assert.Contains(t, result["reason"], baseSHA)
}

func TestStop_BlocksImplementationWithNoChanges(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// Full flow: deny then allow.
	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger))

	// No changes -- Stop now blocks; the unified message must offer
	// both branches so Claude can either confirm done or ask for input.
	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])

	reason, ok := result["reason"].(string)
	require.True(t, ok)
	assert.Contains(t, reason, "completed the implementation")
	assert.Contains(t, reason, "If you are not done")
	assert.Contains(t, reason, "If you have a question for the user")
}

// TestStop_AllowsAfterPostImplAUQ pins the release semantics: answering
// the post-impl AskUserQuestion releases the Stop gate for the rest of
// the plan cycle.
func TestStop_AllowsAfterPostImplAUQ(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	// Full plan flow: deny then allow.
	planInput := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, cfg, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, cfg, dir, logger))

	// Make a code change and commit (the implementation).
	require.NoError(t, os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("code\n"), 0o644))

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "implement"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	// User answers the post-impl AskUserQuestion -- releases the gate.
	askInput := askInputWithLabel(t, "s1", "/review-implementation")
	require.NoError(t, handlePostAskUserQuestion(t.Context(), askInput, store, cfg, logger))

	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(),
		"Stop should allow through after the post-impl AUQ is answered")
}

func TestStop_BlocksInPlanMode(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	require.NoError(t, store.SetInPlanMode(ctx, "s1", true))

	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])
	assert.Contains(t, result["reason"], "plan mode")
	assert.Contains(t, result["reason"], "ExitPlanMode")
}

// TestStop_BlocksInPlanMode_WithPlanPathFromPriorRound covers the
// pathological case where ResetSession was skipped and the row carries
// both in_plan_mode=1 and a plan_path from a prior session. The
// plan-mode block must still win, otherwise the impl-mode message
// would render mid-plan.
func TestStop_BlocksInPlanMode_WithPlanPathFromPriorRound(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	require.NoError(t, store.SetPlanPath(ctx, "s1", "/old/plan.md", "old-sha"))
	require.NoError(t, store.SetInPlanMode(ctx, "s1", true))

	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])
	assert.Contains(t, result["reason"], "plan mode")
}

func TestStop_AllowsAfterCommitClear(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	// Mid-implementation state.
	require.NoError(t, store.SetPlanPath(ctx, "s1", "/plan.md", "sha1"))

	promptInput := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		Prompt:    "/commit",
	})
	require.NoError(t, handleUserPromptSubmit(t.Context(), promptInput, store, cfg, logger))

	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(), "Stop should allow through after /commit clears state")
}

func TestUserPromptSubmit_CommitClearsSession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	require.NoError(t, store.SetPlanPath(ctx, "s1", "/plan.md", "sha1"))
	require.NoError(t, store.SetInPlanMode(ctx, "s1", true))

	input := makeHookJSON(t, hook.Input{SessionID: "s1", Prompt: "/commit"})

	err := handleUserPromptSubmit(t.Context(), input, store, cfg, logger)
	require.NoError(t, err)

	count, planPath, baseSHA, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, "", planPath)
	assert.Equal(t, "", baseSHA)

	inPlanMode, err := store.InPlanMode(ctx, "s1")
	require.NoError(t, err)
	assert.False(t, inPlanMode)
}

func TestUserPromptSubmit_CommitWithArgsClears(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	require.NoError(t, store.SetPlanPath(ctx, "s1", "/plan.md", "sha1"))

	input := makeHookJSON(t, hook.Input{SessionID: "s1", Prompt: "/commit feat: add foo"})

	err := handleUserPromptSubmit(t.Context(), input, store, cfg, logger)
	require.NoError(t, err)

	_, planPath, _, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "", planPath)
}

func TestUserPromptSubmit_CommitPushPrAlsoClears(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	require.NoError(t, store.SetPlanPath(ctx, "s1", "/plan.md", "sha1"))

	input := makeHookJSON(t, hook.Input{SessionID: "s1", Prompt: "/commit-push-pr"})

	err := handleUserPromptSubmit(t.Context(), input, store, cfg, logger)
	require.NoError(t, err)

	_, planPath, _, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "", planPath)
}

func TestUserPromptSubmit_MergeAlsoClears(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	require.NoError(t, store.SetPlanPath(ctx, "s1", "/plan.md", "sha1"))

	input := makeHookJSON(t, hook.Input{SessionID: "s1", Prompt: "/merge"})

	err := handleUserPromptSubmit(t.Context(), input, store, cfg, logger)
	require.NoError(t, err)

	_, planPath, _, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "", planPath)
}

func TestUserPromptSubmit_NonCommitIsNoop(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"plain text":              "just a regular prompt",
		"unrelated slash command": "/rebase",
		"coordinator skill":       "/coordinator do stuff",
		"workmux skill":           "/workmux",
		"mid-sentence mention":    "please /commit this change",
		"prefixed mention":        "the /commit skill",
		"case mismatch":           "/Commit",
		"empty prompt":            "",
	}

	for name, prompt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newTestStore(t)
			logger := slog.New(slog.DiscardHandler)
			ctx := t.Context()

			sessionID := "s-" + name
			require.NoError(t, store.SetPlanPath(ctx, sessionID, "/plan.md", "sha1"))

			input := makeHookJSON(t, hook.Input{SessionID: sessionID, Prompt: prompt})

			err := handleUserPromptSubmit(t.Context(), input, store, cfg, logger)
			require.NoError(t, err)

			_, planPath, _, err := store.Session(ctx, sessionID)
			require.NoError(t, err)
			assert.Equal(t, "/plan.md", planPath, "session must NOT be cleared for prompt %q", prompt)
		})
	}
}

func TestStopHookActive_ClearsAndAllows(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	_ = store.SetPlanPath(ctx, "s1", "/path/plan.md", "sha1")

	stopInput := makeHookJSON(t, hook.Input{
		SessionID:      "s1",
		StopHookActive: true,
	})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())

	// Session should be cleared.
	_, planPath, _, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "", planPath)
}

// askInputWithLabel builds an AskUserQuestion PostToolUse input whose
// single question has one option with the given label. The label is
// chosen by the caller so a test can both exercise the match path
// (label present in the post-impl skill catalog) and the non-match path.
func askInputWithLabel(t *testing.T, sessionID, label string) []byte {
	t.Helper()

	return makeHookJSON(t, hook.Input{
		SessionID: sessionID,
		ToolName:  "AskUserQuestion",
		ToolInput: map[string]any{
			"questions": []any{
				map[string]any{
					"options": []any{
						map[string]any{"label": label},
					},
				},
			},
		},
	})
}

func TestHandlePostAskUserQuestion_MatchingLabelClearsSession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	// Mid-implementation state.
	require.NoError(t, store.SetPlanPath(ctx, "s1", "/plan.md", "sha1"))

	input := askInputWithLabel(t, "s1", "/review-implementation")

	err := handlePostAskUserQuestion(t.Context(), input, store, cfg, logger)
	require.NoError(t, err)

	// Session row is cleared: plan_path is gone...
	_, planPath, _, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "", planPath)

	// ...so a subsequent Stop allows through.
	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})

	var stdout bytes.Buffer

	require.NoError(t, handleStop(t.Context(), stopInput, &stdout, store, cfg, logger))
	assert.Empty(t, stdout.Bytes(), "Stop must allow after the post-impl AUQ clears the session")
}

func TestHandlePostAskUserQuestion_IgnoresUnrelatedQuestion(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	require.NoError(t, store.SetPlanPath(ctx, "s1", "/plan.md", "sha1"))

	// Labels not in the post-impl skill catalog, e.g. an unrelated clarifying question.
	input := askInputWithLabel(t, "s1", "use-default-directory")

	err := handlePostAskUserQuestion(t.Context(), input, store, cfg, logger)
	require.NoError(t, err)

	_, planPath, _, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "/plan.md", planPath, "unrelated question must not clear the session")
}

func TestHandlePostAskUserQuestion_EmptySessionIsNoop(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	input := askInputWithLabel(t, "", "/review-implementation")

	err := handlePostAskUserQuestion(t.Context(), input, store, cfg, logger)
	require.NoError(t, err)

	// No row touched or created for any session. Queried directly because
	// Session() would itself INSERT a row.
	var count int

	require.NoError(t, store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM sessions`).Scan(&count))
	assert.Equal(t, 0, count)
}

func TestHandlePostAskUserQuestion_MalformedToolInputIsNoop(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	cases := map[string]map[string]any{
		"missing questions": {},
		"questions non-array": {
			"questions": "nope",
		},
		"options non-array": {
			"questions": []any{
				map[string]any{"options": "nope"},
			},
		},
		"label non-string": {
			"questions": []any{
				map[string]any{
					"options": []any{
						map[string]any{"label": 42},
					},
				},
			},
		},
	}

	for name, toolInput := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sessionID := "s-" + name
			require.NoError(t, store.SetPlanPath(t.Context(), sessionID, "/plan.md", "sha1"))

			input := makeHookJSON(t, hook.Input{
				SessionID: sessionID,
				ToolName:  "AskUserQuestion",
				ToolInput: toolInput,
			})

			err := handlePostAskUserQuestion(t.Context(), input, store, cfg, logger)
			require.NoError(t, err)

			_, planPath, _, err := store.Session(context.Background(), sessionID)
			require.NoError(t, err)
			assert.Equal(t, "/plan.md", planPath, "malformed tool_input must not clear the session")
		})
	}
}

// TestStop_AllowsAfterAsk_DespiteCommittedEdits is the load-bearing
// regression test for once-per-cycle release: edits made AFTER the
// post-impl question is answered (e.g. by /code-review --fix) must NOT
// re-arm the Stop gate.
func TestStop_AllowsAfterAsk_DespiteCommittedEdits(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	// Set up plan state.
	planInput := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, cfg, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, cfg, dir, logger))

	// Make initial change and commit.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("code\n"), 0o644))

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "implement"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	// User answers AskUserQuestion; handler releases the gate.
	askInput := askInputWithLabel(t, "s1", "/review-implementation")
	require.NoError(t, handlePostAskUserQuestion(t.Context(), askInput, store, cfg, logger))

	// More edits AFTER the user answered.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fix.txt"), []byte("fix\n"), 0o644))

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "fix reviewer feedback"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	// Stop must still allow: the gate fired once this cycle.
	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(),
		"Stop must allow despite committed edits after the post-impl AUQ")
}

// TestStop_AllowsAfterAsk_DespiteUncommittedEdits is the uncommitted
// twin of the committed-edits regression test above.
func TestStop_AllowsAfterAsk_DespiteUncommittedEdits(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	// Set up plan state.
	planInput := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, cfg, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, cfg, dir, logger))

	// Make initial change and commit.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("code\n"), 0o644))

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "implement"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	// User answers AskUserQuestion; handler releases the gate.
	askInput := askInputWithLabel(t, "s1", "/review-implementation")
	require.NoError(t, handlePostAskUserQuestion(t.Context(), askInput, store, cfg, logger))

	// Uncommitted edit AFTER the user answered.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("changed\n"), 0o644))

	// Stop must still allow: the gate fired once this cycle.
	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(),
		"Stop must allow despite uncommitted edits after the post-impl AUQ")
}

// TestStop_ReArmsOnNextPlanCycle pins the re-arm semantics: the
// post-impl AUQ releases Stop only for the current plan cycle.
// EnterPlanMode resets the session and the next approved ExitPlanMode
// records a fresh plan_path, so Stop blocks again.
func TestStop_ReArmsOnNextPlanCycle(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	// Cycle 1: approved plan, post-impl AUQ answered, Stop allows.
	planInput := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, cfg, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, cfg, dir, logger))

	askInput := askInputWithLabel(t, "s1", "/review-implementation")
	require.NoError(t, handlePostAskUserQuestion(t.Context(), askInput, store, cfg, logger))

	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})
	stdout.Reset()
	require.NoError(t, handleStop(t.Context(), stopInput, &stdout, store, cfg, logger))
	assert.Empty(t, stdout.Bytes(), "Stop must allow after the post-impl AUQ")

	// Cycle 2: EnterPlanMode re-arms; approved ExitPlanMode records a
	// fresh plan_path.
	enterInput := makeHookJSON(t, hook.Input{SessionID: "s1"})
	require.NoError(t, handleEnterPlanMode(t.Context(), enterInput, store, testPID, logger))

	plan2Input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan2.md"},
	})

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), plan2Input, &stdout, store, cfg, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), plan2Input, &stdout, store, cfg, dir, logger))

	// Stop blocks again with the new cycle's post-impl question.
	stdout.Reset()
	require.NoError(t, handleStop(t.Context(), stopInput, &stdout, store, cfg, logger))

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])
	assert.Contains(t, result["reason"], "/path/plan2.md")
}

// TestHandleStop_EscapeHatchWorksWhenStoreUnavailable verifies the
// stop_hook_active escape hatch still returns the user to control even
// when the backing store has gone away. Without this, a user wedged
// behind a stuck DB would have no way to finish a session.
func TestHandleStop_EscapeHatchWorksWhenStoreUnavailable(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	// Simulate a dead backend by closing the underlying connection.
	require.NoError(t, store.Close())

	stopInput := makeHookJSON(t, hook.Input{
		SessionID:      "s1",
		StopHookActive: true,
	})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(), "escape hatch must allow through even when store is dead")
}

// TestHandleStop_FailsClosedOnStoreError verifies that when the Session
// read fails (e.g. store is dead and no escape hatch is set), the handler
// blocks with a retry message rather than silently allowing stop.
func TestHandleStop_FailsClosedOnStoreError(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	require.NoError(t, store.Close())

	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])
	assert.Contains(t, result["reason"], "retry")
}

// TestHandleExitPlanModePre_FailsClosedOnStoreError verifies that when
// the increment fails, the handler denies with a retry message rather
// than silently letting ExitPlanMode through (which would bypass the
// plan-reviewer guardrail).
func TestHandleExitPlanModePre_FailsClosedOnStoreError(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	require.NoError(t, store.Close())

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

	hso, ok := result["hookSpecificOutput"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "deny", hso["permissionDecision"])
	assert.Contains(t, hso["permissionDecisionReason"], "retry")
}

func TestStopAllowsEmptySession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	// No plan state set -- Stop should allow through.
	stopInput := makeHookJSON(t, hook.Input{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())
}

func TestHandleExitPlanModePre_FirstCallDoesNotSetPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		Cwd:       dir,
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger))

	// First call denies; pending_plans must be empty.
	_, _, found, err := store.ConsumePendingPlan(t.Context(), testPID, 300)
	require.NoError(t, err)
	assert.False(t, found, "deny path must not write pending_plans")
}

func TestHandleExitPlanModePre_SecondCallSetsPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		Cwd:       dir,
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, cfg, dir, logger))

	planPath, baseSHA, found, err := store.ConsumePendingPlan(t.Context(), testPID, 300)
	require.NoError(t, err)
	require.True(t, found, "second (allowed) ExitPlanMode must write pending_plans")
	assert.Equal(t, "/path/plan.md", planPath)
	assert.Len(t, baseSHA, 40, "base_sha must be the recorded HEAD SHA")
}

func TestHandleSessionStart_MigratesPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	cwd := t.TempDir()

	_, err := store.SetPendingPlan(ctx, testPID, "/plan.md", "sha1")
	require.NoError(t, err)

	input := makeHookJSON(t, hook.Input{
		SessionID: "new-sess",
		Cwd:       cwd,
		Source:    "clear",
	})

	require.NoError(t, handleSessionStart(t.Context(), input, store, testPID, logger))

	_, planPath, baseSHA, err := store.Session(ctx, "new-sess")
	require.NoError(t, err)
	assert.Equal(t, "/plan.md", planPath)
	assert.Equal(t, "sha1", baseSHA)

	// Pending row must be consumed.
	_, _, found, err := store.ConsumePendingPlan(ctx, testPID, 300)
	require.NoError(t, err)
	assert.False(t, found, "pending row must be consumed after migration")
}

func TestHandleSessionStart_NoPendingPlanIsNoOp(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	cwd := t.TempDir()

	input := makeHookJSON(t, hook.Input{
		SessionID: "new-sess",
		Cwd:       cwd,
		Source:    "clear",
	})

	require.NoError(t, handleSessionStart(t.Context(), input, store, testPID, logger))

	_, planPath, baseSHA, err := store.Session(t.Context(), "new-sess")
	require.NoError(t, err)
	assert.Empty(t, planPath)
	assert.Empty(t, baseSHA)
}

func TestHandleSessionStart_EmptySessionIDIsNoOp(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	cwd := t.TempDir()

	_, err := store.SetPendingPlan(ctx, testPID, "/plan.md", "sha1")
	require.NoError(t, err)

	input := makeHookJSON(t, hook.Input{Cwd: cwd, Source: "clear"})

	require.NoError(t, handleSessionStart(t.Context(), input, store, testPID, logger))

	// Pending row must remain since we couldn't migrate.
	_, _, found, err := store.ConsumePendingPlan(ctx, testPID, 300)
	require.NoError(t, err)
	assert.True(t, found, "empty session_id must not consume the pending row")
}

func TestHandleSessionStart_StalePendingPlanIgnored(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	cwd := t.TempDir()

	_, err := store.SetPendingPlan(ctx, testPID, "/plan.md", "sha1")
	require.NoError(t, err)

	// Backdate the row beyond the 3600s TTL.
	_, err = store.DB().ExecContext(ctx,
		`UPDATE pending_plans SET updated_at = datetime('now', '-2 hours') WHERE claude_pid = ?`,
		testPID)
	require.NoError(t, err)

	input := makeHookJSON(t, hook.Input{
		SessionID: "new-sess",
		Cwd:       cwd,
		Source:    "clear",
	})

	require.NoError(t, handleSessionStart(t.Context(), input, store, testPID, logger))

	// Migration must NOT have happened.
	_, planPath, _, err := store.Session(ctx, "new-sess")
	require.NoError(t, err)
	assert.Empty(t, planPath, "stale pending plan must not migrate")
}

func TestHandleEnterPlanMode_DeletesPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	cwd := t.TempDir()

	_, err := store.SetPendingPlan(ctx, testPID, "/plan.md", "sha1")
	require.NoError(t, err)

	input := makeHookJSON(t, hook.Input{SessionID: "s1", Cwd: cwd})
	require.NoError(t, handleEnterPlanMode(t.Context(), input, store, testPID, logger))

	_, _, found, err := store.ConsumePendingPlan(ctx, testPID, 300)
	require.NoError(t, err)
	assert.False(t, found, "EnterPlanMode must drop any stale pending handoff")
}

func TestHandleUserPromptSubmit_CommitSkill_DeletesPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	cwd := t.TempDir()

	_, err := store.SetPendingPlan(ctx, testPID, "/plan.md", "sha1")
	require.NoError(t, err)

	input := makeHookJSON(t, hook.Input{SessionID: "s1", Cwd: cwd, Prompt: "/commit"})
	require.NoError(t, handleUserPromptSubmit(t.Context(), input, store, cfg, logger))

	_, _, found, err := store.ConsumePendingPlan(ctx, testPID, 300)
	require.NoError(t, err)
	assert.False(t, found, "wrap-up skill must drop pending handoff for this window")
}

func TestHandleStop_StopHookActive_DeletesPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	_, err := store.SetPendingPlan(ctx, testPID, "/plan.md", "sha1")
	require.NoError(t, err)

	stopInput := makeHookJSON(t, hook.Input{
		SessionID:      "s1",
		Cwd:            t.TempDir(),
		StopHookActive: true,
	})

	var stdout bytes.Buffer
	require.NoError(t, handleStop(t.Context(), stopInput, &stdout, store, cfg, logger))

	_, _, found, err := store.ConsumePendingPlan(ctx, testPID, 300)
	require.NoError(t, err)
	assert.False(t, found, "stop_hook_active escape must drop pending handoff")
}

func TestHandlePostAskUserQuestion_DeletesPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	_, err := store.SetPendingPlan(ctx, testPID, "/plan.md", "sha1")
	require.NoError(t, err)

	askInput := makeHookJSON(t, hook.Input{
		SessionID: "s1",
		Cwd:       t.TempDir(),
		ToolName:  "AskUserQuestion",
		ToolInput: map[string]any{
			"questions": []any{
				map[string]any{
					"options": []any{
						map[string]any{"label": "/review-implementation"},
					},
				},
			},
		},
	})

	require.NoError(t, handlePostAskUserQuestion(t.Context(), askInput, store, cfg, logger))

	_, _, found, err := store.ConsumePendingPlan(ctx, testPID, 300)
	require.NoError(t, err)
	assert.False(t, found, "post-impl AUQ must drop pending handoff")
}

// TestHandleSessionStart_DoesNotConsumeOtherInstancesPlan exercises
// per-window isolation at the handler layer. Window A writes a pending
// plan; window B's SessionStart must not consume it, and window A's
// own SessionStart (matching PPID) still migrates the plan.
func TestHandleSessionStart_DoesNotConsumeOtherInstancesPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	cwd := t.TempDir()

	// Window A writes its handoff.
	_, err := store.SetPendingPlan(ctx, "A", "/plan-A.md", "sha-A")
	require.NoError(t, err)

	// Window B's SessionStart fires. It must NOT consume A's row.
	bInput := makeHookJSON(t, hook.Input{
		SessionID: "new-sess-B",
		Cwd:       cwd,
		Source:    "clear",
	})

	require.NoError(t, handleSessionStart(t.Context(), bInput, store, "B", logger))

	_, planPathB, baseSHAB, err := store.Session(ctx, "new-sess-B")
	require.NoError(t, err)
	assert.Empty(t, planPathB, "window B must not migrate window A's plan")
	assert.Empty(t, baseSHAB, "window B must not migrate window A's plan")

	// Window A's SessionStart now fires; it must consume the row that B
	// left untouched.
	aInput := makeHookJSON(t, hook.Input{
		SessionID: "new-sess-A",
		Cwd:       cwd,
		Source:    "clear",
	})

	require.NoError(t, handleSessionStart(t.Context(), aInput, store, "A", logger))

	_, planPathA, baseSHAA, err := store.Session(ctx, "new-sess-A")
	require.NoError(t, err)
	assert.Equal(t, "/plan-A.md", planPathA, "window A's SessionStart must migrate its own plan")
	assert.Equal(t, "sha-A", baseSHAA)

	// And the row is now consumed.
	_, _, found, err := store.ConsumePendingPlan(ctx, "A", 300)
	require.NoError(t, err)
	assert.False(t, found, "A's handoff must be consumed after its SessionStart")
}
