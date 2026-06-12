package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
)

// TestSelectDrainsPriorCleanupOnSuccess pins that handler.selectCtx
// drains the prior SA's release closure as soon as the new one is
// fully provisioned. Without this, concurrent Claude sessions that
// share an --output path leak every prior SA until process exit.
func TestSelectDrainsPriorCleanupOnSuccess(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	neutralizeWrapperEnv(t)

	stdout1, err := json.Marshal(HostSelectResult{
		Path: "/k", SAName: "claude-sa-1", Namespace: "ns",
		Kubeconfig: "/admin", Context: "prod",
	})
	require.NoError(t, err)

	stdout2, err := json.Marshal(HostSelectResult{
		Path: "/k", SAName: "claude-sa-2", Namespace: "ns",
		Kubeconfig: "/admin", Context: "prod",
	})
	require.NoError(t, err)

	type call struct {
		sub  string
		args []string
	}

	var (
		calls []call
		step  int
	)

	fake := func(_ context.Context, sub string, args []string) ([]byte, error) {
		calls = append(calls, call{sub: sub, args: append([]string(nil), args...)})

		switch sub {
		case "select":
			step++

			if step == 1 {
				return stdout1, nil
			}

			return stdout2, nil

		case "release":
			return nil, nil
		}

		return nil, errors.New("unexpected sub: " + sub)
	}

	h := &handler{
		kubeconfigPath: "/admin",
		outputPath:     "/k",
		envLookup:      constLookup(""),
		runHost:        fake,
		sa:             serviceaccount.Config{Role: "view", RoleKind: "ClusterRole", Expiration: 3600},
	}

	// First select registers a release closure.
	r1, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, r1.IsError, resultText(t, r1))

	require.Equal(t, []call{{sub: "select", args: calls[0].args}}, calls,
		"first select should not trigger any release calls")

	// Second select should drain the previous closure (one
	// release call) only after host select returns.
	r2, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, r2.IsError, resultText(t, r2))

	require.Len(t, calls, 3)
	assert.Equal(t, "select", calls[0].sub)
	assert.Equal(t, "select", calls[1].sub)
	assert.Equal(t, "release", calls[2].sub)
	assert.Contains(t, calls[2].args, "claude-sa-1")

	h.mu.Lock()
	fns := h.cleanupFuncs
	h.mu.Unlock()

	require.Len(t, fns, 1, "only the most recent cleanup should remain")
}

// TestSelectRestoresPrevCleanupOnFailure pins the other half of the
// drain contract: if host select fails, the previous closure must
// be restored so it still runs at process shutdown.
func TestSelectRestoresPrevCleanupOnFailure(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	neutralizeWrapperEnv(t)

	stdout1, err := json.Marshal(HostSelectResult{
		Path: "/k", SAName: "claude-sa-1", Namespace: "ns",
		Kubeconfig: "/admin", Context: "prod",
	})
	require.NoError(t, err)

	var (
		step         int
		releaseCalls int
	)

	fake := func(_ context.Context, sub string, _ []string) ([]byte, error) {
		switch sub {
		case "select":
			step++

			if step == 1 {
				return stdout1, nil
			}

			return nil, errors.New("forbidden")

		case "release":
			releaseCalls++

			return nil, nil
		}

		return nil, errors.New("unexpected sub: " + sub)
	}

	h := &handler{
		kubeconfigPath: "/admin",
		outputPath:     "/k",
		envLookup:      constLookup(""),
		runHost:        fake,
		sa:             serviceaccount.Config{Role: "view", RoleKind: "ClusterRole", Expiration: 3600},
	}

	r1, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, r1.IsError, resultText(t, r1))

	r2, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.True(t, r2.IsError, "second select should fail")

	assert.Zero(t, releaseCalls, "prev release must not run when new provision fails")

	h.mu.Lock()
	fns := h.cleanupFuncs
	h.mu.Unlock()

	require.Len(t, fns, 1, "prev cleanup must be restored after failed provision")

	// Process-shutdown cleanup runs the restored prev closure.
	for _, fn := range fns {
		fn(t.Context())
	}

	assert.Equal(t, 1, releaseCalls, "prev release must run on shutdown")
}

// TestSelectDoesNotDrainEmptyPrev guards against accidentally
// emitting a host release call when there is no prior cleanup.
func TestSelectDoesNotDrainEmptyPrev(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	neutralizeWrapperEnv(t)

	stdout, err := json.Marshal(HostSelectResult{
		Path: "/k", SAName: "claude-sa-1", Namespace: "ns",
		Kubeconfig: "/admin", Context: "prod",
	})
	require.NoError(t, err)

	subs := []string{}

	fake := func(_ context.Context, sub string, _ []string) ([]byte, error) {
		subs = append(subs, sub)

		return stdout, nil
	}

	h := &handler{
		kubeconfigPath: "/admin",
		outputPath:     "/k",
		envLookup:      constLookup(""),
		runHost:        fake,
		sa:             serviceaccount.Config{Role: "view", RoleKind: "ClusterRole", Expiration: 3600},
	}

	r, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	assert.Equal(t, []string{"select"}, subs, "no host release should be shelled when prev is empty")
}
