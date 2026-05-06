// Mcp-opentofu is an MCP server (stdio transport) that exposes eight tools:
// search_registry, get_provider_details, get_module_details, get_resource_docs,
// get_datasource_docs, validate, init, and plan.
//
// The first five tools talk to https://api.opentofu.org over HTTPS and return
// either human-readable Markdown blocks or the registry's verbatim Markdown
// documentation bodies. The validate, init, and plan tools shell out to a
// local tofu binary: validate runs `tofu validate` and renders diagnostics as
// Markdown; init runs `tofu init` to download providers and modules; plan
// runs `tofu plan` with -detailed-exitcode and reports whether any changes
// are pending.
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
//   - --tofu-bin: path to the tofu binary used by the local-tofu tools
//     (validate, init, plan) (default: "tofu", resolved via PATH when not
//     absolute)
package main
