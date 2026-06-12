package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
)

// shortTempDir returns a temp directory under [os.TempDir] whose
// path is short enough to keep socket paths under macOS's 104-byte
// sun_path limit. [testing.T.TempDir] embeds the test name in the
// path, which combined with the mcp-kubectx-run subdir + slot
// filename overflows the limit on the Nix builder where TMPDIR is
// already deeply nested. Cleanup is registered with t.Cleanup.
func shortTempDir(t *testing.T) string {
	t.Helper()

	// usetesting linter wants t.TempDir() here, but that is exactly
	// the failure mode this helper exists to avoid: t.TempDir embeds
	// the test name in the path, producing paths that exceed the
	// 104-byte sun_path limit on the Nix builder.
	dir, err := os.MkdirTemp(os.TempDir(), "k") //nolint:usetesting // see comment above
	require.NoError(t, err)

	t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // best-effort test cleanup

	return dir
}

// constLookup returns an envLookup that always returns val.
// Tests use it to drive handler.isGuest without mutating process
// env (which is goroutine-unsafe and breaks t.Parallel).
func constLookup(val string) func(string) string {
	return func(string) string { return val }
}

// withHostStdout swaps the package-level hostStdout writer for the
// duration of t and returns a buffer that captures the writes.
func withHostStdout(t *testing.T) *bytes.Buffer {
	t.Helper()

	prev := hostStdout
	buf := &bytes.Buffer{}
	hostStdout = buf

	t.Cleanup(func() { hostStdout = prev })

	return buf
}

// withHostKubeClient swaps the host-side kube.Client factory so
// host * subcommands use a fake client instead of touching the
// real cluster.
func withHostKubeClient(t *testing.T, fake kube.Client) {
	t.Helper()

	prev := hostKubeClient
	hostKubeClient = func(string, string) (kube.Client, error) { return fake, nil }

	t.Cleanup(func() { hostKubeClient = prev })
}

// neutralizeWrapperEnv clears every Claude-launcher env var a
// selectCtx round-trip reads, and pins TMPDIR to a per-test dir.
// Without this, an ambient wrapper environment (a test run from
// inside a live Claude session) leaks into routing decisions via
// localView and -- worse -- publishSidecar overwrites the live
// session's real sidecar symlink.
func neutralizeWrapperEnv(t *testing.T) string {
	t.Helper()

	tmp := t.TempDir()

	t.Setenv("TMPDIR", tmp)
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")
	t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", "")

	return tmp
}
