package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestResolveOutputPath(t *testing.T) { //nolint:tparallel // uses t.Setenv
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	tests := map[string]struct {
		flagVal string
		guest   string
		pid     int
		want    string
	}{
		"flag set": {
			flagVal: "/custom/output",
			pid:     1234,
			want:    "/custom/output",
		},
		"host pid scoped": {
			pid:  1234,
			want: filepath.Join(tmpDir, "mcp-kubectx", "kubeconfig.1234.host.yaml"),
		},
		"guest pid scoped": {
			pid:   1234,
			guest: "1",
			want:  filepath.Join(tmpDir, "mcp-kubectx", "kubeconfig.1234.guest.yaml"),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &handler{
				outputPath: tc.flagVal,
				pid:        tc.pid,
				envLookup:  constLookup(tc.guest),
			}
			assert.Equal(t, tc.want, h.resolveOutputPath())
		})
	}
}

func TestSessionDir(t *testing.T) { //nolint:tparallel // subtests use t.Setenv
	t.Run("creates and cleans up", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		stateHome := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateHome)

		h := &handler{pid: 9999, envLookup: constLookup("")}

		expectedPath := filepath.Join(stateHome, "mcp-kubectx", "kubeconfig.9999.host.yaml")
		expectedDir := filepath.Dir(expectedPath)

		cleanup, err := h.sessionDir()
		require.NoError(t, err)

		_, err = os.Stat(expectedDir)
		require.NoError(t, err, "session directory should exist")

		assert.Equal(t, expectedPath, h.resolveOutputPath())

		// Drop a placeholder file at the resolved path so we can
		// verify cleanup removes it.
		require.NoError(t, os.WriteFile(expectedPath, []byte("placeholder"), 0o600))

		cleanup()

		_, err = os.Stat(expectedPath)
		assert.True(t, os.IsNotExist(err), "kubeconfig file should be removed after cleanup")
	})

	t.Run("idempotent", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("XDG_STATE_HOME", t.TempDir())

		h := &handler{pid: 7777, envLookup: constLookup("")}

		cleanup1, err := h.sessionDir()
		require.NoError(t, err)

		cleanup2, err := h.sessionDir()
		require.NoError(t, err)

		cleanup2()
		cleanup1()
	})
}

// TestSessionDirCleanupRunsResourceCleanupWhenOutputSet asserts
// that the cleanup returned by [handler.sessionDir] still drains
// registered K8s resource cleanups when --output is set. Prior to
// the fix, the flag-set branch returned a no-op closure and
// silently leaked ServiceAccounts on shutdown.
func TestSessionDirCleanupRunsResourceCleanupWhenOutputSet(t *testing.T) {
	t.Parallel()

	h := &handler{outputPath: filepath.Join(t.TempDir(), "out"), pid: 1234, envLookup: constLookup("")}

	var called bool

	h.registerCleanup(func(_ context.Context) { called = true })

	cleanup, err := h.sessionDir()
	require.NoError(t, err)

	cleanup()

	require.True(t, called, "resource cleanup must run when outputPath is set")
}

// TestSessionDirCleanupRunsResourceCleanupWhenOutputUnset asserts
// the directory-creating branch also drains resource cleanup. This
// pins the contract on both branches so future refactors do not
// reintroduce the no-op case.
func TestSessionDirCleanupRunsResourceCleanupWhenOutputUnset(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	// Cannot use t.Parallel with t.Setenv.
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	h := &handler{pid: 6543, envLookup: constLookup("")}

	var called bool

	h.registerCleanup(func(_ context.Context) { called = true })

	cleanup, err := h.sessionDir()
	require.NoError(t, err)

	cleanup()

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

// TestSelectShellsHostSelect pins that handler.selectCtx forwards
// the right argv to host select and parses the JSON result back
// into the MCP success text.
func TestSelectShellsHostSelect(t *testing.T) {
	t.Parallel()

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
