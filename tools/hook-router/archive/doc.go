// Package archive persists the uncompacted content of a rewritten
// output stream to its own file, so a lossy compaction stays
// recoverable: the compacted text gains a one-line pointer naming the
// file, and the dropped detail can be read back on demand.
//
// The no-orphans invariant holds throughout: a pointer is never
// emitted without its file, and a file is never written unless the
// pointer nets a shorter output. A time-based sweep removes archived
// files once they are old enough that no live pointer should name
// them.
package archive
