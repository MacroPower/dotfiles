package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Sentinel errors for the per-`serve` Unix domain socket that
// brokers exec-credential mints to the in-binary `exec-plugin`
// shim. See [*handler.listenSocket] for the create-time invariants.
var (
	ErrSocketInUse        = errors.New("serve socket is already bound by a live peer")
	ErrSocketBind         = errors.New("bind serve socket")
	ErrSocketCleanupStale = errors.New("clean stale serve socket")
	ErrAllSlotsBusy       = errors.New("all serve socket slots are bound by live peers")
)

// Per-connection deadlines. Server-side is shorter than the
// client-side read deadline so the server closes first cleanly.
const (
	serveSocketConnDeadline   = 25 * time.Second
	serveSocketDialDeadline   = 500 * time.Millisecond
	execPluginConnectDeadline = 5 * time.Second
	execPluginReadDeadline    = 30 * time.Second
)

// currentSA holds the descriptor of the ServiceAccount currently
// owned by `serve`. It is loaded atomically by the socket handler
// goroutine when a kubectl exec-plugin connection arrives, and
// stored atomically by [*handler.selectCtx] after a successful
// `host select`. Nil means no SA is selected yet (or the previous
// select failed before storing).
type currentSA struct {
	Kubeconfig string
	Context    string
	SAName     string
	Namespace  string
	Expiration int
}

// socketStateDir returns the parent directory used for per-`serve`
// Unix domain socket files. It mirrors [stateHomeDir] but is rooted
// at a sibling directory name so the socket file does not collide
// with the existing Lima writable bind mount of <state>/mcp-kubectx
// (UDS-over-Lima-bind-mount semantics on macOS-host are unverified;
// the safe design avoids the question entirely by hosting the socket
// outside the bind mount).
func socketStateDir() string {
	return xdgStateSubdir("mcp-kubectx-run")
}

// socketPathForSlot returns the absolute path of the per-`serve`
// Unix domain socket for a given slot index. Slot indices are dense
// (0..N-1); each slot maps 1:1 to a literal entry in the Claude Code
// sandbox's `allowUnixSockets` allowlist, which is matched as exact
// strings rather than as glob patterns. Per-env discriminators
// (`host` vs `guest`) keep host- and guest-side serves on the same
// machine from stomping on each other's sockets.
func socketPathForSlot(slot int, forGuest bool) string {
	return filepath.Join(
		socketStateDir(),
		fmt.Sprintf("serve.%d.%s.sock", slot, envTag(forGuest)),
	)
}

// acquireServeSocket walks slots 0..maxSlots-1 and binds the first
// free one. The returned listener and cleanup are owned by the
// caller (typically [main.runServe]); the socket inode is unlinked
// when cleanup runs.
//
// Two errors are treated as "this slot is taken, try the next":
// [ErrSocketInUse] (a live peer was detected by [clearStaleSocket]'s
// dial probe) and a wrapped [syscall.EADDRINUSE] from `lc.Listen`
// (a peer bound between our probe and our bind). Every other error
// from [*handler.listenSocket] propagates immediately. Exhaustion
// returns [ErrAllSlotsBusy] with the slot count and state directory
// embedded in the message so operators can grow the pool.
func (h *handler) acquireServeSocket(
	ctx context.Context,
	forGuest bool,
	maxSlots int,
) (string, net.Listener, func(), error) {
	for slot := range maxSlots {
		path := socketPathForSlot(slot, forGuest)

		listener, cleanup, err := h.listenSocket(ctx, path)
		if err == nil {
			return path, listener, cleanup, nil
		}

		if errors.Is(err, ErrSocketInUse) || errors.Is(err, syscall.EADDRINUSE) {
			continue
		}

		return "", nil, nil, err
	}

	return "", nil, nil, fmt.Errorf("%w: %d slots in %s",
		ErrAllSlotsBusy, maxSlots, socketStateDir())
}

// listenSocket binds a Unix domain socket at path, returning the
// listener and a cleanup closure that closes it and unlinks the
// file. The parent dir is created with mode 0700 and the socket
// file is left at mode 0600 (single-user trust boundary).
//
// Existing-path handling intentionally does not silently steal a
// peer's live socket. If the path exists, listenSocket dial-tests
// it: a successful connection means a live peer holds it and
// returns [ErrSocketInUse]; ECONNREFUSED (or a wrapped equivalent)
// means the file is stale and is unlinked before the bind. ENOENT
// just falls through to the bind.
func (h *handler) listenSocket(ctx context.Context, path string) (net.Listener, func(), error) {
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: create directory: %w", ErrSocketBind, err)
	}

	err = clearStaleSocket(ctx, path)
	if err != nil {
		return nil, nil, err
	}

	var lc net.ListenConfig

	l, err := lc.Listen(ctx, "unix", path)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrSocketBind, err)
	}

	// Pin the socket inode mode to 0600 explicitly. Avoids touching
	// the process-wide umask (which is not goroutine-local and would
	// race with concurrent fs work in any future refactor).
	chmodErr := os.Chmod(path, 0o600)
	if chmodErr != nil {
		_ = l.Close()       //nolint:errcheck // best-effort cleanup; chmodErr is what we surface
		_ = os.Remove(path) //nolint:errcheck // best-effort cleanup; chmodErr is what we surface

		return nil, nil, fmt.Errorf("%w: chmod: %w", ErrSocketBind, chmodErr)
	}

	cleanup := func() {
		_ = l.Close()       //nolint:errcheck // best-effort cleanup
		_ = os.Remove(path) //nolint:errcheck // best-effort cleanup
	}

	return l, cleanup, nil
}

// clearStaleSocket implements the existing-path branch of
// [listenSocket]: probe with a short-deadline dial; a successful
// dial means a live peer owns the path and we abort with
// [ErrSocketInUse]. Any dial error is treated as "leftover state"
// and the path is unlinked. The two errnos that show up in
// practice are ECONNREFUSED (a stale socket inode left behind by
// SIGKILLed serve) and ENOTSOCK (a regular file at the path),
// but the same recovery applies to anything else short of a
// successful connect: the only state we care about preserving is
// "live peer holding the inode", which Dial detects positively.
// ENOENT just falls through.
func clearStaleSocket(ctx context.Context, path string) error {
	_, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf("%w: stat: %w", ErrSocketCleanupStale, err)
	}

	dialer := net.Dialer{Timeout: serveSocketDialDeadline}

	conn, dialErr := dialer.DialContext(ctx, "unix", path)
	if dialErr == nil {
		_ = conn.Close() //nolint:errcheck // probe-only conn; we only care that it dialed
		return fmt.Errorf("%w: %s", ErrSocketInUse, path)
	}

	rmErr := os.Remove(path)
	if rmErr != nil && !os.IsNotExist(rmErr) {
		return fmt.Errorf("%w: remove: %w", ErrSocketCleanupStale, rmErr)
	}

	return nil
}

// serveSocket runs the accept loop for the per-`serve` Unix domain
// socket. It returns when ctx is canceled (the goroutine that
// watches ctx closes the listener, which surfaces as [net.ErrClosed]
// in Accept). Each accepted connection is handled in its own
// goroutine tracked by wg so the shutdown path can drain in-flight
// handlers before the higher-level cleanup unlinks the socket file
// and the kubeconfig.
func (h *handler) serveSocket(ctx context.Context, l net.Listener, wg *sync.WaitGroup) {
	go func() {
		<-ctx.Done()

		_ = l.Close() //nolint:errcheck // best-effort listener close on ctx cancel
	}()

	const acceptBackoff = 100 * time.Millisecond

	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}

			slog.WarnContext(ctx, "serve socket accept",
				slog.Any("error", err),
			)

			// Tiny backoff on persistent errors (e.g. EMFILE)
			// so the loop does not spin hot. Bail early if ctx
			// has been canceled in the meantime.
			select {
			case <-ctx.Done():
				return
			case <-time.After(acceptBackoff):
			}

			continue
		}

		wg.Add(1)

		go func(c net.Conn) {
			defer wg.Done()

			h.handleSocketConn(ctx, c)
		}(conn)
	}
}

// handleSocketConn writes the bytes of an [ExecCredential] JSON
// document for the currently-selected SA back to the kubectl shim.
// When no SA is selected (currentSA is nil) the handler logs a
// structured warning and closes the connection without writing any
// bytes; the shim's n==0 check turns this into a deterministic
// non-zero exit kubectl surfaces clearly.
func (h *handler) handleSocketConn(ctx context.Context, conn net.Conn) {
	defer conn.Close() //nolint:errcheck // already responded; close error has nowhere to go

	//nolint:errcheck // best-effort; kernel default applies on failure
	_ = conn.SetDeadline(time.Now().Add(serveSocketConnDeadline))

	saPtr := h.currentSA.Load()
	if saPtr == nil {
		slog.WarnContext(ctx, "exec-plugin request before select",
			slog.String("remote", "uds"),
		)

		return
	}

	sa := *saPtr

	args := []string{
		"--kubeconfig", sa.Kubeconfig,
		"--context", sa.Context,
		"--sa", sa.SAName,
		"--namespace", sa.Namespace,
		fmt.Sprintf("--sa-expiration=%d", sa.Expiration),
	}

	stdout, err := h.runHost(ctx, "token", args)
	if err != nil {
		slog.WarnContext(ctx, "exec-plugin token mint",
			slog.String("sa", sa.SAName),
			slog.String("namespace", sa.Namespace),
			slog.Any("error", err),
		)

		return
	}

	if len(stdout) == 0 {
		slog.WarnContext(ctx, "exec-plugin token mint returned empty stdout",
			slog.String("sa", sa.SAName),
			slog.String("namespace", sa.Namespace),
		)

		return
	}

	_, err = conn.Write(stdout)
	if err != nil {
		slog.WarnContext(ctx, "exec-plugin write",
			slog.String("sa", sa.SAName),
			slog.Any("error", err),
		)
	}
}

// socketShutdown closes the listener and blocks until every
// in-flight connection handler returns. It is called from
// [*handler.sessionDir]'s cleanup so that no handler is mid-token
// mint when the next steps unlink the kubeconfig and the socket
// file.
func (h *handler) socketShutdown() {
	h.mu.Lock()
	l := h.socketListener
	h.socketListener = nil
	h.mu.Unlock()

	if l != nil {
		_ = l.Close() //nolint:errcheck // idempotent; ctx-cancel goroutine may have already closed
	}

	h.socketWG.Wait()
}
