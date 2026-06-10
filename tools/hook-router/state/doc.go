// Package state persists hook-router's session state in SQLite:
// plan-guard lifecycle rows keyed by session_id, pending plan handoffs
// keyed by the Claude Code window PID, and a log of failed Bash
// commands for later analysis.
//
// The store is built for many short-lived concurrent processes
// sharing one database file: WAL mode, per-connection busy timeouts, a
// version-gated migration path serialized under BEGIN IMMEDIATE, and
// probabilistic pruning of stale rows.
package state
