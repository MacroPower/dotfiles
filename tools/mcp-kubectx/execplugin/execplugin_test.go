package execplugin_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/execplugin"
)

// shortTempDir returns a temp directory under [os.TempDir] whose
// path is short enough to keep socket paths under macOS's 104-byte
// sun_path limit. [testing.T.TempDir] embeds the test name in the
// path, which combined with the socket filename overflows the limit
// on the Nix builder where TMPDIR is already deeply nested.
func shortTempDir(t *testing.T) string {
	t.Helper()

	// usetesting wants t.TempDir() here, but that is exactly the
	// failure mode this helper exists to avoid: t.TempDir embeds the
	// test name in the path, producing paths that exceed the
	// 104-byte sun_path limit on the Nix builder.
	dir, err := os.MkdirTemp(os.TempDir(), "k") //nolint:usetesting // see comment above
	require.NoError(t, err)

	t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // best-effort test cleanup

	return dir
}

// TestNewUniform pins the shape of the kubectl exec plugin block.
// The shape is a function only of the socket path; every other input
// that the previous two-variant design carried (kubeconfig path,
// context, SA name, namespace, expiration, for-guest) is
// deliberately gone.
func TestNewUniform(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		socket string
	}{
		"host-style path": {
			socket: "/Users/me/.local/state/mcp-kubectx-run/serve.0.host.sock",
		},
		"guest-style path": {
			socket: "/home/dev/.local/state/mcp-kubectx-run/serve.1.guest.sock",
		},
		"trivially short": {
			socket: "/tmp/x.sock",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			plugin := execplugin.New(tc.socket)

			assert.Equal(t, execplugin.APIVersion, plugin.APIVersion)
			assert.Equal(t, "Never", plugin.InteractiveMode)
			assert.Equal(t, "mcp-kubectx", plugin.Command,
				"command must be the bare program name (PATH lookup), not an absolute store path")
			assert.Equal(t, []string{"exec-plugin", "--socket", tc.socket}, plugin.Args)
		})
	}
}

// TestFetchReturnsBytes pins the happy path: Fetch returns whatever
// valid JSON the listener writes, verbatim.
func TestFetchReturnsBytes(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "ok.sock")

	want := []byte(
		`{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{"token":"tok"}}` + "\n",
	)

	listener, err := net.Listen("unix", path) //nolint:noctx // synchronous test fixture
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() }) //nolint:errcheck // best-effort test cleanup

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}

		defer conn.Close() //nolint:errcheck // best-effort test cleanup

		_, _ = conn.Write(want) //nolint:errcheck // shim will read what it gets
	}()

	got, err := execplugin.Fetch(t.Context(), path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestFetchConnectFailure pins that connect-time errors are wrapped
// with the public sentinel so kubectl-visible failure messages stay
// clean and tests stay platform-agnostic.
func TestFetchConnectFailure(t *testing.T) {
	t.Parallel()

	bogus := filepath.Join(t.TempDir(), "no-such.sock")

	_, err := execplugin.Fetch(t.Context(), bogus)
	require.Error(t, err)
	assert.ErrorIs(t, err, execplugin.ErrConnect)
}

// TestFetchReadDeadline asserts that a listener that never writes
// terminates Fetch within its read deadline rather than hanging
// kubectl indefinitely.
func TestFetchReadDeadline(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "slow.sock")

	listener, err := net.Listen("unix", path) //nolint:noctx // synchronous test fixture
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() }) //nolint:errcheck // best-effort test cleanup

	// Accept and never write so the client hits its read deadline.
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}

		defer conn.Close() //nolint:errcheck // best-effort test cleanup

		<-t.Context().Done()
	}()

	done := make(chan error, 1)

	go func() {
		_, fetchErr := execplugin.Fetch(t.Context(), path)
		done <- fetchErr
	}()

	select {
	case err := <-done:
		require.Error(t, err, "deadline must surface as an error")
	case <-time.After(execplugin.ReadDeadline + 5*time.Second):
		t.Fatal("read deadline did not fire")
	}
}

// TestFetchTruncated pins that a server closing mid-document does
// not return success: partial JSON is withheld and surfaced as
// [execplugin.ErrMalformedCredential], so kubectl re-invokes cleanly
// instead of choking on torn output.
func TestFetchTruncated(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "trunc.sock")

	listener, err := net.Listen("unix", path) //nolint:noctx // synchronous test fixture
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() }) //nolint:errcheck // best-effort test cleanup

	// Write a JSON prefix and close, simulating a server-side
	// deadline expiring mid-write.
	partial := []byte(`{"apiVersion":"client.authentication.k8s.io/v1","kind":"Exec`)

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}

		_, _ = conn.Write(partial) //nolint:errcheck // deliberate partial write
		_ = conn.Close()           //nolint:errcheck // simulate abrupt server close
	}()

	got, err := execplugin.Fetch(t.Context(), path)
	require.ErrorIs(t, err, execplugin.ErrMalformedCredential)
	assert.Nil(t, got, "truncated JSON must not be returned")
}

// TestFetchEmptyResponse dials a real socket whose server closes
// without writing. Fetch must surface ErrEmptyCredential so the
// caller sees a non-zero exit rather than misinterpreting empty
// stdout.
func TestFetchEmptyResponse(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "noop.sock")

	listener, err := net.Listen("unix", path) //nolint:noctx // synchronous test fixture
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() }) //nolint:errcheck // best-effort test cleanup

	// Accept and close: zero bytes returned.
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}

		_ = conn.Close() //nolint:errcheck // probe-only conn
	}()

	got, err := execplugin.Fetch(t.Context(), path)
	require.ErrorIs(t, err, execplugin.ErrEmptyCredential)
	assert.Nil(t, got)
}
