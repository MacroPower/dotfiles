package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func TestGenerate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		setup   func(t *testing.T) string
		err     error
		wantMsg string
	}{
		"missing file": {
			setup: func(t *testing.T) string {
				t.Helper()

				return "/nonexistent/path/config.yaml"
			},
			wantMsg: "reading config",
		},
		"invalid YAML": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				path := filepath.Join(dir, "config.yaml")
				require.NoError(t, os.WriteFile(path, []byte(":\n  :\n  - [invalid"), 0o644))

				return path
			},
			wantMsg: "parsing config",
		},
		"empty FQDN selector": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				path := filepath.Join(dir, "config.yaml")
				cfg := "egress:\n  - toFQDNs:\n      - {}\n"
				require.NoError(t, os.WriteFile(path, []byte(cfg), 0o644))

				return path
			},
			err:     sandbox.ErrFQDNSelectorEmpty,
			wantMsg: "parsing config",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := tt.setup(t)

			cfg, err := sandbox.Generate(t.Context(), path)
			require.Error(t, err)
			require.Nil(t, cfg)
			require.ErrorContains(t, err, tt.wantMsg)

			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
		})
	}
}

func TestGenerateDeterministicOutput(t *testing.T) {
	t.Parallel()

	yamlCfg := []byte("egress:\n" +
		"  - toPorts:\n" +
		"      - ports:\n" +
		"          - port: \"443\"\n" +
		"          - port: \"80\"\n" +
		"    toFQDNs:\n" +
		"      - matchName: api.example.com\n" +
		"      - matchName: cdn.example.com\n" +
		"  - toPorts:\n" +
		"      - ports:\n" +
		"          - port: \"8080\"\n" +
		"    toFQDNs:\n" +
		"      - matchPattern: \"*.example.org\"\n")

	cfg, err := sandbox.ParseConfig(yamlCfg)
	require.NoError(t, err)

	const iterations = 10

	envoyResults := make([]string, iterations)
	ipv4Results := make([]string, iterations)
	ipv6Results := make([]string, iterations)

	for i := range iterations {
		envoy, err := sandbox.GenerateEnvoyFromConfig(cfg, "", "")
		require.NoError(t, err)

		ipv4, ipv6 := sandbox.GenerateIptablesRules(cfg)
		envoyResults[i] = envoy
		ipv4Results[i] = ipv4
		ipv6Results[i] = ipv6
	}

	for i := 1; i < iterations; i++ {
		assert.Equal(t, envoyResults[0], envoyResults[i], "envoy config differs on iteration %d", i)
		assert.Equal(t, ipv4Results[0], ipv4Results[i], "ipv4 rules differ on iteration %d", i)
		assert.Equal(t, ipv6Results[0], ipv6Results[i], "ipv6 rules differ on iteration %d", i)
	}
}
