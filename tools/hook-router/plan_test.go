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
)

func makeHookJSON(t *testing.T, hook HookInput) []byte {
	t.Helper()

	b, err := json.Marshal(hook)
	require.NoError(t, err)

	return b
}

// testCatalog returns a [*PostImplCatalog] matching the canonical
// skills declared in home/claude.nix. Tests that exercise
// handlePostAskUserQuestion or handleStop pass this via cfg so the
// Nix-driven validation and block-message rendering paths are the
// same shape as production.
func testCatalog() *PostImplCatalog {
	return NewPostImplCatalog([]PostImplSkill{
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
}

func TestHandleExitPlanModePre_FirstCallDenies(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger)
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

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// First call: denied.
	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger)
	require.NoError(t, err)
	assert.NotEmpty(t, stdout.Bytes())

	// Second call: allowed (no output), and records plan path.
	stdout.Reset()

	err = handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())

	// Verify plan_path and base_sha are now recorded.
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

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger)
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

	input := makeHookJSON(t, HookInput{
		SessionID: "",
		ToolInput: map[string]any{"planFilePath": "/plan.md"},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger)
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

	input := makeHookJSON(t, HookInput{SessionID: "s1"})

	err := handleEnterPlanMode(t.Context(), input, store, logger)
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

	input := makeHookJSON(t, HookInput{SessionID: "s1"})

	err := handleEnterPlanMode(t.Context(), input, store, logger)
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

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/plan.md"},
	})

	var stdout bytes.Buffer

	// First call denies.
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger))

	// in_plan_mode still set after deny (only cleared on allow).
	inPlanMode, err := store.InPlanMode(ctx, "s1")
	require.NoError(t, err)
	assert.True(t, inPlanMode, "in_plan_mode must remain set across the deny call")

	// Second call allows and clears in_plan_mode.
	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger))

	inPlanMode, err = store.InPlanMode(ctx, "s1")
	require.NoError(t, err)
	assert.False(t, inPlanMode)
}

func TestBugFix_OnlyDenied_StopAllowsThrough(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// PreToolUse:ExitPlanMode fires once -- denied.
	// The session never reaches the "allow" path (plan review cycle ongoing).
	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger))

	// Stop fires -- should allow through because plan_path is empty
	// (only recorded on allow, not on deny).
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(), "Stop should allow through when only deny was issued")
}

func TestStopBlocksWithChanges(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// Full flow: deny then allow (which records plan path).
	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger))

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

	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err = handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
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

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// Full flow: deny then allow.
	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger))

	// No changes -- Stop now blocks; the unified message must offer
	// both branches so Claude can either confirm done or ask for input.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])

	reason, ok := result["reason"].(string)
	require.True(t, ok)
	assert.Contains(t, reason, "completed the implementation")
	assert.Contains(t, reason, "If you are not done")
}

func TestStop_AllowsAfterPostImplAUQ_NoChanges(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	planInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

	// No changes, but user answers a post-impl AUQ -- captures the
	// fingerprint of the (unchanged) state so Stop can short-circuit.
	askInput := askInputWithLabel(t, "s1", "/review-implementation")
	require.NoError(t, handlePostAskUserQuestion(t.Context(), askInput, store, cfg, dir, logger))

	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(),
		"Stop should allow through after a post-impl AUQ even with no changes")
}

func TestStop_BlocksInPlanMode(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()
	dir := initTestRepo(t)

	require.NoError(t, store.SetInPlanMode(ctx, "s1", true))

	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
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
	dir := initTestRepo(t)

	require.NoError(t, store.SetPlanPath(ctx, "s1", "/old/plan.md", "old-sha"))
	require.NoError(t, store.SetInPlanMode(ctx, "s1", true))

	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
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
	dir := initTestRepo(t)

	// Mid-implementation state.
	require.NoError(t, store.SetPlanPath(ctx, "s1", "/plan.md", "sha1"))

	promptInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		Prompt:    "/commit",
	})
	require.NoError(t, handleUserPromptSubmit(t.Context(), promptInput, store, cfg, logger))

	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
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

	input := makeHookJSON(t, HookInput{SessionID: "s1", Prompt: "/commit"})

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

	input := makeHookJSON(t, HookInput{SessionID: "s1", Prompt: "/commit feat: add foo"})

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

	input := makeHookJSON(t, HookInput{SessionID: "s1", Prompt: "/commit-push-pr"})

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

	input := makeHookJSON(t, HookInput{SessionID: "s1", Prompt: "/merge"})

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

			input := makeHookJSON(t, HookInput{SessionID: sessionID, Prompt: prompt})

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
	dir := initTestRepo(t)

	_ = store.SetPlanPath(ctx, "s1", "/path/plan.md", "sha1")

	stopInput := makeHookJSON(t, HookInput{
		SessionID:      "s1",
		StopHookActive: true,
	})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
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

	return makeHookJSON(t, HookInput{
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

func TestHandlePostAskUserQuestion_RecordsFingerprintOnMatchingLabel(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := askInputWithLabel(t, "s1", "/review-implementation")

	err := handlePostAskUserQuestion(t.Context(), input, store, cfg, dir, logger)
	require.NoError(t, err)

	headSHA, wtHash, err := store.AskFingerprint(context.Background(), "s1")
	require.NoError(t, err)
	assert.Len(t, headSHA, 40)
	assert.Len(t, wtHash, 64)
}

func TestHandlePostAskUserQuestion_IgnoresUnrelatedQuestion(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	// Labels not in the post-impl skill catalog, e.g. an unrelated clarifying question.
	input := askInputWithLabel(t, "s1", "use-default-directory")

	err := handlePostAskUserQuestion(t.Context(), input, store, cfg, dir, logger)
	require.NoError(t, err)

	headSHA, wtHash, err := store.AskFingerprint(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "", headSHA)
	assert.Equal(t, "", wtHash)
}

func TestHandlePostAskUserQuestion_EmptySessionIsNoop(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := askInputWithLabel(t, "", "/review-implementation")

	err := handlePostAskUserQuestion(t.Context(), input, store, cfg, dir, logger)
	require.NoError(t, err)

	// Nothing written for the empty session, and no write for any session.
	headSHA, _, err := store.AskFingerprint(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "", headSHA)
}

func TestHandlePostAskUserQuestion_MalformedToolInputIsNoop(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

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

			input := makeHookJSON(t, HookInput{
				SessionID: "s-" + name,
				ToolName:  "AskUserQuestion",
				ToolInput: toolInput,
			})

			err := handlePostAskUserQuestion(t.Context(), input, store, cfg, dir, logger)
			require.NoError(t, err)

			headSHA, _, err := store.AskFingerprint(context.Background(), "s-"+name)
			require.NoError(t, err)
			assert.Equal(t, "", headSHA)
		})
	}
}

func TestStop_AllowsWhenAskRanAgainstCurrentState(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	// Full plan flow: deny then allow.
	planInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

	// Make a code change and commit.
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

	// Simulate user answering post-impl AskUserQuestion -- captures
	// fingerprint of current state.
	askInput := askInputWithLabel(t, "s1", "/review-implementation")

	err := handlePostAskUserQuestion(t.Context(), askInput, store, cfg, dir, logger)
	require.NoError(t, err)

	// Stop should allow through since the post-impl question was
	// answered against current state.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err = handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(), "Stop should allow through when ask ran against current state")
}

func TestStop_BlocksWhenCommittedEditsAfterAsk(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	// Set up plan state.
	planInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

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

	// User answers AskUserQuestion; handler captures fingerprint.
	askInput := askInputWithLabel(t, "s1", "/review-implementation")
	require.NoError(t, handlePostAskUserQuestion(t.Context(), askInput, store, cfg, dir, logger))

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

	// Stop should block since state changed after Ask; re-block message
	// should re-prompt with the instructional AskUserQuestion content.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])
	assert.Contains(t, result["reason"], "AskUserQuestion")
	assert.Contains(t, result["reason"], "/review-implementation")
}

func TestStop_BlocksWhenUncommittedEditsAfterAsk(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	// Set up plan state.
	planInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

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

	// User answers AskUserQuestion; handler captures fingerprint.
	askInput := askInputWithLabel(t, "s1", "/review-implementation")
	require.NoError(t, handlePostAskUserQuestion(t.Context(), askInput, store, cfg, dir, logger))

	// Uncommitted edit AFTER the user answered.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("changed\n"), 0o644))

	// Stop should block since working tree changed; re-block message
	// should cite the AskUserQuestion instructions.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])
	assert.Contains(t, result["reason"], "AskUserQuestion")
	assert.Contains(t, result["reason"], "/review-implementation")
}

// TestHandleStop_EscapeHatchWorksWhenStoreUnavailable verifies the
// stop_hook_active escape hatch still returns the user to control even
// when the backing store has gone away. Without this, a user wedged
// behind a stuck DB would have no way to finish a session.
func TestHandleStop_EscapeHatchWorksWhenStoreUnavailable(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	// Simulate a dead backend by closing the underlying connection.
	require.NoError(t, store.Close())

	stopInput := makeHookJSON(t, HookInput{
		SessionID:      "s1",
		StopHookActive: true,
	})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
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
	dir := initTestRepo(t)

	require.NoError(t, store.Close())

	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
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

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger)
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
	dir := initTestRepo(t)

	// No plan state set -- Stop should allow through.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())
}

func TestParsePostImplSkills(t *testing.T) {
	t.Parallel()

	type check func(t *testing.T, cat *PostImplCatalog)

	cases := map[string]struct {
		in    string
		err   bool
		check check
	}{
		"empty string yields empty catalog": {
			in: "",
			check: func(t *testing.T, cat *PostImplCatalog) {
				t.Helper()
				assert.True(t, cat.Empty())
				assert.False(t, cat.HasLabel("/review-implementation"))
			},
		},
		"entry round-trips": {
			in: `[{"label":"commit","description":"Create a git commit."}]`,
			check: func(t *testing.T, cat *PostImplCatalog) {
				t.Helper()
				assert.True(t, cat.HasLabel("commit"))
				// BuildAskReason renders the single bullet.
				reason := cat.BuildAskReason("/p.md", "abc123")
				assert.Contains(t, reason, "commit: Create a git commit.")
			},
		},
		"malformed JSON returns error": {
			in:  `[{"label":`,
			err: true,
		},
		"duplicate labels are not deduped": {
			in: `[{"label":"commit","description":"first"},{"label":"commit","description":"second"}]`,
			check: func(t *testing.T, cat *PostImplCatalog) {
				t.Helper()
				assert.True(t, cat.HasLabel("commit"))

				reason := cat.BuildAskReason("/p.md", "abc123")
				// Both bullets render -- catalog trusts the Nix list as source of truth.
				assert.Contains(t, reason, "commit: first")
				assert.Contains(t, reason, "commit: second")
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cat, err := parsePostImplSkills(tc.in)
			if tc.err {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cat)
			tc.check(t, cat)
		})
	}
}

// TestBuildAskReason_EmptyCatalog documents the degraded-mode
// fallback: with no skills, Stop still renders a (bullet-less) block
// message rather than panicking. Production should never hit this
// path (mainErr logs a warning when the catalog is empty) but the
// invariant that handlers can call BuildAskReason without a nil-guard
// is enforced here.
func TestBuildAskReason_EmptyCatalog(t *testing.T) {
	t.Parallel()

	cat := NewPostImplCatalog(nil)
	reason := cat.BuildAskReason("/p.md", "abc123")

	assert.Contains(t, reason, "AskUserQuestion")
	assert.Contains(t, reason, "/p.md")
	assert.Contains(t, reason, "abc123")
	assert.Contains(t, reason, "If you are not done")
	assert.NotContains(t, reason, "  - ") // zero bullets rendered
}

// TestBuildAskReason_NotDoneBranchPresent enforces the unified-message
// contract: the populated-catalog rendering must include both branches
// so Claude can choose between confirming done (post-impl AUQ) or
// asking a clarifying question.
func TestBuildAskReason_NotDoneBranchPresent(t *testing.T) {
	t.Parallel()

	reason := testCatalog().BuildAskReason("/p.md", "abc123")

	assert.Contains(t, reason, "completed the implementation")
	assert.Contains(t, reason, "If you are not done")
	assert.Contains(t, reason, "/review-implementation")
	assert.Contains(t, reason, "/p.md")
	assert.Contains(t, reason, "abc123")
}

func TestHandleExitPlanModePre_FirstCallDoesNotSetPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		Cwd:       dir,
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger))

	// First call denies; pending_plans must be empty.
	_, _, found, err := store.ConsumePendingPlan(t.Context(), dir, 300)
	require.NoError(t, err)
	assert.False(t, found, "deny path must not write pending_plans")
}

func TestHandleExitPlanModePre_SecondCallSetsPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		Cwd:       dir,
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), input, &stdout, store, dir, logger))

	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	planPath, baseSHA, found, err := store.ConsumePendingPlan(t.Context(), resolved, 300)
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
	resolved, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)

	_, err = store.SetPendingPlan(ctx, resolved, "/plan.md", "sha1")
	require.NoError(t, err)

	input := makeHookJSON(t, HookInput{
		SessionID: "new-sess",
		Cwd:       cwd,
		Source:    "clear",
	})

	require.NoError(t, handleSessionStart(t.Context(), input, store, logger))

	_, planPath, baseSHA, err := store.Session(ctx, "new-sess")
	require.NoError(t, err)
	assert.Equal(t, "/plan.md", planPath)
	assert.Equal(t, "sha1", baseSHA)

	// Pending row must be consumed.
	_, _, found, err := store.ConsumePendingPlan(ctx, resolved, 300)
	require.NoError(t, err)
	assert.False(t, found, "pending row must be consumed after migration")
}

func TestHandleSessionStart_NoPendingPlanIsNoOp(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	cwd := t.TempDir()

	input := makeHookJSON(t, HookInput{
		SessionID: "new-sess",
		Cwd:       cwd,
		Source:    "clear",
	})

	require.NoError(t, handleSessionStart(t.Context(), input, store, logger))

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
	resolved, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)

	_, err = store.SetPendingPlan(ctx, resolved, "/plan.md", "sha1")
	require.NoError(t, err)

	input := makeHookJSON(t, HookInput{Cwd: cwd, Source: "clear"})

	require.NoError(t, handleSessionStart(t.Context(), input, store, logger))

	// Pending row must remain since we couldn't migrate.
	_, _, found, err := store.ConsumePendingPlan(ctx, resolved, 300)
	require.NoError(t, err)
	assert.True(t, found, "empty session_id must not consume the pending row")
}

func TestHandleSessionStart_EmptyCwdIsNoOp(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	input := makeHookJSON(t, HookInput{SessionID: "s1", Source: "clear"})

	require.NoError(t, handleSessionStart(t.Context(), input, store, logger))

	_, planPath, _, err := store.Session(t.Context(), "s1")
	require.NoError(t, err)
	assert.Empty(t, planPath)
}

func TestHandleSessionStart_StalePendingPlanIgnored(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	cwd := t.TempDir()
	resolved, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)

	_, err = store.SetPendingPlan(ctx, resolved, "/plan.md", "sha1")
	require.NoError(t, err)

	// Backdate the row beyond the 300s TTL.
	_, err = store.db.ExecContext(ctx,
		`UPDATE pending_plans SET updated_at = datetime('now', '-1 hour') WHERE cwd = ?`,
		resolved)
	require.NoError(t, err)

	input := makeHookJSON(t, HookInput{
		SessionID: "new-sess",
		Cwd:       cwd,
		Source:    "clear",
	})

	require.NoError(t, handleSessionStart(t.Context(), input, store, logger))

	// Migration must NOT have happened.
	_, planPath, _, err := store.Session(ctx, "new-sess")
	require.NoError(t, err)
	assert.Empty(t, planPath, "stale pending plan must not migrate")
}

func TestHandleSessionStart_SymlinkedCwdResolvesToSameRow(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	realDir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(realDir, link))

	resolved, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err)

	// Write keyed by resolved path (as handleExitPlanModePre would do).
	_, err = store.SetPendingPlan(ctx, resolved, "/plan.md", "sha1")
	require.NoError(t, err)

	// SessionStart fires with the symlinked cwd; resolveCwd must hit the
	// same row.
	input := makeHookJSON(t, HookInput{
		SessionID: "new-sess",
		Cwd:       link,
		Source:    "clear",
	})

	require.NoError(t, handleSessionStart(t.Context(), input, store, logger))

	_, planPath, _, err := store.Session(ctx, "new-sess")
	require.NoError(t, err)
	assert.Equal(t, "/plan.md", planPath, "symlinked cwd must resolve to the same row")
}

// TestHandleSessionStart_SymlinkWriteRealRead verifies the inverse:
// writing via a symlinked cwd and consuming via the real path also
// hits the same row, since both sides go through resolveCwd.
func TestHandleSessionStart_SymlinkWriteRealRead(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	realDir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(realDir, link))

	// Plan accept fires from the symlinked cwd; resolveCwd writes under
	// the real path.
	planInput := makeHookJSON(t, HookInput{
		SessionID: "old-sess",
		Cwd:       link,
		ToolInput: map[string]any{"planFilePath": "/plan.md"},
	})

	dir := initTestRepo(t)

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

	// Now SessionStart fires with the real path -- must consume the row.
	resolved, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err)

	startInput := makeHookJSON(t, HookInput{
		SessionID: "new-sess",
		Cwd:       resolved,
		Source:    "clear",
	})
	require.NoError(t, handleSessionStart(t.Context(), startInput, store, logger))

	_, planPath, _, err := store.Session(ctx, "new-sess")
	require.NoError(t, err)
	assert.Equal(t, "/plan.md", planPath,
		"writing via symlink and reading via real path must hit the same row")
}

func TestHandleEnterPlanMode_DeletesPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	cwd := t.TempDir()
	resolved, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)

	_, err = store.SetPendingPlan(ctx, resolved, "/plan.md", "sha1")
	require.NoError(t, err)

	input := makeHookJSON(t, HookInput{SessionID: "s1", Cwd: cwd})
	require.NoError(t, handleEnterPlanMode(t.Context(), input, store, logger))

	_, _, found, err := store.ConsumePendingPlan(ctx, resolved, 300)
	require.NoError(t, err)
	assert.False(t, found, "EnterPlanMode must drop any stale pending handoff")
}

func TestHandleUserPromptSubmit_CommitSkill_DeletesPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()

	cwd := t.TempDir()
	resolved, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)

	_, err = store.SetPendingPlan(ctx, resolved, "/plan.md", "sha1")
	require.NoError(t, err)

	input := makeHookJSON(t, HookInput{SessionID: "s1", Cwd: cwd, Prompt: "/commit"})
	require.NoError(t, handleUserPromptSubmit(t.Context(), input, store, cfg, logger))

	_, _, found, err := store.ConsumePendingPlan(ctx, resolved, 300)
	require.NoError(t, err)
	assert.False(t, found, "wrap-up skill must drop pending handoff for this cwd")
}

func TestHandleStop_StopHookActive_DeletesPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()
	dir := initTestRepo(t)

	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	_, err = store.SetPendingPlan(ctx, resolved, "/plan.md", "sha1")
	require.NoError(t, err)

	stopInput := makeHookJSON(t, HookInput{
		SessionID:      "s1",
		Cwd:            dir,
		StopHookActive: true,
	})

	var stdout bytes.Buffer
	require.NoError(t, handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger))

	_, _, found, err := store.ConsumePendingPlan(ctx, resolved, 300)
	require.NoError(t, err)
	assert.False(t, found, "stop_hook_active escape must drop pending handoff")
}

func TestHandleStop_FingerprintMatch_DeletesPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()
	dir := initTestRepo(t)

	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	planInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		Cwd:       dir,
		ToolInput: map[string]any{"planFilePath": "/plan.md"},
	})

	var stdout bytes.Buffer
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

	stdout.Reset()
	require.NoError(t, handleExitPlanModePre(t.Context(), planInput, &stdout, store, dir, logger))

	// Pending row exists from the second ExitPlanMode call.
	_, _, found, err := store.ConsumePendingPlan(ctx, resolved, 300)
	require.NoError(t, err)
	require.True(t, found)

	// Reseed (Consume just deleted it).
	_, err = store.SetPendingPlan(ctx, resolved, "/plan.md", "sha1")
	require.NoError(t, err)

	// Answer post-impl AUQ -- captures fingerprint.
	askInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		Cwd:       dir,
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
	require.NoError(t, handlePostAskUserQuestion(t.Context(), askInput, store, cfg, dir, logger))

	// handlePostAskUserQuestion deletes pending too; reseed and trigger
	// Stop's fingerprint-match path explicitly.
	_, err = store.SetPendingPlan(ctx, resolved, "/plan.md", "sha1")
	require.NoError(t, err)

	stopInput := makeHookJSON(t, HookInput{SessionID: "s1", Cwd: dir})

	stdout.Reset()
	require.NoError(t, handleStop(t.Context(), stopInput, &stdout, store, cfg, dir, logger))

	// Stop allowed through (fingerprint match) and dropped pending.
	assert.Empty(t, stdout.Bytes())

	_, _, found, err = store.ConsumePendingPlan(ctx, resolved, 300)
	require.NoError(t, err)
	assert.False(t, found, "fingerprint-match short-circuit must drop pending handoff")
}

func TestHandlePostAskUserQuestion_DeletesPendingPlan(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := t.Context()
	dir := initTestRepo(t)

	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	_, err = store.SetPendingPlan(ctx, resolved, "/plan.md", "sha1")
	require.NoError(t, err)

	askInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		Cwd:       dir,
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

	require.NoError(t, handlePostAskUserQuestion(t.Context(), askInput, store, cfg, dir, logger))

	_, _, found, err := store.ConsumePendingPlan(ctx, resolved, 300)
	require.NoError(t, err)
	assert.False(t, found, "post-impl AUQ must drop pending handoff")
}
