package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// constLookup returns an envLookup that always returns val.
// Tests use it to drive handler.isGuest without mutating process
// env (which is goroutine-unsafe and breaks t.Parallel).
func constLookup(val string) func(string) string {
	return func(string) string { return val }
}

func TestHostExecArgsHost(t *testing.T) {
	t.Parallel()

	h := &handler{envLookup: constLookup("")}

	cmd, argv, err := h.hostExecArgs("list", []string{"--kubeconfig", "/k"})
	require.NoError(t, err)

	assert.NotEqual(t, "workmux", cmd, "host variant must not invoke workmux")
	assert.Equal(t, []string{"host", "list", "--kubeconfig", "/k"}, argv)
}

func TestHostExecArgsGuest(t *testing.T) {
	t.Parallel()

	h := &handler{envLookup: constLookup("1")}

	cmd, argv, err := h.hostExecArgs("select", []string{"prod", "--out-path", "/k"})
	require.NoError(t, err)

	assert.Equal(t, "workmux", cmd)
	assert.Equal(t, []string{
		"host-exec", "mcp-kubectx",
		"host", "select", "prod", "--out-path", "/k",
	}, argv)
}

func TestIsGuest(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		val  string
		want bool
	}{
		"unset":  {val: "", want: false},
		"one":    {val: "1", want: true},
		"zero":   {val: "0", want: false},
		"truthy": {val: "true", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &handler{envLookup: constLookup(tc.val)}
			assert.Equal(t, tc.want, h.isGuest())
		})
	}
}

// fakeExecutable writes a small shell script at $TMPDIR/<name> with
// the given body and returns its absolute path. The defaultRunHost
// wrapping is tested by pointing it at this script via a custom
// hostExecArgs override on the *handler.
func fakeExecutable(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "fake-mcp-kubectx")

	err := writeFileSecure(path, []byte("#!/bin/sh\n"+body+"\n"))
	require.NoError(t, err)

	require.NoError(t, exec.CommandContext(t.Context(), "chmod", "+x", path).Run())

	return path
}

func TestDefaultRunHostNonZeroExit(t *testing.T) {
	t.Parallel()

	exePath := fakeExecutable(t, `echo "stdout-from-fake"; echo "stderr-from-fake" 1>&2; exit 9`)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	c := exec.CommandContext(ctx, exePath, "host", "list")

	stdout, err := c.Output()
	require.Error(t, err, "non-zero exit must surface as an error")

	var exitErr *exec.ExitError

	require.ErrorAs(t, err, &exitErr)

	assert.Equal(t, 9, exitErr.ExitCode())
	assert.Contains(t, string(stdout), "stdout-from-fake", "stdout captured even on non-zero exit")
	assert.Contains(t, string(exitErr.Stderr), "stderr-from-fake")
}

func TestDefaultRunHostBinaryNotFound(t *testing.T) {
	t.Parallel()

	bogus := filepath.Join(t.TempDir(), "no-such-binary")

	c := exec.CommandContext(t.Context(), bogus, "host", "list")
	err := c.Run()
	require.Error(t, err, "missing binary must surface as an error")
}

// TestHostTokenSkipsWorkmuxWhenEnvSetToGuest pins the recursion
// guard. Even with WM_SANDBOX_GUEST=1 in process env, runHostToken
// reaches k8s directly because it does not own a *handler and
// therefore has no path to runHost. The fake KubeClient records
// exactly one CreateTokenRequest call.
func TestHostTokenSkipsWorkmuxWhenEnvSetToGuest(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	t.Setenv(guestEnvVar, "1")

	mock := &mockKubeClient{
		token:       "tok-abc",
		tokenExpiry: time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC),
	}

	prevClient := hostKubeClient
	hostKubeClient = func(string, string) (KubeClient, error) { return mock, nil }
	t.Cleanup(func() { hostKubeClient = prevClient })

	var buf strings.Builder

	prevStdout := hostStdout
	hostStdout = &buf
	t.Cleanup(func() { hostStdout = prevStdout })

	err := runHostToken(t.Context(), []string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-x",
		"--namespace", "kube-system",
		"--sa-expiration", "3600",
	})
	require.NoError(t, err)

	mock.mu.Lock()
	defer mock.mu.Unlock()

	require.Len(t, mock.tokenRequests, 1, "host token must call k8s exactly once")
	assert.Equal(t, "kube-system/claude-sa-x", mock.tokenRequests[0])
	assert.Contains(t, buf.String(), `"token":"tok-abc"`)
}

// TestSelectArgsForGuestFlag pins that handler.selectCtx forwards
// --for-guest=BOOL based on envLookup, --pid is always forwarded,
// --out-path is omitted when h.outputPath is empty (host select
// then defaults the path), and --socket-path forwards the
// per-`serve` UDS path keyed off pid + env so guest serve's host
// select still resolves the path the guest fs holds.
func TestSelectArgsForGuestFlag(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	tests := map[string]struct {
		guest string
		want  string
		sock  string
	}{
		"host": {
			guest: "",
			want:  "--for-guest=false",
			sock:  filepath.Join(stateHome, "mcp-kubectx-run", "serve.4242.host.sock"),
		},
		"guest": {
			guest: "1",
			want:  "--for-guest=true",
			sock:  filepath.Join(stateHome, "mcp-kubectx-run", "serve.4242.guest.sock"),
		},
	}

	for name, tc := range tests { //nolint:paralleltest // shares t.Setenv state
		t.Run(name, func(t *testing.T) {
			h := &handler{
				kubeconfigPath: "/k",
				pid:            4242,
				sa:             saConfig{role: "view", roleKind: "ClusterRole", expiration: 3600},
				envLookup:      constLookup(tc.guest),
			}

			args := h.selectArgs("prod")

			require.Contains(t, args, tc.want)
			require.Contains(t, args, "--pid")
			require.Contains(t, args, "4242")
			require.NotContains(t, args, "--out-path",
				"--out-path must be omitted when h.outputPath is empty")

			require.Contains(t, args, "--socket-path")
			require.Contains(t, args, tc.sock,
				"--socket-path must be forwarded keyed off pid + env tag")
		})
	}
}
