// Package mcprules resolves MCP tool names to PreToolUse permission
// decisions. It re-implements Claude Code's deny > ask > allow rule
// evaluation for the MCP entries of settings.json permission lists,
// because plan mode ignores those lists for subagent-originated MCP
// calls (anthropics/claude-code#73633) while PreToolUse hook decisions
// still apply. Patterns follow the settings rule format: exact tool
// names, bare server names covering every tool on a server, and
// trailing-* prefix globs. Tools matching no pattern resolve to no
// decision, leaving them to the normal permission flow.
package mcprules
