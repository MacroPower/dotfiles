package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestSocketPathForSlot(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	tests := map[string]struct {
		slot     int
		forGuest bool
		want     string
	}{
		"host slot 0": {
			slot:     0,
			forGuest: false,
			want:     filepath.Join(stateHome, "mcp-kubectx-run", "serve.0.host.sock"),
		},
		"guest slot 1": {
			slot:     1,
			forGuest: true,
			want:     filepath.Join(stateHome, "mcp-kubectx-run", "serve.1.guest.sock"),
		},
		"host slot 7": {
			slot:     7,
			forGuest: false,
			want:     filepath.Join(stateHome, "mcp-kubectx-run", "serve.7.host.sock"),
		},
	}

	for name, tc := range tests { //nolint:paralleltest // shares t.Setenv state
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, socketPathForSlot(tc.slot, tc.forGuest))
		})
	}
}

// TestAcquireServeSocketPicksFirstFreeSlot pre-binds slots 0..k-1
// with raw net.Listen calls so [*handler.acquireServeSocket]'s
// dial probe sees them as live, then asserts acquire returns slot k.
func TestAcquireServeSocketPicksFirstFreeSlot(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	const k = 3

	for slot := range k {
		path := socketPathForSlot(slot, false)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))

		live, err := net.Listen("unix", path) //nolint:noctx // synchronous test fixture
		require.NoError(t, err)

		t.Cleanup(func() { _ = live.Close() }) //nolint:errcheck // best-effort test cleanup

		go func() {
			for {
				conn, err := live.Accept()
				if err != nil {
					return
				}

				_ = conn.Close() //nolint:errcheck // best-effort test cleanup
			}
		}()
	}

	h := &handler{}

	gotPath, listener, cleanup, err := h.acquireServeSocket(t.Context(), false, 8)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	assert.Equal(t, socketPathForSlot(k, false), gotPath)
	assert.NotNil(t, listener)
}

// TestAcquireServeSocketSkipsStaleSlot drops a regular file at slot 0
// (simulating a SIGKILLed serve's leftover) and asserts acquire
// reuses slot 0 because [clearStaleSocket] unlinks the leftover
// before the bind.
func TestAcquireServeSocketSkipsStaleSlot(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	staleSlot := socketPathForSlot(0, false)
	require.NoError(t, os.MkdirAll(filepath.Dir(staleSlot), 0o700))
	require.NoError(t, os.WriteFile(staleSlot, []byte("stale"), 0o600))

	h := &handler{}

	gotPath, _, cleanup, err := h.acquireServeSocket(t.Context(), false, 8)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	assert.Equal(t, staleSlot, gotPath, "stale leftover at slot 0 must be reclaimed")

	info, err := os.Stat(gotPath)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSocket, info.Mode().Type(),
		"stale file should have been replaced by a real socket")
}

// TestAcquireServeSocketDoesNotStealLivePeer pre-binds slot 0 with
// a live listener, calls acquireServeSocket, and verifies (a) the
// returned path is slot 1, and (b) the live peer's socket inode is
// still bound by the original listener after acquire returned. Pins
// the property that the slot loop never silently unlinks a live
// peer's socket.
func TestAcquireServeSocketDoesNotStealLivePeer(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	livePath := socketPathForSlot(0, false)
	require.NoError(t, os.MkdirAll(filepath.Dir(livePath), 0o700))

	live, err := net.Listen("unix", livePath) //nolint:noctx // synchronous test fixture
	require.NoError(t, err)
	t.Cleanup(func() { _ = live.Close() }) //nolint:errcheck // best-effort test cleanup

	go func() {
		for {
			conn, err := live.Accept()
			if err != nil {
				return
			}

			_ = conn.Close() //nolint:errcheck // best-effort test cleanup
		}
	}()

	h := &handler{}

	gotPath, _, cleanup, err := h.acquireServeSocket(t.Context(), false, 8)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	assert.Equal(t, socketPathForSlot(1, false), gotPath,
		"live peer at slot 0 must push acquire to slot 1")

	// The live peer's path must still accept dials. If acquire had
	// stolen the inode, the next Dial would fail with ECONNREFUSED
	// (no listener on that path anymore).
	conn, dialErr := net.DialTimeout("unix", livePath, 2*time.Second) //nolint:noctx // synchronous test fixture
	require.NoError(t, dialErr, "live peer's socket must still be bound after acquire")

	_ = conn.Close() //nolint:errcheck // best-effort test cleanup
}

// TestAcquireServeSocketExhaustion binds every slot in the pool and
// asserts acquire surfaces [ErrAllSlotsBusy] with a message that
// names the slot count and state directory so operators have
// actionable detail.
func TestAcquireServeSocketExhaustion(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	const maxSlots = 4

	for slot := range maxSlots {
		path := socketPathForSlot(slot, false)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))

		live, err := net.Listen("unix", path) //nolint:noctx // synchronous test fixture
		require.NoError(t, err)

		t.Cleanup(func() { _ = live.Close() }) //nolint:errcheck // best-effort test cleanup

		go func() {
			for {
				conn, err := live.Accept()
				if err != nil {
					return
				}

				_ = conn.Close() //nolint:errcheck // best-effort test cleanup
			}
		}()
	}

	h := &handler{}

	_, _, _, err := h.acquireServeSocket(t.Context(), false, maxSlots)
	require.ErrorIs(t, err, ErrAllSlotsBusy)
	assert.Contains(t, err.Error(), "4 slots")
	assert.Contains(t, err.Error(), filepath.Join(stateHome, "mcp-kubectx-run"))
}

func TestListenSocketUnlinksStale(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "stale.sock")

	// A regular file at the path simulates a leftover from a
	// previous serve that exited without unlinking.
	require.NoError(t, os.WriteFile(path, []byte("stale"), 0o600))

	h := &handler{}

	l, cleanup, err := h.listenSocket(t.Context(), path)
	require.NoError(t, err)

	t.Cleanup(cleanup)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSocket, info.Mode().Type(),
		"stale file should have been replaced by a real socket")

	require.NotNil(t, l)
}

func TestListenSocketPermissions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "perm.sock")

	h := &handler{}

	l, cleanup, err := h.listenSocket(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	t.Cleanup(func() { _ = l.Close() }) //nolint:errcheck // best-effort test cleanup

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "socket file must be 0600")

	parent, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), parent.Mode().Perm(), "parent dir must be 0700")
}

func TestListenSocketCleanupRemoves(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "cleanup.sock")

	h := &handler{}

	_, cleanup, err := h.listenSocket(t.Context(), path)
	require.NoError(t, err)

	cleanup()

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "cleanup must remove the socket file")
}

// TestServeSocketBindOnExistingPath asserts that listenSocket
// returns ErrSocketInUse when a live peer already holds the path.
// We deliberately do not silently steal a live socket; only
// stale-and-removable files get unlinked. Guarding against
// regressions that would silently break a peer process.
func TestServeSocketBindOnExistingPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "live.sock")

	live, err := net.Listen("unix", path) //nolint:noctx // synchronous test fixture
	require.NoError(t, err)
	t.Cleanup(func() { _ = live.Close() }) //nolint:errcheck // best-effort test cleanup

	go func() {
		for {
			conn, err := live.Accept()
			if err != nil {
				return
			}

			_ = conn.Close() //nolint:errcheck // best-effort test cleanup
		}
	}()

	h := &handler{}

	_, _, err = h.listenSocket(t.Context(), path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSocketInUse)
}

// TestServeSocketReturnsTokenJSON spins up a fake runHost that
// returns canned ExecCredential bytes, populates currentSA,
// dials the socket, and asserts the read bytes match byte-for-byte.
func TestServeSocketReturnsTokenJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "ok.sock")

	want := mustEncodeCred(t, ExecCredential{
		APIVersion: execAuthAPIVersion,
		Kind:       "ExecCredential",
		Status: ExecCredentialStatus{
			ExpirationTimestamp: time.Date(2026, 5, 1, 13, 0, 0, 0, time.UTC).Format(time.RFC3339),
			Token:               "tok-from-fake",
		},
	})

	h := newServeSocketHandler(t, want, nil)
	h.currentSA.Store(&currentSA{
		Kubeconfig: "/admin/kube",
		Context:    "prod",
		SAName:     "claude-sa-1",
		Namespace:  "ns",
		Expiration: 3600,
	})

	l, cleanup, err := h.listenSocket(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go h.serveSocket(ctx, l, &h.socketWG)

	got := dialAndRead(t, path)
	assert.Equal(t, want, got)
}

// TestServeSocketBeforeSelect dials the socket before any select
// has populated currentSA. The handler must close cleanly with no
// bytes written, so the shim's n==0 check turns into a deterministic
// non-zero exit.
func TestServeSocketBeforeSelect(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.sock")

	h := newServeSocketHandler(t, nil, nil)

	l, cleanup, err := h.listenSocket(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go h.serveSocket(ctx, l, &h.socketWG)

	got := dialAndRead(t, path)
	assert.Empty(t, got, "no SA selected -> empty response")
}

// TestServeSocketConcurrent fires 50 concurrent dials and asserts
// every reply matches the canned credential bytes. Pins the
// per-conn handler isolation under load.
func TestServeSocketConcurrent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.sock")

	want := []byte("tok-bytes-concurrent\n")

	h := newServeSocketHandler(t, want, nil)
	h.currentSA.Store(&currentSA{
		Kubeconfig: "/admin/kube",
		Context:    "prod",
		SAName:     "sa",
		Namespace:  "ns",
		Expiration: 3600,
	})

	l, cleanup, err := h.listenSocket(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go h.serveSocket(ctx, l, &h.socketWG)

	const n = 50

	var wg sync.WaitGroup

	results := make(chan []byte, n)

	for range n {
		wg.Go(func() {
			results <- dialAndRead(t, path)
		})
	}

	wg.Wait()
	close(results)

	for got := range results {
		assert.Equal(t, want, got)
	}
}

// TestServeSocketRotationDuringRequest swaps currentSA between
// dials and asserts that each response is internally coherent: it
// always reflects exactly one snapshot, never a torn mix. Drives
// 20 dials with the swap interleaved.
func TestServeSocketRotationDuringRequest(t *testing.T) {
	t.Parallel()

	// shortTempDir keeps the path under macOS's 104-byte sun_path
	// limit; t.TempDir() embeds the long test name and combined
	// with `rotate.sock` overflows on the Nix builder.
	dir := shortTempDir(t)
	path := filepath.Join(dir, "rotate.sock")

	saA := &currentSA{
		Kubeconfig: "/admin/kube",
		Context:    "prod",
		SAName:     "sa-A",
		Namespace:  "ns",
		Expiration: 3600,
	}
	saB := &currentSA{
		Kubeconfig: "/admin/kube",
		Context:    "prod",
		SAName:     "sa-B",
		Namespace:  "ns",
		Expiration: 3600,
	}

	h := &handler{}

	h.runHost = func(_ context.Context, sub string, args []string) ([]byte, error) {
		require.Equal(t, "token", sub)

		// Echo the SA name back so the test can map response to
		// the snapshot the handler observed.
		for i, a := range args {
			if a == "--sa" {
				return []byte(args[i+1]), nil
			}
		}

		return nil, nil
	}

	h.currentSA.Store(saA)

	l, cleanup, err := h.listenSocket(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go h.serveSocket(ctx, l, &h.socketWG)

	const n = 20

	var (
		wg      sync.WaitGroup
		swapped atomic.Bool
		results = make(chan string, n)
	)

	for i := range n {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			if i == n/2 && swapped.CompareAndSwap(false, true) {
				h.currentSA.Store(saB)
			}

			results <- string(dialAndRead(t, path))
		}(i)
	}

	wg.Wait()
	close(results)

	for got := range results {
		assert.True(t, got == "sa-A" || got == "sa-B",
			"each response must reflect one snapshot, got %q", got)
	}
}

// TestServeSocketHandlerWaitsForCleanup pins that socketShutdown
// blocks until in-flight handlers complete, so later steps in
// sessionDir do not unlink files while a handler is mid-mint.
func TestServeSocketHandlerWaitsForCleanup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "wait.sock")

	release := make(chan struct{})
	entered := make(chan struct{})

	h := &handler{}
	h.runHost = func(_ context.Context, _ string, _ []string) ([]byte, error) {
		close(entered)
		<-release

		return []byte("ok"), nil
	}
	h.currentSA.Store(&currentSA{
		Kubeconfig: "/admin/kube",
		Context:    "prod",
		SAName:     "sa",
		Namespace:  "ns",
		Expiration: 3600,
	})

	l, _, err := h.listenSocket(t.Context(), path)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	h.mu.Lock()
	h.socketListener = l
	h.mu.Unlock()

	go h.serveSocket(ctx, l, &h.socketWG)

	connDone := make(chan struct{})

	go func() {
		_ = dialAndRead(t, path)

		close(connDone)
	}()

	<-entered

	cancel()

	shutdownDone := make(chan struct{})

	go func() {
		h.socketShutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		t.Fatal("socketShutdown returned before in-flight handler completed")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("socketShutdown did not complete after handler released")
	}

	<-connDone

	_ = os.Remove(path) //nolint:errcheck // best-effort test cleanup
}

// newServeSocketHandler builds a handler with a fake runHost that
// returns canned bytes (or err) for the "token" subcommand.
func newServeSocketHandler(t *testing.T, tokenStdout []byte, tokenErr error) *handler {
	t.Helper()

	h := &handler{}
	h.runHost = func(_ context.Context, sub string, _ []string) ([]byte, error) {
		require.Equal(t, "token", sub)
		return tokenStdout, tokenErr
	}

	return h
}

// dialAndRead opens a UDS conn, reads to EOF, returns the bytes.
// Uses assert (not require) so callers from goroutines do not trip
// the testifylint go-require check; failures still mark the test
// failed via [testing.T.Errorf].
func dialAndRead(t *testing.T, path string) []byte {
	t.Helper()

	conn, err := net.DialTimeout("unix", path, 2*time.Second) //nolint:noctx // synchronous test fixture
	// assert (not require) so callers from goroutines stay legal.
	if !assert.NoError(t, err) { //nolint:testifylint // see comment above
		return nil
	}

	defer conn.Close() //nolint:errcheck // best-effort test cleanup

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck // best-effort test deadline

	buf := make([]byte, 0, 256)
	tmp := make([]byte, 256)

	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}

		if err != nil {
			break
		}
	}

	return buf
}

// mustEncodeCred encodes c the same way runHostToken does so the
// test bytes match production format byte-for-byte.
func mustEncodeCred(t *testing.T, c ExecCredential) []byte {
	t.Helper()

	data, err := json.Marshal(&c)
	require.NoError(t, err)

	return append(data, '\n')
}
