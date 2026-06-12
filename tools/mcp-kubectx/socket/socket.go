package socket

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statedir"
	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statefile"
)

// Sentinel errors for the per-`serve` Unix domain socket. See
// [Listen] for the create-time invariants.
var (
	ErrInUse        = errors.New("serve socket is already bound by a live peer")
	ErrBind         = errors.New("bind serve socket")
	ErrCleanupStale = errors.New("clean stale serve socket")
	ErrAllSlotsBusy = errors.New("all serve socket slots are bound by live peers")
)

// ConnDeadline is the server-side per-connection deadline. It is
// deliberately shorter than the shim's read deadline
// (execplugin.ReadDeadline) so the server closes first cleanly.
const ConnDeadline = 25 * time.Second

// dialDeadline bounds the probe dials used by stale-slot recovery
// and liveness discovery.
const dialDeadline = 500 * time.Millisecond

// maxSunPathLen is the conservative cross-platform bound on a Unix
// domain socket path: sun_path holds 104 bytes on Darwin and 108 on
// Linux, minus a trailing NUL. Checked explicitly in [Listen]
// because bind(2) otherwise surfaces a bare EINVAL that names
// neither the path nor the reason.
const maxSunPathLen = 103

// StateDir returns the parent directory used for per-`serve` Unix
// domain socket files. It mirrors [statedir.Dir] but is rooted at a
// sibling directory name so the socket file does not collide with
// the existing Lima writable bind mount of <state>/mcp-kubectx
// (UDS-over-Lima-bind-mount semantics on macOS-host are unverified;
// the safe design avoids the question entirely by hosting the
// socket outside the bind mount).
func StateDir() string {
	return statedir.Subdir("mcp-kubectx-run")
}

// PathForSlot returns the absolute path of the per-`serve` Unix
// domain socket for a given slot index. Slot indices are dense
// (0..N-1); each slot maps 1:1 to a literal entry in the Claude Code
// sandbox's `allowUnixSockets` allowlist, which is matched as exact
// strings rather than as glob patterns. Per-env discriminators
// (`host` vs `guest`) keep host- and guest-side serves on the same
// machine from stomping on each other's sockets.
func PathForSlot(slot int, forGuest bool) string {
	return filepath.Join(
		StateDir(),
		fmt.Sprintf("serve.%d.%s.sock", slot, statedir.EnvTag(forGuest)),
	)
}

// SidecarPath returns the path of the per-slot sidecar file that
// records the instance id of the serve currently bound to the
// socket at socketPath. Co-located with the socket inode so the
// pair lives and dies together. An empty socketPath returns empty
// so callers can pass conditionally-set paths without an outer
// guard -- otherwise `SidecarPath("")` would yield `".id"` and land
// in CWD.
func SidecarPath(socketPath string) string {
	if socketPath == "" {
		return ""
	}

	return socketPath + ".id"
}

// Acquire walks slots 0..maxSlots-1 and binds the first free one.
// The returned listener and cleanup are owned by the caller; the
// socket inode is unlinked when cleanup runs.
//
// instanceID is written atomically into a per-slot sidecar file
// alongside the socket inode (see [SidecarPath]) so a future serve's
// [DiscoverLive] can attribute the live slot back to its owning
// instance. An empty instanceID skips the sidecar write -- used
// only by tests and by callers that do not need attribution.
//
// Two errors are treated as "this slot is taken, try the next":
// [ErrInUse] (a live peer was detected by the dial probe) and a
// wrapped [syscall.EADDRINUSE] from the bind (a peer bound between
// the probe and the bind). Every other error from [Listen]
// propagates immediately. Exhaustion returns [ErrAllSlotsBusy] with
// the slot count and state directory embedded in the message so
// operators can grow the pool.
func Acquire(
	ctx context.Context,
	forGuest bool,
	maxSlots int,
	instanceID string,
) (string, net.Listener, func(), error) {
	for slot := range maxSlots {
		path := PathForSlot(slot, forGuest)

		listener, cleanup, err := Listen(ctx, path, instanceID)
		if err == nil {
			return path, listener, cleanup, nil
		}

		if errors.Is(err, ErrInUse) || errors.Is(err, syscall.EADDRINUSE) {
			continue
		}

		return "", nil, nil, err
	}

	return "", nil, nil, fmt.Errorf("%w: %d slots in %s",
		ErrAllSlotsBusy, maxSlots, StateDir())
}

// Listen binds a Unix domain socket at path, returning the listener
// and a cleanup closure that closes it and unlinks the file. The
// parent dir is created with mode 0700 and the socket file is left
// at mode 0600 (single-user trust boundary).
//
// Existing-path handling intentionally does not silently steal a
// peer's live socket. If the path exists, Listen dial-tests it: a
// successful connection means a live peer holds it and returns
// [ErrInUse]; ECONNREFUSED (or a wrapped equivalent) means the file
// is stale and is unlinked before the bind. ENOENT just falls
// through to the bind.
//
// instanceID, when non-empty, is atomically written to the per-slot
// sidecar at [SidecarPath] after the socket inode is chmodded. The
// sidecar is the means by which a future serve's [DiscoverLive]
// attributes a live slot back to its bound serve, so the write must
// happen before this function returns to the caller. On any failure
// between the bind and a successful return, the socket inode AND the
// sidecar (and the sidecar tmp file, if present) are unlinked
// best-effort.
func Listen(
	ctx context.Context,
	path, instanceID string,
) (net.Listener, func(), error) {
	if len(path) > maxSunPathLen {
		return nil, nil, fmt.Errorf(
			"%w: path %q exceeds the %d-byte unix socket path limit; set XDG_STATE_HOME to a shorter path",
			ErrBind, path, maxSunPathLen,
		)
	}

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: create directory: %w", ErrBind, err)
	}

	err = clearStale(ctx, path)
	if err != nil {
		return nil, nil, err
	}

	var lc net.ListenConfig

	l, err := lc.Listen(ctx, "unix", path)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrBind, err)
	}

	// Pin the socket inode mode to 0600 explicitly. Avoids touching
	// the process-wide umask (which is not goroutine-local and would
	// race with concurrent fs work in any future refactor).
	chmodErr := os.Chmod(path, 0o600)
	if chmodErr != nil {
		_ = l.Close()                    //nolint:errcheck // best-effort cleanup; chmodErr is what we surface
		_ = os.Remove(path)              //nolint:errcheck // best-effort cleanup; chmodErr is what we surface
		_ = os.Remove(SidecarPath(path)) //nolint:errcheck // best-effort cleanup of any prior sidecar

		return nil, nil, fmt.Errorf("%w: chmod: %w", ErrBind, chmodErr)
	}

	if instanceID != "" {
		sidecarErr := writeSidecar(path, instanceID)
		if sidecarErr != nil {
			_ = l.Close()       //nolint:errcheck // best-effort cleanup; sidecarErr is what we surface
			_ = os.Remove(path) //nolint:errcheck // best-effort cleanup; sidecarErr is what we surface

			return nil, nil, fmt.Errorf("%w: sidecar: %w", ErrBind, sidecarErr)
		}
	}

	cleanup := func() {
		_ = l.Close()                    //nolint:errcheck // best-effort cleanup
		_ = os.Remove(path)              //nolint:errcheck // best-effort cleanup
		_ = os.Remove(SidecarPath(path)) //nolint:errcheck // best-effort cleanup
	}

	return l, cleanup, nil
}

// writeSidecar atomically records the bound serve's instance id in
// the per-slot sidecar at [SidecarPath]. The atomicity matters:
// [DiscoverLive] reads the file with no coordination, so a torn
// write (zero bytes or partial bytes) would be misclassified as
// "stale" and the slot's serve dropped from the live set.
func writeSidecar(socketPath, instanceID string) error {
	//nolint:wrapcheck // WriteAtomic errors are self-describing
	return statefile.WriteAtomic(SidecarPath(socketPath), []byte(instanceID), 0o600)
}

// clearStale implements the existing-path branch of [Listen]: probe
// with a short-deadline dial; a successful dial means a live peer
// owns the path and we abort with [ErrInUse]. Any dial error is
// treated as "leftover state" and the path is unlinked. The two
// errnos that show up in practice are ECONNREFUSED (a stale socket
// inode left behind by a SIGKILLed serve) and ENOTSOCK (a regular
// file at the path), but the same recovery applies to anything else
// short of a successful connect: the only state we care about
// preserving is "live peer holding the inode", which Dial detects
// positively. ENOENT just falls through.
//
// In both the no-inode and dead-inode branches the per-slot sidecar
// at [SidecarPath] is also unlinked best-effort. This prevents a
// SIGKILLed serve's sidecar from outliving its socket and being
// misread by a future [DiscoverLive].
func clearStale(ctx context.Context, path string) error {
	_, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.Remove(SidecarPath(path)) //nolint:errcheck // best-effort; orphan sidecar from prior crash
			return nil
		}

		return fmt.Errorf("%w: stat: %w", ErrCleanupStale, err)
	}

	if dialProbe(ctx, path) {
		return fmt.Errorf("%w: %s", ErrInUse, path)
	}

	rmErr := os.Remove(path)
	if rmErr != nil && !os.IsNotExist(rmErr) {
		return fmt.Errorf("%w: remove: %w", ErrCleanupStale, rmErr)
	}

	_ = os.Remove(SidecarPath(path)) //nolint:errcheck // best-effort; pair the socket inode with its sidecar

	return nil
}

// DiscoverLive walks slots 0..maxSlots-1 and returns the set of
// instance ids whose UDS socket dial-tests live and whose per-slot
// sidecar contains a readable non-empty id. The calling serve's own
// slot is included automatically because [Acquire] wrote its sidecar
// before returning, and on Linux the [net.ListenConfig.Listen] call
// puts the socket into the LISTEN state with a SOMAXCONN backlog
// immediately; the accept loop does not need to be running for
// clients to dial successfully.
//
// Probes run concurrently because each slot can hit the full dial
// deadline when the socket inode points at a stuck listener; serial
// probing of a 16-slot pool would block startup for up to 8 seconds.
//
// Skip rules:
//   - dial succeeds + sidecar present, non-empty: add id to set.
//   - dial succeeds + sidecar missing: skip. The socket owner is
//     either mid-startup or running an older binary; preserve
//     conservatively rather than misclassifying.
//   - dial succeeds + sidecar zero bytes: skip. Defensive against a
//     torn write (mostly historical; the atomic sidecar write
//     precludes this for current binaries).
//   - dial fails: not live; the next [Acquire] on that slot unlinks
//     the sidecar best-effort.
//
// Dependency note: this function relies on Linux's UDS semantics
// that a freshly bound listener accepts dials immediately. If a
// future refactor either delays the sidecar write until after the
// accept loop starts or starts the loop before the sidecar exists,
// the own-slot dial behavior here could change.
func DiscoverLive(ctx context.Context, maxSlots int, forGuest bool) map[string]struct{} {
	results := make(chan string, maxSlots)

	var wg sync.WaitGroup

	for slot := range maxSlots {
		wg.Go(func() {
			path := PathForSlot(slot, forGuest)

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

// dialProbe runs the same short-deadline dial used by [clearStale]
// and reports whether a live peer is on the other end. Probe-only:
// the connection is closed immediately.
func dialProbe(ctx context.Context, path string) bool {
	dialer := net.Dialer{Timeout: dialDeadline}

	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return false
	}

	_ = conn.Close() //nolint:errcheck // probe-only conn

	return true
}

// readSidecar returns the trimmed contents of the per-slot sidecar
// at [SidecarPath]. Returns the empty string on any error or when
// the file is empty so [DiscoverLive] uses a single skip predicate.
func readSidecar(socketPath string) string {
	data, err := os.ReadFile(SidecarPath(socketPath))
	if err != nil {
		return ""
	}

	return string(bytes.TrimSpace(data))
}
