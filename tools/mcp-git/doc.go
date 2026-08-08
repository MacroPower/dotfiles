// Mcp-git is an MCP server (stdio transport) that exposes git
// operations that contact a remote.
//
// git_clone clones a repository if the destination does not already
// exist, or pulls (fast-forward only) if it does, holding a flock on
// <dest>.lock beside the destination. git_fetch,
// git_pull, and git_push operate on an existing local repository:
// they resolve the repository's git directory (worktree-aware), take
// a flock on <gitdir>/mcp-git.lock, resolve the named remote's URL
// so the GH_TOKEN credential helper can authenticate it, and run the
// corresponding git subcommand with captured output. Remote and ref
// inputs are plain names, never URLs or refspecs, so remote deletes
// and force pushes cannot be expressed (git_push offers
// force_with_lease explicitly).
//
// # Flags
//
//   - --allow-dir: restrict repository and destination paths to
//     subdirectories of this path (repeatable; if omitted, all paths
//     are allowed)
//   - --allow-insecure: permit unencrypted URL schemes (http, git)
//   - --timeout: max duration for a single git operation (0 disables)
//
// The flock acquired before the main git invocation is not
// context-aware, so a repository contended by another caller can
// extend the wall-clock duration past --timeout: the deadline
// expires during the flock wait and the next git invocation aborts
// immediately once flock returns.
//
// # Environment
//
//   - GH_TOKEN: when set, configures a git credential helper that
//     authenticates HTTPS requests to github.com using this token.
//     This enables cloning, fetching, pulling, and pushing private
//     repositories.
package main
