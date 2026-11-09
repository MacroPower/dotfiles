// Package writeguard reports whether Claude Code's sandbox lets a
// sandboxed command write a path. A hook uses the answer to hold
// Claude's file tools to the same boundary.
//
// The sandbox confines only Bash and the other shell tools. Write,
// Edit, MultiEdit, and NotebookEdit follow permission rules instead,
// and the bypassPermissions mode skips those rules. A [Policy] rebuilds
// the sandbox's write boundary from the settings files, and
// [Policy.Check] tests one path against it.
//
// # Boundary
//
// A [Policy] lets a path through when it sits under one of these
// directories:
//
//   - The project directory, which stands in for the working directory.
//   - The per-user temp directory.
//   - Every permissions.additionalDirectories entry.
//   - Every sandbox.filesystem.allowWrite entry.
//
// It refuses a path under a sandbox.filesystem.denyWrite entry or under
// one of the sandbox's protected paths, even inside an allowed
// directory. The project directory and every directory above it
// protect the .claude settings files, .claude/scheduled_tasks.json, and
// .mcp.json. They also protect the skills, agents, commands, hooks, and
// workflows directories under .claude. The project directory alone
// also protects the shell startup files, .gitconfig, the .vscode and
// .idea directories, and .git/hooks and .git/config.
//
// The settings come from the user settings file and from the project's
// .claude/settings.json and .claude/settings.local.json. Entries merge
// across the files. Sandbox paths resolve by their prefix the way
// Claude Code resolves them. A `~/` entry resolves under the home
// directory. A relative entry resolves under the configuration
// directory for the user file, and under the project directory for the
// other two. A trailing `/**` covers the same directory as the bare
// path.
//
// Every path resolves through its symlinks before a comparison, so a
// link inside the project that points outside it is outside. The plans
// directory under the configuration directory always passes, because
// Claude Code writes the session's plan file there in plan mode.
//
// # Non-Goals
//
//   - Directories added with --add-dir or /add-dir. A hook does not
//     receive them, so [Policy.Check] refuses a write there.
//   - Edit permission rules, managed settings, files passed with
//     --settings, and the plansDirectory setting.
//   - The grant on a linked worktree's shared .git directory, and the
//     files that turn the working directory into a bare repository.
//   - Protected paths that Claude Code's documentation names only by
//     example, beyond the ones the Boundary section lists.
package writeguard
