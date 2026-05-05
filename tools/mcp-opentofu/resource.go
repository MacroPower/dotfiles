package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registryInfoURI is the canonical URI of the [registryInfoText] static MCP
// resource exposed by the server. The opaque-URI form (scheme + ":" + name,
// no authority or path) keeps the listing short and matches the example
// resource in the SDK's `examples/server/everything` server.
const registryInfoURI = "opentofu:registry-info"

// registryInfoMIMEType is advertised both in the [mcp.Resource] entry and in
// the [mcp.ResourceContents] returned by [registryInfoHandler] so clients can
// pick a Markdown renderer.
const registryInfoMIMEType = "text/markdown"

// registryInfoName is the programmatic name surfaced via `resources/list`.
// Clients fall back to it as the display label when no Title is set.
const registryInfoName = "OpenTofu Registry overview"

// registryInfoDescription is the human-readable hint surfaced alongside the
// resource entry to help models decide when to read it.
const registryInfoDescription = "Tool overview and naming conventions for the OpenTofu Registry MCP server."

// registryInfoText is the verbatim Markdown body returned by
// [registryInfoHandler]. It documents the available tools and the naming
// conventions the model should follow when building tool inputs (no
// `terraform-provider-` / `terraform-aws-` prefixes, short resource names).
const registryInfoText = `The OpenTofu Registry is a public index of providers, modules, resources, and data sources for OpenTofu and Terraform.
You can:
- **Search** for providers, modules, resources, and data sources using the ` + "`search_registry`" + ` tool.
- **Get detailed information** about a provider or module using ` + "`get_provider_details`" + ` or ` + "`get_module_details`" + `.
- **Retrieve documentation** for a specific resource or data source using ` + "`get_resource_docs`" + ` or ` + "`get_datasource_docs`" + `.

**Tips:**
- Do **not** include prefixes like ` + "`terraform-provider-`" + ` or ` + "`terraform-aws-`" + ` in names.
- Use simple search terms (e.g., ` + "`aws`" + `, ` + "`kubernetes`" + `, ` + "`s3`" + `, ` + "`database`" + `).
- For resources and data sources, use the short name (e.g., ` + "`s3_bucket`" + `, ` + "`instance`" + `, ` + "`ami`" + `).

This MCP server is designed to work with OpenTofu (a fork of HashiCorp Terraform) and provides access to the OpenTofu Registry.
For more details, use the search and info tools above to explore the registry.
`

// registryInfoResource describes the static resource registered by
// [addRegistryInfoResource].
func registryInfoResource() *mcp.Resource {
	return &mcp.Resource{
		Name:        registryInfoName,
		Description: registryInfoDescription,
		MIMEType:    registryInfoMIMEType,
		URI:         registryInfoURI,
	}
}

// registryInfoHandler answers `resources/read` requests for
// [registryInfoURI] with the static [registryInfoText] body. The returned
// [*mcp.ResourceContents] echoes the request URI verbatim so the client can
// correlate the response with the request.
func registryInfoHandler(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: registryInfoMIMEType,
			Text:     registryInfoText,
		}},
	}, nil
}

// addRegistryInfoResource registers the static `opentofu:registry-info`
// resource on srv. Registering any resource auto-advertises
// [mcp.ResourceCapabilities] with ListChanged=true, so callers do not need to
// configure capabilities explicitly.
func addRegistryInfoResource(srv *mcp.Server) {
	srv.AddResource(registryInfoResource(), registryInfoHandler)
}
