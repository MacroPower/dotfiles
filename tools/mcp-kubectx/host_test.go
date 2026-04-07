package main

import (
	"bytes"
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
)

// withHostStdout swaps the package-level hostStdout writer for the
// duration of t and returns a buffer that captures the writes.
func withHostStdout(t *testing.T) *bytes.Buffer {
	t.Helper()

	prev := hostStdout
	buf := &bytes.Buffer{}
	hostStdout = buf

	t.Cleanup(func() { hostStdout = prev })

	return buf
}

// withHostKubeClient swaps the host-side KubeClient factory so
// host * subcommands use a fake client instead of touching the
// real cluster.
func withHostKubeClient(t *testing.T, mock KubeClient) {
	t.Helper()

	prev := hostKubeClient
	hostKubeClient = func(string, string) (KubeClient, error) { return mock, nil }

	t.Cleanup(func() { hostKubeClient = prev })
}

func TestHostList(t *testing.T) { //nolint:paralleltest // uses withHostStdout, package-level state
	tests := map[string]struct {
		cfg  kubeConfig
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
			cfg:  kubeConfig{},
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
	require.ErrorIs(t, err, ErrLoadKubeconfig)
}

func TestHostSelectMissingOutPath(t *testing.T) {
	t.Parallel()

	path := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSelect([]string{
		"prod",
		"--kubeconfig", path,
		"--sa-role-name", "view",
	})
	require.ErrorIs(t, err, ErrSelectMissingOutPath)
}

func TestHostSelectMissingContext(t *testing.T) {
	t.Parallel()

	path := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSelect([]string{
		"--kubeconfig", path,
		"--out-path", filepath.Join(t.TempDir(), "k.yaml"),
		"--sa-role-name", "view",
	})
	require.ErrorIs(t, err, ErrMissingContext)
}

func TestHostSelectContextNotFound(t *testing.T) {
	t.Parallel()

	path := writeTestKubeconfig(t, testKubeconfig())

	err := runHostSelect([]string{
		"nonexistent",
		"--kubeconfig", path,
		"--out-path", filepath.Join(t.TempDir(), "k.yaml"),
		"--sa-role-name", "view",
	})
	require.ErrorIs(t, err, ErrContextNotFound)
}

func TestHostSelectGuestVariant(t *testing.T) { //nolint:paralleltest // mutates package-level state
	withHostKubeClient(t, &mockKubeClient{token: "t", tokenExpiry: time.Now()})

	buf := withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	err := runHostSelect([]string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--for-guest=true",
		"--sa-role-name", "view",
		"--sa-role-kind", "ClusterRole",
		"--sa-namespace", "kube-system",
		"--sa-expiration", "3600",
	})
	require.NoError(t, err)

	var result HostSelectResult

	err = json.NewDecoder(strings.NewReader(buf.String())).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, outPath, result.Path)
	assert.Equal(t, "kube-system", result.Namespace)
	assert.Equal(t, "prod", result.Context)
	assert.Equal(t, path, result.Kubeconfig)
	assert.Contains(t, result.SAName, "claude-sa-")

	plugin := readKubeconfigExec(t, outPath)
	assert.Equal(t, "workmux", plugin.Command)
	require.NotEmpty(t, plugin.Args)
	assert.Equal(t, "host-exec", plugin.Args[0])
	assert.Contains(t, plugin.Args, "mcp-kubectx")
	assert.Contains(t, plugin.Args, "token")
}

// TestHostSelectBindingNameConvention pins the contract that
// [bindingNameForSA] is the single source of truth: the binding
// created by `host select` matches `<sa>-binding`, and `host
// release` uses the same convention to delete it.
func TestHostSelectBindingNameConvention(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{token: "t", tokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect([]string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--sa-namespace", "ns",
	}))

	require.Len(t, mock.createdSAs, 1)
	require.Len(t, mock.createdRoleBindings, 1)

	saFullName := mock.createdSAs[0]
	saName := saFullName[len("ns/"):]
	assert.Equal(t, "ns/"+bindingNameForSA(saName), mock.createdRoleBindings[0])
}

// TestHostSelectHostVariant pins the argv shape of the
// host-variant exec plugin. We assert the leading flags directly
// so a regression in buildExecPlugin shows up loudly.
func TestHostSelectHostVariant(t *testing.T) { //nolint:paralleltest // mutates package-level state
	withHostKubeClient(t, &mockKubeClient{token: "t", tokenExpiry: time.Now()})

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect([]string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--for-guest=false",
		"--sa-role-name", "view",
		"--sa-namespace", "kube-system",
		"--sa-expiration", "3600",
	}))

	plugin := readKubeconfigExec(t, outPath)

	assert.True(t, filepath.IsAbs(plugin.Command),
		"host variant must use an absolute executable path: %s", plugin.Command)
	assert.NotEqual(t, "workmux", plugin.Command)

	require.Len(t, plugin.Args, 12)
	assert.Equal(t, "host", plugin.Args[0])
	assert.Equal(t, "token", plugin.Args[1])
	assert.Equal(t, "--kubeconfig", plugin.Args[2])
	assert.Equal(t, path, plugin.Args[3])
	assert.Equal(t, "--context", plugin.Args[4])
	assert.Equal(t, "prod", plugin.Args[5])
	assert.Equal(t, "--sa", plugin.Args[6])
	assert.Contains(t, plugin.Args[7], "claude-sa-")
	assert.Equal(t, "--namespace", plugin.Args[8])
	assert.Equal(t, "kube-system", plugin.Args[9])
	assert.Equal(t, "--sa-expiration", plugin.Args[10])
	assert.Equal(t, "3600", plugin.Args[11])
}

func TestHostSelectDefaultNamespace(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{token: "t", tokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect([]string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--sa-expiration", "3600",
	}))

	require.Len(t, mock.createdSAs, 1)
	assert.Contains(t, mock.createdSAs[0], "default/claude-sa-")
}

func TestHostSelectContextNamespace(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{token: "t", tokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect([]string{
		"staging",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--sa-expiration", "3600",
	}))

	require.Len(t, mock.createdSAs, 1)
	assert.Contains(t, mock.createdSAs[0], "default/claude-sa-")
}

func TestHostSelectFilePermissions(t *testing.T) { //nolint:paralleltest // mutates package-level state
	withHostKubeClient(t, &mockKubeClient{token: "t", tokenExpiry: time.Now()})
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect([]string{
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
	mock := &mockKubeClient{token: "t", tokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect([]string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--allow-apiserver-host", "prod.example.com",
		"--allow-apiserver-host", "staging.example.com",
	}))

	assert.NotEmpty(t, mock.createdSAs, "allowed select must create the SA")

	_, statErr := os.Stat(outPath)
	assert.NoError(t, statErr, "allowed select must write the kubeconfig")
}

func TestHostSelectAPIServerDenied(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{token: "t", tokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	err := runHostSelect([]string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--allow-apiserver-host", "staging.example.com",
	})
	require.ErrorIs(t, err, ErrAPIServerNotAllowed)
	assert.Contains(t, err.Error(), "prod.example.com")

	assert.Empty(t, mock.createdSAs, "denied select must not create the SA")

	_, statErr := os.Stat(outPath)
	assert.True(t, os.IsNotExist(statErr), "denied select must not write the kubeconfig")
}

func TestHostSelectAPIServerEmptyAllowlistAllowsAny(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{token: "t", tokenExpiry: time.Now()}
	withHostKubeClient(t, mock)
	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	// Pin: omitting --allow-apiserver-host entirely lets any
	// apiserver through. Selecting `dev` (cluster `dev.example.com`,
	// not present in any allowlist test above) succeeds.
	require.NoError(t, runHostSelect([]string{
		"dev",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
	}))

	assert.NotEmpty(t, mock.createdSAs, "no allowlist must still create the SA")

	_, statErr := os.Stat(outPath)
	assert.NoError(t, statErr, "no allowlist must still write the kubeconfig")
}

func TestHostSelectClusterScoped(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{token: "t", tokenExpiry: time.Now()}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	path := writeTestKubeconfig(t, testKubeconfig())
	outPath := filepath.Join(t.TempDir(), "k.yaml")

	require.NoError(t, runHostSelect([]string{
		"prod",
		"--kubeconfig", path,
		"--out-path", outPath,
		"--sa-role-name", "view",
		"--sa-cluster-scoped=true",
	}))

	require.Len(t, mock.createdClusterRoleBindings, 1)
	assert.Empty(t, mock.createdRoleBindings)
}

func TestHostToken(t *testing.T) { //nolint:paralleltest // mutates package-level state
	expiry := time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC)
	mock := &mockKubeClient{token: "tok-xyz", tokenExpiry: expiry}
	withHostKubeClient(t, mock)

	buf := withHostStdout(t)

	require.NoError(t, runHostToken([]string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
		"--sa-expiration", "3600",
	}))

	var cred ExecCredential

	require.NoError(t, json.NewDecoder(strings.NewReader(buf.String())).Decode(&cred))

	assert.Equal(t, execAuthAPIVersion, cred.APIVersion)
	assert.Equal(t, "ExecCredential", cred.Kind)
	assert.Equal(t, "tok-xyz", cred.Status.Token)
	assert.Equal(t, expiry.Format(time.RFC3339), cred.Status.ExpirationTimestamp)
}

func TestHostTokenMissingFlags(t *testing.T) {
	t.Parallel()

	err := runHostToken([]string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--namespace", "ns",
	})
	require.Error(t, err)
}

func TestHostTokenAPIError(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{tokenRequestErr: errors.New("unauthorized")}
	withHostKubeClient(t, mock)

	withHostStdout(t)

	err := runHostToken([]string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	})
	require.ErrorIs(t, err, ErrTokenRequest)
}

func TestHostReleaseRoleBinding(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{}
	withHostKubeClient(t, mock)

	require.NoError(t, runHostRelease([]string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	}))

	require.Len(t, mock.deletedRoleBindings, 1)
	assert.Equal(t, "ns/claude-sa-1-binding", mock.deletedRoleBindings[0])
	require.Len(t, mock.deletedSAs, 1)
	assert.Equal(t, "ns/claude-sa-1", mock.deletedSAs[0])
	assert.Empty(t, mock.deletedClusterRoleBindings)
}

func TestHostReleaseClusterRoleBinding(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{}
	withHostKubeClient(t, mock)

	require.NoError(t, runHostRelease([]string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
		"--sa-cluster-scoped=true",
	}))

	require.Len(t, mock.deletedClusterRoleBindings, 1)
	require.Len(t, mock.deletedSAs, 1)
	assert.Empty(t, mock.deletedRoleBindings)
}

// TestHostReleaseAlwaysSucceeds asserts that release exits cleanly
// even when both Delete* calls fail. Returning non-zero would force
// serve to retry the release for the entire process lifetime over
// a single transient error.
func TestHostReleaseAlwaysSucceeds(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{
		deleteSAErr:          errors.New("not found"),
		deleteRoleBindingErr: errors.New("api hiccup"),
	}
	withHostKubeClient(t, mock)

	require.NoError(t, runHostRelease([]string{
		"--kubeconfig", "/dev/null",
		"--context", "prod",
		"--sa", "claude-sa-1",
		"--namespace", "ns",
	}))
}

func TestHostReleaseMissingFlags(t *testing.T) { //nolint:paralleltest // mutates package-level state
	mock := &mockKubeClient{}
	withHostKubeClient(t, mock)

	require.NoError(t, runHostRelease([]string{
		"--kubeconfig", "/dev/null",
	}))

	assert.Empty(t, mock.deletedSAs, "missing flags must skip the cluster call entirely")
}

// readKubeconfigExec reads the kubeconfig at path and returns its
// users[0].user.exec block decoded into [execPlugin]. Helper for
// asserting host select wrote the right exec-plugin shape.
func readKubeconfigExec(t *testing.T, path string) execPlugin {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var raw struct {
		Users []struct {
			User struct {
				Exec execPlugin `yaml:"exec"`
			} `yaml:"user"`
		} `yaml:"users"`
	}

	require.NoError(t, yaml.Unmarshal(data, &raw))
	require.Len(t, raw.Users, 1)

	return raw.Users[0].User.Exec
}

// Compile-time guard: hostStdout must implement io.Writer.
var _ io.Writer = hostStdout
