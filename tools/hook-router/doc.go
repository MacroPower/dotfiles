// Hook-router is a Claude Code hook handler that inspects tool invocations
// and manages plan-mode lifecycle state.
//
// It handles PreToolUse, PostToolUse, and Stop hook events:
//
//   - PreToolUse:Bash             -- evaluates command deny/ask rules from
//     --command-rules JSON and rewrites kubectl with KUBECONFIG
//   - PreToolUse:ExitPlanMode     -- gates plan exit behind plan-reviewer, records
//     plan path and baseline commit on approval
//   - PreToolUse:EnterPlanMode    -- resets plan session state
//   - PostToolUse:AskUserQuestion -- when the question's option labels identify it
//     as the Stop-gate question, clears the session,
//     releasing the Stop gate for the plan cycle
//   - PostToolUse:Bash            -- compacts redundant successful output (ANSI
//     strip + run collapse) via updatedToolOutput
//   - PostToolUse:Write/Edit/MultiEdit -- runs the matching file formatter
//   - Stop                        -- blocks (with an AskUserQuestion-instructing
//     message) until the post-impl question has been
//     answered once for the current plan cycle
//
// Session state is persisted in a SQLite database.
package main
