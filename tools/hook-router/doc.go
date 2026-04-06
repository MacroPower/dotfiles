// Hook-router is a Claude Code hook handler that inspects tool invocations
// and manages plan-mode lifecycle state.
//
// It handles PreToolUse, PostToolUse, and Stop hook events:
//
//   - PreToolUse:Bash             -- denies git stash save/push
//   - PreToolUse:ExitPlanMode     -- gates plan exit behind plan-reviewer, records
//     plan path and baseline commit on approval
//   - PreToolUse:EnterPlanMode    -- resets plan session state
//   - PostToolUse:AskUserQuestion -- when the question's option labels identify it
//     as the Stop-gate question, captures a git
//     fingerprint so Stop can short-circuit
//   - Stop                        -- blocks (with an AskUserQuestion-instructing
//     message) until the recorded fingerprint matches
//     the current git state
//
// Session state is persisted in a SQLite database. Unmatched Bash commands
// are forwarded to an optional downstream hook.
//
// # Environment
//
//   - RTK_REWRITE: path to a downstream hook binary; unmatched input is piped to
//     its stdin when set.
package main
