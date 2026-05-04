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
		t.Setenv("KUBECONFIG", "/env/kubeconfig")

		assert.Equal(t, "/env/kubeconfig", resolveHostKubeconfigPath(""))
	})

	t.Run("default", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
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

func TestList(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		stdout  []byte
		err     error
		want    string
		isError bool
	}{
		"success": {
			stdout: []byte("Available contexts:\n- prod (current)\n- staging\n- dev\n"),
			want:   "Available contexts:\n- prod (current)\n- staging\n- dev\n",
		},
		"empty": {
			stdout: []byte("No contexts found."),
			want:   "No contexts found.",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

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

// TestSelectShellsHostSelect pins that handler.selectCtx forwards
// the right argv to host select and parses the JSON result back
// into the MCP success text.
func TestSelectShellsHostSelect(t *testing.T) { //nolint:paralleltest // uses t.Setenv for TMPDIR isolation
	// Cannot use t.Parallel with t.Setenv. selectCtx publishes the
	// hook-router sidecar symlink under TMPDIR; isolate it to
	// t.TempDir so test runs do not pollute the developer's
	// /tmp/claude-kubectx/.
	t.Setenv("TMPDIR", t.TempDir())

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

// TestSessionDirCleanupOrdering pins that socketShutdown drains
// in-flight handlers (waiting for them to exit) before the cleanup
// closure proceeds to unlink files. Drives this with a runHost
// that holds a handler in-flight via a shared channel; the test
// verifies the unlink does not happen until the handler is
// released.
func TestSessionDirCleanupOrdering(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv("TMPDIR", t.TempDir())

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

	listener, listenCleanup, err := h.listenSocket(t.Context(), socketPath)
	require.NoError(t, err)

	t.Cleanup(listenCleanup)

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
