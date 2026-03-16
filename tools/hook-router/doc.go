// Hook-router is a Claude Code PreToolUse hook that rewrites shell commands
// before they are executed.
//
// It reads hook JSON from stdin, parses any shell command in the tool input,
// and rewrites git clone invocations to use git-idempotent. Hooks that don't
// match are forwarded to an optional downstream hook.
//
// # Environment
//
//   - GIT_IDEMPOTENT: path to the git-idempotent binary.
//     (default: "git-idempotent")
//   - RTK_REWRITE: path to a downstream hook binary; unmatched input is piped to
//     its stdin when set.
package main
