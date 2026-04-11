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

func TestHandleExitPlanModePre_FirstCallDenies(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(input, &stdout, store, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))

	hso := result["hookSpecificOutput"].(map[string]any)
	assert.Equal(t, "deny", hso["permissionDecision"])
	assert.Contains(t, hso["permissionDecisionReason"], "plan-reviewer")
	assert.Contains(t, hso["permissionDecisionReason"], "/path/plan.md")
}

func TestHandleExitPlanModePre_SecondCallAllows(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// First call: denied.
	var stdout bytes.Buffer

	err := handleExitPlanModePre(input, &stdout, store, logger)
	require.NoError(t, err)
	assert.NotEmpty(t, stdout.Bytes())

	// Second call: allowed (no output).
	stdout.Reset()

	err = handleExitPlanModePre(input, &stdout, store, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())
}

func TestHandleExitPlanModePre_NoPlanPath(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(input, &stdout, store, logger)
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

	input := makeHookJSON(t, HookInput{
		SessionID: "",
		ToolInput: map[string]any{"planFilePath": "/plan.md"},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(input, &stdout, store, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())
}

func TestHandleExitPlanModePost(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	err := handleExitPlanModePost(input, store, dir, logger)
	require.NoError(t, err)

	_, planPath, baseSHA, err := store.Session(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "/path/plan.md", planPath)
	assert.Len(t, baseSHA, 40)
}

func TestHandleEnterPlanMode_ResetsSession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	// Set some state.
	_, _ = store.IncrementExitPlanCount(ctx, "s1")
	_ = store.SetPlanPath(ctx, "s1", "/plan.md", "sha1")

	input := makeHookJSON(t, HookInput{SessionID: "s1"})

	err := handleEnterPlanMode(input, store, logger)
	require.NoError(t, err)

	count, planPath, baseSHA, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Equal(t, "", planPath)
	assert.Equal(t, "", baseSHA)
}

func TestBugFix_ExitPlanRejected_StopAllowsThrough(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// PreToolUse:ExitPlanMode fires twice (deny then allow).
	var stdout bytes.Buffer
	_ = handleExitPlanModePre(input, &stdout, store, logger)

	stdout.Reset()
	_ = handleExitPlanModePre(input, &stdout, store, logger)

	// PostToolUse:ExitPlanMode NEVER fires (user rejected).
	// This is the bug fix: plan_path stays empty.

	// Stop fires -- should allow through because plan_path is empty.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(stopInput, &stdout, store, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(), "Stop should allow through when plan_path is empty")
}

func TestStopBlocksWithChanges(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}

	baseSHA, err := git.HeadSHA(ctx)
	require.NoError(t, err)

	_ = store.SetPlanPath(ctx, "s1", "/path/plan.md", baseSHA)

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

	var stdout bytes.Buffer

	err = handleStop(stopInput, &stdout, store, dir, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])
	assert.Contains(t, result["reason"], "implementation-reviewer")
	assert.Contains(t, result["reason"], baseSHA)
}

func TestStopAllowsNoChanges(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	dir := initTestRepo(t)
	git := &GitRunner{Dir: dir}

	baseSHA, err := git.HeadSHA(ctx)
	require.NoError(t, err)

	_ = store.SetPlanPath(ctx, "s1", "/path/plan.md", baseSHA)

	// No changes -- Stop should allow through.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})

	var stdout bytes.Buffer

	err = handleStop(stopInput, &stdout, store, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(), "Stop should allow through when no changes")
}

func TestStopHookActive_ClearsAndAllows(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	ctx := context.Background()
	dir := initTestRepo(t)

	_ = store.SetPlanPath(ctx, "s1", "/path/plan.md", "sha1")

	stopInput := makeHookJSON(t, HookInput{
		SessionID:      "s1",
		StopHookActive: true,
	})

	var stdout bytes.Buffer

	err := handleStop(stopInput, &stdout, store, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())

	// Session should be cleared.
	_, planPath, _, err := store.Session(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "", planPath)
}

func TestStopAllowsEmptySession(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	// No plan state set -- Stop should allow through.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})

	var stdout bytes.Buffer

	err := handleStop(stopInput, &stdout, store, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())
}
