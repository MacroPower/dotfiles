package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubeconfig"
)

// testBinding is the binding of the SA of each `host select` result
// that the tests of selectCtx fake.
const testBinding = `ClusterRole "view" (cluster-scoped)`

// testProdCluster returns the cluster entry of the context prod of
// [testKubeconfig].
func testProdCluster() kubeconfig.NamedCluster {
	return kubeconfig.NamedCluster{
		Name:    "prod-cluster",
		Cluster: map[string]any{"server": "https://prod.example.com"},
	}
}

// testSelectResult returns the result of a `host select` of the
// context prod that created the SA ns/saName.
func testSelectResult(saName string) HostSelectResult {
	return HostSelectResult{
		Cluster:   testProdCluster(),
		SAName:    saName,
		Namespace: "ns",
		Context:   "prod",
		Binding:   testBinding,
	}
}

// TestSelectDrainsPriorCleanupOnSuccess pins that handler.selectCtx
// drains the prior SA's release closure as soon as the new one is
// fully provisioned. Without this, concurrent Claude sessions that
// share an --output path leak every prior SA until process exit.
func TestSelectDrainsPriorCleanupOnSuccess(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	neutralizeWrapperEnv(t)

	stdout1, err := json.Marshal(testSelectResult("claude-sa-1"))
	require.NoError(t, err)

	stdout2, err := json.Marshal(testSelectResult("claude-sa-2"))
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
		outputPath: filepath.Join(t.TempDir(), "k.yaml"),
		envLookup:  constLookup(""),
		runHost:    fake,
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

	stdout1, err := json.Marshal(testSelectResult("claude-sa-1"))
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
		outputPath: filepath.Join(t.TempDir(), "k.yaml"),
		envLookup:  constLookup(""),
		runHost:    fake,
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

// TestSelectRunsFailedReleaseOneTime pins that serve runs each
// release closure one time, whatever the exit status of `host
// release`. A release that fails during the drain of a later select
// leaves no closure behind. So the shutdown cleanup runs only the
// release of the latest SA.
func TestSelectRunsFailedReleaseOneTime(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	neutralizeWrapperEnv(t)

	var (
		step     int
		released []string
	)

	fake := func(_ context.Context, sub string, args []string) ([]byte, error) {
		switch sub {
		case "select":
			step++

			return json.Marshal(testSelectResult(fmt.Sprintf("claude-sa-%d", step)))

		case "release":
			i := slices.Index(args, "--sa")
			require.NotEqual(t, -1, i, "release args must name the SA")
			require.Less(t, i+1, len(args), "--sa must carry a value")

			released = append(released, args[i+1])

			return nil, fmt.Errorf("%w %q: exit 1: release refused", ErrHostExec, sub)
		}

		return nil, errors.New("unexpected sub: " + sub)
	}

	h := &handler{
		outputPath: filepath.Join(t.TempDir(), "k.yaml"),
		envLookup:  constLookup(""),
		runHost:    fake,
	}

	for range 2 {
		r, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
		require.NoError(t, err)
		require.False(t, r.IsError, resultText(t, r))
	}

	assert.Equal(t, []string{"claude-sa-1"}, released,
		"the second select must drain the release of the first SA")

	h.mu.Lock()
	fns := h.cleanupFuncs
	h.mu.Unlock()

	// Process-shutdown cleanup runs each remaining closure.
	for _, fn := range fns {
		fn(t.Context())
	}

	assert.Equal(t, []string{"claude-sa-1", "claude-sa-2"}, released,
		"shutdown must not run the failed release of the first SA again")
}

// TestSelectDoesNotDrainEmptyPrev guards against accidentally
// emitting a host release call when there is no prior cleanup.
func TestSelectDoesNotDrainEmptyPrev(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	neutralizeWrapperEnv(t)

	stdout, err := json.Marshal(testSelectResult("claude-sa-1"))
	require.NoError(t, err)

	subs := []string{}

	fake := func(_ context.Context, sub string, _ []string) ([]byte, error) {
		subs = append(subs, sub)

		return stdout, nil
	}

	h := &handler{
		outputPath: filepath.Join(t.TempDir(), "k.yaml"),
		envLookup:  constLookup(""),
		runHost:    fake,
	}

	r, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	assert.Equal(t, []string{"select"}, subs, "no host release should be shelled when prev is empty")
}

// TestSelectWritesScopedKubeconfig pins the scoped kubeconfig that
// serve writes after a `host select`. serve writes it on its own
// machine, in [statedir.RunDir] under a name of its pid and its env,
// with mode 0600. The file holds the cluster entry of the result and
// one context of that cluster, whose user is the new SA. Its
// user.exec block runs the bare `mcp-kubectx exec-plugin` against
// the socket of serve, in the host env and in the guest env alike.
func TestSelectWritesScopedKubeconfig(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	tests := map[string]struct {
		guest string
		want  string
		sock  string
	}{
		"host env": {
			want: "kubeconfig.4242.host.yaml",
			sock: "/state/mcp-kubectx-run/serve.0.host.sock",
		},
		"guest env": {
			guest: "1",
			want:  "kubeconfig.4242.guest.yaml",
			sock:  "/state/mcp-kubectx-run/serve.3.guest.sock",
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests use t.Setenv
		t.Run(name, func(t *testing.T) {
			neutralizeWrapperEnv(t)

			stateHome := t.TempDir()
			t.Setenv("XDG_STATE_HOME", stateHome)

			stdout, err := json.Marshal(testSelectResult("claude-sa-1"))
			require.NoError(t, err)

			fake := &fakeRunHost{stdout: map[string][]byte{"select": stdout}}

			h := &handler{
				pid:        4242,
				socketPath: tc.sock,
				envLookup:  constLookup(tc.guest),
				runHost:    fake.run,
			}

			result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			path := filepath.Join(stateHome, "mcp-kubectx-run", tc.want)
			assert.Contains(t, resultText(t, result), "Kubeconfig written to "+path)

			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

			cfg, err := kubeconfig.Load(path)
			require.NoError(t, err)
			assert.Equal(t, "prod", cfg.CurrentContext)
			assert.Equal(t, []kubeconfig.NamedCluster{testProdCluster()}, cfg.Clusters)
			assert.Equal(t, []kubeconfig.NamedContext{{
				Name: "prod",
				Context: kubeconfig.Context{
					Cluster:   "prod-cluster",
					User:      "claude-sa-1",
					Namespace: "ns",
				},
			}}, cfg.Contexts)

			plugin := readKubeconfigExec(t, path)
			assert.Equal(t, "mcp-kubectx", plugin.Command,
				"command must be the bare program name, not workmux or an absolute path")
			assert.Equal(t, []string{"exec-plugin", "--socket", tc.sock}, plugin.Args)

			sidecar, err := os.Readlink(sidecarSymlinkPath())
			require.NoError(t, err)
			assert.Equal(t, path, sidecar)
		})
	}
}

// TestSelectReleasesServiceAccountWhenWriteFails pins what selectCtx
// does when it cannot write the scoped kubeconfig. No kubeconfig
// names the new SA, so selectCtx releases it at once. It keeps the
// prior selection, its SA, and its release closure, as after a
// failed `host select`.
func TestSelectReleasesServiceAccountWhenWriteFails(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	neutralizeWrapperEnv(t)

	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	stdout, err := json.Marshal(testSelectResult("claude-sa-new"))
	require.NoError(t, err)

	var released [][]string

	fake := func(_ context.Context, sub string, args []string) ([]byte, error) {
		switch sub {
		case "select":
			return stdout, nil
		case "release":
			released = append(released, append([]string(nil), args...))

			return nil, nil
		}

		return nil, errors.New("unexpected sub: " + sub)
	}

	prev := &currentSA{Context: "prod", SAName: "claude-sa-prev", Namespace: "ns"}

	h := &handler{
		outputPath: filepath.Join(blocker, "k.yaml"),
		envLookup:  constLookup(""),
		runHost:    fake,
		hostID:     testHostID,
		instanceID: testInstanceID,
	}
	h.currentSA.Store(prev)

	prevReleased := false

	h.registerCleanup(func(context.Context) { prevReleased = true })

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.ErrorIs(t, extractError(result), kubeconfig.ErrWrite)

	assert.Equal(t, [][]string{{
		"--context", "prod",
		"--sa", "claude-sa-new",
		"--namespace", "ns",
		"--host-id", testHostID,
		"--instance-id", testInstanceID,
	}}, released, "the new SA must be released")

	assert.Equal(t, prev, h.currentSA.Load())
	assert.False(t, prevReleased)

	h.mu.Lock()
	fns := h.cleanupFuncs
	h.mu.Unlock()

	require.Len(t, fns, 1, "the prior release closure must stay")
}
