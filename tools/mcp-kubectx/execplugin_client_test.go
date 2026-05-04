package main

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunExecPluginClientCopiesBytes pins the happy path: the
// shim copies whatever the listener writes to hostStdout.
func TestRunExecPluginClientCopiesBytes(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
	dir := t.TempDir()
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

	buf := withHostStdout(t)

	require.NoError(t, runExecPluginClient(t.Context(), []string{"--socket", path}))
	assert.Equal(t, string(want), buf.String())
}

// TestRunExecPluginClientConnectFailure pins that the shim wraps
// connect-time errors with the public sentinel so kubectl-visible
// failure messages stay clean and tests stay platform-agnostic.
func TestRunExecPluginClientConnectFailure(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
	withHostStdout(t)

	bogus := filepath.Join(t.TempDir(), "no-such.sock")

	err := runExecPluginClient(t.Context(), []string{"--socket", bogus})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrConnectExecPlugin)
}

// TestRunExecPluginClientReadDeadline asserts that a listener that
// never writes terminates the shim within its read deadline rather
// than hanging kubectl indefinitely.
func TestRunExecPluginClientReadDeadline(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
	dir := t.TempDir()
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

	withHostStdout(t)

	// Override the package read-deadline window by leaning on the
	// const indirectly: the test asserts behavior, not a specific
	// duration. Bound the test by a wall clock as a safety net.
	done := make(chan error, 1)

	go func() {
		done <- runExecPluginClient(t.Context(), []string{"--socket", path})
	}()

	select {
	case err := <-done:
		require.Error(t, err, "deadline must surface as an error")
	case <-time.After(execPluginReadDeadline + 5*time.Second):
		t.Fatal("read deadline did not fire")
	}
}

// TestRunExecPluginClientMissingSocketFlag pins that omitting
// --socket fails fast with the public sentinel.
func TestRunExecPluginClientMissingSocketFlag(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
	withHostStdout(t)

	err := runExecPluginClient(t.Context(), nil)
	require.ErrorIs(t, err, ErrExecPluginMissingSocket)
}

// TestRunExecPluginClientNoHandler dials a real socket whose
// serve has no currentSA. The handler closes with no bytes; the
// shim must surface ErrEmptyCredential so kubectl sees a non-zero
// exit rather than misinterpreting empty stdout.
func TestRunExecPluginClientNoHandler(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
	dir := t.TempDir()
	path := filepath.Join(dir, "noop.sock")

	listener, err := net.Listen("unix", path) //nolint:noctx // synchronous test fixture
	require.NoError(t, err)
	//nolint:errcheck // best-effort test cleanup
	t.Cleanup(func() { _ = listener.Close() })

	// Accept and close: zero bytes returned.
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}

		_ = conn.Close() //nolint:errcheck // probe-only conn
	}()

	buf := withHostStdout(t)

	err = runExecPluginClient(t.Context(), []string{"--socket", path})
	require.ErrorIs(t, err, ErrEmptyCredential)
	assert.Empty(t, buf.String())
}
