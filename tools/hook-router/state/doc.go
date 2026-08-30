// Package state persists hook-router's session state in SQLite:
// plan-guard lifecycle rows keyed by session_id, pending plan handoffs
// keyed by the Claude Code window PID, a log of failed Bash commands
// for later analysis, the subagents each session spawned, and the
// teammates the idle gate has already blocked once.
//
// Everything keyed by session_id shares that key's lifecycle: clearing
// or resetting a session drops the rows that gate its next plan cycle,
// and pruning removes them on the same 24-hour window.
//
// The store is built for many short-lived concurrent processes
// sharing one database file: WAL mode, per-connection busy timeouts, a
// version-gated migration path serialized under BEGIN IMMEDIATE, and
// probabilistic pruning of stale rows.
package state
