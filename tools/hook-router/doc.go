// Hook-router is a Claude Code PreToolUse hook that inspects and rewrites
// shell commands before they are executed.
//
// It reads hook JSON from stdin, parses any shell command in the tool input,
// and applies two checks in order:
//
//  1. Deny: commands containing grep or find are rejected with a hint to use
//     rg or fd instead. git stash save/push forms are also denied to prevent
//     shelving changes as an avoidance mechanism.
//  2. Rewrite: git clone invocations are rewritten to use git-idempotent.
//
// Hooks that don't match either check are forwarded to an optional downstream
// hook.
//
// # Environment
//
//   - GIT_IDEMPOTENT: path to the git-idempotent binary.
//     (default: "git-idempotent")
//   - RTK_REWRITE: path to a downstream hook binary; unmatched input is piped to
//     its stdin when set.
package main
