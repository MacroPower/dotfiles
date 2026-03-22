// Hook-router is a Claude Code PreToolUse hook that inspects shell commands
// before they are executed.
//
// It reads hook JSON from stdin, parses any shell command in the tool input,
// and denies commands containing find (use fd instead) and git stash save/push
// forms.
//
// Hooks that don't match are forwarded to an optional downstream hook.
//
// # Environment
//
//   - RTK_REWRITE: path to a downstream hook binary; unmatched input is piped to
//     its stdin when set.
package main
