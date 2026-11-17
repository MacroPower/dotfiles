package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/ledger"
)

// runMainEnv names the environment variable that makes the test
// binary run [run] in place of the tests. A test sets it to "1" to
// start a `host` subcommand as a child process.
const runMainEnv = "MCP_KUBECTX_TEST_RUN_MAIN"

// TestMain keeps the provision records of every test out of the
// state directory of the user who runs the tests. `host release`
// and `host sweep` read the ledger on every call, and `host select`
// writes to it. A test that asserts on records takes a directory of
// its own with [withHostLedger].
//
// With [runMainEnv] set to "1", TestMain runs [run] on the
// arguments of the process and exits with its status.
func TestMain(m *testing.M) {
	if os.Getenv(runMainEnv) == "1" {
		os.Exit(run())
	}

	dir, err := os.MkdirTemp("", "mcp-kubectx-ledger")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create ledger directory: %v\n", err)
		os.Exit(1)
	}

	hostLedgerDir = func() string { return filepath.Join(dir, "ledger") }

	code := m.Run()

	_ = os.RemoveAll(dir) //nolint:errcheck // best-effort test cleanup

	os.Exit(code)
}

// shortTempDir returns a temp directory under [os.TempDir] whose
// path is short enough to keep socket paths under macOS's 104-byte
// sun_path limit. [testing.T.TempDir] embeds the test name in the
// path, which combined with the mcp-kubectx-run subdir + slot
// filename overflows the limit on the Nix builder where TMPDIR is
// already deeply nested. Cleanup is registered with t.Cleanup.
func shortTempDir(t *testing.T) string {
	t.Helper()

	// usetesting linter wants t.TempDir() here, but that is exactly
	// the failure mode this helper exists to avoid: t.TempDir embeds
	// the test name in the path, producing paths that exceed the
	// 104-byte sun_path limit on the Nix builder.
	dir, err := os.MkdirTemp(os.TempDir(), "k") //nolint:usetesting // see comment above
	require.NoError(t, err)

	t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // best-effort test cleanup

	return dir
}

// constLookup returns an envLookup that always returns val.
// Tests use it to drive handler.isGuest without mutating process
// env (which is goroutine-unsafe and breaks t.Parallel).
func constLookup(val string) func(string) string {
	return func(string) string { return val }
}

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

// withHostKubeClient swaps the host-side kube.Client factory so
// host * subcommands use a fake client instead of touching the
// real cluster.
func withHostKubeClient(t *testing.T, fake kube.Client) {
	t.Helper()

	prev := hostKubeClient
	hostKubeClient = func(string, string) (kube.Client, error) { return fake, nil }

	t.Cleanup(func() { hostKubeClient = prev })
}

// withHostLedger points the host subcommands at an empty ledger in
// a directory of the test, and returns that ledger.
func withHostLedger(t *testing.T) *ledger.Ledger {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "ledger")

	prev := hostLedgerDir
	hostLedgerDir = func() string { return dir }

	t.Cleanup(func() { hostLedgerDir = prev })

	return ledger.New(dir)
}

// withUnreadableHostLedger points the host subcommands at a ledger
// that [ledger.Ledger.List] cannot read, because a regular file
// lies at the path of the ledger directory.
func withUnreadableHostLedger(t *testing.T) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "ledger")
	require.NoError(t, os.WriteFile(dir, nil, 0o600))

	prev := hostLedgerDir
	hostLedgerDir = func() string { return dir }

	t.Cleanup(func() { hostLedgerDir = prev })

	_, err := ledger.New(dir).List()
	require.ErrorIs(t, err, ledger.ErrList, "the test needs a ledger that List cannot read")
}

// errNoTestClient is the error of [kubeClients] for a context that
// the test gave no client.
var errNoTestClient = errors.New("no test client for context")

// kubeClientCall formats the arguments of one hostKubeClient call
// the way [kubeClients] records them.
func kubeClientCall(kubeconfigPath, contextName string) string {
	return contextName + " in " + kubeconfigPath
}

// kubeClients is a host-side client factory that gives each context
// a client of its own, so a test can tell the clusters apart. It
// records the arguments of each call. The record pass of `host
// sweep` builds its clients in parallel, so a mutex guards the
// recording.
type kubeClients struct {
	clients map[string]kube.Client
	calls   []string
	mu      sync.Mutex
}

// Calls returns one [kubeClientCall] string for every call so far.
func (k *kubeClients) Calls() []string {
	k.mu.Lock()
	defer k.mu.Unlock()

	return append([]string(nil), k.calls...)
}

func (k *kubeClients) client(kubeconfigPath, contextName string) (kube.Client, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.calls = append(k.calls, kubeClientCall(kubeconfigPath, contextName))

	client, ok := k.clients[contextName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNoTestClient, contextName)
	}

	return client, nil
}

// withHostKubeClients swaps the host-side kube.Client factory for
// one that maps each context name to the client in clients. The
// factory fails for every other context.
func withHostKubeClients(t *testing.T, clients map[string]kube.Client) *kubeClients {
	t.Helper()

	k := &kubeClients{clients: clients}

	prev := hostKubeClient
	hostKubeClient = k.client

	t.Cleanup(func() { hostKubeClient = prev })

	return k
}

// notFoundErr returns the error that [kube.Clientset] returns for a
// delete of an object that does not exist.
func notFoundErr(resource, name string) error {
	return fmt.Errorf("delete: %w", apierrors.NewNotFound(schema.GroupResource{Resource: resource}, name))
}

// forbiddenErr returns the error that [kube.Clientset] returns for a
// create that RBAC refuses.
func forbiddenErr(resource, name string) error {
	return fmt.Errorf("create: %w", apierrors.NewForbidden(
		schema.GroupResource{Resource: resource}, name, errors.New("denied"),
	))
}

// neutralizeWrapperEnv clears every Claude-launcher env var a
// selectCtx round-trip reads, and pins TMPDIR to a per-test dir.
// Without this, an ambient wrapper environment (a test run from
// inside a live Claude session) leaks into routing decisions via
// localView and -- worse -- publishSidecar overwrites the live
// session's real sidecar symlink.
func neutralizeWrapperEnv(t *testing.T) string {
	t.Helper()

	tmp := t.TempDir()

	t.Setenv("TMPDIR", tmp)
	t.Setenv("KUBECONFIG", "")
	t.Setenv("CLAUDE_KUBECTX_DIR", "")
	t.Setenv("CLAUDE_KUBECTX_LOCAL", "")
	t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")
	t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", "")

	return tmp
}
