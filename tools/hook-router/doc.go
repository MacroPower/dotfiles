// Hook-router is a Claude Code hook handler that inspects tool invocations
// and manages plan-mode lifecycle state.
//
// It handles PreToolUse, PostToolUse, and Stop hook events:
//
//   - PreToolUse:Bash             -- evaluates command deny/ask rules from
//     --command-rules JSON and rewrites kubectl with KUBECONFIG
//   - PreToolUse:MCP              -- evaluates MCP tool allow/ask/deny lists
//     from --mcp-rules JSON ("MCP" is a routing sentinel; the tool name
//     comes from the payload)
//   - PreToolUse:ExitPlanMode     -- gates plan exit behind plan-reviewer, records
//     plan path and baseline commit on approval
//   - PreToolUse:EnterPlanMode    -- resets plan session state
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
//
// Session state is persisted in a SQLite database.
//
// The binary is thin wiring: event dispatch, flag parsing, and the
// handlers that connect Claude Code's hook protocol to the underlying
// engines. The engines themselves live in independent, importable
// subpackages -- hook (protocol I/O), cmdrules (command deny/ask
// rules), mcprules (MCP tool allow/ask/deny resolution),
// formatter (file-formatter routing), compact (output
// compaction), archive (uncompacted-output archiving), state (SQLite
// session state), kubectx (kubectl gating and session dir lifecycle),
// git (repo queries), and postimpl (post-implementation skill
// catalog). None of the subpackages import each other; only this
// package composes them.
package main
