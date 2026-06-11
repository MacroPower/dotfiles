// Package store persists one row per fetch attempt in SQLite and serves
// the aggregate queries behind the `stats` report.
//
// The schema, the [Outcome] vocabulary written to the `outcome` column,
// and the [FetchRecord] data transfer object live here so producers (the
// fetch handler) and consumers (the stats reporter) share one source of
// truth. The store targets one short-lived process per session: WAL
// mode, a per-connection busy timeout, a version-gated schema check, and
// probabilistic pruning of rows past their retention window.
package store
