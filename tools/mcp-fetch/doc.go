// Mcp-fetch is an MCP server (stdio transport) that exposes a single "fetch"
// tool for retrieving web content.
//
// It fetches a URL over HTTP, optionally converts HTML to Markdown using
// readability extraction, and returns the result with pagination support.
// When --db is set, every fetch attempt (success or failure) is recorded
// to a SQLite database; the `stats` subcommand reads that database and
// prints a summary report.
//
// # Subcommands
//
//   - (default): run the MCP server.
//   - stats:     read the recorded fetch history and print a summary.
//   - version:   print the binary version.
//   - help:      print server flag usage.
//
// # Server flags
//
//   - --user-agent: HTTP User-Agent header (default: "MCP-Fetch/0.1.0")
//   - --ignore-robots-txt: skip robots.txt checks
//   - --ignore-llms-txt: skip llms.txt discovery and notice
//   - --proxy-url: HTTP proxy URL
//   - --rules-file: path to JSON URL allow/deny rules file
//   - --log-file: path to JSON log file (append); logs allow/deny decisions
//   - --db: path to SQLite store; empty disables recording
//
// # Stats flags
//
//   - --db: path to SQLite store (default: $XDG_STATE_HOME/mcp-fetch/fetches.db)
//   - --since: only count rows newer than this duration (24h, 30m, 7d)
//   - --last: append the most recent N rows below the summary
//   - --top: how many hosts to list under "Top hosts"
package main
