// Mcp-git is an MCP server (stdio transport) that exposes git
// operations that contact a remote.
//
// git_clone clones a repository if the destination does not already
// exist, or pulls (fast-forward only) if it does, holding a flock on
// <dest>.lock beside the destination. When the destination holds a
// .git directory that git cannot open, git_clone renames the
// destination to <dest>.corrupt-<timestamp> and clones again.
// git_fetch, git_pull, and git_push operate on an existing local
// repository: they resolve its git directory (worktree-aware), take
// a flock on <gitdir>/mcp-git.lock, resolve the named remote's URL
// so the GH_TOKEN credential helper can authenticate it, and run the
// corresponding git subcommand with captured output. Remote and ref
// inputs are plain names, never URLs or refspecs, so remote deletes
// and force pushes cannot be expressed (git_push offers
// force_with_lease explicitly).
//
// git_fetch, git_pull, and git_push never run the hook files of the
// repository. A hook runs with the server's privileges, outside any
// sandbox that confines the caller, and the caller can edit the code
// that a hook runs. The tools therefore point core.hooksPath at
// /dev/null. No tool input enables hooks.
//
// git_push skips every pre-push hook. It passes --no-verify, which
// also skips a pre-push hook that git config defines with
// hook.<name>.command. Git does not pass --no-verify on to the push
// that it starts in a submodule, so git_push never pushes a
// submodule. The git config can ask git push to recurse into
// submodules, with push.recurseSubmodules or submodule.recurse. In
// that case git_push makes git check the submodules instead. The
// check refuses a push that references a submodule commit that no
// remote-tracking ref of the submodule reaches. The caller then
// pushes the submodule with a git_push call of its own. Otherwise
// git_push checks no submodule, like a git push with the default
// config.
//
// git_push refuses to push to a repository on the local file system,
// such as a path or a file:// URL. Git pushes to such a repository by
// starting receive-pack itself, and receive-pack runs the hooks of
// that repository with the server's privileges. git_push runs git
// with protocol.file.allow=never, so git enforces the refusal for
// each URL that the push resolves. Before the push, git_push also
// checks the push URLs of the remote, so that the error names the
// remote.
//
// A pre-push hook is repository policy, so git_push reports the
// pre-push hooks that it skips. The result of a successful push names
// the executable pre-push hook file and each enabled pre-push hook
// that git config defines. The result also tells the caller to run
// the checks of each hook itself, inside its sandbox.
//
// The tools trust the git config of the repository, and git runs the
// commands that the config names. A hook that the config defines for
// an event other than pre-push, such as reference-transaction, is
// one of those commands.
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
