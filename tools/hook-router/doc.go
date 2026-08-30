// Hook-router is a Claude Code hook handler that inspects tool invocations
// and manages plan-mode lifecycle state.
//
// It handles these hook events:
//
//   - PreToolUse:Bash             -- evaluates command deny/ask rules from
//     --command-rules JSON, denies a foreground `sleep` over the
//     --sleep-guard-config ceiling (run_in_background calls are exempt),
//     rewrites read-only grep/find into rg/bfs, and rewrites kubectl
//     with KUBECONFIG
//   - PreToolUse:MCP              -- evaluates MCP tool allow/ask/deny lists
//     from --mcp-rules JSON ("MCP" is a routing sentinel; the tool name
//     comes from the payload)
//   - PreToolUse:ExitPlanMode     -- gates plan exit behind plan-reviewer, records
//     plan path and baseline commit on approval
//   - PreToolUse:EnterPlanMode    -- resets plan session state
//   - PreToolUse:Write/Edit/MultiEdit -- denies changes that introduce
//     non-ASCII typographic characters (curly quotes, ellipsis, dashes
//     other than '-') and suggests ASCII equivalents ("FileWrite" is
//     a routing sentinel; the tool name comes from the payload)
//   - PostToolUse:AskUserQuestion -- when the question's option labels identify it
//     as the Stop-gate question, clears the session,
//     releasing the Stop gate for the plan cycle
//   - PostToolUse:Bash            -- compacts redundant successful output (ANSI
//     strip + run collapse) via updatedToolOutput; when archiving is on,
//     writes the uncompacted stream to a file and appends a pointer so
//     the dropped detail stays recoverable
//   - PostToolUse:Write/Edit/MultiEdit -- runs the matching file formatter
//   - Stop                        -- blocks (with an AskUserQuestion-instructing
//     message) until the post-impl question has been
//     answered once for the current plan cycle
//   - TeammateIdle                -- applies that same post-impl gate to an
//     in-process teammate, once per teammate, through exit code 2
//   - SubagentStart               -- records the spawned agent's type and id
//   - PreCompact                  -- records the compaction trigger on the session
//   - SessionStart                -- migrates a pending plan handoff onto the new
//     session, and sweeps orphaned kubectx dirs and expired archives
//   - SessionEnd                  -- removes the session's kubectx dir
//   - UserPromptSubmit            -- clears plan-guard state when a wrap-up skill
//     from --commit-skills is invoked
//
// An event with no case here is a no-op; the router never guesses a
// handler for one it does not recognize.
//
// Session state is persisted in a SQLite database.
//
// The binary is thin wiring: event dispatch, flag parsing, and the
// handlers that connect Claude Code's hook protocol to the underlying
// engines. The engines themselves live in independent, importable
// subpackages -- hook (protocol I/O), cmdrules (command deny/ask
// rules), mcprules (MCP tool allow/ask/deny resolution), formatter
// (file-formatter routing), typography (detection of newly introduced
// typographic characters), compact (output compaction), archive
// (uncompacted-output archiving), searchrewrite (grep->rg / find->bfs
// rewriting), sleepguard (foreground-sleep guard), state (SQLite
// session state), kubectx (kubectl gating and session dir lifecycle),
// git (repo queries), and postimpl (post-implementation skill
// catalog). None of the subpackages import each other; only this
// package composes them.
package main
