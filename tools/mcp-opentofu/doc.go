// Mcp-opentofu is an MCP server (stdio transport) that exposes eight tools:
// search_registry, get_provider_details, get_module_details, get_resource_docs,
// get_datasource_docs, run_init, run_validate, and run_test.
//
// The first five tools talk to https://api.opentofu.org over HTTPS and return
// either human-readable Markdown blocks or the registry's verbatim Markdown
// documentation bodies. The run_init, run_validate, and run_test tools shell
// out to a local tofu binary: run_init runs `tofu init` to download providers
// and modules; run_validate runs `tofu validate` and renders diagnostics as
// Markdown; run_test runs `tofu test` to execute *.tftest.hcl /
// *.tofutest.hcl files.
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
//     (run_init, run_validate, run_test) (default: "tofu", resolved via PATH
//     when not absolute)
package main
