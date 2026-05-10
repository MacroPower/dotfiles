// Mcp-git is an MCP server (stdio transport) that exposes idempotent git
// clone operations.
//
// It clones a repository if the destination does not already exist, or pulls
// (fast-forward only) if it does. A file-based flock prevents concurrent
// operations on the same destination path.
//
// # Flags
//
//   - --allow-dir: restrict dest to subdirectories of this path
//     (repeatable; if omitted, all paths are allowed)
//   - --allow-insecure: permit unencrypted URL schemes (http, git)
//   - --timeout: max duration for a single git operation (0 disables)
//
// The per-destination flock acquired before any git invocation is
// not context-aware, so a destination contended by another caller
// can extend the wall-clock duration past --timeout: the deadline
// expires during the flock wait and the next git invocation aborts
// immediately once flock returns.
//
// # Environment
//
//   - GH_TOKEN: when set, configures a git credential helper that
//     authenticates HTTPS requests to github.com using this token.
//     This enables cloning private repositories.
package main
