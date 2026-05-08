package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP tool names. Used as the tool argument to [*handler.toolError],
// [*handler.execError], and [*handler.logStderr] so a typo can't misroute a
// log entry.
const (
	toolSearch          = "search_registry"
	toolProviderDetails = "get_provider_details"
	toolModuleDetails   = "get_module_details"
	toolResourceDocs    = "get_resource_docs"
	toolDatasourceDocs  = "get_datasource_docs"
	toolValidate        = "validate"
	toolInit            = "init"
	toolTest            = "test"
)

// SearchInput is the input schema for the search_registry tool.
type SearchInput struct {
	Query      string `json:"query"                jsonschema:"Search query for finding OpenTofu components (e.g., 'aws', 'kubernetes', 'database', 's3')"`
	Type       string `json:"type,omitzero"        jsonschema:"Type of registry items to search for: provider, module, resource, data-source, or all (default all)"`
	MaxLength  int    `json:"max_length,omitzero"  jsonschema:"Maximum number of characters to return (default 5000)"`
	StartIndex int    `json:"start_index,omitzero" jsonschema:"Character offset to start reading from (default 0)"`
}

// ProviderDetailsInput is the input schema for the get_provider_details tool.
type ProviderDetailsInput struct {
	Namespace  string `json:"namespace"            jsonschema:"Provider namespace (e.g., 'hashicorp', 'opentofu')"`
	Name       string `json:"name"                 jsonschema:"Provider name WITHOUT 'terraform-provider-' prefix (e.g., 'aws', 'kubernetes', 'azurerm')"`
	MaxLength  int    `json:"max_length,omitzero"  jsonschema:"Maximum number of characters to return (default 5000)"`
	StartIndex int    `json:"start_index,omitzero" jsonschema:"Character offset to start reading from (default 0)"`
}

// ModuleDetailsInput is the input schema for the get_module_details tool.
type ModuleDetailsInput struct {
	Namespace  string `json:"namespace"            jsonschema:"Module namespace without prefix (e.g., 'terraform-aws-modules')"`
	Name       string `json:"name"                 jsonschema:"Simple module name WITHOUT 'terraform-aws-' or similar prefix (e.g., 'vpc', 's3-bucket')"`
	Target     string `json:"target"               jsonschema:"Module target platform (e.g., 'aws', 'kubernetes', 'azurerm')"`
	MaxLength  int    `json:"max_length,omitzero"  jsonschema:"Maximum number of characters to return (default 5000)"`
	StartIndex int    `json:"start_index,omitzero" jsonschema:"Character offset to start reading from (default 0)"`
}

// ResourceDocsInput is the input schema for the get_resource_docs tool.
type ResourceDocsInput struct {
	Namespace  string `json:"namespace"            jsonschema:"Provider namespace (e.g., 'hashicorp', 'opentofu')"`
	Name       string `json:"name"                 jsonschema:"Provider name WITHOUT 'terraform-provider-' prefix (e.g., 'aws', 'kubernetes')"`
	Resource   string `json:"resource"             jsonschema:"Resource name WITHOUT provider prefix (e.g., 's3_bucket', 'instance')"`
	Version    string `json:"version,omitzero"     jsonschema:"Provider version (e.g., 'v4.0.0'); if omitted, the latest version is used"`
	MaxLength  int    `json:"max_length,omitzero"  jsonschema:"Maximum number of characters to return (default 5000)"`
	StartIndex int    `json:"start_index,omitzero" jsonschema:"Character offset to start reading from (default 0)"`
}

// DataSourceDocsInput is the input schema for the get_datasource_docs tool.
type DataSourceDocsInput struct {
	Namespace  string `json:"namespace"            jsonschema:"Provider namespace (e.g., 'hashicorp', 'opentofu')"`
	Name       string `json:"name"                 jsonschema:"Provider name WITHOUT 'terraform-provider-' prefix (e.g., 'aws', 'kubernetes')"`
	DataSource string `json:"data_source"          jsonschema:"Data source name WITHOUT provider prefix (e.g., 'ami', 'vpc')"`
	Version    string `json:"version,omitzero"     jsonschema:"Provider version (e.g., 'v4.0.0'); if omitted, the latest version is used"`
	MaxLength  int    `json:"max_length,omitzero"  jsonschema:"Maximum number of characters to return (default 5000)"`
	StartIndex int    `json:"start_index,omitzero" jsonschema:"Character offset to start reading from (default 0)"`
}

// handler holds the shared state for the tool handlers.
type handler struct {
	client    *Client
	log       *slog.Logger
	tofu      tofuExecutor
	policies  Policies
	allowRoot string
}

func (h *handler) handleSearch(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in SearchInput,
) (*mcp.CallToolResult, any, error) {
	items, err := h.client.Search(ctx, in.Query)
	if err != nil {
		return h.toolError(ctx, toolSearch,
			fmt.Errorf("searching the OpenTofu Registry: %w", err),
		)
	}

	text := renderSearch(in.Query, in.Type, items)

	return textResult(Truncate(text, in.StartIndex, in.MaxLength)), nil, nil
}

func (h *handler) handleProviderDetails(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ProviderDetailsInput,
) (*mcp.CallToolResult, any, error) {
	prov, err := h.client.Provider(ctx, in.Namespace, in.Name)
	if err != nil {
		return h.toolError(ctx, toolProviderDetails,
			fmt.Errorf("getting details for provider %s/%s: %w", in.Namespace, in.Name, err),
		)
	}

	ver := latestVersion(prov.Versions)
	if ver == "" {
		text := renderProvider(prov, ProviderVersion{})
		return textResult(Truncate(text, in.StartIndex, in.MaxLength)), nil, nil
	}

	pv, err := h.client.ProviderVersion(ctx, in.Namespace, in.Name, ver)
	if err != nil {
		return h.toolError(ctx, toolProviderDetails,
			fmt.Errorf("getting details for provider %s/%s: %w", in.Namespace, in.Name, err),
		)
	}

	text := renderProvider(prov, pv)

	return textResult(Truncate(text, in.StartIndex, in.MaxLength)), nil, nil
}

func (h *handler) handleModuleDetails(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ModuleDetailsInput,
) (*mcp.CallToolResult, any, error) {
	mod, err := h.client.Module(ctx, in.Namespace, in.Name, in.Target)
	if err != nil {
		return h.toolError(ctx, toolModuleDetails,
			fmt.Errorf("getting details for module %s/%s (%s): %w", in.Namespace, in.Name, in.Target, err),
		)
	}

	text := renderModule(mod)

	return textResult(Truncate(text, in.StartIndex, in.MaxLength)), nil, nil
}

func (h *handler) handleResourceDocs(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ResourceDocsInput,
) (*mcp.CallToolResult, any, error) {
	ver, err := h.resolveVersion(ctx, in.Version, in.Namespace, in.Name)
	if err != nil {
		return h.toolError(ctx, toolResourceDocs,
			fmt.Errorf("getting documentation for resource %s_%s: %w", in.Name, in.Resource, err),
		)
	}

	body, err := h.client.ResourceDocs(ctx, in.Namespace, in.Name, ver, in.Resource)
	if err != nil {
		return h.toolError(ctx, toolResourceDocs,
			fmt.Errorf("getting documentation for resource %s_%s: %w", in.Name, in.Resource, err),
		)
	}

	return textResult(Truncate(body, in.StartIndex, in.MaxLength)), nil, nil
}

func (h *handler) handleDatasourceDocs(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in DataSourceDocsInput,
) (*mcp.CallToolResult, any, error) {
	ver, err := h.resolveVersion(ctx, in.Version, in.Namespace, in.Name)
	if err != nil {
		return h.toolError(ctx, toolDatasourceDocs,
			fmt.Errorf("getting documentation for data source %s_%s: %w", in.Name, in.DataSource, err),
		)
	}

	body, err := h.client.DatasourceDocs(ctx, in.Namespace, in.Name, ver, in.DataSource)
	if err != nil {
		return h.toolError(ctx, toolDatasourceDocs,
			fmt.Errorf("getting documentation for data source %s_%s: %w", in.Name, in.DataSource, err),
		)
	}

	return textResult(Truncate(body, in.StartIndex, in.MaxLength)), nil, nil
}

// resolveVersion returns ver verbatim when non-empty, otherwise fetches the
// provider index and computes the latest semver-ranked version.
func (h *handler) resolveVersion(ctx context.Context, ver, ns, name string) (string, error) {
	if ver != "" {
		return ver, nil
	}

	prov, err := h.client.Provider(ctx, ns, name)
	if err != nil {
		return "", err
	}

	v := latestVersion(prov.Versions)
	if v == "" {
		return "", fmt.Errorf("%w: no versions listed for %s/%s", ErrRegistry, ns, name)
	}

	return v, nil
}

// toolError logs err and wraps it as a tool-level [*mcp.CallToolResult] with
// [*mcp.CallToolResult.IsError] set to true so the model sees the failure
// reason. Internal errors that should bubble up to the transport (context
// cancellation, OS errors) are returned directly by callers without going
// through this helper.
func (h *handler) toolError(ctx context.Context, tool string, err error) (*mcp.CallToolResult, any, error) {
	h.log.WarnContext(ctx, "tool error",
		slog.String("tool", tool),
		slog.Any("error", err),
	)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}, nil, nil
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
