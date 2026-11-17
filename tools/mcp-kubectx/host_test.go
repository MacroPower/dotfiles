package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/execplugin"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/identity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubeconfig"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubetest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/serviceaccount"
)

func TestHostList(t *testing.T) { //nolint:paralleltest // uses withHostStdout, package-level state
	tests := map[string]struct {
		cfg  kubeconfig.Config
		want string
	}{
		"multiple contexts": {
			cfg: testKubeconfig(),
			want: "Available contexts:\n" +
				"- prod (current)\n" +
				"- staging\n" +
				"- dev\n",
		},
		"empty contexts": {
			cfg:  kubeconfig.Config{},
			want: "No contexts found.",
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests share withHostStdout
		t.Run(name, func(t *testing.T) {
			buf := withHostStdout(t)

			path := writeTestKubeconfig(t, tc.cfg)

			err := runHostList(path, nil)
			require.NoError(t, err)

			assert.Equal(t, tc.want, buf.String())
		})
	}
}

func TestHostListMissingFile(t *testing.T) {
	t.Parallel()

	err := runHostList("/no/such/kubeconfig", nil)
	require.ErrorIs(t, err, kubeconfig.ErrLoad)
}

// TestHostSubcommandsRejectHostSideFlags pins that no `host`
// subcommand takes a host-side input from its argv. A Lima guest
// runs these subcommands through `workmux host-exec` with an argv of
// its own choice. A flag that picks the kubeconfig runs an exec
// plugin of the guest on the host. A flag that picks the role or the
// allowlist mints a ServiceAccount beyond the host config. A flag
// that picks an output path writes a file where the guest chose.
func TestHostSubcommandsRejectHostSideFlags(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		run  func(t *testing.T, path string, args []string) error
		err  error
		args []string
	}{
		"list kubeconfig": {
			run:  func(_ *testing.T, path string, args []string) error { return runHostList(path, args) },
			args: []string{"--kubeconfig", "/guest/kubeconfig"},
			err:  ErrParseHostListFlags,
		},
		"select role": {
			run:  runSelectForTest,
			args: []string{"--sa-role-name", "cluster-admin"},
			err:  ErrParseHostSelectFlags,
		},
		"select cluster scope": {
			run:  runSelectForTest,
			args: []string{"--sa-cluster-scoped=true"},
			err:  ErrParseHostSelectFlags,
		},
		"select allowlist": {
			run:  runSelectForTest,
			args: []string{"--allow-apiserver-host", "evil.example.com"},
			err:  ErrParseHostSelectFlags,
		},
		"select kubeconfig": {
			run:  runSelectForTest,
			args: []string{"--kubeconfig", "/guest/kubeconfig"},
			err:  ErrParseHostSelectFlags,
		},
		"select out path": {
			run:  runSelectForTest,
			args: []string{"--out-path", "/home/user/.local/state/mcp-kubectx-run/host.key"},
			err:  ErrParseHostSelectFlags,
		},
		"select env": {
			run:  runSelectForTest,
			args: []string{"--for-guest=false"},
			err:  ErrParseHostSelectFlags,
		},
		"token kubeconfig": {
			run: func(t *testing.T, path string, args []string) error {
				t.Helper()

				return runHostToken(t.Context(), path, testHostConfig(),
					append([]string{"--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns"}, args...))
			},
			args: []string{"--kubeconfig", "/guest/kubeconfig"},
			err:  ErrParseHostTokenFlags,
		},
		"token expiration": {
			run: func(t *testing.T, path string, args []string) error {
				t.Helper()

				return runHostToken(t.Context(), path, testHostConfig(),
					append([]string{"--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns"}, args...))
			},
			args: []string{"--sa-expiration", "86400"},
			err:  ErrParseHostTokenFlags,
		},
		"sweep kubeconfig": {
			run: func(t *testing.T, path string, args []string) error {
				t.Helper()

				return runHostSweep(t.Context(), path, append([]string{"--host-id", testHostID}, args...))
			},
			args: []string{"--kubeconfig", "/guest/kubeconfig"},
			err:  ErrParseHostSweepFlags,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
			withHostKubeClient(t, mock)
			withHostLedger(t)
			withHostStdout(t)

			path := writeTestKubeconfig(t, testKubeconfig())

			err := tc.run(t, path, tc.args)
			require.ErrorIs(t, err, tc.err)

			assert.Empty(t, mock.CreatedSAs, "a rejected flag must not create an SA")
			assert.Empty(t, mock.TokenRequests, "a rejected flag must not mint a token")
		})
	}
}

// runSelectForTest runs `host select prod` with the owner of the
// tests, testHostConfig, and the extra args.
func runSelectForTest(t *testing.T, path string, args []string) error {
	t.Helper()

	return runHostSelect(t.Context(), path, testHostConfig(), append([]string{
		"prod",
		"--sa-instance-id", testInstanceID,
		"--sa-host-id", testHostID,
	}, args...))
}

// TestResolveHostKubeconfigPathSkipsScopedKubeconfig pins the
// fallthrough that keeps `list` working when a user relies on the
// default ~/.kube/config. The launcher wrapper then rewrites
// $KUBECONFIG to the scoped output under $CLAUDE_KUBECTX_DIR and
// sets no $KUBECONFIG_HOST. So the resolution skips that scoped
// path, which does not exist until the first select.
func TestResolveHostKubeconfigPathSkipsScopedKubeconfig(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	scopedDir := t.TempDir()
	scoped := filepath.Join(scopedDir, "kubeconfig")

	t.Setenv("KUBECONFIG_HOST", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", scopedDir)
	t.Setenv("KUBECONFIG", scoped)

	got, err := resolveHostKubeconfigPath()
	require.NoError(t, err)
	assert.NotEqual(t, scoped, got, "scoped kubeconfig must not be treated as a host source")
	assert.True(t, strings.HasSuffix(got, filepath.Join(".kube", "config")))
}

// TestResolveHostKubeconfigPathHonorsRealKubeconfig pins the
// out-of-wrapper case: a $KUBECONFIG that does not sit inside
// $CLAUDE_KUBECTX_DIR is a real source, and the resolution returns
// it as-is.
func TestResolveHostKubeconfigPathHonorsRealKubeconfig(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	realPath := filepath.Join(t.TempDir(), "config")

	t.Setenv("KUBECONFIG_HOST", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "/run/claude-kubectx.1")
	t.Setenv("KUBECONFIG", realPath)

	got, err := resolveHostKubeconfigPath()
	require.NoError(t, err)
	assert.Equal(t, realPath, got)
}

// TestResolveHostKubeconfigPathAbsolute pins that the path is
// absolute, so that `host select`, `host token`, and a later `host
// sweep` name the same file in each provision record, whatever
// their working directories.
func TestResolveHostKubeconfigPathAbsolute(t *testing.T) { //nolint:paralleltest // uses t.Setenv, t.Chdir
	dir := t.TempDir()
	t.Chdir(dir)

	t.Setenv("KUBECONFIG_HOST", "config")
	t.Setenv("KUBECONFIG", "")

	got, err := resolveHostKubeconfigPath()
	require.NoError(t, err)

	want, err := filepath.Abs("config")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestResolveHostKubeconfigPathNeedsHome pins that the default
// ~/.kube/config never becomes a path relative to the working
// directory. `workmux host-exec` runs each `host` subcommand in a
// worktree that the guest can write, so a relative kubeconfig there
// would run an exec plugin of the guest on the host.
func TestResolveHostKubeconfigPathNeedsHome(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	tests := map[string]struct {
		home string
	}{
		"unset home":    {home: ""},
		"relative home": {home: "home"},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests use t.Setenv
		t.Run(name, func(t *testing.T) {
			t.Setenv("KUBECONFIG_HOST", "")
			t.Setenv("KUBECONFIG", "")
			t.Setenv("HOME", tc.home)

			_, err := resolveHostKubeconfigPath()
			require.ErrorIs(t, err, ErrNoHome)
		})
	}
}

// TestHostSelectRequiresOwner pins that `host select` needs both ids
// of the `serve` that owns the SA. `host token` and `host release`
// act only on an SA whose provision record names that owner, so an
// SA without a record serves no caller. A malformed id fails before
// any cluster-side mutation and before the record write.
func TestHostSelectRequiresOwner(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		err  error
		args []string
	}{
		"no ids": {
			err: ErrInvalidHostID,
		},
		"host id only": {
			args: []string{"--sa-host-id", testHostID},
			err:  ErrInvalidInstanceID,
		},
		"instance id only": {
			args: []string{"--sa-instance-id", testInstanceID},
			err:  ErrInvalidHostID,
		},
		"malformed host id": {
			args: []string{"--sa-host-id", "../host", "--sa-instance-id", testInstanceID},
			err:  ErrInvalidHostID,
		},
		"malformed instance id": {
			args: []string{"--sa-host-id", testHostID, "--sa-instance-id", "UPPERCASE0000000"},
			err:  ErrInvalidInstanceID,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			mock := &kubetest.Fake{}
			withHostKubeClient(t, mock)

			records := withHostLedger(t)

			withHostStdout(t)

			path := writeTestKubeconfig(t, testKubeconfig())

			err := runHostSelect(t.Context(), path, testHostConfig(), append([]string{"prod"}, tc.args...))
			require.ErrorIs(t, err, tc.err)

			assert.Empty(t, mock.CreatedSAs)
			assert.Empty(t, listRecords(t, records))
		})
	}
}

func TestHostSelectMissingContext(t *testing.T) {
	t.Parallel()

	path := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSelect(t.Context(), path, testHostConfig(), []string{
		"--sa-instance-id", testInstanceID,
		"--sa-host-id", testHostID,
	})
	require.ErrorIs(t, err, ErrMissingContext)
}

func TestHostSelectContextNotFound(t *testing.T) {
	t.Parallel()

	path := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSelect(t.Context(), path, testHostConfig(), []string{
		"nonexistent",
		"--sa-instance-id", testInstanceID,
		"--sa-host-id", testHostID,
	})
	require.ErrorIs(t, err, ErrContextNotFound)
}

// TestHostSelectInvalidConfig pins that `host select` refuses a host
// config that [serviceaccount.Config.Validate] rejects, before any
// cluster-side mutation.
func TestHostSelectInvalidConfig(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{}
	withHostKubeClient(t, mock)
	withHostLedger(t)
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSelect(t.Context(), path, hostConfig{}, []string{
		"prod",
		"--sa-instance-id", testInstanceID,
		"--sa-host-id", testHostID,
	})
	require.ErrorIs(t, err, serviceaccount.ErrMissingRole)

	assert.Empty(t, mock.CreatedSAs)
}

// TestHostSelectClusterEntryMissing pins that `host select` refuses
// a context whose cluster entry is absent from the kubeconfig before
// any cluster-side mutation, even with no apiserver allowlist.
// Without the guard, the SA and binding stay behind a scoped
// kubeconfig with a dangling cluster reference.
func TestHostSelectClusterEntryMissing(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostLedger(t)
	withHostStdout(t)

	cfg := testKubeconfig()
	cfg.Contexts = append(cfg.Contexts, kubeconfig.NamedContext{
		Name:    "dangling",
		Context: kubeconfig.Context{Cluster: "no-such-cluster", User: "admin"},
	})

	path := writeTestKubeconfig(t, cfg)

	err := runHostSelect(t.Context(), path, testHostConfig(), []string{
		"dangling",
		"--sa-instance-id", testInstanceID,
		"--sa-host-id", testHostID,
	})
	require.ErrorIs(t, err, ErrClusterNotFound)

	assert.Empty(t, mock.CreatedSAs, "missing cluster entry must not create the SA")
}

// TestHostSelectResult pins the JSON result of `host select`. It
// carries the cluster entry of the context, from which `serve`
// writes its scoped kubeconfig, and the binding that the host
// config picked. `host select` writes no file outside the ledger.
func TestHostSelectResult(t *testing.T) { //nolint:paralleltest // mutates package-level state, uses t.Setenv
	mock := &kubetest.Fake{}
	withHostKubeClient(t, mock)
	withHostLedger(t)

	buf := withHostStdout(t)

	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	path := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSelect(t.Context(), path, testHostConfig(), []string{
		"prod",
		"--sa-instance-id", testInstanceID,
		"--sa-host-id", testHostID,
	}))

	var result HostSelectResult

	require.NoError(t, json.NewDecoder(strings.NewReader(buf.String())).Decode(&result))

	require.Len(t, mock.CreatedSAs, 1)
	assert.Equal(t, HostSelectResult{
		Cluster: kubeconfig.NamedCluster{
			Name:    "prod-cluster",
			Cluster: map[string]any{"server": "https://prod.example.com"},
		},
		SAName:    strings.TrimPrefix(mock.CreatedSAs[0], "default/"),
		Namespace: "default",
		Context:   "prod",
		Binding:   `ClusterRole "view" (namespaced)`,
	}, result)

	entries, err := os.ReadDir(stateHome)
	require.NoError(t, err)
	assert.Empty(t, entries, "host select must not write a file in the state home")
}

// TestHostSelectBindingNameConvention pins the contract that
// [bindingNameForSA] is the single source of truth: the binding
// created by `host select` matches `<sa>-binding`, and `host
// release` uses the same convention to delete it.
func TestHostSelectBindingNameConvention(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostLedger(t)
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())

	cfg := testHostConfig()
	cfg.Namespace = "ns"

	require.NoError(t, runHostSelect(t.Context(), path, cfg, []string{
		"prod",
		"--sa-instance-id", testInstanceID,
		"--sa-host-id", testHostID,
	}))

	require.Len(t, mock.CreatedSAs, 1)
	require.Len(t, mock.CreatedRoleBindings, 1)

	saFullName := mock.CreatedSAs[0]
	saName := saFullName[len("ns/"):]
	assert.Equal(t, "ns/"+serviceaccount.BindingName(saName), mock.CreatedRoleBindings[0])
}

// TestHostSelectAppliesInstanceAndHostLabels pins the labels of
// every created resource. The instance-id label carries the
// --sa-instance-id flag. The host-id label carries the
// [identity.Digest] of --sa-host-id and never the host id itself. A
// Lima guest with a `view` token reads the labels, and the host id
// is the proof of ownership that `host token`, `host release`, and
// `host sweep` take.
func TestHostSelectAppliesInstanceAndHostLabels(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostLedger(t)
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())

	require.NoError(t, runHostSelect(t.Context(), path, testHostConfig(), []string{
		"prod",
		"--sa-instance-id", testInstanceID,
		"--sa-host-id", testHostID,
	}))

	digest := identity.Digest(testHostID)

	require.Len(t, mock.CreatedSALabels, 1)
	assert.Equal(t, testInstanceID, mock.CreatedSALabels[0][serviceaccount.InstanceIDLabel])
	assert.Equal(t, digest, mock.CreatedSALabels[0][serviceaccount.HostIDLabel])

	require.Len(t, mock.CreatedRoleBindingLabels, 1)
	assert.Equal(t, testInstanceID, mock.CreatedRoleBindingLabels[0][serviceaccount.InstanceIDLabel])
	assert.Equal(t, digest, mock.CreatedRoleBindingLabels[0][serviceaccount.HostIDLabel])

	for _, labels := range append(mock.CreatedSALabels, mock.CreatedRoleBindingLabels...) {
		for key, value := range labels {
			assert.NotEqual(t, testHostID, value, "label %s must not carry the host id", key)
		}
	}
}

// TestHostSelectNamespace pins where the SA lies: the namespace of
// the host config, then the namespace of the context, then
// "default".
func TestHostSelectNamespace(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		context   string
		namespace string
		want      string
	}{
		"context without namespace": {
			context: "prod",
			want:    "default/claude-sa-",
		},
		"context namespace": {
			context: "staging",
			want:    "default/claude-sa-",
		},
		"config namespace": {
			context:   "staging",
			namespace: "kube-system",
			want:      "kube-system/claude-sa-",
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
			withHostKubeClient(t, mock)
			withHostLedger(t)
			withHostStdout(t)

			path := writeTestKubeconfig(t, testKubeconfig())

			cfg := testHostConfig()
			cfg.Namespace = tc.namespace

			require.NoError(t, runHostSelect(t.Context(), path, cfg, []string{
				tc.context,
				"--sa-instance-id", testInstanceID,
				"--sa-host-id", testHostID,
			}))

			require.Len(t, mock.CreatedSAs, 1)
			assert.True(t, strings.HasPrefix(mock.CreatedSAs[0], tc.want), "created %s", mock.CreatedSAs[0])
		})
	}
}

// TestHostSelectAPIServerAllowlist pins the apiserver allowlist of
// the host config. An empty list allows every apiserver. A
// non-empty list allows only the hosts that it names, whatever
// their case, and refuses every other host before any cluster-side
// mutation.
func TestHostSelectAPIServerAllowlist(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		err     error
		server  string
		context string
		allowed []string
	}{
		"allowed": {
			context: "prod",
			allowed: []string{"prod.example.com", "staging.example.com"},
		},
		"allowed whatever the case": {
			context: "prod",
			server:  "https://PROD.Example.COM:6443",
			allowed: []string{"prod.example.com"},
		},
		"empty allowlist allows any": {
			context: "dev",
		},
		"denied": {
			context: "prod",
			allowed: []string{"staging.example.com"},
			err:     ErrAPIServerNotAllowed,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
			withHostKubeClient(t, mock)

			records := withHostLedger(t)

			withHostStdout(t)

			kc := testKubeconfig()
			if tc.server != "" {
				kc.Clusters[0].Cluster = map[string]any{"server": tc.server}
			}

			path := writeTestKubeconfig(t, kc)

			cfg := testHostConfig()
			cfg.AllowedAPIServerHosts = tc.allowed

			err := runHostSelect(t.Context(), path, cfg, []string{
				tc.context,
				"--sa-instance-id", testInstanceID,
				"--sa-host-id", testHostID,
			})
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.Contains(t, err.Error(), "prod.example.com")
				assert.Empty(t, mock.CreatedSAs, "denied select must not create the SA")
				assert.Empty(t, listRecords(t, records), "denied select must not write a record")

				return
			}

			require.NoError(t, err)
			assert.Len(t, mock.CreatedSAs, 1, "allowed select must create the SA")
		})
	}
}

// TestHostSelectBindingFromConfig pins that the host config picks the
// role and the binding kind of each SA. The argv of `host select`
// has no flag for either.
func TestHostSelectBindingFromConfig(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		cfg                 func(cfg *hostConfig)
		wantRoleBindings    int
		wantClusterBindings int
	}{
		"role binding": {
			cfg:              func(*hostConfig) {},
			wantRoleBindings: 1,
		},
		"cluster role binding": {
			cfg:                 func(cfg *hostConfig) { cfg.ClusterScoped = true },
			wantClusterBindings: 1,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
			withHostKubeClient(t, mock)
			withHostLedger(t)
			withHostStdout(t)

			path := writeTestKubeconfig(t, testKubeconfig())

			cfg := testHostConfig()
			tc.cfg(&cfg)

			require.NoError(t, runHostSelect(t.Context(), path, cfg, []string{
				"prod",
				"--sa-instance-id", testInstanceID,
				"--sa-host-id", testHostID,
			}))

			assert.Len(t, mock.CreatedRoleBindings, tc.wantRoleBindings)
			assert.Len(t, mock.CreatedClusterRoleBindings, tc.wantClusterBindings)
		})
	}
}

func TestHostToken(t *testing.T) { //nolint:paralleltest // mutates package-level state
	expiry := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC)
	mock := &kubetest.Fake{Token: "tok-xyz", TokenExpiry: expiry}
	withHostKubeClient(t, mock)

	records := withHostLedger(t)
	buf := withHostStdout(t)

	addOwnedRecord(t, records, "ns", "claude-sa-1", false)

	cfg := testHostConfig()
	cfg.Expiration = 600

	require.NoError(t, runHostToken(t.Context(), testOwnedKubeconfig, cfg, append([]string{
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	}, ownerFlags()...)))

	var cred execplugin.Credential

	require.NoError(t, json.NewDecoder(strings.NewReader(buf.String())).Decode(&cred))

	assert.Equal(t, execplugin.APIVersion, cred.APIVersion)
	assert.Equal(t, "ExecCredential", cred.Kind)
	assert.Equal(t, "tok-xyz", cred.Status.Token)
	assert.Equal(t, expiry.Format(time.RFC3339), cred.Status.ExpirationTimestamp)

	assert.Equal(t, []string{"ns/claude-sa-1"}, mock.TokenRequests)
	assert.Equal(t, []time.Duration{10 * time.Minute}, mock.TokenExpirations,
		"the lifetime of the token comes from the host config")
}

// TestHostTokenRequiresRecord pins that `host token` mints only for
// an SA whose provision record names the caller as its owner. The
// host kubeconfig can mint a token for any SA of a cluster, such as
// an SA with cluster-admin in kube-system. A Lima guest that runs
// `host token` through `workmux host-exec` must not reach such an
// SA, nor the SA of another `serve`.
func TestHostTokenRequiresRecord(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		args []string
	}{
		"no record": {
			args: append([]string{"--context", "prod", "--sa", "admin", "--namespace", "kube-system"}, ownerFlags()...),
		},
		"record of another host id": {
			args: []string{
				"--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns",
				"--host-id", "fedcba9876543210", "--instance-id", testInstanceID,
			},
		},
		"host id digest in place of the host id": {
			args: []string{
				"--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns",
				"--host-id", identity.Digest(testHostID), "--instance-id", testInstanceID,
			},
		},
		"record of another instance": {
			args: []string{
				"--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns",
				"--host-id", testHostID, "--instance-id", "2222222222222222",
			},
		},
		"record of another context": {
			args: append([]string{"--context", "staging", "--sa", "claude-sa-1", "--namespace", "ns"}, ownerFlags()...),
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
			withHostKubeClient(t, mock)

			records := withHostLedger(t)
			buf := withHostStdout(t)

			addOwnedRecord(t, records, "ns", "claude-sa-1", false)

			err := runHostToken(t.Context(), testOwnedKubeconfig, testHostConfig(), tc.args)
			require.ErrorIs(t, err, ErrNotProvisioned)

			assert.Empty(t, mock.TokenRequests, "a token without a record must not reach the apiserver")
			assert.Empty(t, buf.String())
		})
	}
}

func TestHostTokenMissingFlags(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		err  error
		args []string
	}{
		"no service account": {
			args: append([]string{"--context", "prod", "--namespace", "ns"}, ownerFlags()...),
			err:  ErrTokenMissingSA,
		},
		"no namespace": {
			args: append([]string{"--context", "prod", "--sa", "claude-sa-1"}, ownerFlags()...),
			err:  ErrTokenMissingNS,
		},
		"no host id": {
			args: []string{
				"--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns",
				"--instance-id", testInstanceID,
			},
			err: ErrInvalidHostID,
		},
		"no instance id": {
			args: []string{
				"--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns",
				"--host-id", testHostID,
			},
			err: ErrInvalidInstanceID,
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
			withHostKubeClient(t, mock)

			records := withHostLedger(t)
			withHostStdout(t)

			addOwnedRecord(t, records, "ns", "claude-sa-1", false)

			err := runHostToken(t.Context(), testOwnedKubeconfig, testHostConfig(), tc.args)
			require.ErrorIs(t, err, tc.err)

			assert.Empty(t, mock.TokenRequests)
		})
	}
}

// TestHostTokenRejectsExcessiveExpiration pins that `host token`
// enforces the 86400-second cap on the lifetime from the host
// config. An unbounded value would both violate the documented cap
// and overflow time.Duration for very large inputs.
func TestHostTokenRejectsExcessiveExpiration(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	records := withHostLedger(t)
	withHostStdout(t)

	addOwnedRecord(t, records, "ns", "claude-sa-1", false)

	cfg := testHostConfig()
	cfg.Expiration = 100000

	err := runHostToken(t.Context(), testOwnedKubeconfig, cfg, append([]string{
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	}, ownerFlags()...))
	require.ErrorIs(t, err, serviceaccount.ErrExpirationTooLong)

	assert.Empty(t, mock.TokenRequests, "an out-of-cap expiration must not reach the apiserver")
}

func TestHostTokenAPIError(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{TokenRequestErr: errors.New("unauthorized")}
	withHostKubeClient(t, mock)

	records := withHostLedger(t)
	withHostStdout(t)

	addOwnedRecord(t, records, "ns", "claude-sa-1", false)

	err := runHostToken(t.Context(), testOwnedKubeconfig, testHostConfig(), append([]string{
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	}, ownerFlags()...))
	require.ErrorIs(t, err, ErrTokenRequest)
}

// TestHostReleaseBindingKind pins that `host release` deletes the
// SA and the binding of the kind that the provision record names.
func TestHostReleaseBindingKind(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		wantRoleBindings    []string
		wantClusterBindings []string
		clusterScoped       bool
	}{
		"role binding": {
			wantRoleBindings: []string{"ns/claude-sa-1-binding"},
		},
		"cluster role binding": {
			clusterScoped:       true,
			wantClusterBindings: []string{"claude-sa-1-binding"},
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			mock := &kubetest.Fake{}
			withHostKubeClient(t, mock)

			records := withHostLedger(t)

			addOwnedRecord(t, records, "ns", "claude-sa-1", tc.clusterScoped)

			require.NoError(t, runHostRelease(t.Context(), testOwnedKubeconfig, append([]string{
				"--context", "prod",
				"--sa", "claude-sa-1",
				"--namespace", "ns",
			}, ownerFlags()...)))

			assert.Equal(t, []string{"ns/claude-sa-1"}, mock.DeletedSAs)
			assert.Equal(t, tc.wantRoleBindings, mock.DeletedRoleBindings)
			assert.Equal(t, tc.wantClusterBindings, mock.DeletedClusterRoleBindings)
			assert.Empty(t, listRecords(t, records), "a release removes the record")
		})
	}
}

// TestHostReleaseRequiresRecord pins that `host release` deletes
// only an SA whose provision record names the caller as its owner.
// A Lima guest that runs `host release` through `workmux host-exec`
// must not delete an SA of the cluster through the host kubeconfig,
// nor the SA of another `serve`.
func TestHostReleaseRequiresRecord(t *testing.T) { //nolint:paralleltest // mutates package-level state
	tests := map[string]struct {
		args []string
	}{
		"no record": {
			args: append(
				[]string{"--context", "prod", "--sa", "coredns", "--namespace", "kube-system"},
				ownerFlags()...,
			),
		},
		"record of another host id": {
			args: []string{
				"--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns",
				"--host-id", "fedcba9876543210", "--instance-id", testInstanceID,
			},
		},
		"malformed host id": {
			args: []string{
				"--context", "prod", "--sa", "claude-sa-1", "--namespace", "ns",
				"--host-id", "x", "--instance-id", testInstanceID,
			},
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests mutate package-level state
		t.Run(name, func(t *testing.T) {
			mock := &kubetest.Fake{}
			withHostKubeClient(t, mock)

			records := withHostLedger(t)

			kept := addOwnedRecord(t, records, "ns", "claude-sa-1", false)

			require.NoError(t, runHostRelease(t.Context(), testOwnedKubeconfig, tc.args), "host release always exits 0")

			assert.Empty(t, mock.DeletedSAs)
			assert.Empty(t, mock.DeletedRoleBindings)
			assert.Empty(t, mock.DeletedClusterRoleBindings)
			assert.Len(t, listRecords(t, records), 1)
			assert.Equal(t, kept.ServiceAccount, listRecords(t, records)[0].ServiceAccount)
		})
	}
}

// TestHostReleaseAlwaysSucceeds asserts that release exits cleanly
// even when both Delete* calls fail. Returning non-zero would force
// serve to retry the release for the entire process lifetime over
// a single transient error.
func TestHostReleaseAlwaysSucceeds(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{
		DeleteSAErr:          errors.New("not found"),
		DeleteRoleBindingErr: errors.New("api hiccup"),
	}
	withHostKubeClient(t, mock)

	records := withHostLedger(t)

	addOwnedRecord(t, records, "ns", "claude-sa-1", false)

	require.NoError(t, runHostRelease(t.Context(), testOwnedKubeconfig, append([]string{
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	}, ownerFlags()...)))
}

func TestHostReleaseMissingFlags(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{}
	withHostKubeClient(t, mock)
	withHostLedger(t)

	require.NoError(t, runHostRelease(t.Context(), testOwnedKubeconfig, nil))

	assert.Empty(t, mock.DeletedSAs, "missing flags must skip the cluster call entirely")
}

// readKubeconfigExec reads the kubeconfig at path and returns its
// users[0].user.exec block decoded into [execplugin.Plugin]. Helper for
// asserting that serve wrote the right exec-plugin block.
func readKubeconfigExec(t *testing.T, path string) execplugin.Plugin {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var raw struct {
		Users []struct {
			User struct {
				Exec execplugin.Plugin `yaml:"exec"`
			} `yaml:"user"`
		} `yaml:"users"`
	}

	require.NoError(t, yaml.Unmarshal(data, &raw))
	require.Len(t, raw.Users, 1)

	return raw.Users[0].User.Exec
}

// Compile-time guard: hostStdout must implement io.Writer.
var _ io.Writer = hostStdout
