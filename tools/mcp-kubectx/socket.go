package main

import (
	"bytes"
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

// maxSunPathLen is the conservative cross-platform bound on a Unix
// domain socket path: sun_path holds 104 bytes on Darwin and 108 on
// Linux, minus a trailing NUL. Checked explicitly in
// [*handler.listenSocket] because bind(2) otherwise surfaces a bare
// EINVAL that names neither the path nor the reason.
const maxSunPathLen = 103

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

// sidecarPath returns the path of the per-slot sidecar file that
// records the instance id of the serve currently bound to the
// socket at socketPath. Co-located with the socket inode so the
// pair lives and dies together. An empty socketPath returns empty
// so callers can pass conditionally-set paths without an outer
// guard — otherwise `sidecarPath("")` would yield `".id"` and land
// in CWD, which would surprise [bestEffortRemove].
func sidecarPath(socketPath string) string {
	if socketPath == "" {
		return ""
	}

	return socketPath + ".id"
}

// acquireServeSocket walks slots 0..maxSlots-1 and binds the first
// free one. The returned listener and cleanup are owned by the
// caller (typically [main.runServe]); the socket inode is unlinked
// when cleanup runs.
//
// instanceID is written atomically into a per-slot sidecar file
// alongside the socket inode (see [sidecarPath]) so the next
// `serve`'s [*handler.discoverLiveInstances] can attribute a live
// slot back to its owning instance. An empty instanceID skips the
// sidecar write — used only by tests and by the standalone
// `host select` CLI path that does not need attribution.
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
	instanceID string,
) (string, net.Listener, func(), error) {
	for slot := range maxSlots {
		path := socketPathForSlot(slot, forGuest)

		listener, cleanup, err := h.listenSocket(ctx, path, instanceID)
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
//
// instanceID, when non-empty, is atomically written to the
// per-slot sidecar at [sidecarPath] after the socket inode is
// chmodded. The sidecar is the means by which a future serve's
// [*handler.discoverLiveInstances] attributes a live slot back to
// its bound serve, so the write must happen before this function
// returns to the caller. On any failure between [net.ListenConfig.Listen]
// and a successful return, the socket inode AND the sidecar (and
// the sidecar tmp file, if present) are unlinked best-effort.
func (h *handler) listenSocket(
	ctx context.Context,
	path, instanceID string,
) (net.Listener, func(), error) {
	if len(path) > maxSunPathLen {
		return nil, nil, fmt.Errorf(
			"%w: path %q exceeds the %d-byte unix socket path limit; set XDG_STATE_HOME to a shorter path",
			ErrSocketBind, path, maxSunPathLen,
		)
	}

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
		_ = l.Close()                    //nolint:errcheck // best-effort cleanup; chmodErr is what we surface
		_ = os.Remove(path)              //nolint:errcheck // best-effort cleanup; chmodErr is what we surface
		_ = os.Remove(sidecarPath(path)) //nolint:errcheck // best-effort cleanup of any prior sidecar

		return nil, nil, fmt.Errorf("%w: chmod: %w", ErrSocketBind, chmodErr)
	}

	if instanceID != "" {
		sidecarErr := writeSidecar(path, instanceID)
		if sidecarErr != nil {
			_ = l.Close()       //nolint:errcheck // best-effort cleanup; sidecarErr is what we surface
			_ = os.Remove(path) //nolint:errcheck // best-effort cleanup; sidecarErr is what we surface

			return nil, nil, fmt.Errorf("%w: sidecar: %w", ErrSocketBind, sidecarErr)
		}
	}

	cleanup := func() {
		_ = l.Close()                    //nolint:errcheck // best-effort cleanup
		_ = os.Remove(path)              //nolint:errcheck // best-effort cleanup
		_ = os.Remove(sidecarPath(path)) //nolint:errcheck // best-effort cleanup
	}

	return l, cleanup, nil
}

// writeSidecar atomically records the bound serve's instance id in
// the per-slot sidecar at [sidecarPath]. The atomicity matters:
// [*handler.discoverLiveInstances] reads the file with no
// coordination, so a torn write (zero bytes or partial bytes)
// would be misclassified as "stale" and the slot's serve dropped
// from the live set.
func writeSidecar(socketPath, instanceID string) error {
	return writeFileAtomic(sidecarPath(socketPath), []byte(instanceID), 0o600)
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
//
// In both the no-inode and dead-inode branches the per-slot
// sidecar at [sidecarPath] is also unlinked best-effort. This
// prevents a SIGKILLed serve's sidecar from outliving its socket
// and being misread by a future [*handler.discoverLiveInstances].
func clearStaleSocket(ctx context.Context, path string) error {
	_, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.Remove(sidecarPath(path)) //nolint:errcheck // best-effort; orphan sidecar from prior crash
			return nil
		}

		return fmt.Errorf("%w: stat: %w", ErrSocketCleanupStale, err)
	}

	if dialProbe(ctx, path) {
		return fmt.Errorf("%w: %s", ErrSocketInUse, path)
	}

	rmErr := os.Remove(path)
	if rmErr != nil && !os.IsNotExist(rmErr) {
		return fmt.Errorf("%w: remove: %w", ErrSocketCleanupStale, rmErr)
	}

	_ = os.Remove(sidecarPath(path)) //nolint:errcheck // best-effort; pair the socket inode with its sidecar

	return nil
}

// startServeSocket launches [*handler.serveSocket] on its own
// goroutine with the accept loop itself registered on wg. Holding a
// wg token for the loop's entire lifetime makes the per-connection
// wg.Add inside serveSocket safe: the counter cannot reach zero
// (so a concurrent [*handler.socketShutdown] Wait cannot return)
// until the loop has exited, after which no further Add can occur.
// Without the loop token, an Accept racing the shutdown's Close
// could Add from a zero counter concurrently with Wait — the one
// ordering [sync.WaitGroup] forbids — letting the shutdown unlink
// session files under an in-flight token mint.
func (h *handler) startServeSocket(ctx context.Context, l net.Listener, wg *sync.WaitGroup) {
	wg.Go(func() {
		h.serveSocket(ctx, l, wg)
	})
}

// serveSocket runs the accept loop for the per-`serve` Unix domain
// socket. It returns when ctx is canceled (the goroutine that
// watches ctx closes the listener, which surfaces as [net.ErrClosed]
// in Accept). Each accepted connection is handled in its own
// goroutine tracked by wg so the shutdown path can drain in-flight
// handlers before the higher-level cleanup unlinks the socket file
// and the kubeconfig. Callers that drain wg concurrently must enter
// through [*handler.startServeSocket] so the loop holds its own wg
// token before the first Accept.
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
// When no SA is selected (currentSA is nil) or the token mint
// fails, the handler logs a structured warning and closes the
// connection without writing any bytes; the shim's zero-byte check
// turns either case into a deterministic non-zero exit kubectl
// surfaces clearly. The wire protocol cannot distinguish the two,
// so [ErrEmptyCredential]'s text names both and points at the
// serve logs.
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

// discoverLiveInstances walks slots 0..maxSlots-1 and returns the
// set of instance ids whose UDS socket dial-tests live and whose
// per-slot sidecar contains a readable non-empty id. The serve's
// own slot is included automatically because [*handler.acquireServeSocket]
// wrote its sidecar before returning, and on Linux the [net.ListenConfig.Listen]
// call puts the socket into the LISTEN state with a SOMAXCONN
// backlog immediately; the accept loop does not need to be running
// for clients to dial successfully.
//
// Probes run concurrently because each slot can hit the full
// [serveSocketDialDeadline] when the socket inode points at a
// stuck listener; serial probing of a 16-slot pool would block
// startup for up to 8 seconds.
//
// Skip rules:
//   - dial succeeds + sidecar present, non-empty → add id to set.
//   - dial succeeds + sidecar missing → skip. The socket owner is
//     either mid-startup or running an older binary; preserve
//     conservatively rather than misclassifying.
//   - dial succeeds + sidecar zero bytes → skip. Defensive against
//     a torn write (mostly historical; the atomic write at
//     [writeSidecar] precludes this for current binaries).
//   - dial fails → not live; [clearStaleSocket] on the next slot
//     acquisition will unlink the sidecar best-effort.
//
// Dependency note: this function relies on Linux's UDS semantics
// that a freshly bound listener accepts dials immediately. If a
// future refactor either delays sidecar write until after
// [*handler.serveSocket] starts or starts serveSocket before the
// sidecar exists, the own-slot dial behavior here could change.
func (h *handler) discoverLiveInstances(ctx context.Context, maxSlots int) map[string]struct{} {
	results := make(chan string, maxSlots)

	var wg sync.WaitGroup

	forGuest := h.isGuest()

	for slot := range maxSlots {
		wg.Go(func() {
			path := socketPathForSlot(slot, forGuest)

			if !dialProbe(ctx, path) {
				return
			}

			id := readSidecar(path)
			if id == "" {
				return
			}

			results <- id
		})
	}

	wg.Wait()
	close(results)

	live := make(map[string]struct{})
	for id := range results {
		live[id] = struct{}{}
	}

	return live
}

// dialProbe runs the same short-deadline dial used by
// [clearStaleSocket] and reports whether a live peer is on the
// other end. Probe-only: connection is closed immediately.
func dialProbe(ctx context.Context, path string) bool {
	dialer := net.Dialer{Timeout: serveSocketDialDeadline}

	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return false
	}

	_ = conn.Close() //nolint:errcheck // probe-only conn

	return true
}

// readSidecar returns the trimmed contents of the per-slot sidecar
// at [sidecarPath]. Returns the empty string on any error or when
// the file is empty so [*handler.discoverLiveInstances] uses a
// single skip predicate.
func readSidecar(socketPath string) string {
	data, err := os.ReadFile(sidecarPath(socketPath))
	if err != nil {
		return ""
	}

	return string(bytes.TrimSpace(data))
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
