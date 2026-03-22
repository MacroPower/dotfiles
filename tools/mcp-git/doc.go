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
package main
