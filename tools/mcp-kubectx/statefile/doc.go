// Package statefile writes small state files with the durability and
// permission guarantees mcp-kubectx needs: tmp+rename atomicity so
// uncoordinated concurrent readers never observe a torn or zero-byte
// file, 0600 file modes matching the single-user socket trust
// boundary, and atomic symlink replacement for pointer files that
// must never be observed missing.
package statefile
