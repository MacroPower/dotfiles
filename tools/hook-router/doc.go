// Hook-router is a Claude Code PreToolUse hook that inspects shell commands
// before they are executed.
//
// It reads hook JSON from stdin, parses any shell command in the tool input,
// and applies the following checks in order:
//
//  1. Denies git stash save/push forms.
//  2. Denies direct kubectl invocations (use the MCP server instead).
//  3. Rewrites docker commands to route through a socket proxy, ensuring the
//     proxy container is running via ensure-docker-proxy.
//
// Commands that don't match any check are forwarded to an optional downstream
// hook.
//
// # Environment
//
//   - RTK_REWRITE: path to a downstream hook binary; unmatched input is piped to
//     its stdin when set.
//   - DOCKER_PROXY_ENSURE: path to the ensure-docker-proxy binary. When set,
//     docker commands are rewritten to use DOCKER_HOST pointing at the proxy.
//     When unset, docker commands pass through unchanged.
//   - DOCKER_PROXY_PORT: TCP port the proxy listens on (default 2375). Must
//     match the port configured for ensure-docker-proxy.
package main
