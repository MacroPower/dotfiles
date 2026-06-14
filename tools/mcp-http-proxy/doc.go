// Mcp-http-proxy is a stdio MCP server that proxies to an upstream Streamable
// HTTP MCP endpoint.
//
// It exists so that hosts which only understand stdio MCP servers (Claude
// Code's mcpActivationGuard, sops-backed wrapper scripts, CA env injection)
// can interact with HTTP-only MCP endpoints. The proxy also provides a single
// place to log JSON-RPC traffic for debugging.
//
// # Flags
//
//   - --url: upstream Streamable HTTP MCP endpoint (required)
//   - --header K=V: repeatable; values are expanded with os.ExpandEnv so
//     secrets can be passed through environment without landing in args
//   - --allow-tool: repeatable; when any are set, only matching tool names
//     survive in tools/list responses
//   - --deny-tool: repeatable; tool names dropped from tools/list responses
//     (takes precedence over --allow-tool)
//   - --log-file: path to JSON log file (append); logs forwarded methods
//
// # Forwarding scope
//
// Client-initiated requests that are forwarded to the upstream session:
// tools/list, tools/call, resources/list, resources/read,
// resources/templates/list, prompts/list, prompts/get. Subscribe, unsubscribe,
// and completion/complete are forwarded when the upstream advertises the
// matching capability.
//
// tools/list responses are filtered by the --allow-tool/--deny-tool lists
// before being returned, so tools the operator has classified as unused or
// denied are removed from the client's view (and stop costing context tokens)
// even though the upstream still serves them. tools/call is not filtered: a
// dropped tool simply never appears for the client to call.
//
// # Limitations
//
// Notifications are not forwarded in either direction. The SDK's generic
// middleware hook requires returning a value of an unexported Result type to
// respond to notification-shaped methods, which cannot be constructed from
// outside the mcp package. As a result:
//
//   - Local notifications/cancelled, notifications/progress, logging/setLevel
//     fall through to the SDK's default handlers and do not reach upstream.
//   - Server-initiated requests/notifications from upstream
//     (sampling/createMessage, elicitation/create, tools/list_changed, etc.)
//     are consumed by the proxy's client session and not relayed.
//
// This is acceptable for stateless, tool-focused upstreams (e.g. GitHub's
// readonly MCP endpoint). A richer proxy would either need SDK support for
// exported forwarding primitives or a custom jsonrpc2-layer implementation.
package main
