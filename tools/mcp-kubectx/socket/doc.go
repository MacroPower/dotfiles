// Package socket manages the per-`serve` Unix domain sockets that
// broker exec-credential mints to the in-binary `exec-plugin` shim:
// a dense slot pool of literal paths, per-slot sidecar files that
// attribute a bound socket to its owning serve instance, stale-inode
// recovery, a drainable accept loop, and the liveness discovery the
// orphan sweep keys on.
//
// # Slot pool
//
// The per-`serve` UDS lives at serve.<slot>.<env>.sock in
// [statedir.RunDir], where <slot> is a dense integer 0..N-1 picked
// at startup by [Acquire]. Slot indices replace PID-based naming
// because Claude Code's sandbox `allowUnixSockets` setting matches
// entries as literal paths, not globs. So the only way to allow a
// per-`serve` socket whose filename varies is to list one literal
// per slot.
// [Acquire] walks 0..N-1, skipping slots held by a live peer
// (detected by a dial probe) or that race-lose at bind time (wrapped
// [syscall.EADDRINUSE]). Crash-leftover inodes are unlinked and the
// slot reused. When every slot is held by a live peer, [Acquire]
// fails with [ErrAllSlotsBusy] naming the slot count and the state
// directory.
//
// Sockets live in [statedir.RunDir], which workmux does not mount
// into a Lima guest. The behavior of a UDS on a Lima bind mount of a
// macOS host is unverified. A socket on the local filesystem of each
// profile avoids the question.
//
// # Sidecars and liveness
//
// [Acquire] writes the caller's instance id into a per-slot sidecar
// file co-located with the socket inode, and [DiscoverLive] joins
// "dial succeeds" with "sidecar readable and non-empty" across the
// pool to build the live-instance set the sweep preserves.
// Stale-slot recovery unlinks both halves of the pair, so a
// SIGKILLed serve's sidecar can never vouch for a dead slot.
//
// Closing the listener that [Acquire] returns unlinks the sidecar
// and then the socket inode. The listener holds the slot until the
// socket inode is gone, so the close unlinks only the files of its
// own serve. A later serve that takes the freed slot keeps its
// files when the old listener closes a second time.
package socket
