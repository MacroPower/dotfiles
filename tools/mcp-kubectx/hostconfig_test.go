package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
)

// TestLoadHostConfig pins the format of the host config file. The
// file holds one JSON object with the keys of
// [serviceaccount.Config] and allowedAPIServerHosts. loadHostConfig
// applies the defaults of [serviceaccount.Config.Validate]. It
// refuses an unknown key, so that a misspelled allowlist never
// becomes an empty one that accepts every apiserver.
func TestLoadHostConfig(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		err  error
		data string
		want hostConfig
	}{
		"every key": {
			data: `{"role":"edit","roleKind":"Role","namespace":"claude","expiration":600,` +
				`"clusterScoped":false,"allowedAPIServerHosts":["a.example.com","b.example.com"]}`,
			want: hostConfig{
				AllowedAPIServerHosts: []string{"a.example.com", "b.example.com"},
				Config: serviceaccount.Config{
					Role:       "edit",
					RoleKind:   "Role",
					Namespace:  "claude",
					Expiration: 600,
				},
			},
		},
		"defaults": {
			data: `{"role":"view","clusterScoped":true}`,
			want: hostConfig{Config: serviceaccount.Config{
				Role:          "view",
				RoleKind:      serviceaccount.RoleKindClusterRole,
				Expiration:    serviceaccount.DefaultExpiration,
				ClusterScoped: true,
			}},
		},
		"misspelled key": {
			data: `{"role":"view","allowedAPIServers":["a.example.com"]}`,
			err:  ErrLoadHostConfig,
		},
		"data after the object": {
			data: `{"role":"view"} {"role":"cluster-admin"}`,
			err:  ErrLoadHostConfig,
		},
		"not JSON": {
			data: `role: view`,
			err:  ErrLoadHostConfig,
		},
		"no role": {
			data: `{}`,
			err:  serviceaccount.ErrMissingRole,
		},
		"cluster scope without a ClusterRole": {
			data: `{"role":"edit","roleKind":"Role","clusterScoped":true}`,
			err:  serviceaccount.ErrClusterScopedRole,
		},
		"expiration past the cap": {
			data: `{"role":"view","expiration":86401}`,
			err:  serviceaccount.ErrExpirationTooLong,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			withHostConfigFile(t, tc.data)

			got, err := loadHostConfig()
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				require.ErrorIs(t, err, ErrLoadHostConfig)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestLoadHostConfigMissingFile pins that a missing host config
// fails the load. A `host select` without the file then creates no
// ServiceAccount, rather than one with a role from somewhere else.
func TestLoadHostConfigMissingFile(t *testing.T) { //nolint:paralleltest // mutates package-level state
	path := withHostConfigFile(t, "")
	require.NoError(t, os.Remove(path))

	_, err := loadHostConfig()
	require.ErrorIs(t, err, ErrLoadHostConfig)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestDefaultHostConfigPath pins where the host config lies. An
// absolute $XDG_CONFIG_HOME holds it, and otherwise ~/.config does.
// The path never resolves in the working directory, because
// `workmux host-exec` runs each `host` subcommand in a worktree that
// the guest can write.
func TestDefaultHostConfigPath(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	home := t.TempDir()
	config := t.TempDir()

	tests := map[string]struct {
		err       error
		home      string
		configDir string
		want      string
	}{
		"XDG_CONFIG_HOME": {
			home:      home,
			configDir: config,
			want:      filepath.Join(config, "mcp-kubectx", "config.json"),
		},
		"home": {
			home: home,
			want: filepath.Join(home, ".config", "mcp-kubectx", "config.json"),
		},
		"relative XDG_CONFIG_HOME": {
			home:      home,
			configDir: "config",
			want:      filepath.Join(home, ".config", "mcp-kubectx", "config.json"),
		},
		"no home": {
			err: ErrNoHome,
		},
		"relative home": {
			home: "home",
			err:  ErrNoHome,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests use t.Setenv
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", tc.home)
			t.Setenv("XDG_CONFIG_HOME", tc.configDir)

			got, err := defaultHostConfigPath()
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestDispatchHostResolvesHostSide pins the inputs that dispatchHost
// resolves before it runs a `host` subcommand. `host select` and
// `host token` need the host config, so they fail without it.
// `host list` reads only the host kubeconfig. A ledger directory
// that is not absolute fails every subcommand, because it resolves
// in the working directory.
//
//nolint:paralleltest // mutates package-level state, uses t.Setenv
func TestDispatchHostResolvesHostSide(t *testing.T) {
	tests := map[string]struct {
		err         error
		ledgerDir   string
		args        []string
		writeConfig bool
	}{
		"select without host config": {
			args: []string{subSelect, "prod", "--sa-host-id", testHostID, "--sa-instance-id", testInstanceID},
			err:  ErrLoadHostConfig,
		},
		"token without host config": {
			args: append(
				[]string{subToken, "--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns"},
				ownerFlags()...,
			),
			err: ErrLoadHostConfig,
		},
		"list without host config": {
			args: []string{subList},
		},
		"select with a host config reads the ledger and the kubeconfig": {
			args:        []string{subSelect, "missing", "--sa-host-id", testHostID, "--sa-instance-id", testInstanceID},
			writeConfig: true,
			err:         ErrContextNotFound,
		},
		"relative ledger directory": {
			args:      []string{subList},
			ledgerDir: "ledger",
			err:       ErrRelativeLedgerDir,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			withHostStdout(t)
			withHostLedger(t)

			if tc.ledgerDir != "" {
				hostLedgerDir = func() string { return tc.ledgerDir }
			}

			path := withHostConfigFile(t, `{"role":"view"}`)
			if !tc.writeConfig {
				require.NoError(t, os.Remove(path))
			}

			t.Setenv("KUBECONFIG_HOST", writeTestKubeconfig(t, testKubeconfig()))

			err := dispatchHost(t.Context(), tc.args)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
		})
	}
}

// TestDispatchHostReleaseSetupFailure pins that `host release` exits
// 0 when dispatchHost cannot resolve the ledger directory or the
// host kubeconfig, as [runHostRelease] does on every failure. It
// logs the error as a warning on stderr instead. Every other
// subcommand fails with the error.
//
//nolint:paralleltest // mutates package-level state and os.Stderr, uses t.Setenv
func TestDispatchHostReleaseSetupFailure(t *testing.T) {
	release := append(
		[]string{subRelease, "--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns"},
		ownerFlags()...,
	)

	tests := map[string]struct {
		err       error
		logged    error
		ledgerDir string
		args      []string
		noHome    bool
	}{
		"release with a relative ledger directory": {
			args:      release,
			ledgerDir: "ledger",
			logged:    ErrRelativeLedgerDir,
		},
		"release without a home directory": {
			args:   release,
			noHome: true,
			logged: ErrNoHome,
		},
		"sweep with a relative ledger directory": {
			args:      []string{subSweep, "--host-id", testHostID},
			ledgerDir: "ledger",
			err:       ErrRelativeLedgerDir,
		},
		"sweep without a home directory": {
			args:   []string{subSweep, "--host-id", testHostID},
			noHome: true,
			err:    ErrNoHome,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			stdout := withHostStdout(t)
			withHostLedger(t)

			if tc.ledgerDir != "" {
				hostLedgerDir = func() string { return tc.ledgerDir }
			}

			if tc.noHome {
				t.Setenv("HOME", "")
				t.Setenv("KUBECONFIG", "")
				t.Setenv("KUBECONFIG_HOST", "")
			} else {
				t.Setenv("KUBECONFIG_HOST", writeTestKubeconfig(t, testKubeconfig()))
			}

			stderr, err := os.CreateTemp(t.TempDir(), "stderr")
			require.NoError(t, err)

			prevStderr, prevLogger := os.Stderr, slog.Default()
			os.Stderr = stderr

			t.Cleanup(func() {
				os.Stderr = prevStderr

				slog.SetDefault(prevLogger)
				require.NoError(t, stderr.Close())
			})

			err = dispatchHost(t.Context(), tc.args)

			logged, readErr := os.ReadFile(stderr.Name())
			require.NoError(t, readErr)
			assert.Empty(t, stdout.String())

			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Contains(t, string(logged), "host release skipped")
			assert.Contains(t, string(logged), tc.logged.Error())
		})
	}
}
