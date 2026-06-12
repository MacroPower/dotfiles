package main

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/execplugin"
)

// TestRunExecPluginClientCopiesBytes pins the happy path: the
// shim relays whatever credential bytes [execplugin.Fetch] returns
// to hostStdout. Read-path behaviors (deadlines, truncation, empty
// responses) are pinned in the execplugin package tests.
func TestRunExecPluginClientCopiesBytes(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
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

	buf := withHostStdout(t)

	require.NoError(t, runExecPluginClient(t.Context(), []string{"--socket", path}))
	assert.Equal(t, string(want), buf.String())
}

// TestRunExecPluginClientConnectFailure pins that connect-time
// errors surface through the execplugin package sentinel so
// kubectl-visible failure messages stay clean.
func TestRunExecPluginClientConnectFailure(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
	withHostStdout(t)

	bogus := filepath.Join(t.TempDir(), "no-such.sock")

	err := runExecPluginClient(t.Context(), []string{"--socket", bogus})
	require.Error(t, err)
	assert.ErrorIs(t, err, execplugin.ErrConnect)
}

// TestRunExecPluginClientEmptyCredential pins that a server that
// closes without writing surfaces [execplugin.ErrEmptyCredential]
// and writes nothing to stdout.
func TestRunExecPluginClientEmptyCredential(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
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

	buf := withHostStdout(t)

	err = runExecPluginClient(t.Context(), []string{"--socket", path})
	require.ErrorIs(t, err, execplugin.ErrEmptyCredential)
	assert.Empty(t, buf.String())
}

// TestRunExecPluginClientMissingSocketFlag pins that omitting
// --socket fails fast with the public sentinel.
func TestRunExecPluginClientMissingSocketFlag(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
	withHostStdout(t)

	err := runExecPluginClient(t.Context(), nil)
	require.ErrorIs(t, err, ErrExecPluginMissingSocket)
}
