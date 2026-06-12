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

			err := runHostList([]string{"--kubeconfig", path})
			require.NoError(t, err)

			assert.Equal(t, tc.want, buf.String())
		})
	}
}

func TestHostListMissingFile(t *testing.T) {
	t.Parallel()

	err := runHostList([]string{"--kubeconfig", "/no/such/kubeconfig"})
	require.ErrorIs(t, err, kubeconfig.ErrLoad)
}

// TestResolveHostKubeconfigPathSkipsScopedKubeconfig pins the
// fallthrough that keeps `list` working when a user relies on the
// default ~/.kube/config: the launcher wrapper rewrites $KUBECONFIG
// to the scoped output under $CLAUDE_KUBECTX_DIR without preserving
// $KUBECONFIG_HOST, so resolution must skip that scoped path rather
// than read a kubeconfig that does not exist until the first select.
func TestResolveHostKubeconfigPathSkipsScopedKubeconfig(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	scopedDir := t.TempDir()
	scoped := filepath.Join(scopedDir, "kubeconfig")

	t.Setenv("KUBECONFIG_HOST", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", scopedDir)
	t.Setenv("KUBECONFIG", scoped)

	got := resolveHostKubeconfigPath("")
	assert.NotEqual(t, scoped, got, "scoped kubeconfig must not be treated as a host source")
	assert.True(t, strings.HasSuffix(got, filepath.Join(".kube", "config")))
}

// TestResolveHostKubeconfigPathHonorsRealKubeconfig pins the
// out-of-wrapper case: a $KUBECONFIG that does not sit inside
// $CLAUDE_KUBECTX_DIR is a real source and must be returned as-is.
func TestResolveHostKubeconfigPathHonorsRealKubeconfig(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	realPath := filepath.Join(t.TempDir(), "config")

	t.Setenv("KUBECONFIG_HOST", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "/run/claude-kubectx.1")
	t.Setenv("KUBECONFIG", realPath)

	assert.Equal(t, realPath, resolveHostKubeconfigPath(""))
}

func TestHostSelectMissingPid(t *testing.T) {
	t.Parallel()

	path := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSelect(t.Context(), []string{
		"prod",
		"--kubeconfig", path,
		"--sa-role-name", "view",
	})
	require.ErrorIs(t, err, ErrSelectMissingPid)
}

// TestHostSelectDefaultsOutPath pins two distinct defaults that
// fire when the corresponding flag is omitted: --out-path falls
// back to <stateHomeDir>/kubeconfig.<pid>.<env>.yaml (where <pid>
// is the value of --pid, used purely as a filename discriminator),
// and --socket-path falls back to
// <socketStateDir>/serve.0.<env>.sock (slot 0 of the slot pool;
// independent of --pid). Both have <env> driven by --for-guest.
// In production serve always forwards an explicit --socket-path so
// the slot-0 default is only hit by tests.
func TestHostSelectDefaultsOutPath(t *testing.T) { //nolint:paralleltest // mutates package-level state, uses t.Setenv
	tests := map[string]struct {
		forGuest   string
		want       string
		wantSocket string
	}{
		"host env": {
			forGuest:   "false",
			want:       "kubeconfig.1234.host.yaml",
			wantSocket: "serve.0.host.sock",
		},
		"guest env": {
			forGuest:   "true",
			want:       "kubeconfig.1234.guest.yaml",
			wantSocket: "serve.0.guest.sock",
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests use t.Setenv
		t.Run(name, func(t *testing.T) {
			withHostKubeClient(t, &kubetest.Fake{Token: "t", TokenExpiry: time.Now()})

			buf := withHostStdout(t)

			stateHome := t.TempDir()
			t.Setenv("XDG_STATE_HOME", stateHome)

			path := writeTestKubeconfig(t, testKubeconfig())

			err := runHostSelect(t.Context(), []string{
				"prod",
				"--kubeconfig", path,
				"--pid", "1234",
				"--for-guest=" + tc.forGuest,
				"--sa-role-name", "view",
			})
			require.NoError(t, err)

			expected := filepath.Join(stateHome, "mcp-kubectx", tc.want)
			expectedSocket := filepath.Join(stateHome, "mcp-kubectx-run", tc.wantSocket)

			var result HostSelectResult

			require.NoError(t, json.NewDecoder(strings.NewReader(buf.String())).Decode(&result))
			assert.Equal(t, expected, result.Path)

			_, statErr := os.Stat(expected)
			require.NoError(t, statErr, "kubeconfig file must exist at the defaulted path")

			plugin := readKubeconfigExec(t, expected)
			assert.Equal(t, []string{"exec-plugin", "--socket", expectedSocket}, plugin.Args,
				"defaulted socket path must be embedded in the exec plugin args")
		})
	}
}

func TestHostSelectMissingContext(t *testing.T) {
	t.Parallel()

	path := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSelect(t.Context(), []string{
		"--kubeconfig", path,
		"--out-path", filepath.Join(t.TempDir(), "k.yaml"),
		"--sa-role-name", "view",
	})
	require.ErrorIs(t, err, ErrMissingContext)
}

func TestHostSelectContextNotFound(t *testing.T) {
	t.Parallel()

	path := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSelect(t.Context(), []string{
		"nonexistent",
		"--kubeconfig", path,
		"--out-path", filepath.Join(t.TempDir(), "k.yaml"),
		"--sa-role-name", "view",
	})
	require.ErrorIs(t, err, ErrContextNotFound)
}

// TestHostSelectClusterEntryMissing pins that a context whose
// cluster entry is absent from the kubeconfig is refused before any
// cluster-side mutation, even with no apiserver allowlist. Without
// the guard, the SA and binding would be created and then stranded
// behind a scoped kubeconfig with a dangling cluster reference.
func TestHostSelectClusterEntryMissing(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostStdout(t)

	cfg := testKubeconfig()
	cfg.Contexts = append(cfg.Contexts, kubeconfig.NamedContext{
		Name:    "dangling",
		Context: kubeconfig.Context{Cluster: "no-such-cluster", User: "admin"},
	})

	path := writeTestKubeconfig(t, cfg)
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	err := runHostSelect(t.Context(), []string{
		"dangling",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
	})
	require.ErrorIs(t, err, ErrClusterNotFound)

	assert.Empty(t, mock.CreatedSAs, "missing cluster entry must not create the SA")

	_, statErr := os.Stat(outPath)
	assert.True(t, os.IsNotExist(statErr), "missing cluster entry must not write the kubeconfig")
}

// TestHostSelectExecPluginShape pins the uniform exec-plugin
// shape across forGuest=true/false. The two variants no longer
// differ -- both write the same `mcp-kubectx exec-plugin --socket
// <path>` block. Only the socket path env discriminator (`host`
// vs. `guest`) flips. The test forwards --socket-path explicitly
// (rather than relying on the slot-0 default) to mirror serve's
// production behavior.
func TestHostSelectExecPluginShape(t *testing.T) { //nolint:paralleltest // mutates package-level state, uses t.Setenv
	tests := map[string]struct {
		forGuest    string
		wantSocket  string
		clusterRole string
	}{
		"host env": {
			forGuest:    "false",
			wantSocket:  "serve.0.host.sock",
			clusterRole: "ClusterRole",
		},
		"guest env": {
			forGuest:    "true",
			wantSocket:  "serve.0.guest.sock",
			clusterRole: "ClusterRole",
		},
	}

	for name, tc := range tests { //nolint:paralleltest // subtests use t.Setenv
		t.Run(name, func(t *testing.T) {
			withHostKubeClient(t, &kubetest.Fake{Token: "t", TokenExpiry: time.Now()})

			buf := withHostStdout(t)

			stateHome := t.TempDir()
			t.Setenv("XDG_STATE_HOME", stateHome)

			path := writeTestKubeconfig(t, testKubeconfig())
			outPath := filepath.Join(t.TempDir(), "k.yaml")

			err := runHostSelect(t.Context(), []string{
				"prod",
				"--kubeconfig", path,
				"--out-path", outPath,
				"--pid", "4242",
				"--for-guest=" + tc.forGuest,
				"--sa-role-name", "view",
				"--sa-role-kind", tc.clusterRole,
				"--sa-namespace", "kube-system",
				"--sa-expiration", "3600",
			})
			require.NoError(t, err)

			var result HostSelectResult

			require.NoError(t, json.NewDecoder(strings.NewReader(buf.String())).Decode(&result))
			assert.Equal(t, outPath, result.Path)

			plugin := readKubeconfigExec(t, outPath)
			assert.Equal(t, "mcp-kubectx", plugin.Command,
				"command must be the bare program name, not workmux or an absolute path")
			require.Len(t, plugin.Args, 3)
			assert.Equal(t, "exec-plugin", plugin.Args[0])
			assert.Equal(t, "--socket", plugin.Args[1])

			expectedSocket := filepath.Join(stateHome, "mcp-kubectx-run", tc.wantSocket)
			assert.Equal(t, expectedSocket, plugin.Args[2])
		})
	}
}

// TestHostSelectBindingNameConvention pins the contract that
// [bindingNameForSA] is the single source of truth: the binding
// created by `host select` matches `<sa>-binding`, and `host
// release` uses the same convention to delete it.
func TestHostSelectBindingNameConvention(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect(t.Context(), []string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--sa-namespace", "ns",
	}))

	require.Len(t, mock.CreatedSAs, 1)
	require.Len(t, mock.CreatedRoleBindings, 1)

	saFullName := mock.CreatedSAs[0]
	saName := saFullName[len("ns/"):]
	assert.Equal(t, "ns/"+serviceaccount.BindingName(saName), mock.CreatedRoleBindings[0])
}

// TestHostSelectOmitsInstanceAndHostLabelsWhenEmpty pins the
// contract that `host select` only emits the new labels when the
// corresponding flags are non-empty. Preserves existing test
// invariants for standalone CLI use.
//
//nolint:paralleltest // mutates package-level state
func TestHostSelectOmitsInstanceAndHostLabelsWhenEmpty(t *testing.T) {
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect(t.Context(), []string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
	}))

	require.Len(t, mock.CreatedSALabels, 1)

	labels := mock.CreatedSALabels[0]
	assert.Equal(t, serviceaccount.ManagedByValue, labels[serviceaccount.ManagedByLabel])

	_, hasInstance := labels[serviceaccount.InstanceIDLabel]
	_, hasHost := labels[serviceaccount.HostIDLabel]

	assert.False(t, hasInstance, "missing --sa-instance-id must omit the instance-id label")
	assert.False(t, hasHost, "missing --sa-host-id must omit the host-id label")
}

// TestHostSelectAppliesInstanceAndHostLabels pins that explicit
// --sa-instance-id and --sa-host-id flags get tagged onto every
// created resource so the sweep can attribute them.
func TestHostSelectAppliesInstanceAndHostLabels(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect(t.Context(), []string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--sa-instance-id", "instance-xyz",
		"--sa-host-id", "host-abc",
	}))

	require.Len(t, mock.CreatedSALabels, 1)
	assert.Equal(t, "instance-xyz", mock.CreatedSALabels[0][serviceaccount.InstanceIDLabel])
	assert.Equal(t, "host-abc", mock.CreatedSALabels[0][serviceaccount.HostIDLabel])

	require.Len(t, mock.CreatedRoleBindingLabels, 1)
	assert.Equal(t, "instance-xyz", mock.CreatedRoleBindingLabels[0][serviceaccount.InstanceIDLabel])
	assert.Equal(t, "host-abc", mock.CreatedRoleBindingLabels[0][serviceaccount.HostIDLabel])
}

func TestHostSelectDefaultNamespace(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect(t.Context(), []string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--sa-expiration", "3600",
	}))

	require.Len(t, mock.CreatedSAs, 1)
	assert.Contains(t, mock.CreatedSAs[0], "default/claude-sa-")
}

func TestHostSelectContextNamespace(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect(t.Context(), []string{
		"staging",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--sa-expiration", "3600",
	}))

	require.Len(t, mock.CreatedSAs, 1)
	assert.Contains(t, mock.CreatedSAs[0], "default/claude-sa-")
}

func TestHostSelectFilePermissions(t *testing.T) { //nolint:paralleltest // mutates package-level state
	withHostKubeClient(t, &kubetest.Fake{Token: "t", TokenExpiry: time.Now()})
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect(t.Context(), []string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
	}))

	info, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestHostSelectAPIServerAllowed(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect(t.Context(), []string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--allow-apiserver-host", "prod.example.com",
		"--allow-apiserver-host", "staging.example.com",
	}))

	assert.NotEmpty(t, mock.CreatedSAs, "allowed select must create the SA")

	_, statErr := os.Stat(outPath)
	assert.NoError(t, statErr, "allowed select must write the kubeconfig")
}

// TestHostSelectAPIServerAllowedCaseInsensitive pins that the
// allowlist comparison ignores case: DNS hostnames are
// case-insensitive, so a mixed-case `cluster.server` URL must
// still match a lowercase allowlist entry.
func TestHostSelectAPIServerAllowedCaseInsensitive(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostStdout(t)

	cfg := testKubeconfig()
	cfg.Clusters[0].Cluster = map[string]any{"server": "https://PROD.Example.COM:6443"}

	path := writeTestKubeconfig(t, cfg)
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect(t.Context(), []string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--allow-apiserver-host", "prod.example.com",
	}))

	assert.NotEmpty(t, mock.CreatedSAs, "case-different host must still match the allowlist")
}

func TestHostSelectAPIServerDenied(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	err := runHostSelect(t.Context(), []string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--allow-apiserver-host", "staging.example.com",
	})
	require.ErrorIs(t, err, ErrAPIServerNotAllowed)
	assert.Contains(t, err.Error(), "prod.example.com")

	assert.Empty(t, mock.CreatedSAs, "denied select must not create the SA")

	_, statErr := os.Stat(outPath)
	assert.True(t, os.IsNotExist(statErr), "denied select must not write the kubeconfig")
}

func TestHostSelectAPIServerEmptyAllowlistAllowsAny(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	// Pin: omitting --allow-apiserver-host entirely lets any
	// apiserver through. Selecting `dev` (cluster `dev.example.com`,
	// not present in any allowlist test above) succeeds.
	require.NoError(t, runHostSelect(t.Context(), []string{
		"dev",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
	}))

	assert.NotEmpty(t, mock.CreatedSAs, "no allowlist must still create the SA")

	_, statErr := os.Stat(outPath)
	assert.NoError(t, statErr, "no allowlist must still write the kubeconfig")
}

func TestHostSelectClusterScoped(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect(t.Context(), []string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--sa-cluster-scoped=true",
	}))

	require.Len(t, mock.CreatedClusterRoleBindings, 1)
	assert.Empty(t, mock.CreatedRoleBindings)
}

func TestHostToken(t *testing.T) { //nolint:paralleltest // mutates package-level state
	expiry := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC)
	mock := &kubetest.Fake{Token: "tok-xyz", TokenExpiry: expiry}
	withHostKubeClient(t, mock)

	buf := withHostStdout(t)

	require.NoError(t, runHostToken(t.Context(), []string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
		"--sa-expiration", "3600",
	}))

	var cred execplugin.Credential

	require.NoError(t, json.NewDecoder(strings.NewReader(buf.String())).Decode(&cred))

	assert.Equal(t, execplugin.APIVersion, cred.APIVersion)
	assert.Equal(t, "ExecCredential", cred.Kind)
	assert.Equal(t, "tok-xyz", cred.Status.Token)
	assert.Equal(t, expiry.Format(time.RFC3339), cred.Status.ExpirationTimestamp)
}

func TestHostTokenMissingFlags(t *testing.T) {
	t.Parallel()

	err := runHostToken(t.Context(), []string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--namespace", "ns",
	})
	require.Error(t, err)
}

// TestHostTokenRejectsExcessiveExpiration pins that `host token`
// enforces the same 86400-second cap as saConfig.validate on the
// serve path. The subcommand is reachable directly, and an
// unbounded value would both violate the documented cap and
// overflow time.Duration for very large inputs.
func TestHostTokenRejectsExcessiveExpiration(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{Token: "t", TokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostStdout(t)

	err := runHostToken(t.Context(), []string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
		"--sa-expiration", "100000",
	})
	require.ErrorIs(t, err, serviceaccount.ErrExpirationTooLong)

	assert.Empty(t, mock.TokenRequests, "an out-of-cap expiration must not reach the apiserver")
}

func TestHostTokenAPIError(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{TokenRequestErr: errors.New("unauthorized")}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	err := runHostToken(t.Context(), []string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	})
	require.ErrorIs(t, err, ErrTokenRequest)
}

func TestHostReleaseRoleBinding(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{}
	withHostKubeClient(t, mock)

	require.NoError(t, runHostRelease(t.Context(), []string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	}))

	require.Len(t, mock.DeletedRoleBindings, 1)
	assert.Equal(t, "ns/claude-sa-1-binding", mock.DeletedRoleBindings[0])
	require.Len(t, mock.DeletedSAs, 1)
	assert.Equal(t, "ns/claude-sa-1", mock.DeletedSAs[0])
	assert.Empty(t, mock.DeletedClusterRoleBindings)
}

func TestHostReleaseClusterRoleBinding(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{}
	withHostKubeClient(t, mock)

	require.NoError(t, runHostRelease(t.Context(), []string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
		"--sa-cluster-scoped=true",
	}))

	require.Len(t, mock.DeletedClusterRoleBindings, 1)
	require.Len(t, mock.DeletedSAs, 1)
	assert.Empty(t, mock.DeletedRoleBindings)
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

	require.NoError(t, runHostRelease(t.Context(), []string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	}))
}

func TestHostReleaseMissingFlags(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &kubetest.Fake{}
	withHostKubeClient(t, mock)

	require.NoError(t, runHostRelease(t.Context(), []string{
		"--kubeconfig", "/dev/null",
	}))

	assert.Empty(t, mock.DeletedSAs, "missing flags must skip the cluster call entirely")
}

// readKubeconfigExec reads the kubeconfig at path and returns its
// users[0].user.exec block decoded into [execplugin.Plugin]. Helper for
// asserting host select wrote the right exec-plugin shape.
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
