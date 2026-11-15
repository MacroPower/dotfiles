// Hook-router is a Claude Code hook handler that inspects tool invocations
// and manages plan-mode lifecycle state.
//
// It handles these hook events:
//
//   - PreToolUse:Bash evaluates command deny and ask rules from
//     --command-rules JSON. It denies a foreground `sleep` over the
//     --sleep-guard-config ceiling, and never checks a run_in_background
//     call. With --rm-guard, it denies an rm or rmdir whose target
//     Claude Code stops on a permission prompt. It lints the commit message or
//     pull request text a `git commit` or `gh pr create` carries through
//     --message-lint-config and denies with the findings. It rewrites
//     read-only grep and find into rg and bfs, and rewrites kubectl with
//     KUBECONFIG.
//   - PreToolUse:MCP evaluates MCP tool allow, ask, and deny lists from
//     --mcp-rules JSON. "MCP" is a routing sentinel, and the tool name
//     comes from the payload. A conditional rule in the same JSON
//     allows a call of its tool when the arguments meet every
//     condition. Every other call of that tool gets an ask. A
//     condition on a branch name reads the current branch of the
//     repository that the call names when the call names no branch. A
//     condition on a pull request holds when the state database
//     records that an agent opened it. It lints the tool arguments
//     that the toolFields of --message-lint-config name, and findings
//     deny the call before the allow or ask decision. A deny-listed
//     tool skips the lint.
//   - PreToolUse:FileWrite asks, under --write-guard, before a Write,
//     Edit, MultiEdit, or NotebookEdit call in plan or
//     bypassPermissions mode writes a path that the sandbox denies to
//     commands. "FileWrite" is a routing sentinel like "MCP".
//   - PreToolUse:ExitPlanMode gates plan exit behind plan-reviewer, and
//     records the plan path and baseline commit on approval.
//   - PreToolUse:EnterPlanMode resets plan session state.
//   - PostToolUse:AskUserQuestion clears the session when the
//     question's option labels identify it as the Stop-gate question,
//     so the Stop gate releases for the plan cycle.
//   - PostToolUse:Bash runs the matching file formatter on each file
//     the command changed (tool_response.bashEditDiff.changedFiles,
//     present when Claude Code's bashEditDiffEnabled setting is on).
//     Then it compacts redundant successful output (ANSI strip and run
//     collapse) through updatedToolOutput. When archiving is on, it
//     writes the uncompacted stream to a file and appends a pointer, so
//     the dropped detail stays recoverable.
//   - PostToolUse:CreatePullRequest records the pull request that a
//     create_pull_request call of the GitHub MCP server opened, so a
//     conditional rule can allow later edits to it. "CreatePullRequest"
//     is a routing sentinel like "MCP", and the hook matcher names the
//     tool.
//   - PostToolUse:Write/Edit/MultiEdit runs the matching file formatter
//     on the written file, then the matching linter from --linter-rules.
//     Findings block through exit code 2, which reaches the agent that
//     made the edit. A Write reports every finding, and an Edit reports
//     those on the lines it wrote and those it created elsewhere in the
//     file. Under --escape-restore-config, a Write or an Edit first
//     gets back the `\uXXXX` escapes that Claude Code decoded in its
//     text. An escape that the handler cannot put back blocks through
//     exit code 2 as well.
//   - Stop blocks with a message that instructs an AskUserQuestion call,
//     until the post-impl question has an answer for the current plan
//     cycle.
//   - TeammateIdle applies that same post-impl gate to an in-process
//     teammate, once per teammate, through exit code 2.
//   - SubagentStart records the spawned agent's type and id.
//   - PreCompact records the compaction trigger on the session.
//   - SessionStart migrates a pending plan handoff onto the new
//     session, and sweeps orphaned kubectx dirs and expired archives.
//   - SessionEnd removes the session's kubectx dir.
//   - UserPromptSubmit clears plan-guard state when the prompt invokes
//     a wrap-up skill from --commit-skills.
//
// An event with no case here is a no-op; the router never guesses a
// handler for one it does not recognize.
//
// With --agent-scope, the router handles only the payloads from the
// main conversation or only those from a subagent, and ignores the
// rest. Two hook entries on one event can then each serve one caller.
//
// Session state is persisted in a SQLite database.
//
// With --log-file, the router appends its log to that file, one JSON
// record per line. When the file reaches --log-max-bytes, the router
// renames it to the same path with ".1" appended and starts an empty
// file. It keeps --log-backups renamed files, numbered from the most
// recent, and removes the files past that count.
//
// The binary is thin wiring: event dispatch, flag parsing, and the
// handlers that connect Claude Code's hook protocol to the underlying
// engines. The engines themselves live in independent, importable
// subpackages:
//
//   - hook: protocol I/O.
//   - cmdrules: command deny and ask rules.
//   - mcprules: MCP tool allow, ask, and deny resolution, by tool name
//     and by the arguments of a call.
//   - formatter: file-formatter routing.
//   - linter: file-linter routing and finding filtering.
//   - wireinput: the tool input as the model sent it, and the restore
//     of the escapes that Claude Code decoded in it.
//   - compact: output compaction.
//   - archive: uncompacted-output archiving.
//   - searchrewrite: rewriting grep into rg and find into bfs.
//   - sleepguard: the foreground-sleep guard.
//   - rmguard: the unresolvable rm target guard.
//   - writeguard: the sandbox write boundary for file tools.
//   - msglint: commit message, pull request, and MCP tool argument
//     linting.
//   - state: SQLite session state.
//   - logrotate: rotation of the log file by size.
//   - kubectx: kubectl gating and session dir lifecycle.
//   - git: repo queries.
//   - postimpl: the post-implementation skill catalog.
//
// The one import between subpackages is msglint's use of
// linter for findings and mcprules for tool patterns. Only this package
// composes them.
package main
