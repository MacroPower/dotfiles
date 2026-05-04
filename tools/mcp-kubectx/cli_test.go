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

// TestServeSocketSlotsRejectsNonPositive pins runServe's post-parse
// validation: --socket-slots <= 0 surfaces [ErrInvalidSocketSlots]
// before any cluster or socket work happens. flag.Int accepts the
// raw integer; the explicit check is what enforces the contract
// matched by the Nix bundle's `types.ints.positive`.
func TestServeSocketSlotsRejectsNonPositive(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		val string
	}{
		"zero":     {val: "0"},
		"negative": {val: "-1"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := runServe(t.Context(), []string{"--socket-slots", tc.val})
			require.ErrorIs(t, err, ErrInvalidSocketSlots)
		})
	}
}
