// Mcp-opentofu is an MCP server (stdio transport) that exposes the OpenTofu
// Registry as five tools: search_registry, get_provider_details,
// get_module_details, get_resource_docs, and get_datasource_docs.
//
// All tools talk to https://api.opentofu.org over HTTPS and return either
// human-readable Markdown blocks or the registry's verbatim Markdown
// documentation bodies.
//
// The server also exposes a single static MCP resource at URI
// opentofu:registry-info carrying naming conventions and a tool overview the
// model can read on demand without inflating every tool description.
//
// # Flags
//
//   - --user-agent: HTTP User-Agent header (default: "MCP-OpenTofu/0.1.0")
//   - --proxy-url: HTTP proxy URL (default: "", direct connection)
//   - --log-file: path to JSON log file (append); logs registry calls and
//     tool errors
package main
