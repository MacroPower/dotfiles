package main

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSandbox(t *testing.T) {
	t.Parallel()

	t.Run("off returns noop", func(t *testing.T) {
		t.Parallel()

		s, err := New(SandboxOff)
		require.NoError(t, err)
		assert.Equal(t, "noop", s.Name())
	})

	t.Run("unknown mode errors", func(t *testing.T) {
		t.Parallel()

		_, err := New("nonsense")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrSandbox)
	})
}

func TestNoopSandboxPreservesCommand(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(context.Background(), "/bin/echo", "hello")
	cmd.Dir = "/tmp"

	require.NoError(t, noopSandbox{}.Wrap(cmd, Policy{}))

	assert.Equal(t, "/bin/echo", cmd.Path)
	assert.Equal(t, []string{"/bin/echo", "hello"}, cmd.Args)
}

func TestPolicyForReassignDoesNotAffectMap(t *testing.T) {
	t.Parallel()

	h := &handler{
		policies: Policies{
			toolInit: {
				AllowedDomains: []string{"example.com"},
				AllowRead:      []string{"/a"},
			},
		},
	}

	got := h.policyFor(toolInit)
	got.AllowRead = mergeAllowRead(got.AllowRead, []string{"/b"})

	assert.Equal(t, []string{"/a"}, h.policies[toolInit].AllowRead,
		"reassigning the local Policy field must not mutate the source map")
}

func TestPolicyForUnknownTool(t *testing.T) {
	t.Parallel()

	h := &handler{policies: Policies{}}
	got := h.policyFor("missing")
	assert.Empty(t, got.AllowedDomains)
	assert.Empty(t, got.AllowRead)
	assert.Empty(t, got.AllowWrite)
}

func TestHandleInitPassesPolicy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stdout: "ok"})
	h := newTofuTestHandler(t, fake)
	h.policies = Policies{
		toolInit: {AllowedDomains: []string{"registry.opentofu.org"}},
	}

	r, _, err := h.handleInit(t.Context(), nil, InitInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	require.Len(t, fake.calls, 1)
	assert.Equal(t, []string{"registry.opentofu.org"}, fake.calls[0].policy.AllowedDomains)
}

func TestHandlePlanPassesInitAndPlanPolicies(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(
		fakeResponse{stdout: "Initialized!"},
		fakeResponse{stdout: "No changes."},
	)
	h := newTofuTestHandler(t, fake)
	h.policies = Policies{
		toolInit: {AllowedDomains: []string{"registry.opentofu.org"}},
		toolPlan: {AllowedDomains: []string{}},
	}

	r, _, err := h.handlePlan(t.Context(), nil, PlanInput{
		WorkingDirectory: dir,
		Init:             true,
	})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	require.Len(t, fake.calls, 2)
	assert.Equal(t, []string{"registry.opentofu.org"}, fake.calls[0].policy.AllowedDomains,
		"init step must use init policy")
	assert.Empty(t, fake.calls[1].policy.AllowedDomains,
		"plan step must use plan policy")
}

func TestHandleValidateRejectsBadAllowedPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor()
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{
		WorkingDirectory: dir,
		AllowedPaths:     []string{"relative/path"},
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError)
	assert.Contains(t, resultText(t, r), "must be absolute")
	assert.Empty(t, fake.calls, "executor must not run when AllowedPaths validation fails")
}
