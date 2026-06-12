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

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/execplugin"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/socket"
)

// TestServeSocketReturnsTokenJSON spins up a fake runHost that
// returns canned credential bytes, populates currentSA, dials the
// socket, and asserts the read bytes match byte-for-byte.
func TestServeSocketReturnsTokenJSON(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "ok.sock")

	want := mustEncodeCred(t, execplugin.Credential{
		APIVersion: execplugin.APIVersion,
		Kind:       "ExecCredential",
		Status: execplugin.CredentialStatus{
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

	l, cleanup, err := socket.Listen(t.Context(), path, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go socket.Serve(ctx, l, &h.socketWG, h.handleSocketConn)

	got := dialAndRead(t, path)
	assert.Equal(t, want, got)
}

// TestServeSocketBeforeSelect dials the socket before any select
// has populated currentSA. The handler must close cleanly with no
// bytes written, so the shim's n==0 check turns into a deterministic
// non-zero exit.
func TestServeSocketBeforeSelect(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "empty.sock")

	h := newServeSocketHandler(t, nil, nil)

	l, cleanup, err := socket.Listen(t.Context(), path, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go socket.Serve(ctx, l, &h.socketWG, h.handleSocketConn)

	got := dialAndRead(t, path)
	assert.Empty(t, got, "no SA selected -> empty response")
}

// TestServeSocketConcurrent fires 50 concurrent dials and asserts
// every reply matches the canned credential bytes. Pins the
// per-conn handler isolation under load.
func TestServeSocketConcurrent(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
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

	l, cleanup, err := socket.Listen(t.Context(), path, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go socket.Serve(ctx, l, &h.socketWG, h.handleSocketConn)

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

	l, cleanup, err := socket.Listen(t.Context(), path, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go socket.Serve(ctx, l, &h.socketWG, h.handleSocketConn)

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

	dir := shortTempDir(t)
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

	l, _, err := socket.Listen(t.Context(), path, "")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	h.mu.Lock()
	h.socketListener = l
	h.mu.Unlock()

	go socket.Serve(ctx, l, &h.socketWG, h.handleSocketConn)

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
func mustEncodeCred(t *testing.T, c execplugin.Credential) []byte {
	t.Helper()

	data, err := json.Marshal(&c)
	require.NoError(t, err)

	return append(data, '\n')
}
