// Git-idempotent is an idempotent git clone wrapper.
//
// It clones a repository if the destination does not exist, or pulls
// (fast-forward only) if it does. A file-based lock prevents concurrent
// operations on the same destination.
//
// Usage:
//
//	git-idempotent clone [flags] [--] <url> <dest>
//
// All flags are passed through to git clone verbatim.
package main
