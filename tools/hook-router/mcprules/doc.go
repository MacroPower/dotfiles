// Package mcprules resolves MCP tool calls to PreToolUse permission
// decisions. It re-implements Claude Code's deny > ask > allow rule
// evaluation for the MCP entries of settings.json permission lists.
// Plan mode ignores those lists for subagent-originated MCP calls
// (anthropics/claude-code#73633), but PreToolUse hook decisions still
// apply. Patterns follow the settings rule format: exact tool names,
// bare server names covering every tool on a server, and trailing-*
// prefix globs. Tools matching no pattern resolve to no decision, so
// the normal permission flow decides them.
//
// Settings rules cannot read the arguments of an MCP tool call, so the
// package adds conditional allow rules. Each rule names one tool and a
// set of conditions on top-level tool_input fields, such as a string
// that matches a regular expression or a boolean that is true. A call
// that meets every condition of one rule resolves to allow, and every
// other call of that tool resolves to ask. A deny pattern still wins
// over the rule.
//
// One test reads a branch name. A call that names no branch acts on the
// current branch of a repository. The test then reads that branch
// through the [BranchResolver] of the ruleset. The caller supplies the
// resolver, and the package itself runs no git command.
//
// Claude Code prompts on a settings ask rule even when a hook allows
// the call. A tool with a conditional rule must therefore have no
// settings ask or allow entry. The hook decision is then the only one
// that the tool gets.
package mcprules
