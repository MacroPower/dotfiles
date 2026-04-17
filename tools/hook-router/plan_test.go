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
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	var stdout bytes.Buffer

	err := handleExitPlanModePre(input, &stdout, store, dir, logger)
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

	err := handleExitPlanModePre(input, &stdout, store, dir, logger)
	require.NoError(t, err)
	assert.NotEmpty(t, stdout.Bytes())

	// Second call: allowed (no output), and records plan path.
	stdout.Reset()

	err = handleExitPlanModePre(input, &stdout, store, dir, logger)
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

	err := handleExitPlanModePre(input, &stdout, store, dir, logger)
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

	err := handleExitPlanModePre(input, &stdout, store, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes())
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
	_ = handleExitPlanModePre(input, &stdout, store, dir, logger)

	// Stop fires -- should allow through because plan_path is empty
	// (only recorded on allow, not on deny).
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(stopInput, &stdout, store, dir, logger)
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
	_ = handleExitPlanModePre(input, &stdout, store, dir, logger)

	stdout.Reset()
	_ = handleExitPlanModePre(input, &stdout, store, dir, logger)

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
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"planFilePath": "/path/plan.md"},
	})

	// Full flow: deny then allow.
	var stdout bytes.Buffer
	_ = handleExitPlanModePre(input, &stdout, store, dir, logger)

	stdout.Reset()
	_ = handleExitPlanModePre(input, &stdout, store, dir, logger)

	// No changes -- Stop should allow through.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(stopInput, &stdout, store, dir, logger)
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

func TestHandleAgentPre_RecordsFingerprint(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{
			"subagent_type": "implementation-reviewer",
			"prompt":        "review the changes",
		},
	})

	var stdout bytes.Buffer

	err := handleAgentPre(input, &stdout, store, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(), "should always allow through")

	headSHA, wtHash, err := store.ReviewFingerprint(context.Background(), "s1")
	require.NoError(t, err)
	assert.Len(t, headSHA, 40)
	assert.Len(t, wtHash, 64)
}

func TestHandleAgentPre_IgnoresNonReviewer(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{
			"subagent_type": "Explore",
			"prompt":        "search the codebase",
		},
	})

	var stdout bytes.Buffer

	err := handleAgentPre(input, &stdout, store, dir, logger)
	require.NoError(t, err)

	// No fingerprint should be recorded.
	headSHA, wtHash, err := store.ReviewFingerprint(context.Background(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "", headSHA)
	assert.Equal(t, "", wtHash)
}

func TestHandleAgentPre_PlanReviewer(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	logger := slog.New(slog.DiscardHandler)
	dir := initTestRepo(t)

	input := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{
			"subagent_type": "plan-reviewer",
			"prompt":        "review the plan",
		},
	})

	var stdout bytes.Buffer

	err := handleAgentPre(input, &stdout, store, dir, logger)
	require.NoError(t, err)

	headSHA, _, err := store.ReviewFingerprint(context.Background(), "s1")
	require.NoError(t, err)
	assert.Len(t, headSHA, 40, "plan-reviewer should also record fingerprint")
}

func TestStop_AllowsWhenReviewerRanAgainstCurrentState(t *testing.T) {
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
	_ = handleExitPlanModePre(planInput, &stdout, store, dir, logger)

	stdout.Reset()
	_ = handleExitPlanModePre(planInput, &stdout, store, dir, logger)

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

	// Simulate reviewer spawn -- captures fingerprint of current state.
	agentInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"subagent_type": "implementation-reviewer"},
	})

	stdout.Reset()

	err := handleAgentPre(agentInput, &stdout, store, dir, logger)
	require.NoError(t, err)

	// Stop should allow through since reviewer ran against current state.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err = handleStop(stopInput, &stdout, store, dir, logger)
	require.NoError(t, err)
	assert.Empty(t, stdout.Bytes(), "Stop should allow through when reviewer ran against current state")
}

func TestStop_BlocksWhenCommittedEditsAfterReviewer(t *testing.T) {
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
	_ = handleExitPlanModePre(planInput, &stdout, store, dir, logger)

	stdout.Reset()
	_ = handleExitPlanModePre(planInput, &stdout, store, dir, logger)

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

	// Reviewer runs and captures fingerprint.
	agentInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"subagent_type": "implementation-reviewer"},
	})

	stdout.Reset()
	_ = handleAgentPre(agentInput, &stdout, store, dir, logger)

	// More edits AFTER the reviewer ran.
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

	// Stop should block since state changed after reviewer.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(stopInput, &stdout, store, dir, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])
}

func TestStop_BlocksWhenUncommittedEditsAfterReviewer(t *testing.T) {
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
	_ = handleExitPlanModePre(planInput, &stdout, store, dir, logger)

	stdout.Reset()
	_ = handleExitPlanModePre(planInput, &stdout, store, dir, logger)

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

	// Reviewer runs and captures fingerprint.
	agentInput := makeHookJSON(t, HookInput{
		SessionID: "s1",
		ToolInput: map[string]any{"subagent_type": "implementation-reviewer"},
	})

	stdout.Reset()
	_ = handleAgentPre(agentInput, &stdout, store, dir, logger)

	// Uncommitted edit AFTER the reviewer ran.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "impl.txt"), []byte("changed\n"), 0o644))

	// Stop should block since working tree changed.
	stopInput := makeHookJSON(t, HookInput{SessionID: "s1"})
	stdout.Reset()

	err := handleStop(stopInput, &stdout, store, dir, logger)
	require.NoError(t, err)

	var result map[string]any

	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	assert.Equal(t, "block", result["decision"])
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
