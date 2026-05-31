package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	require.Len(t, r.Content, 1)

	tc, ok := r.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	return tc.Text
}

func writeTestKubeconfig(t *testing.T, cfg kubeConfig) string {
	t.Helper()

	data, err := yaml.Marshal(&cfg)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	return path
}

func testKubeconfig() kubeConfig {
	return kubeConfig{
		APIVersion:     "v1",
		Kind:           "Config",
		CurrentContext: "prod",
		Clusters: []namedCluster{
			{Name: "prod-cluster", Cluster: map[string]any{"server": "https://prod.example.com"}},
			{Name: "staging-cluster", Cluster: map[string]any{"server": "https://staging.example.com"}},
			{Name: "dev-cluster", Cluster: map[string]any{"server": "https://dev.example.com"}},
		},
		Contexts: []namedContext{
			{Name: "prod", Context: contextDetails{Cluster: "prod-cluster", User: "admin"}},
			{
				Name:    "staging",
				Context: contextDetails{Cluster: "staging-cluster", User: "dev-user", Namespace: "default"},
			},
			{Name: "dev", Context: contextDetails{Cluster: "dev-cluster", User: "dev-user"}},
		},
		Users: []namedUser{
			{Name: "admin", User: map[string]any{"token": "admin-token"}},
			{Name: "dev-user", User: map[string]any{"token": "dev-token"}},
		},
	}
}

func TestResolveKubeconfigPath(t *testing.T) { //nolint:tparallel // subtests use t.Setenv
	t.Run("flag set", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "/custom/kubeconfig", resolveHostKubeconfigPath("/custom/kubeconfig"))
	})

	t.Run("env set", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("KUBECONFIG_HOST", "")
		t.Setenv("KUBECONFIG", "/env/kubeconfig")

		assert.Equal(t, "/env/kubeconfig", resolveHostKubeconfigPath(""))
	})

	t.Run("KUBECONFIG_HOST wins over KUBECONFIG", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		// The Claude Code launcher wrapper sets KUBECONFIG to a
		// per-session symlink and preserves the user's original
		// KUBECONFIG as KUBECONFIG_HOST. mcp-kubectx must read the
		// preserved value when listing contexts or creating an SA.
		t.Setenv("KUBECONFIG_HOST", "/user/kubeconfig")
		t.Setenv("KUBECONFIG", "/run/user/1000/claude-kubectx.42/kubeconfig")

		assert.Equal(t, "/user/kubeconfig", resolveHostKubeconfigPath(""))
	})

	t.Run("default", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("KUBECONFIG_HOST", "")
		t.Setenv("KUBECONFIG", "")

		home, err := os.UserHomeDir()
		require.NoError(t, err)

		assert.Equal(t, filepath.Join(home, ".kube", "config"), resolveHostKubeconfigPath(""))
	})
}

func TestSessionDir(t *testing.T) {
	t.Parallel()

	t.Run("removes lastOutputPath on cleanup", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		outPath := filepath.Join(dir, "kubeconfig.yaml")
		require.NoError(t, os.WriteFile(outPath, []byte("placeholder"), 0o600))

		h := &handler{
			pid:            9999,
			envLookup:      constLookup(""),
			lastOutputPath: outPath,
		}

		cleanup := h.sessionDir()

		cleanup()

		_, err := os.Stat(outPath)
		assert.True(t, os.IsNotExist(err), "kubeconfig file should be removed after cleanup")
	})

	t.Run("noop when lastOutputPath empty", func(t *testing.T) {
		t.Parallel()

		h := &handler{pid: 6666, envLookup: constLookup("")}

		h.sessionDir()()
	})

	t.Run("missing file is best-effort", func(t *testing.T) {
		t.Parallel()

		h := &handler{
			pid:            5555,
			envLookup:      constLookup(""),
			lastOutputPath: filepath.Join(t.TempDir(), "never-written.yaml"),
		}

		h.sessionDir()()
	})
}

// TestSessionDirCleanupRunsResourceCleanupWhenOutputSet asserts
// that the cleanup returned by [handler.sessionDir] still drains
// registered K8s resource cleanups when --output is set.
func TestSessionDirCleanupRunsResourceCleanupWhenOutputSet(t *testing.T) {
	t.Parallel()

	h := &handler{
		outputPath: filepath.Join(t.TempDir(), "out"),
		pid:        1234,
		envLookup:  constLookup(""),
	}

	var called bool

	h.registerCleanup(func(_ context.Context) { called = true })

	h.sessionDir()()

	require.True(t, called, "resource cleanup must run when outputPath is set")
}

// TestSessionDirCleanupRunsResourceCleanupWhenOutputUnset asserts
// the same drain happens when no --output is set, pinning the
// contract on both branches so future refactors do not reintroduce
// the no-op case.
func TestSessionDirCleanupRunsResourceCleanupWhenOutputUnset(t *testing.T) {
	t.Parallel()

	h := &handler{pid: 6543, envLookup: constLookup("")}

	var called bool

	h.registerCleanup(func(_ context.Context) { called = true })

	h.sessionDir()()

	require.True(t, called, "resource cleanup must run when outputPath is unset")
}

// TestDrainSweepReturnsWhenGoroutineCompletes pins the fast path:
// a sweep goroutine that finishes before the timeout lets
// drainSweep return immediately. The deferred Done is what makes
// the WaitGroup wakeable; absence of a timeout-induced log line
// is verified indirectly (drainSweep does not panic and returns
// promptly).
func TestDrainSweepReturnsWhenGoroutineCompletes(t *testing.T) {
	t.Parallel()

	h := &handler{}
	h.sweepWG.Go(func() {})

	done := make(chan struct{})

	go func() {
		defer close(done)

		h.drainSweep()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("drainSweep blocked after the sweep goroutine returned")
	}
}

// TestDrainSweepHonorsTimeout pins the slow-path safety net: a
// hung sweep goroutine must not block process shutdown. The test
// installs a short package-level timeout, kicks off a goroutine
// that never returns until the test ends, and asserts drainSweep
// exits within the bound.
//
//nolint:paralleltest // mutates package-level sweepDrainTimeout
func TestDrainSweepHonorsTimeout(t *testing.T) {
	prev := sweepDrainTimeout
	sweepDrainTimeout = 50 * time.Millisecond

	t.Cleanup(func() { sweepDrainTimeout = prev })

	h := &handler{}

	release := make(chan struct{})
	h.sweepWG.Go(func() { <-release })

	t.Cleanup(func() { close(release) })

	start := time.Now()

	h.drainSweep()

	elapsed := time.Since(start)

	assert.Less(t, elapsed, 1*time.Second,
		"drainSweep must return promptly after the timeout fires")
}

// fakeRunHost records each (sub, args) pair and returns canned
// stdout/error pairs by subcommand. It substitutes for the real
// shell-out so handler.list and handler.selectCtx can be tested
// without touching the cluster.
type fakeRunHost struct {
	stdout map[string][]byte
	errs   map[string]error
	calls  []fakeRunHostCall
}

type fakeRunHostCall struct {
	sub  string
	args []string
}

func (f *fakeRunHost) run(_ context.Context, sub string, args []string) ([]byte, error) {
	f.calls = append(f.calls, fakeRunHostCall{sub: sub, args: append([]string(nil), args...)})

	return f.stdout[sub], f.errs[sub]
}

// TestList pins the serve-side plumbing: list shells out to `host
// list` and relays the merged result. With no local kubeconfig
// ($CLAUDE_KUBECTX_LOCAL cleared) there are no local contexts and no
// merged current-context, so the host list's own ` (current)` suffix
// is stripped and no marker is reapplied.
func TestList(t *testing.T) { //nolint:tparallel,paralleltest // subtests use t.Setenv
	tests := map[string]struct {
		stdout  []byte
		err     error
		want    string
		isError bool
	}{
		"strips host current marker when no local current-context": {
			stdout: []byte("Available contexts:\n- prod (current)\n- staging\n- dev\n"),
			want:   "Available contexts:\n- prod\n- staging\n- dev\n",
		},
		"empty": {
			stdout: []byte("No contexts found."),
			want:   "No contexts found.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Cannot use t.Parallel with t.Setenv.
			t.Setenv("CLAUDE_KUBECTX_LOCAL", "")

			fake := &fakeRunHost{
				stdout: map[string][]byte{"list": tc.stdout},
				errs:   map[string]error{"list": tc.err},
			}

			h := &handler{
				kubeconfigPath: "/k",
				envLookup:      constLookup(""),
				runHost:        fake.run,
			}

			result, _, err := h.list(t.Context(), nil, ListInput{})
			require.NoError(t, err)
			assert.Equal(t, tc.isError, result.IsError)
			assert.Equal(t, tc.want, resultText(t, result))

			require.Len(t, fake.calls, 1)
			assert.Equal(t, "list", fake.calls[0].sub)
			assert.Contains(t, fake.calls[0].args, "/k")
		})
	}
}

// TestListMergesLocalContexts pins the merged view: local contexts
// are appended tagged `(local)`, the host list's own `(current)`
// suffix is dropped, and the single `(current)` marker reflects the
// local file's current-context.
func TestListMergesLocalContexts(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	local := writeTestKubeconfig(t, kubeConfig{
		APIVersion:     "v1",
		Kind:           "Config",
		CurrentContext: "kind-dev",
		Contexts: []namedContext{
			{Name: "kind-dev", Context: contextDetails{Cluster: "kind", User: "kind"}},
		},
	})
	t.Setenv("CLAUDE_KUBECTX_LOCAL", local)

	fake := &fakeRunHost{
		stdout: map[string][]byte{
			"list": []byte("Available contexts:\n- prod (current)\n- staging\n"),
		},
	}

	h := &handler{
		kubeconfigPath: "/k",
		envLookup:      constLookup(""),
		runHost:        fake.run,
	}

	result, _, err := h.list(t.Context(), nil, ListInput{})
	require.NoError(t, err)
	require.False(t, result.IsError)

	assert.Equal(t,
		"Available contexts:\n- prod\n- staging\n- kind-dev (local) (current)\n",
		resultText(t, result),
	)
}

// runListWithHost drives handler.list with a canned `host list`
// stdout and returns the merged text, asserting the call succeeds.
// The local/guest sources are taken from the ambient
// $CLAUDE_KUBECTX_LOCAL / $CLAUDE_KUBECTX_GUEST_CONFIG the caller set.
func runListWithHost(t *testing.T, hostOut string) string {
	t.Helper()

	fake := &fakeRunHost{stdout: map[string][]byte{"list": []byte(hostOut)}}
	h := &handler{
		kubeconfigPath: "/k",
		envLookup:      constLookup(""),
		runHost:        fake.run,
	}

	result, _, err := h.list(t.Context(), nil, ListInput{})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	return resultText(t, result)
}

// TestListMergesGuestContexts pins the three-way merged view: when
// $CLAUDE_KUBECTX_GUEST_CONFIG is set, the guest's ~/.kube/config
// contexts join local.yaml's as `(local)`, a name in both is listed
// once, a missing guest file is tolerated, and a guest name colliding
// with an external one drops the external line (local/guest wins).
func TestListMergesGuestContexts(t *testing.T) { //nolint:tparallel,paralleltest // subtests use t.Setenv
	t.Run("guest-only contexts enumerated as local", func(t *testing.T) {
		local := writeTestKubeconfig(t, kubeConfig{
			APIVersion: "v1", Kind: "Config", CurrentContext: "tald",
		})
		guest := writeTestKubeconfig(t, kubeConfig{
			APIVersion: "v1", Kind: "Config",
			Contexts: []namedContext{
				{Name: "tald", Context: contextDetails{Cluster: "tald", User: "tald"}},
			},
		})
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", guest)

		assert.Equal(t,
			"Available contexts:\n- prod\n- staging\n- tald (local) (current)\n",
			runListWithHost(t, "Available contexts:\n- prod (current)\n- staging\n"),
		)
	})

	t.Run("name in both local and guest is listed once", func(t *testing.T) {
		local := writeTestKubeconfig(t, kubeConfig{
			APIVersion: "v1", Kind: "Config", CurrentContext: "shared",
			Contexts: []namedContext{
				{Name: "shared", Context: contextDetails{Cluster: "shared", User: "shared"}},
			},
		})
		guest := writeTestKubeconfig(t, kubeConfig{
			APIVersion: "v1", Kind: "Config",
			Contexts: []namedContext{
				{Name: "shared", Context: contextDetails{Cluster: "shared", User: "shared"}},
				{Name: "tald", Context: contextDetails{Cluster: "tald", User: "tald"}},
			},
		})
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", guest)

		assert.Equal(t,
			"Available contexts:\n- prod\n- shared (local) (current)\n- tald (local)\n",
			runListWithHost(t, "Available contexts:\n- prod\n"),
		)
	})

	t.Run("missing guest file is tolerated", func(t *testing.T) {
		local := writeTestKubeconfig(t, kubeConfig{
			APIVersion: "v1", Kind: "Config", CurrentContext: "kind-dev",
			Contexts: []namedContext{
				{Name: "kind-dev", Context: contextDetails{Cluster: "kind", User: "kind"}},
			},
		})
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", filepath.Join(t.TempDir(), "absent"))

		assert.Equal(t,
			"Available contexts:\n- prod\n- kind-dev (local) (current)\n",
			runListWithHost(t, "Available contexts:\n- prod\n"),
		)
	})

	t.Run("guest name shadows colliding external", func(t *testing.T) {
		local := writeTestKubeconfig(t, kubeConfig{
			APIVersion: "v1", Kind: "Config", CurrentContext: "shared",
		})
		guest := writeTestKubeconfig(t, kubeConfig{
			APIVersion: "v1", Kind: "Config",
			Contexts: []namedContext{
				{Name: "shared", Context: contextDetails{Cluster: "shared", User: "shared"}},
			},
		})
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", guest)

		assert.Equal(t,
			"Available contexts:\n- prod\n- shared (local) (current)\n",
			runListWithHost(t, "Available contexts:\n- prod (current)\n- shared\n"),
		)
	})
}

// TestMergeListOutput pins the pure merge transform, including the
// local-wins collision rule.
func TestMergeListOutput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		hostOut    string
		localNames []string
		current    string
		want       string
	}{
		"external only, external current": {
			hostOut: "Available contexts:\n- prod (current)\n- dev\n",
			current: "prod",
			want:    "Available contexts:\n- prod (current)\n- dev\n",
		},
		"external only, no current": {
			hostOut: "Available contexts:\n- prod (current)\n- dev\n",
			want:    "Available contexts:\n- prod\n- dev\n",
		},
		"local appended and current": {
			hostOut:    "Available contexts:\n- prod\n",
			localNames: []string{"kind-dev"},
			current:    "kind-dev",
			want:       "Available contexts:\n- prod\n- kind-dev (local) (current)\n",
		},
		"local shadows colliding external": {
			hostOut:    "Available contexts:\n- prod (current)\n- shared\n",
			localNames: []string{"shared"},
			current:    "shared",
			want:       "Available contexts:\n- prod\n- shared (local) (current)\n",
		},
		"no contexts at all": {
			hostOut: "No contexts found.",
			want:    "No contexts found.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, mergeListOutput(tc.hostOut, tc.localNames, tc.current))
		})
	}
}

func TestListShellError(t *testing.T) {
	t.Parallel()

	fake := &fakeRunHost{
		errs: map[string]error{"list": fmt.Errorf("%w %q: exit 7: load kubeconfig", ErrHostExec, "list")},
	}

	h := &handler{
		kubeconfigPath: "/k",
		envLookup:      constLookup(""),
		runHost:        fake.run,
	}

	result, _, err := h.list(t.Context(), nil, ListInput{})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "load kubeconfig")
}

// TestListOmitsKubeconfigWhenUnset pins that handler.list does not
// pre-resolve the kubeconfig default on the serve side. The serve
// process may run inside a Lima guest where os.UserHomeDir() differs
// from the host that ultimately runs `host list`; forwarding a
// resolved path would leak the guest's $HOME into the host argv.
func TestListOmitsKubeconfigWhenUnset(t *testing.T) {
	t.Parallel()

	fake := &fakeRunHost{
		stdout: map[string][]byte{"list": []byte("No contexts found.")},
	}

	h := &handler{
		envLookup: constLookup(""),
		runHost:   fake.run,
	}

	_, _, err := h.list(t.Context(), nil, ListInput{})
	require.NoError(t, err)

	require.Len(t, fake.calls, 1)
	assert.NotContains(t, fake.calls[0].args, "--kubeconfig",
		"empty kubeconfigPath must not emit the flag")
}

// TestSelectArgsOmitsKubeconfigWhenUnset pins the same no-leak
// invariant for selectArgs: an unset kubeconfigPath must not emit
// the flag, so `host select` resolves the default in its own
// process where os.UserHomeDir() reflects the host environment.
func TestSelectArgsOmitsKubeconfigWhenUnset(t *testing.T) {
	t.Parallel()

	h := newSelectArgsHandler(nil)
	h.kubeconfigPath = ""

	args := h.selectArgs("prod")
	assert.NotContains(t, args, "--kubeconfig",
		"empty kubeconfigPath must not emit the flag")
}

func TestSelectCtxValidation(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input SelectInput
		err   error
	}{
		"empty context name": {
			input: SelectInput{},
			err:   ErrMissingContext,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeRunHost{}
			h := &handler{
				envLookup: constLookup(""),
				runHost:   fake.run,
				sa:        saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
			}

			result, _, err := h.selectCtx(t.Context(), nil, tc.input)
			require.NoError(t, err)
			assert.True(t, result.IsError)
			require.ErrorIs(t, extractError(result), tc.err)
		})
	}
}

// TestSelectPublishesSidecarSymlink pins that a successful
// selectCtx publishes the per-Claude-session symlink at
// [sidecarSymlinkPath] pointing at the kubeconfig path returned by
// `host select`. hook-router reads kubectl context through this
// symlink (see tools/hook-router/main.go's configFromEnv).
func TestSelectPublishesSidecarSymlink(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	// Cannot use t.Parallel with t.Setenv.
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	// Clear the wrapper env so the PPID-based fallback is exercised.
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

	stdout, err := json.Marshal(HostSelectResult{
		Path:       "/canonical/kubeconfig.yaml",
		SAName:     "claude-sa-1",
		Namespace:  "ns",
		Kubeconfig: "/admin/kube",
		Context:    "prod",
	})
	require.NoError(t, err)

	fake := &fakeRunHost{stdout: map[string][]byte{"select": stdout}}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		outputPath:     "/canonical/kubeconfig.yaml",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	sidecar := sidecarSymlinkPath()
	require.NotEmpty(t, sidecar, "test must run with PPID > 1")
	require.True(t, strings.HasPrefix(sidecar, tmpDir),
		"sidecar must live under the stubbed TMPDIR")

	info, err := os.Lstat(sidecar)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "sidecar must be a symlink")

	target, err := os.Readlink(sidecar)
	require.NoError(t, err)
	assert.Equal(t, "/canonical/kubeconfig.yaml", target)
}

// TestSelectReplacesStaleSidecarSymlink pins that selectCtx
// overwrites a pre-existing symlink at the sidecar path. Without
// atomic replacement the second select would fail with EEXIST and
// silently leave hook-router pointed at the wrong kubeconfig.
func TestSelectReplacesStaleSidecarSymlink(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	// Cannot use t.Parallel with t.Setenv.
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

	sidecar := sidecarSymlinkPath()
	require.NotEmpty(t, sidecar, "test must run with PPID > 1")

	require.NoError(t, os.MkdirAll(filepath.Dir(sidecar), 0o700))
	require.NoError(t, os.Symlink("/dev/null", sidecar))

	stdout, err := json.Marshal(HostSelectResult{
		Path:       "/fresh/kubeconfig.yaml",
		SAName:     "claude-sa-2",
		Namespace:  "ns",
		Kubeconfig: "/admin/kube",
		Context:    "prod",
	})
	require.NoError(t, err)

	fake := &fakeRunHost{stdout: map[string][]byte{"select": stdout}}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		outputPath:     "/fresh/kubeconfig.yaml",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}

	_, _, err = h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)

	target, err := os.Readlink(sidecar)
	require.NoError(t, err)
	assert.Equal(t, "/fresh/kubeconfig.yaml", target)
}

// TestSessionDirCleanupRemovesSidecarSymlink pins that the cleanup
// closure returned by sessionDir removes the sidecar symlink. The
// parent dir is intentionally left in place to avoid racing peer
// serves under the same Claude PPID.
func TestSessionDirCleanupRemovesSidecarSymlink(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	// Cannot use t.Parallel with t.Setenv.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

	sidecar := sidecarSymlinkPath()
	require.NotEmpty(t, sidecar, "test must run with PPID > 1")

	require.NoError(t, os.MkdirAll(filepath.Dir(sidecar), 0o700))
	require.NoError(t, os.Symlink("/somewhere", sidecar))

	h := &handler{pid: 4242, envLookup: constLookup("")}

	h.sessionDir()()

	_, err := os.Lstat(sidecar)
	assert.True(t, os.IsNotExist(err), "sidecar symlink should be removed after cleanup")

	_, err = os.Stat(filepath.Dir(sidecar))
	require.NoError(t, err, "sidecar parent dir should be left in place")
}

// TestSidecarSymlinkPathEnvBranch exercises the env-var lookup in
// [sidecarSymlinkPath]: when the Claude Code launcher wrapper sets
// both $CLAUDE_KUBECTX_DIR and $KUBECONFIG (with KUBECONFIG inside
// the dir), mcp-kubectx writes the symlink at $KUBECONFIG directly.
// Otherwise it falls back to the PPID-based path so non-wrapper
// invocations keep working during the transition.
func TestSidecarSymlinkPathEnvBranch(t *testing.T) { //nolint:tparallel,paralleltest // subtests use t.Setenv
	t.Run("CLAUDE_KUBECTX_SIDECAR wins over colon-list KUBECONFIG", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv. The wrapper sets
		// $KUBECONFIG to a colon-list (local.yaml:sidecar) that the
		// containment branch cannot match; the explicit sidecar var
		// must take precedence so the symlink still resolves.
		dir := "/run/user/1000/claude-kubectx.42"
		sidecar := dir + "/kubeconfig"

		t.Setenv("CLAUDE_KUBECTX_DIR", dir)
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", sidecar)
		t.Setenv("KUBECONFIG", dir+"/local.yaml:"+sidecar)

		assert.Equal(t, sidecar, sidecarSymlinkPath())
	})

	t.Run("KUBECONFIG inside CLAUDE_KUBECTX_DIR: honored verbatim", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

		dir := "/run/user/1000/claude-kubectx.42"
		kc := dir + "/kubeconfig"

		t.Setenv("CLAUDE_KUBECTX_DIR", dir)
		t.Setenv("KUBECONFIG", kc)

		assert.Equal(t, kc, sidecarSymlinkPath())
	})

	t.Run("KUBECONFIG outside CLAUDE_KUBECTX_DIR: falls back to PPID path", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		// Guards the user's real kubeconfig: a stray KUBECONFIG that
		// points outside the wrapper dir (dev mode, ad-hoc invocation)
		// must not be overwritten by publishSidecar.
		t.Setenv("TMPDIR", t.TempDir())
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")
		t.Setenv("CLAUDE_KUBECTX_DIR", "/run/user/1000/claude-kubectx.42")
		t.Setenv("KUBECONFIG", "/home/user/.kube/config")

		got := sidecarSymlinkPath()
		require.NotEmpty(t, got, "test must run with PPID > 1")
		assert.NotEqual(t, "/home/user/.kube/config", got,
			"the user's real kubeconfig must never be returned as the symlink path")
		assert.Contains(t, got, "claude-kubectx",
			"fallback path must live under the PPID-based tree")
	})

	t.Run("CLAUDE_KUBECTX_DIR unset: falls back to PPID path", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("TMPDIR", t.TempDir())
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")
		t.Setenv("CLAUDE_KUBECTX_DIR", "")
		t.Setenv("KUBECONFIG", "/run/user/1000/claude-kubectx.42/kubeconfig")

		got := sidecarSymlinkPath()
		require.NotEmpty(t, got, "test must run with PPID > 1")
		assert.NotEqual(t, "/run/user/1000/claude-kubectx.42/kubeconfig", got,
			"without CLAUDE_KUBECTX_DIR the env branch must not match")
	})

	t.Run("sibling-prefix attack: HasPrefix with trailing sep rejects it", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		// Without the trailing path separator on the prefix check,
		// CLAUDE_KUBECTX_DIR=/run/claude-kubectx.1 would falsely match
		// KUBECONFIG=/run/claude-kubectx.12/kubeconfig and let the
		// publishSidecar write into the wrong session's dir.
		t.Setenv("TMPDIR", t.TempDir())
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")
		t.Setenv("CLAUDE_KUBECTX_DIR", "/run/claude-kubectx.1")
		t.Setenv("KUBECONFIG", "/run/claude-kubectx.12/kubeconfig")

		got := sidecarSymlinkPath()
		require.NotEmpty(t, got, "test must run with PPID > 1")
		assert.NotEqual(t, "/run/claude-kubectx.12/kubeconfig", got,
			"sibling-prefix path must be rejected by the containment check")
	})

	t.Run("KUBECONFIG unset: falls back to PPID path", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("TMPDIR", t.TempDir())
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")
		t.Setenv("CLAUDE_KUBECTX_DIR", "/run/user/1000/claude-kubectx.42")
		t.Setenv("KUBECONFIG", "")

		got := sidecarSymlinkPath()
		require.NotEmpty(t, got, "test must run with PPID > 1")
		assert.Contains(t, got, "claude-kubectx",
			"PPID fallback path must apply when KUBECONFIG is unset")
	})
}

// TestSelectShellsHostSelect pins that handler.selectCtx forwards
// the right argv to host select and parses the JSON result back
// into the MCP success text.
func TestSelectShellsHostSelect(t *testing.T) { //nolint:paralleltest // uses t.Setenv for TMPDIR isolation
	// Cannot use t.Parallel with t.Setenv. selectCtx publishes the
	// hook-router sidecar symlink under TMPDIR; isolate it to
	// t.TempDir so test runs do not pollute the developer's
	// /tmp/claude-kubectx/.
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

	stdout, err := json.Marshal(HostSelectResult{
		Path:       "/tmp/k.yaml",
		SAName:     "claude-sa-1",
		Namespace:  "ns",
		Kubeconfig: "/admin/kube",
		Context:    "prod",
	})
	require.NoError(t, err)

	fake := &fakeRunHost{
		stdout: map[string][]byte{"select": stdout},
	}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		outputPath:     "/tmp/k.yaml",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	text := resultText(t, result)
	assert.Contains(t, text, "Created ServiceAccount for context \"prod\"")
	assert.Contains(t, text, "/tmp/k.yaml")

	require.Len(t, fake.calls, 1)
	assert.Equal(t, "select", fake.calls[0].sub)

	args := fake.calls[0].args
	assert.Equal(t, "prod", args[0])
	assert.Contains(t, args, "--for-guest=false")
	assert.Contains(t, args, "--out-path")
	assert.Contains(t, args, "/tmp/k.yaml")
	assert.Contains(t, args, "--kubeconfig")
	assert.Contains(t, args, "/admin/kube")
}

func newSelectArgsHandler(hosts []string) *handler {
	return &handler{
		kubeconfigPath:  "/k",
		outputPath:      "/tmp/k.yaml",
		allowedAPIHosts: hosts,
		envLookup:       constLookup(""),
		sa:              saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}
}

func TestSelectArgsOmitsAllowedAPIHostsWhenEmpty(t *testing.T) {
	t.Parallel()

	args := newSelectArgsHandler(nil).selectArgs("prod")
	assert.NotContains(t, args, "--allow-apiserver-host",
		"empty allowlist must not emit the flag")
}

// TestSelectArgsForwardsAllowedAPIHosts pins both the order and
// adjacency of the --allow-apiserver-host flag/value pairs by
// asserting they form the literal tail of the argv. Substring
// matching would silently accept padding or interleaving.
func TestSelectArgsForwardsAllowedAPIHosts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		hosts []string
		want  []string
	}{
		"single host": {
			hosts: []string{"a.example.com"},
			want:  []string{"--allow-apiserver-host", "a.example.com"},
		},
		"multiple hosts repeat the flag": {
			hosts: []string{"a.example.com", "b.example.com"},
			want: []string{
				"--allow-apiserver-host", "a.example.com",
				"--allow-apiserver-host", "b.example.com",
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			args := newSelectArgsHandler(tc.hosts).selectArgs("prod")

			require.GreaterOrEqual(t, len(args), len(tc.want))
			assert.Equal(t, tc.want, args[len(args)-len(tc.want):],
				"--allow-apiserver-host flags must form the argv tail")
		})
	}
}

func TestSelectShellError(t *testing.T) {
	t.Parallel()

	fake := &fakeRunHost{
		errs: map[string]error{"select": errors.New("forbidden")},
	}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		outputPath:     "/tmp/k.yaml",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "forbidden")
}

func TestSelectShellInvalidJSON(t *testing.T) {
	t.Parallel()

	fake := &fakeRunHost{
		stdout: map[string][]byte{"select": []byte("not json")},
	}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		outputPath:     "/tmp/k.yaml",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	assert.True(t, result.IsError)
	require.ErrorIs(t, extractError(result), ErrParseHostResult)
}

// TestSelectCtxPopulatesCurrentSA pins that a successful selectCtx
// stores a [currentSA] descriptor populated from the host select
// JSON result plus the handler's expiration.
func TestSelectCtxPopulatesCurrentSA(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

	stdout, err := json.Marshal(HostSelectResult{
		Path:       "/canonical/kubeconfig.yaml",
		SAName:     "claude-sa-new",
		Namespace:  "kube-system",
		Kubeconfig: "/admin/kube",
		Context:    "prod",
	})
	require.NoError(t, err)

	fake := &fakeRunHost{stdout: map[string][]byte{"select": stdout}}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		outputPath:     "/canonical/kubeconfig.yaml",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 7200},
	}

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	got := h.currentSA.Load()
	require.NotNil(t, got, "currentSA must be populated after success")
	assert.Equal(t, "/admin/kube", got.Kubeconfig)
	assert.Equal(t, "prod", got.Context)
	assert.Equal(t, "claude-sa-new", got.SAName)
	assert.Equal(t, "kube-system", got.Namespace)
	assert.Equal(t, 7200, got.Expiration)
}

// TestSelectCtxRestoresCurrentSAOnFailure pins that a host-select
// shell error leaves the prior currentSA descriptor in place so
// kubectl on the existing kubeconfig keeps working until the
// caller selects a new context that does succeed.
func TestSelectCtxRestoresCurrentSAOnFailure(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

	prev := &currentSA{
		Kubeconfig: "/admin/kube",
		Context:    "prod",
		SAName:     "claude-sa-prev",
		Namespace:  "ns",
		Expiration: 3600,
	}

	fake := &fakeRunHost{
		errs: map[string]error{"select": errors.New("forbidden")},
	}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		outputPath:     "/tmp/k.yaml",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}
	h.currentSA.Store(prev)

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.True(t, result.IsError)

	got := h.currentSA.Load()
	require.NotNil(t, got, "prior currentSA must be restored after failure")
	assert.Equal(t, prev, got)
}

// TestSelectCtxRestoresCurrentSAOnInvalidJSON pins the same
// restore semantics on the JSON-parse failure branch.
func TestSelectCtxRestoresCurrentSAOnInvalidJSON(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

	prev := &currentSA{
		Kubeconfig: "/admin/kube",
		Context:    "prod",
		SAName:     "claude-sa-prev",
		Namespace:  "ns",
		Expiration: 3600,
	}

	fake := &fakeRunHost{
		stdout: map[string][]byte{"select": []byte("not json")},
	}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		outputPath:     "/tmp/k.yaml",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}
	h.currentSA.Store(prev)

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.True(t, result.IsError)

	got := h.currentSA.Load()
	require.NotNil(t, got)
	assert.Equal(t, prev, got)
}

// TestSelectExternalWritesLocalCurrentContext pins that the external
// select path records current-context in $CLAUDE_KUBECTX_LOCAL (the
// merged-view source of truth) while leaving the scoped/sidecar
// kubeconfig file byte-for-byte untouched.
func TestSelectExternalWritesLocalCurrentContext(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

	local := filepath.Join(tmp, "local.yaml")
	require.NoError(t, os.WriteFile(local, []byte("apiVersion: v1\nkind: Config\n"), 0o600))
	t.Setenv("CLAUDE_KUBECTX_LOCAL", local)

	scoped := filepath.Join(tmp, "scoped.yaml")
	scopedBytes := []byte("apiVersion: v1\nkind: Config\nclusters: []\n")
	require.NoError(t, os.WriteFile(scoped, scopedBytes, 0o600))

	stdout, err := json.Marshal(HostSelectResult{
		Path:       scoped,
		SAName:     "claude-sa-1",
		Namespace:  "ns",
		Kubeconfig: "/admin/kube",
		Context:    "prod",
	})
	require.NoError(t, err)

	fake := &fakeRunHost{stdout: map[string][]byte{"select": stdout}}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		outputPath:     scoped,
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "prod"})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	cfg, err := loadKubeconfig(local)
	require.NoError(t, err)
	assert.Equal(t, "prod", cfg.CurrentContext,
		"external select must record current-context in the local file")

	got, err := os.ReadFile(scoped)
	require.NoError(t, err)
	assert.Equal(t, scopedBytes, got,
		"the scoped/sidecar kubeconfig must stay byte-for-byte intact")
}

// TestSelectLocalContext pins the local dispatch: a context defined
// only in $CLAUDE_KUBECTX_LOCAL takes the no-shell-out path. No `host
// select` runs, currentSA is cleared (idling the UDS token path),
// current-context is set in the local file, and the prior external
// SA's release closure is drained.
func TestSelectLocalContext(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	tmp := t.TempDir()
	local := filepath.Join(tmp, "local.yaml")

	data, err := yaml.Marshal(&kubeConfig{
		APIVersion: "v1",
		Kind:       "Config",
		Contexts: []namedContext{
			{Name: "kind-dev", Context: contextDetails{Cluster: "kind", User: "kind"}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(local, data, 0o600))
	t.Setenv("CLAUDE_KUBECTX_LOCAL", local)

	fake := &fakeRunHost{}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}
	h.currentSA.Store(&currentSA{Context: "prod", SAName: "claude-sa-prev", Expiration: 3600})

	released := make(chan struct{}, 1)
	h.registerCleanup(func(_ context.Context) { released <- struct{}{} })

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "kind-dev"})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Empty(t, fake.calls, "local select must not shell out to any host subcommand")
	assert.Nil(t, h.currentSA.Load(), "local select must clear currentSA")

	got, err := loadKubeconfig(local)
	require.NoError(t, err)
	assert.Equal(t, "kind-dev", got.CurrentContext)

	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("prior external SA was not released on local select")
	}
}

// TestSelectLocalContextWriteFailureKeepsPriorState pins that a
// setLocalCurrentContext write error on the local path leaves
// currentSA and the prior cleanup list intact, so the prior
// selection keeps working and no SA is leaked.
func TestSelectLocalContextWriteFailureKeepsPriorState(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	if os.Geteuid() == 0 {
		t.Skip("write-permission failure cannot be provoked as root")
	}

	dir := t.TempDir()
	local := filepath.Join(dir, "local.yaml")

	data, err := yaml.Marshal(&kubeConfig{
		APIVersion: "v1",
		Kind:       "Config",
		Contexts: []namedContext{
			{Name: "kind-dev", Context: contextDetails{Cluster: "kind", User: "kind"}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(local, data, 0o600))
	t.Setenv("CLAUDE_KUBECTX_LOCAL", local)

	// Read-only parent: loadKubeconfig still reads the existing file,
	// but writeFileAtomic's tmp create fails.
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:errcheck // best-effort restore so TempDir cleanup can remove dir

	prevSA := &currentSA{Context: "prod", SAName: "claude-sa-prev", Expiration: 3600}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		envLookup:      constLookup(""),
		runHost:        (&fakeRunHost{}).run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}
	h.currentSA.Store(prevSA)

	var released bool

	h.registerCleanup(func(_ context.Context) { released = true })

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "kind-dev"})
	require.NoError(t, err)
	require.True(t, result.IsError, "write failure must surface as a tool error")

	assert.Equal(t, prevSA, h.currentSA.Load(),
		"prior SA must be intact after a local-path write failure")
	assert.False(t, released, "prior cleanup must not be drained on write failure")

	h.mu.Lock()
	n := len(h.cleanupFuncs)
	h.mu.Unlock()
	assert.Equal(t, 1, n, "prior cleanup list must be untouched on write failure")
}

// TestSelectGuestContextRoutesLocal pins that a context defined only
// in the guest's ~/.kube/config ($CLAUDE_KUBECTX_GUEST_CONFIG) routes
// to the local path: no `host select` shell-out, no SA, currentSA
// cleared, and current-context written to local.yaml only. Its creds
// resolve from the guest config (the middle merge entry); local.yaml
// is never given a cluster/user entry for it.
func TestSelectGuestContextRoutesLocal(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	tmp := t.TempDir()
	local := filepath.Join(tmp, "local.yaml")
	require.NoError(t, os.WriteFile(local, []byte("apiVersion: v1\nkind: Config\n"), 0o600))

	guest := writeTestKubeconfig(t, kubeConfig{
		APIVersion: "v1", Kind: "Config",
		Contexts: []namedContext{
			{Name: "tald", Context: contextDetails{Cluster: "tald", User: "tald"}},
		},
	})

	t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
	t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", guest)

	fake := &fakeRunHost{}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}
	h.currentSA.Store(&currentSA{Context: "prod", SAName: "claude-sa-prev", Expiration: 3600})

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "tald"})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Empty(t, fake.calls, "guest-local select must not shell out to any host subcommand")
	assert.Nil(t, h.currentSA.Load(), "guest-local select must clear currentSA")

	got, err := loadKubeconfig(local)
	require.NoError(t, err)
	assert.Equal(t, "tald", got.CurrentContext, "selection must be recorded in local.yaml")
	assert.Empty(t, got.Contexts, "guest context must not be copied into local.yaml")
}

// TestSelectGuestCollisionRoutesLocal pins the safety-critical
// precedence: when a guest context name collides with an external
// context name, select routes local (guest wins, no SA minted). Were
// it to route external, `host select` would mint an SA against an
// apiserver unreachable from the host.
func TestSelectGuestCollisionRoutesLocal(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	tmp := t.TempDir()
	local := filepath.Join(tmp, "local.yaml")
	require.NoError(t, os.WriteFile(local, []byte("apiVersion: v1\nkind: Config\n"), 0o600))

	guest := writeTestKubeconfig(t, kubeConfig{
		APIVersion: "v1", Kind: "Config",
		Contexts: []namedContext{
			{Name: "shared", Context: contextDetails{Cluster: "shared", User: "shared"}},
		},
	})

	t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
	t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", guest)

	fake := &fakeRunHost{}

	h := &handler{
		kubeconfigPath: "/admin/kube",
		envLookup:      constLookup(""),
		runHost:        fake.run,
		sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
	}

	result, _, err := h.selectCtx(t.Context(), nil, SelectInput{Context: "shared"})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.Empty(t, fake.calls, "a colliding name must route local, never mint an SA")

	got, err := loadKubeconfig(local)
	require.NoError(t, err)
	assert.Equal(t, "shared", got.CurrentContext)
}

// TestSessionDirCleanupOrdering pins that socketShutdown drains
// in-flight handlers (waiting for them to exit) before the cleanup
// closure proceeds to unlink files. Drives this with a runHost
// that holds a handler in-flight via a shared channel; the test
// verifies the unlink does not happen until the handler is
// released.
func TestSessionDirCleanupOrdering(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

	socketPath := filepath.Join(t.TempDir(), "ordering.sock")
	kubeconfigPath := filepath.Join(t.TempDir(), "kubeconfig.yaml")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte("placeholder"), 0o600))

	release := make(chan struct{})
	entered := make(chan struct{})

	h := &handler{
		pid:            7777,
		envLookup:      constLookup(""),
		lastOutputPath: kubeconfigPath,
		socketPath:     socketPath,
	}
	h.runHost = func(_ context.Context, _ string, _ []string) ([]byte, error) {
		close(entered)
		<-release

		return []byte("ok"), nil
	}
	h.currentSA.Store(&currentSA{
		Kubeconfig: "/admin/kube",
		Context:    "prod",
		SAName:     "sa",
		Namespace:  "ns",
		Expiration: 3600,
	})

	listener, listenCleanup, err := h.listenSocket(t.Context(), socketPath, "ordering-inst")
	require.NoError(t, err)

	t.Cleanup(listenCleanup)

	_, err = os.Stat(sidecarPath(socketPath))
	require.NoError(t, err, "sidecar must exist after listenSocket with a non-empty instance id")

	h.mu.Lock()
	h.socketListener = listener
	h.mu.Unlock()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go h.serveSocket(ctx, listener, &h.socketWG)

	connDone := make(chan struct{})

	go func() {
		defer close(connDone)

		conn, dialErr := net.DialTimeout("unix", socketPath, 2*time.Second) //nolint:noctx // synchronous test fixture
		if !assert.NoError(t, dialErr) {
			return
		}

		defer conn.Close() //nolint:errcheck // best-effort test cleanup

		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck // best-effort test deadline

		_, _ = io.ReadAll(conn) //nolint:errcheck // test only consumes EOF, payload checked elsewhere
	}()

	<-entered

	cancel()

	cleanup := h.sessionDir()

	cleanupDone := make(chan struct{})

	go func() {
		cleanup()
		close(cleanupDone)
	}()

	select {
	case <-cleanupDone:
		t.Fatal("sessionDir cleanup returned before in-flight socket handler completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case <-cleanupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("sessionDir cleanup did not complete after handler released")
	}

	<-connDone

	_, err = os.Stat(socketPath)
	assert.True(t, os.IsNotExist(err), "socket file must be unlinked by cleanup")

	_, err = os.Stat(sidecarPath(socketPath))
	assert.True(t, os.IsNotExist(err), "sidecar file must be unlinked by cleanup")

	_, err = os.Stat(kubeconfigPath)
	assert.True(t, os.IsNotExist(err), "kubeconfig file must be unlinked by cleanup")
}

// extractError wraps the tool result text as an error for ErrorIs
// matching against sentinel errors.
type resultTextError string

func (e resultTextError) Error() string { return string(e) }

func (e resultTextError) Is(target error) bool {
	return strings.Contains(string(e), target.Error())
}

func extractError(r *mcp.CallToolResult) error {
	if !r.IsError || len(r.Content) == 0 {
		return nil
	}

	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		return nil
	}

	return resultTextError(tc.Text)
}
