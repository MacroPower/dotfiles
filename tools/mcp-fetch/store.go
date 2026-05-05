package main

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS fetches (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              TEXT    NOT NULL DEFAULT (datetime('now')),
    url             TEXT    NOT NULL,
    host            TEXT    NOT NULL DEFAULT '',
    outcome         TEXT    NOT NULL,
    status_code     INTEGER NOT NULL DEFAULT 0,
    content_type    TEXT    NOT NULL DEFAULT '',
    response_bytes  INTEGER NOT NULL DEFAULT 0,
    output_bytes    INTEGER NOT NULL DEFAULT 0,
    raw_mode        INTEGER NOT NULL DEFAULT 0,
    max_length      INTEGER NOT NULL DEFAULT 0,
    start_index     INTEGER NOT NULL DEFAULT 0,
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    cache_hit       INTEGER NOT NULL DEFAULT 0,
    truncated       INTEGER NOT NULL DEFAULT 0,
    error           TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_fetches_ts          ON fetches(ts);
CREATE INDEX IF NOT EXISTS idx_fetches_host        ON fetches(host);
CREATE INDEX IF NOT EXISTS idx_fetches_outcome_ts  ON fetches(outcome, ts);
`

const (
	busyTimeoutMs = 30000
	schemaVersion = 1

	// pruneAgeDays bounds the on-disk history. Long-lived servers prune
	// once on startup; rows older than this are deleted on the next
	// process start. mcp-fetch retains user-visible history at 90d
	// where hook-router prunes ephemeral session state at 24h.
	pruneAgeDays = 90
)

// Outcome values stored in the `outcome` column. Each branch of
// [*fetchHandler.handle] sets exactly one of these.
const (
	OutcomeOK            = "ok"
	OutcomeDenied        = "denied"
	OutcomeRobotsDenied  = "robots_denied"
	OutcomeHTTPError     = "http_error"
	OutcomeFetchError    = "fetch_error"
	OutcomeInvalidURL    = "invalid_url"
	OutcomeInternalError = "internal_error"
)

// FetchRecord is the metadata persisted for one fetch attempt.
//
// `RawMode`, `CacheHit`, and `Truncated` are stored as 0/1 because
// modernc.org/sqlite has no automatic bool conversion; [boolToInt]
// keeps the conversion explicit.
//
// Cache-hit rows have `StatusCode=0`, `ContentType=""`, and
// `ResponseBytes=0`. The pair `cache_hit=1 AND status_code=0` is the
// "no fresh HTTP" marker. Transport errors (no response object) also
// have `StatusCode=0`.
type FetchRecord struct {
	Timestamp     time.Time
	URL           string
	Host          string
	Outcome       string
	Error         string
	ContentType   string
	ResponseBytes int
	OutputBytes   int
	RawMode       int
	MaxLength     int
	StartIndex    int
	DurationMs    int64
	CacheHit      int
	Truncated     int
	StatusCode    int
}

// Store manages the SQLite-backed fetch history.
type Store struct {
	db *sql.DB
}

// OpenStore opens (or creates) the SQLite database at path and applies
// the schema. Concurrency settings are passed in the DSN so every pooled
// connection inherits them (busy_timeout is per-connection). WAL and
// synchronous=NORMAL are the standard pairing for concurrent readers and
// a single writer at a time. The context bounds ping and schema setup.
func OpenStore(ctx context.Context, path string) (*Store, error) {
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return nil, fmt.Errorf("creating store directory: %w", err)
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)",
		path, busyTimeoutMs,
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// MaxOpen=1 serializes intra-process writes on the Go connection
	// mutex; inter-process contention is handled by the DSN busy_timeout.
	// MaxIdle=1 is defensive — with MaxOpen=1 the pool can't hold more
	// idle connections anyway, but it makes the intent explicit.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	err = db.PingContext(ctx)
	if err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("pinging database: %w (close: %w)", err, closeErr)
		}

		return nil, fmt.Errorf("pinging database: %w", err)
	}

	s := &Store{db: db}

	err = s.ensureSchema(ctx)
	if err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return nil, fmt.Errorf("%w (close: %w)", err, closeErr)
		}

		return nil, err
	}

	return s, nil
}

// ensureSchema creates the schema on a fresh or out-of-date database
// and is a cheap no-op (one PRAGMA read) on an already-current
// database. The schema version gate keeps the hot path free of DDL
// writes under concurrent load.
//
// Cold-start race: when N processes open a fresh database concurrently,
// all observe user_version == 0 and run CREATE TABLE IF NOT EXISTS plus
// CREATE INDEX IF NOT EXISTS. The DDL is safe to race on. The final
// PRAGMA user_version write serializes on SQLite's write lock under
// the busy_timeout window, so readers see the version flip atomically
// and converge on schemaVersion.
func (s *Store) ensureSchema(ctx context.Context) error {
	var version int

	err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version)
	if err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	if version == schemaVersion {
		return nil
	}

	_, err = s.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	// PRAGMA does not accept bound parameters; the version constant is a
	// trusted int, so string interpolation is safe here.
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
	if err != nil {
		return fmt.Errorf("setting schema version: %w", err)
	}

	return nil
}

// MaybePruneStale runs the [pruneAgeDays]-day cleanup with ~5%
// probability per invocation. The probabilistic gate spreads cleanup
// writes across invocations so concurrent processes don't all contend
// on the write lock. Returns ran=true when the gate passed and
// [*Store.pruneStale] was invoked, plus any error from the prune.
//
// The MCP server is one process per Claude session, so this only
// fires on startup. Multi-day sessions accumulate stale rows until
// the next restart.
func (s *Store) MaybePruneStale(ctx context.Context) (bool, error) {
	// Probabilistic gate, not a security-sensitive choice; weak RNG is fine.
	if rand.IntN(20) != 0 { //nolint:gosec // statistical cleanup gate
		return false, nil
	}

	return true, s.pruneStale(ctx)
}

// pruneStale deletes `fetches` rows older than [pruneAgeDays] days.
// Split out from [*Store.MaybePruneStale] so tests can exercise the
// cleanup path directly without fighting the probabilistic gate.
func (s *Store) pruneStale(ctx context.Context) error {
	cutoff := fmt.Sprintf("-%d days", pruneAgeDays)

	_, err := s.db.ExecContext(ctx,
		`DELETE FROM fetches WHERE ts < datetime('now', ?)`, cutoff)
	if err != nil {
		return fmt.Errorf("pruning stale fetches: %w", err)
	}

	return nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	err := s.db.Close()
	if err != nil {
		return fmt.Errorf("closing database: %w", err)
	}

	return nil
}

// Record inserts one row for a completed fetch attempt. Errors are
// returned to the caller for logging at slog Warn; the caller MUST NOT
// propagate them to the MCP user - the fetch already succeeded from
// their point of view.
func (s *Store) Record(ctx context.Context, r FetchRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO fetches (
			url, host, outcome, status_code, content_type,
			response_bytes, output_bytes, raw_mode, max_length,
			start_index, duration_ms, cache_hit, truncated, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.URL, r.Host, r.Outcome, r.StatusCode, r.ContentType,
		r.ResponseBytes, r.OutputBytes, r.RawMode, r.MaxLength,
		r.StartIndex, r.DurationMs, r.CacheHit, r.Truncated, r.Error,
	)
	if err != nil {
		return fmt.Errorf("recording fetch: %w", err)
	}

	return nil
}

// SummaryStats is the aggregated view of the `fetches` table within a
// time window. Used by the `stats` subcommand.
type SummaryStats struct {
	Earliest           time.Time
	Latest             time.Time
	OutcomeCounts      map[string]int
	Total              int
	TotalResponseBytes int64
	TotalOutputBytes   int64
	AvgDurationMs      float64
	CacheHits          int
}

// HostCount is one row of the top-hosts breakdown.
type HostCount struct {
	Host  string
	Count int
}

// sinceClause returns a `ts >= ?` SQL fragment plus the formatted
// argument when since is non-zero, or empty fragment plus nil when
// since is zero ("all time"). The UTC + [time.DateTime] formatting
// is shared by every query that filters on `ts`.
func sinceClause(since time.Time) (string, []any) {
	if since.IsZero() {
		return "", nil
	}

	return "ts >= ?", []any{since.UTC().Format(time.DateTime)}
}

// Summary aggregates the entire `fetches` table over rows where
// `ts >= since`. A zero `since` means "all time".
func (s *Store) Summary(ctx context.Context, since time.Time) (SummaryStats, error) {
	out := SummaryStats{OutcomeCounts: map[string]int{}}

	clause, args := sinceClause(since)

	where := ""
	if clause != "" {
		where = " WHERE " + clause
	}

	var (
		earliest, latest sql.NullString
		avgDuration      sql.NullFloat64
	)

	err := s.db.QueryRowContext(ctx,
		`SELECT
			COUNT(*),
			MIN(ts), MAX(ts),
			COALESCE(SUM(response_bytes), 0),
			COALESCE(SUM(output_bytes), 0),
			AVG(duration_ms),
			COALESCE(SUM(cache_hit), 0)
		FROM fetches`+where, args...).Scan(
		&out.Total, &earliest, &latest,
		&out.TotalResponseBytes, &out.TotalOutputBytes,
		&avgDuration, &out.CacheHits,
	)
	if err != nil {
		return SummaryStats{}, fmt.Errorf("querying summary: %w", err)
	}

	if earliest.Valid {
		if t, parseErr := time.Parse(time.DateTime, earliest.String); parseErr == nil {
			out.Earliest = t
		}
	}

	if latest.Valid {
		if t, parseErr := time.Parse(time.DateTime, latest.String); parseErr == nil {
			out.Latest = t
		}
	}

	if avgDuration.Valid {
		out.AvgDurationMs = avgDuration.Float64
	}

	counts, err := s.OutcomeCounts(ctx, since)
	if err != nil {
		return SummaryStats{}, err
	}

	out.OutcomeCounts = counts

	return out, nil
}

// OutcomeCounts groups rows by outcome within the window.
func (s *Store) OutcomeCounts(ctx context.Context, since time.Time) (map[string]int, error) {
	clause, args := sinceClause(since)

	where := ""
	if clause != "" {
		where = " WHERE " + clause
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT outcome, COUNT(*) FROM fetches`+where+` GROUP BY outcome`, args...)
	if err != nil {
		return nil, fmt.Errorf("querying outcome counts: %w", err)
	}

	defer func() { _ = rows.Close() }()

	out := map[string]int{}

	for rows.Next() {
		var (
			outcome string
			count   int
		)

		err = rows.Scan(&outcome, &count)
		if err != nil {
			return nil, fmt.Errorf("scanning outcome count: %w", err)
		}

		out[outcome] = count
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterating outcome counts: %w", err)
	}

	return out, nil
}

// TopHosts returns the top `limit` hosts by row count within the
// window, descending. Empty hosts (e.g. invalid_url rows) are
// excluded.
func (s *Store) TopHosts(ctx context.Context, since time.Time, limit int) ([]HostCount, error) {
	if limit <= 0 {
		limit = 10
	}

	clause, args := sinceClause(since)

	where := " WHERE host != ''"
	if clause != "" {
		where += " AND " + clause
	}

	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx,
		`SELECT host, COUNT(*) AS c FROM fetches`+where+
			` GROUP BY host ORDER BY c DESC, host ASC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("querying top hosts: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var out []HostCount

	for rows.Next() {
		var hc HostCount

		err = rows.Scan(&hc.Host, &hc.Count)
		if err != nil {
			return nil, fmt.Errorf("scanning host count: %w", err)
		}

		out = append(out, hc)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterating top hosts: %w", err)
	}

	return out, nil
}

// RecentFetches returns the most recent `limit` rows ordered newest
// first. Used by the `stats --last N` tail.
func (s *Store) RecentFetches(ctx context.Context, limit int) ([]FetchRecord, error) {
	if limit <= 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT ts, url, host, outcome, status_code, content_type,
			response_bytes, output_bytes, raw_mode, max_length,
			start_index, duration_ms, cache_hit, truncated, error
		FROM fetches ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("querying recent fetches: %w", err)
	}

	defer func() { _ = rows.Close() }()

	out := make([]FetchRecord, 0, limit)

	for rows.Next() {
		var (
			r  FetchRecord
			ts string
		)

		err = rows.Scan(
			&ts, &r.URL, &r.Host, &r.Outcome, &r.StatusCode,
			&r.ContentType, &r.ResponseBytes, &r.OutputBytes,
			&r.RawMode, &r.MaxLength, &r.StartIndex, &r.DurationMs,
			&r.CacheHit, &r.Truncated, &r.Error,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning recent fetch: %w", err)
		}

		if t, parseErr := time.Parse(time.DateTime, ts); parseErr == nil {
			r.Timestamp = t
		}

		out = append(out, r)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterating recent fetches: %w", err)
	}

	return out, nil
}

// boolToInt maps a Go bool to the 0/1 representation used in the
// schema. modernc.org/sqlite does not auto-convert booleans.
func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// parseSinceFlag parses the `--since` flag for the stats subcommand.
// `time.ParseDuration` does not accept a `d` unit, so values matching
// exactly `^[0-9]+d$` are rewritten to `<N>*24h` before parsing.
// Anything else - `1.5d`, `7d12h`, `7D` - is passed straight to
// [time.ParseDuration] and surfaces its native parse error.
func parseSinceFlag(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	if isIntegerDays(s) {
		days, err := time.ParseDuration(s[:len(s)-1] + "h")
		if err != nil {
			return 0, fmt.Errorf("parsing days: %w", err)
		}

		return days * 24, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("parsing duration: %w", err)
	}

	return d, nil
}

func isIntegerDays(s string) bool {
	if len(s) < 2 || s[len(s)-1] != 'd' {
		return false
	}

	for _, r := range s[:len(s)-1] {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
