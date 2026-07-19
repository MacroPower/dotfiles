// Package fetch implements the MCP "fetch" tool: it retrieves a URL over
// HTTP, optionally extracts readable content and converts it to Markdown,
// applies grep-style line filtering and rune-window pagination, and
// returns the result.
//
// A [Handler] composes the independent policy packages around a single
// shared [*http.Client]: [go.jacobcolvin.com/dotfiles/tools/mcp-fetch/rules]
// for URL allow/deny, [go.jacobcolvin.com/dotfiles/tools/mcp-fetch/robots]
// for robots.txt, [go.jacobcolvin.com/dotfiles/tools/mcp-fetch/llmstxt] for
// llms.txt discovery, and
// [go.jacobcolvin.com/dotfiles/tools/mcp-fetch/store] for recording each
// attempt. The same allow/deny and robots checks run on every redirect
// hop. Content shaping is delegated to
// [go.jacobcolvin.com/dotfiles/tools/mcp-fetch/content] (grep filtering
// and pagination) and
// [go.jacobcolvin.com/dotfiles/tools/mcp-fetch/markdown] (HTML to
// Markdown). When the render_js input is set,
// [go.jacobcolvin.com/dotfiles/tools/mcp-fetch/render] executes the
// page's JavaScript in a headless browser first, best-effort: a failed
// or empty render degrades to the plain conversion with a notice.
package fetch
