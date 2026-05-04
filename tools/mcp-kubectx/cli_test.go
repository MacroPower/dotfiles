package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDispatchExecPlugin pins that `mcp-kubectx exec-plugin
// --socket <path>` reaches runExecPluginClient (asserted by the
// connect-time error sentinel), confirming the dispatcher routes
// the new subcommand correctly.
func TestDispatchExecPlugin(t *testing.T) { //nolint:paralleltest // mutates package-level hostStdout
	withHostStdout(t)

	bogus := filepath.Join(t.TempDir(), "no-such.sock")

	err := dispatch(t.Context(), []string{"mcp-kubectx", "exec-plugin", "--socket", bogus})
	require.ErrorIs(t, err, ErrConnectExecPlugin,
		"exec-plugin must route to runExecPluginClient")
}
