package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBuildExecPluginUniform pins the shape of the kubectl exec
// plugin block. The shape is a function only of [execPluginParams.SocketPath];
// every other input that the previous two-variant design carried
// (kubeconfig path, context, SA name, namespace, expiration,
// for-guest) is deliberately gone.
func TestBuildExecPluginUniform(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		socket string
	}{
		"host-style path": {
			socket: "/Users/me/.local/state/mcp-kubectx-run/serve.4242.host.sock",
		},
		"guest-style path": {
			socket: "/home/dev/.local/state/mcp-kubectx-run/serve.9999.guest.sock",
		},
		"trivially short": {
			socket: "/tmp/x.sock",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			plugin := buildExecPlugin(execPluginParams{SocketPath: tc.socket})

			assert.Equal(t, execAuthAPIVersion, plugin.APIVersion)
			assert.Equal(t, "Never", plugin.InteractiveMode)
			assert.Equal(t, "mcp-kubectx", plugin.Command,
				"command must be the bare program name (PATH lookup), not an absolute store path")
			assert.Equal(t, []string{"exec-plugin", "--socket", tc.socket}, plugin.Args)
		})
	}
}
