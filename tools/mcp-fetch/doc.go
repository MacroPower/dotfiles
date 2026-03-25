// Mcp-fetch is an MCP server (stdio transport) that exposes a single "fetch"
// tool for retrieving web content.
//
// It fetches a URL over HTTP, optionally converts HTML to Markdown using
// readability extraction, and returns the result with pagination support.
//
// # Flags
//
//   - --user-agent: HTTP User-Agent header (default: "MCP-Fetch/0.1.0")
//   - --ignore-robots-txt: skip robots.txt checks
//   - --proxy-url: HTTP proxy URL
//   - --rules-file: path to JSON URL allow/deny rules file
//   - --log-file: path to JSON log file (append); logs allow/deny decisions
package main
