// Package socket manages the per-`serve` Unix domain sockets that
// broker exec-credential mints to the in-binary `exec-plugin` shim:
// a dense slot pool of literal paths, per-slot sidecar files that
// attribute a bound socket to its owning serve instance, stale-inode
// recovery, a drainable accept loop, and the liveness discovery the
// orphan sweep keys on.
//
// # Slot pool
//
// The per-`serve` UDS lives at <StateDir>/serve.<slot>.<env>.sock,
// where <slot> is a dense integer 0..N-1 picked at startup by
// [Acquire]. Slot indices replace PID-based naming because Claude
// Code's sandbox `allowUnixSockets` setting matches entries as
// literal paths, not globs; enumerating one literal per slot is the
// only way to allow a per-`serve` socket whose filename varies.
// [Acquire] walks 0..N-1, skipping slots held by a live peer
// (detected by a dial probe) or that race-lose at bind time (wrapped
// [syscall.EADDRINUSE]). Crash-leftover inodes are unlinked and the
// slot reused. When every slot is held by a live peer, [Acquire]
// fails with [ErrAllSlotsBusy] naming the slot count and the state
// directory.
//
// Sockets live under a sibling of the bind-mounted mcp-kubectx state
// dir (see [StateDir]) because UDS-over-Lima-bind-mount semantics on
// a macOS host are unverified; hosting the socket on each profile's
// local filesystem avoids the question entirely.
//
// # Sidecars and liveness
//
// [Acquire] writes the caller's instance id into a per-slot sidecar
// file co-located with the socket inode, and [DiscoverLive] joins
// "dial succeeds" with "sidecar readable and non-empty" across the
// pool to build the live-instance set the sweep preserves. Both
// halves of the pair are unlinked together on cleanup and on
// stale-slot recovery so a SIGKILLed serve's sidecar can never
// vouch for a dead slot.
package socket
