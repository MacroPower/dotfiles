package socket //nolint:testpackage // white-box tests cover unexported stale-socket internals

import (
	"net"
	"os"
	"path/filepath"
	"strings"
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

// bindLive binds a raw listener at path with a draining accept loop
// so dial probes classify the slot as held by a live peer.
func bindLive(t *testing.T, path string) {
	t.Helper()

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

func TestPathForSlot(t *testing.T) { //nolint:paralleltest // uses t.Setenv
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
			assert.Equal(t, tc.want, PathForSlot(tc.slot, tc.forGuest))
		})
	}
}

// TestListenRejectsOverlongPath pins the explicit sun_path guard: a
// socket path past the platform limit must surface a message naming
// the path and the limit, not the bare EINVAL bind(2) would
// otherwise produce.
func TestListenRejectsOverlongPath(t *testing.T) {
	t.Parallel()

	long := filepath.Join(shortTempDir(t), strings.Repeat("x", maxSunPathLen), "serve.sock")

	_, _, err := Listen(t.Context(), long, "")
	require.ErrorIs(t, err, ErrBind)
	assert.Contains(t, err.Error(), "unix socket path limit",
		"the guard must name the cause, not surface a bare EINVAL")
}

// TestAcquirePicksFirstFreeSlot pre-binds slots 0..k-1 with raw
// net.Listen calls so [Acquire]'s dial probe sees them as live, then
// asserts acquire returns slot k.
func TestAcquirePicksFirstFreeSlot(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	const k = 3

	for slot := range k {
		bindLive(t, PathForSlot(slot, false))
	}

	gotPath, listener, cleanup, err := Acquire(t.Context(), false, 8, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	assert.Equal(t, PathForSlot(k, false), gotPath)
	assert.NotNil(t, listener)
}

// TestAcquireSkipsStaleSlot drops a regular file at slot 0
// (simulating a SIGKILLed serve's leftover) and asserts acquire
// reuses slot 0 because the stale-clearance step unlinks the
// leftover before the bind.
func TestAcquireSkipsStaleSlot(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	staleSlot := PathForSlot(0, false)
	require.NoError(t, os.MkdirAll(filepath.Dir(staleSlot), 0o700))
	require.NoError(t, os.WriteFile(staleSlot, []byte("stale"), 0o600))

	gotPath, _, cleanup, err := Acquire(t.Context(), false, 8, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	assert.Equal(t, staleSlot, gotPath, "stale leftover at slot 0 must be reclaimed")

	info, err := os.Stat(gotPath)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSocket, info.Mode().Type(),
		"stale file should have been replaced by a real socket")
}

// TestAcquireDoesNotStealLivePeer pre-binds slot 0 with a live
// listener, calls Acquire, and verifies (a) the returned path is
// slot 1, and (b) the live peer's socket inode is still bound by the
// original listener after acquire returned. Pins the property that
// the slot loop never silently unlinks a live peer's socket.
func TestAcquireDoesNotStealLivePeer(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	livePath := PathForSlot(0, false)
	bindLive(t, livePath)

	gotPath, _, cleanup, err := Acquire(t.Context(), false, 8, "")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	assert.Equal(t, PathForSlot(1, false), gotPath,
		"live peer at slot 0 must push acquire to slot 1")

	// The live peer's path must still accept dials. If acquire had
	// stolen the inode, the next Dial would fail with ECONNREFUSED
	// (no listener on that path anymore).
	conn, dialErr := net.DialTimeout("unix", livePath, 2*time.Second) //nolint:noctx // synchronous test fixture
	require.NoError(t, dialErr, "live peer's socket must still be bound after acquire")

	_ = conn.Close() //nolint:errcheck // best-effort test cleanup
}

// TestAcquireExhaustion binds every slot in the pool and asserts
// acquire surfaces [ErrAllSlotsBusy] with a message that names the
// slot count and state directory so operators have actionable
// detail.
func TestAcquireExhaustion(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	const maxSlots = 4

	for slot := range maxSlots {
		bindLive(t, PathForSlot(slot, false))
	}

	_, _, _, err := Acquire(t.Context(), false, maxSlots, "")
	require.ErrorIs(t, err, ErrAllSlotsBusy)
	assert.Contains(t, err.Error(), "4 slots")
	assert.Contains(t, err.Error(), filepath.Join(stateHome, "mcp-kubectx-run"))
}

func TestListenUnlinksStale(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "stale.sock")

	// A regular file at the path simulates a leftover from a
	// previous serve that exited without unlinking.
	require.NoError(t, os.WriteFile(path, []byte("stale"), 0o600))

	l, cleanup, err := Listen(t.Context(), path, "")
	require.NoError(t, err)

	t.Cleanup(cleanup)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSocket, info.Mode().Type(),
		"stale file should have been replaced by a real socket")

	require.NotNil(t, l)
}

func TestListenPermissions(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "sub", "perm.sock")

	l, cleanup, err := Listen(t.Context(), path, "")
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

func TestListenCleanupRemoves(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "cleanup.sock")

	_, cleanup, err := Listen(t.Context(), path, "")
	require.NoError(t, err)

	cleanup()

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "cleanup must remove the socket file")
}

// TestListenOnExistingLivePath asserts that Listen returns ErrInUse
// when a live peer already holds the path. We deliberately do not
// silently steal a live socket; only stale-and-removable files get
// unlinked. Guarding against regressions that would silently break
// a peer process.
func TestListenOnExistingLivePath(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "live.sock")

	bindLive(t, path)

	_, _, err := Listen(t.Context(), path, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInUse)
}

// TestListenWritesSidecar pins that a non-empty instanceID is
// atomically persisted alongside the socket inode with mode 0600.
// The tmp file must be gone after the rename.
func TestListenWritesSidecar(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "sidecar.sock")

	_, cleanup, err := Listen(t.Context(), path, "inst-test")
	require.NoError(t, err)

	t.Cleanup(cleanup)

	data, err := os.ReadFile(SidecarPath(path))
	require.NoError(t, err)
	assert.Equal(t, "inst-test", string(data))

	info, err := os.Stat(SidecarPath(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	_, err = os.Stat(SidecarPath(path) + ".tmp")
	assert.True(t, os.IsNotExist(err), "tmp file must be removed after rename")
}

// TestListenOmitsSidecarOnEmptyInstanceID pins the test-only path
// where no instance id is provided: the function still binds
// successfully and no sidecar is created.
func TestListenOmitsSidecarOnEmptyInstanceID(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "no-sidecar.sock")

	_, cleanup, err := Listen(t.Context(), path, "")
	require.NoError(t, err)

	t.Cleanup(cleanup)

	_, err = os.Stat(SidecarPath(path))
	assert.True(t, os.IsNotExist(err), "no sidecar should exist when instanceID is empty")
}

// TestListenCleanupRemovesSidecar pins that the returned cleanup
// closure unlinks both the socket inode and the sidecar when called.
func TestListenCleanupRemovesSidecar(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "cleanup-sidecar.sock")

	_, cleanup, err := Listen(t.Context(), path, "inst-cleanup")
	require.NoError(t, err)

	_, err = os.Stat(SidecarPath(path))
	require.NoError(t, err)

	cleanup()

	_, err = os.Stat(SidecarPath(path))
	assert.True(t, os.IsNotExist(err), "cleanup must remove the sidecar file")
}

// TestClearStaleRemovesOrphanedSidecar pins that a sidecar without a
// socket inode (the SIGKILL leftover the next serve inherits) gets
// cleaned up before bind. Without this the next DiscoverLive could
// read a stale id that has no live peer.
func TestClearStaleRemovesOrphanedSidecar(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "orphan.sock")
	sidecar := SidecarPath(path)

	require.NoError(t, os.WriteFile(sidecar, []byte("orphan-id"), 0o600))

	err := clearStale(t.Context(), path)
	require.NoError(t, err, "stale clearance must succeed even without a socket inode")

	_, err = os.Stat(sidecar)
	assert.True(t, os.IsNotExist(err), "orphan sidecar must be unlinked")
}

// TestClearStaleRemovesSidecarOfDeadInode pins that clearStale
// unlinks the sidecar alongside the socket inode when the dial probe
// fails. The next DiscoverLive pass must not see a sidecar attached
// to a dead socket.
func TestClearStaleRemovesSidecarOfDeadInode(t *testing.T) {
	t.Parallel()

	dir := shortTempDir(t)
	path := filepath.Join(dir, "dead.sock")
	sidecar := SidecarPath(path)

	require.NoError(t, os.WriteFile(path, []byte("stale-inode"), 0o600))
	require.NoError(t, os.WriteFile(sidecar, []byte("dead-id"), 0o600))

	err := clearStale(t.Context(), path)
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(sidecar)
	assert.True(t, os.IsNotExist(err), "sidecar of dead inode must be removed")
}

// TestDiscoverLive exercises the full skip matrix. Slot 0 has a live
// socket and a sidecar with id-A; slot 1 has a live socket but no
// sidecar (skipped); slot 2 has a sidecar but no socket (skipped);
// slot 3 has a live socket and an empty sidecar (skipped); slot 4
// has a live socket and an unrelated sidecar (added with id-D).
// Tests the (live, sidecar) join at the heart of the sweep.
func TestDiscoverLive(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	bindWithSidecar := func(slot int, id string) {
		path := PathForSlot(slot, false)
		bindLive(t, path)

		if id != "" {
			require.NoError(t, os.WriteFile(SidecarPath(path), []byte(id), 0o600))
		}
	}

	bindWithSidecar(0, "id-A")
	bindWithSidecar(1, "")

	// Slot 2: stale sidecar pointing at no socket.
	stalePath := PathForSlot(2, false)
	require.NoError(t, os.MkdirAll(filepath.Dir(stalePath), 0o700))
	require.NoError(t, os.WriteFile(SidecarPath(stalePath), []byte("orphan"), 0o600))

	// Slot 3: live socket with empty (zero-byte) sidecar.
	bindWithSidecar(3, "")
	require.NoError(t, os.WriteFile(SidecarPath(PathForSlot(3, false)), []byte(""), 0o600))

	bindWithSidecar(4, "id-D")

	got := DiscoverLive(t.Context(), 5, false)

	want := map[string]struct{}{"id-A": {}, "id-D": {}}
	assert.Equal(t, want, got, "only live+sidecar pairs should be in the live set")
}

// TestDiscoverLiveIncludesOwnSlot pins a fragile-but-load-bearing
// invariant: the serve's own listener accepts dials immediately
// after Listen() returns, before the accept goroutine starts. The
// sweep depends on this so a freshly started serve sees itself in
// the live set before any kubectl client has touched the UDS.
// Verifies by binding via Acquire (which writes the sidecar) and
// immediately calling DiscoverLive.
func TestDiscoverLiveIncludesOwnSlot(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	_, _, cleanup, err := Acquire(t.Context(), false, 8, "own-id")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	got := DiscoverLive(t.Context(), 8, false)
	_, ok := got["own-id"]
	assert.True(t, ok, "own slot's sidecar id must appear in the live set")
}

// TestAcquireWritesSidecar pins that the slot picked by Acquire has
// a sidecar containing the passed instanceID. The cross-coupling
// between the slot walker and the sidecar write is what lets a
// freshly-started serve include itself in DiscoverLive without
// coordination.
func TestAcquireWritesSidecar(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	stateHome := shortTempDir(t)
	t.Setenv("XDG_STATE_HOME", stateHome)

	path, _, cleanup, err := Acquire(t.Context(), false, 8, "acquire-inst")
	require.NoError(t, err)
	t.Cleanup(cleanup)

	data, err := os.ReadFile(SidecarPath(path))
	require.NoError(t, err)
	assert.Equal(t, "acquire-inst", string(data))
}
