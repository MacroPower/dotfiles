// Package stats backs the `stats` subcommand: it reads the fetch history
// from a [go.jacobcolvin.com/dotfiles/tools/mcp-fetch/store.Store] and
// renders a human-readable summary report.
//
// [Run] is the CLI entry point. The pure pieces are exported separately
// so they can be tested without a process: [Build] turns raw store query
// results into a display-ready [Summary], and [Format] writes the
// report. [ResolveDBPath] and [ParseSince] resolve the `--db` and
// `--since` flags.
package stats
