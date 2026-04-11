// Hook-router is a Claude Code hook handler that inspects tool invocations
// and manages plan-mode lifecycle state.
//
// It handles PreToolUse, PostToolUse, and Stop hook events:
//
//   - PreToolUse:Bash -- denies git stash save/push and direct kubectl usage
//   - PreToolUse:ExitPlanMode -- gates plan exit behind plan-reviewer
//   - PreToolUse:EnterPlanMode -- resets plan session state
//   - PostToolUse:ExitPlanMode -- records plan path and baseline commit
//   - Stop -- blocks until implementation-reviewer approves (when plan changes exist)
//
// Session state is persisted in a SQLite database. Unmatched Bash commands
// are forwarded to an optional downstream hook.
//
// # Environment
//
//   - RTK_REWRITE: path to a downstream hook binary; unmatched input is piped to
//     its stdin when set.
package main
