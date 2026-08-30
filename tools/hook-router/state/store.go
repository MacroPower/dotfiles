package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// The review_head_sha / review_wt_hash columns are vestigial: nothing
// reads them. They are retained for schema compatibility (the
// migrations list is append-only and dropping columns needs a table
// rebuild) and written only by [*Store.ResetSession]'s zeroing.
const schema = `
CREATE TABLE IF NOT EXISTS sessions (
    session_id      TEXT PRIMARY KEY,
    exit_plan_count INTEGER NOT NULL DEFAULT 0,
    plan_path       TEXT NOT NULL DEFAULT '',
    base_sha        TEXT NOT NULL DEFAULT '',
    review_head_sha TEXT NOT NULL DEFAULT '',
    review_wt_hash  TEXT NOT NULL DEFAULT '',
    in_plan_mode    INTEGER NOT NULL DEFAULT 0,
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
`

// migrations brings older databases forward to the current schema. Each
// statement runs individually; "duplicate column name" errors are
// silently ignored so the additive ALTERs on `sessions` remain
// re-runnable on databases pre-dating user_version.
//
// The slice is append-only: historical entries replay verbatim on every
// fresh open, and later steps may DROP+CREATE tables that earlier steps
// created. Editing a past entry would change what fresh DBs walk
// through, breaking the equivalence between "upgrade from version N"
// and "walk every step from scratch".
//
// Any `pending_plans` DROP+CREATE step is destructive. Rows there are
// transient (60s freshness, 3600s TTL, 24h prune), so the only data
// lost at upgrade is any in-flight plan-accept handoff at that moment.
// Concurrent opens converge correctly because [*Store.ensureSchema]
// wraps the whole block in BEGIN IMMEDIATE and re-checks user_version
// after acquiring the write lock.
//
// `bash_failures`, `subagent_starts`, and `teammate_idle_blocks` are
// created with CREATE TABLE IF NOT EXISTS, which is re-runnable; the
// "duplicate column name" guard only matches ALTER, so it does not
// apply there. Concurrent migrators are still serialized by the
// BEGIN IMMEDIATE wrapper in [*Store.ensureSchema].
var migrations = []string{
	`ALTER TABLE sessions ADD COLUMN review_head_sha TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN review_wt_hash TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN in_plan_mode INTEGER NOT NULL DEFAULT 0`,
	`DROP TABLE IF EXISTS pending_plans`,
	`CREATE TABLE IF NOT EXISTS pending_plans (
	    cwd        TEXT NOT NULL,
	    claude_pid TEXT NOT NULL,
	    plan_path  TEXT NOT NULL DEFAULT '',
	    base_sha   TEXT NOT NULL DEFAULT '',
	    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
	    PRIMARY KEY (cwd, claude_pid)
	)`,
	// bash_failures: commands whose PostToolUse payload signalled failure.
	// WARNING: command/stdout/stderr can contain secrets (URLs with tokens,
	// env vars echoed by shells). The DB stays local; treat it accordingly.
	// stderr is kept verbatim (tail-biased 16 KiB) for human analysis only;
	// the recorder never re-reads it to decide whether a command failed.
	// Failure precedence: is_error -> interrupted -> exit_code.
	`CREATE TABLE IF NOT EXISTS bash_failures (
	    id              INTEGER PRIMARY KEY,
	    session_id      TEXT NOT NULL,
	    transcript_path TEXT NOT NULL DEFAULT '',
	    hook_event_name TEXT NOT NULL DEFAULT '',
	    cwd             TEXT NOT NULL,
	    command         TEXT NOT NULL,
	    stdout          TEXT NOT NULL DEFAULT '',
	    stderr          TEXT NOT NULL DEFAULT '',
	    is_error        INTEGER NOT NULL DEFAULT 0,
	    interrupted     INTEGER NOT NULL DEFAULT 0,
	    exit_code       INTEGER,
	    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
	)`,
	`CREATE INDEX IF NOT EXISTS bash_failures_created_at_idx
	    ON bash_failures(created_at)`,
	`DROP TABLE IF EXISTS pending_plans`,
	`CREATE TABLE IF NOT EXISTS pending_plans (
	    claude_pid TEXT PRIMARY KEY,
	    plan_path  TEXT NOT NULL DEFAULT '',
	    base_sha   TEXT NOT NULL DEFAULT '',
	    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,
	`ALTER TABLE sessions ADD COLUMN last_compact_trigger TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN last_compact_at TEXT NOT NULL DEFAULT ''`,
	// subagent_starts: one row per Agent-tool spawn, recorded by the
	// SubagentStart hook. Nothing reads it yet; it is the record the
	// plan gate will consult once it detects a real plan-reviewer spawn
	// instead of counting ExitPlanMode calls.
	`CREATE TABLE IF NOT EXISTS subagent_starts (
	    id         INTEGER PRIMARY KEY,
	    session_id TEXT NOT NULL,
	    agent_id   TEXT NOT NULL DEFAULT '',
	    agent_type TEXT NOT NULL DEFAULT '',
	    created_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`,
	`CREATE INDEX IF NOT EXISTS subagent_starts_created_at_idx
	    ON subagent_starts(created_at)`,
	// teammate_idle_blocks: one row per teammate the TeammateIdle gate
	// has already blocked. TeammateIdle carries no stop_hook_active
	// equivalent, so the row bounds the gate to one block per teammate
	// and keeps a teammate that cannot clear the plan itself from
	// looping. Keying on teammate_name rather than session_id alone
	// gates every teammate, not just the first one to go idle.
	`CREATE TABLE IF NOT EXISTS teammate_idle_blocks (
	    session_id    TEXT NOT NULL,
	    teammate_name TEXT NOT NULL,
	    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
	    PRIMARY KEY (session_id, teammate_name)
	)`,
}

const (
	busyTimeoutMs            = 30000
	bashFailureRetentionDays = 30
)

// SchemaVersion is the PRAGMA user_version a fully migrated database
// reports. [Open] migrates older databases forward to it.
const SchemaVersion = 8

// Store manages plan-guard session state in a SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies
// the schema. Concurrency settings are passed in the DSN so every pooled
// connection inherits them (busy_timeout is per-connection). WAL and
// synchronous=NORMAL are the standard pairing for concurrent readers and
// a single writer at a time. The context bounds ping and schema setup.
func Open(ctx context.Context, path string) (*Store, error) {
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

// ensureSchema creates the schema and runs migrations on a fresh or
// out-of-date database. On an already-current database it is a cheap
// no-op (one PRAGMA read). The version gate keeps the hot path free of
// DDL writes under concurrent load.
//
// Concurrency: when N processes race to open the same out-of-date
// database, the slow path runs inside BEGIN IMMEDIATE, so exactly one
// process holds the RESERVED write lock at a time. After acquiring the
// lock the migrator re-reads user_version through the same connection;
// if a peer already bumped it, the migrator commits without touching
// the schema. The re-check is mandatory because any destructive
// `pending_plans` step drops the table, and without it a late waiter
// would race against rows the winner just wrote.
//
// busy_timeout (set per-connection via the DSN) bounds how long
// BEGIN IMMEDIATE waits for the lock. Under contention the timeout
// surfaces as SQLITE_BUSY, which propagates up as an open failure.
func (s *Store) ensureSchema(ctx context.Context) error {
	var version int

	err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version)
	if err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	if version == SchemaVersion {
		return nil
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquiring migration conn: %w", err)
	}
	defer conn.Close()

	_, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE")
	if err != nil {
		return fmt.Errorf("beginning migration transaction: %w", err)
	}

	committed := false

	defer func() {
		if committed {
			return
		}

		// Connection-scoped rollback; the conn is closing via the
		// deferred conn.Close above, so a failed ROLLBACK has nowhere
		// to surface anyway.
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
	}()

	err = conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version)
	if err != nil {
		return fmt.Errorf("rereading schema version: %w", err)
	}

	if version == SchemaVersion {
		_, err = conn.ExecContext(ctx, "COMMIT")
		if err != nil {
			return fmt.Errorf("committing no-op migration: %w", err)
		}

		committed = true

		return nil
	}

	_, err = conn.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	for _, m := range migrations {
		_, err = conn.ExecContext(ctx, m)
		if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("running migration: %w", err)
		}
	}

	// PRAGMA does not accept bound parameters; the version constant is a
	// trusted int, so string interpolation is safe here.
	_, err = conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion))
	if err != nil {
		return fmt.Errorf("setting schema version: %w", err)
	}

	_, err = conn.ExecContext(ctx, "COMMIT")
	if err != nil {
		return fmt.Errorf("committing migration: %w", err)
	}

	committed = true

	return nil
}

// MaybePruneStale runs the 24-hour cleanup with ~5% probability per
// invocation. The probabilistic gate spreads cleanup writes across
// invocations so N concurrent processes don't all contend on the write
// lock at startup. Returns ran=true when the gate passed and
// [*Store.PruneStale] was invoked, plus any error from the prune.
func (s *Store) MaybePruneStale(ctx context.Context) (bool, error) {
	// Probabilistic gate, not a security-sensitive choice; weak RNG is fine.
	if rand.IntN(20) != 0 { //nolint:gosec // statistical cleanup gate
		return false, nil
	}

	return true, s.PruneStale(ctx)
}

// PruneStale removes stale rows: `sessions`, `pending_plans`,
// `subagent_starts`, and `teammate_idle_blocks` past 24 hours, and
// `bash_failures` past [bashFailureRetentionDays] (the failure history
// is kept longer than session state on purpose, since analysis tools
// may want to look back across many sessions). The three session-scoped
// tables share the sessions window because nothing outside the session
// that wrote them reads them. The deterministic entry point behind
// [*Store.MaybePruneStale], for callers (and tests) that want cleanup
// without the probabilistic gate.
func (s *Store) PruneStale(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE updated_at < datetime('now', '-24 hours')`)
	if err != nil {
		return fmt.Errorf("pruning stale sessions: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`DELETE FROM pending_plans WHERE updated_at < datetime('now', '-24 hours')`)
	if err != nil {
		return fmt.Errorf("pruning stale pending plans: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`DELETE FROM subagent_starts WHERE created_at < datetime('now', '-24 hours')`)
	if err != nil {
		return fmt.Errorf("pruning stale subagent starts: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`DELETE FROM teammate_idle_blocks WHERE created_at < datetime('now', '-24 hours')`)
	if err != nil {
		return fmt.Errorf("pruning stale teammate idle blocks: %w", err)
	}

	// bashFailureRetentionDays is a trusted int constant; SQLite's
	// datetime() accepts '-N days' as a relative modifier (same family
	// as the '-24 hours' modifier above).
	_, err = s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM bash_failures WHERE created_at < datetime('now', '-%d days')`,
			bashFailureRetentionDays))
	if err != nil {
		return fmt.Errorf("pruning stale bash failures: %w", err)
	}

	return nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database handle. It is an escape hatch for
// inspection and maintenance (analysis queries over bash_failures, test
// assertions against raw rows); session-lifecycle writes should go
// through the typed methods so their UPSERT and timestamp semantics
// hold.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Session returns the state for a session, creating it if it does not exist.
func (s *Store) Session(ctx context.Context, id string) (exitPlanCount int, planPath string, baseSHA string, err error) {
	_, err = s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO sessions (session_id) VALUES (?)`, id)
	if err != nil {
		return 0, "", "", fmt.Errorf("ensuring session: %w", err)
	}

	err = s.db.QueryRowContext(ctx,
		`SELECT exit_plan_count, plan_path, base_sha FROM sessions WHERE session_id = ?`, id).
		Scan(&exitPlanCount, &planPath, &baseSHA)
	if err != nil {
		return 0, "", "", fmt.Errorf("querying session: %w", err)
	}

	return exitPlanCount, planPath, baseSHA, nil
}

// IncrementExitPlanCount atomically increments the counter and returns
// the new value.
func (s *Store) IncrementExitPlanCount(ctx context.Context, id string) (int, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_id, exit_plan_count)
		 VALUES (?, 1)
		 ON CONFLICT(session_id) DO UPDATE SET
		   exit_plan_count = exit_plan_count + 1,
		   updated_at = datetime('now')`, id)
	if err != nil {
		return 0, fmt.Errorf("incrementing exit_plan_count: %w", err)
	}

	var count int

	err = s.db.QueryRowContext(ctx,
		`SELECT exit_plan_count FROM sessions WHERE session_id = ?`, id).
		Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("reading exit_plan_count: %w", err)
	}

	return count, nil
}

// SetPlanPath records the plan path and base SHA for a session.
func (s *Store) SetPlanPath(ctx context.Context, id, planPath, baseSHA string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_id, plan_path, base_sha)
		 VALUES (?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		   plan_path = excluded.plan_path,
		   base_sha = excluded.base_sha,
		   updated_at = datetime('now')`, id, planPath, baseSHA)
	if err != nil {
		return fmt.Errorf("setting plan path: %w", err)
	}

	return nil
}

// ResetSession clears plan state for a session (used on EnterPlanMode).
//
// in_plan_mode is reset to 0 along with the other columns so the row is
// returned to a clean baseline. EnterPlanMode follows ResetSession with
// an explicit [*Store.SetInPlanMode] call to flip the bit on, keeping
// the bit's lifecycle owned by EnterPlanMode rather than coupling it
// into ResetSession.
//
// The session's `teammate_idle_blocks` rows go too, so a new plan cycle
// re-arms the TeammateIdle gate for every teammate it already blocked.
func (s *Store) ResetSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_id)
		 VALUES (?)
		 ON CONFLICT(session_id) DO UPDATE SET
		   exit_plan_count = 0,
		   plan_path = '',
		   base_sha = '',
		   -- vestigial columns, zeroed for row consistency, read nowhere
		   review_head_sha = '',
		   review_wt_hash = '',
		   in_plan_mode = 0,
		   updated_at = datetime('now')`, id)
	if err != nil {
		return fmt.Errorf("resetting session: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`DELETE FROM teammate_idle_blocks WHERE session_id = ?`, id)
	if err != nil {
		return fmt.Errorf("resetting teammate idle blocks: %w", err)
	}

	return nil
}

// SetInPlanMode records whether the session is currently inside an
// EnterPlanMode/ExitPlanMode bracket. The Stop hook reads this bit to
// block Stop while plan-mode is open.
func (s *Store) SetInPlanMode(ctx context.Context, id string, inPlanMode bool) error {
	v := 0
	if inPlanMode {
		v = 1
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_id, in_plan_mode)
		 VALUES (?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		   in_plan_mode = excluded.in_plan_mode,
		   updated_at = datetime('now')`, id, v)
	if err != nil {
		return fmt.Errorf("setting in_plan_mode: %w", err)
	}

	return nil
}

// InPlanMode reports whether the given session is currently inside an
// EnterPlanMode/ExitPlanMode bracket. Returns false (not an error)
// when the session row does not exist.
func (s *Store) InPlanMode(ctx context.Context, id string) (bool, error) {
	var v int

	err := s.db.QueryRowContext(ctx,
		`SELECT in_plan_mode FROM sessions WHERE session_id = ?`, id).
		Scan(&v)
	if err == sql.ErrNoRows {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("querying in_plan_mode: %w", err)
	}

	return v != 0, nil
}

// ClearSession removes a session entirely, including the
// `teammate_idle_blocks` rows it owns. An answered post-impl question
// clears the session, which re-arms the TeammateIdle gate for the next
// plan cycle.
func (s *Store) ClearSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE session_id = ?`, id)
	if err != nil {
		return fmt.Errorf("clearing session: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`DELETE FROM teammate_idle_blocks WHERE session_id = ?`, id)
	if err != nil {
		return fmt.Errorf("clearing teammate idle blocks: %w", err)
	}

	return nil
}

// SetPendingPlan UPSERTs a pending plan handoff keyed by claudePID.
// The row is consumed by [*Store.ConsumePendingPlan] when the cleared
// session's SessionStart hook fires.
// The PID is the OS-process identity of the Claude Code window, so the
// key partitions handoffs per window: two windows each own their own
// row and cannot overwrite each other.
//
// The first return value reports whether an existing row was overwritten
// while its `updated_at` was within 60 seconds. This only fires when
// the same window calls ExitPlanMode twice inside the freshness window
// without consuming the previous handoff (e.g. the user dismissed the
// accept dialog and re-planned).
//
// The freshness check uses two queries (SELECT then UPSERT). The race
// window between them is benign: a missed signal has no correctness
// impact.
func (s *Store) SetPendingPlan(ctx context.Context, claudePID, planPath, baseSHA string) (bool, error) {
	var fresh int

	err := s.db.QueryRowContext(ctx,
		`SELECT updated_at >= datetime('now', '-60 seconds')
		 FROM pending_plans WHERE claude_pid = ?`, claudePID).Scan(&fresh)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("checking pending plan freshness: %w", err)
	}

	overwroteFresh := err == nil && fresh != 0

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO pending_plans (claude_pid, plan_path, base_sha)
		 VALUES (?, ?, ?)
		 ON CONFLICT(claude_pid) DO UPDATE SET
		   plan_path = excluded.plan_path,
		   base_sha = excluded.base_sha,
		   updated_at = datetime('now')`, claudePID, planPath, baseSHA)
	if err != nil {
		return false, fmt.Errorf("setting pending plan: %w", err)
	}

	return overwroteFresh, nil
}

// ConsumePendingPlan reads and deletes the pending plan for claudePID
// in a single statement, but only when the row's `updated_at` is within
// ttlSeconds. Stale rows are left in place for [*Store.MaybePruneStale]
// to remove on the 24-hour cycle. The PID key scopes consumption to the
// calling window, so a peer window's row is not touched.
//
// The atomic read+delete relies on `MaxOpenConns=1`: DELETE...RETURNING
// is one statement, intra-process serialization comes from the pool
// limit, and inter-process serialization comes from SQLite's write lock
// (busy_timeout). Raising MaxOpenConns to N>1 would still keep
// per-statement atomicity intact.
//
// The third return value reports whether a fresh row matched and was
// deleted; false (with nil err) means no fresh row was present. Callers
// treat not-found as the no-migration path.
func (s *Store) ConsumePendingPlan(
	ctx context.Context,
	claudePID string,
	ttlSeconds int,
) (string, string, bool, error) {
	query := fmt.Sprintf(
		`DELETE FROM pending_plans
		 WHERE claude_pid = ? AND updated_at >= datetime('now', '-%d seconds')
		 RETURNING plan_path, base_sha`, ttlSeconds)

	var planPath, baseSHA string

	err := s.db.QueryRowContext(ctx, query, claudePID).Scan(&planPath, &baseSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}

	if err != nil {
		return "", "", false, fmt.Errorf("consuming pending plan: %w", err)
	}

	return planPath, baseSHA, true, nil
}

// DeletePendingPlan removes the pending plan row for claudePID, if any.
// Used as best-effort cleanup at lifecycle boundaries where the handoff
// is no longer needed. Only the calling window's row is touched; peer
// windows' rows are left intact.
func (s *Store) DeletePendingPlan(ctx context.Context, claudePID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pending_plans WHERE claude_pid = ?`, claudePID)
	if err != nil {
		return fmt.Errorf("deleting pending plan: %w", err)
	}

	return nil
}

// boolToInt maps a Go bool to 0/1. modernc.org/sqlite does not
// auto-convert booleans, so the call sites have to do it.
func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

// BashFailure is the input shape for [*Store.RecordBashFailure].
// ExitCode is a pointer because Claude Code does not always include
// exit_code on the tool_response payload; nil maps to SQL NULL.
type BashFailure struct {
	SessionID      string
	TranscriptPath string
	HookEventName  string
	Cwd            string
	Command        string
	Stdout         string
	Stderr         string
	IsError        bool
	Interrupted    bool
	ExitCode       *int
}

// RecordBashFailure appends a row to bash_failures. The failure
// classification is the caller's job; this method just writes whatever
// it is handed.
func (s *Store) RecordBashFailure(ctx context.Context, f BashFailure) error {
	var exitCode any
	if f.ExitCode != nil {
		exitCode = *f.ExitCode
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO bash_failures (
		    session_id, transcript_path, hook_event_name, cwd, command,
		    stdout, stderr, is_error, interrupted, exit_code
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.SessionID, f.TranscriptPath, f.HookEventName, f.Cwd, f.Command,
		f.Stdout, f.Stderr, boolToInt(f.IsError), boolToInt(f.Interrupted), exitCode)
	if err != nil {
		return fmt.Errorf("recording bash failure: %w", err)
	}

	return nil
}

// SubagentStart is the input shape for [*Store.RecordSubagentStart].
type SubagentStart struct {
	SessionID string
	AgentID   string
	AgentType string
}

// RecordSubagentStart appends a row to subagent_starts, one per Agent
// tool spawn the SubagentStart hook observed. Rows are append-only and
// pruned with the rest of the session state by [*Store.PruneStale].
func (s *Store) RecordSubagentStart(ctx context.Context, a SubagentStart) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO subagent_starts (session_id, agent_id, agent_type)
		 VALUES (?, ?, ?)`,
		a.SessionID, a.AgentID, a.AgentType)
	if err != nil {
		return fmt.Errorf("recording subagent start: %w", err)
	}

	return nil
}

// MarkTeammateIdleBlocked claims the one TeammateIdle block a teammate
// gets per plan cycle. It reports true when this call claimed the
// block and false when the teammate was already blocked once.
//
// The claim is a single INSERT OR IGNORE, so two teammates racing on
// the same key cannot both read true. [*Store.ClearSession] and
// [*Store.ResetSession] drop the rows, which is what re-arms the gate
// for the next plan cycle.
func (s *Store) MarkTeammateIdleBlocked(ctx context.Context, id, teammate string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO teammate_idle_blocks (session_id, teammate_name)
		 VALUES (?, ?)`, id, teammate)
	if err != nil {
		return false, fmt.Errorf("marking teammate idle block: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reading teammate idle block claim: %w", err)
	}

	return n > 0, nil
}

// RecordCompaction stamps the session row with the compaction trigger
// ("manual" or "auto") and the time it fired. The marker is
// observational: nothing reads it yet, and the plan-guard state it sits
// beside already survives compaction because it lives in SQLite rather
// than in the transcript.
func (s *Store) RecordCompaction(ctx context.Context, id, trigger string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_id, last_compact_trigger, last_compact_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(session_id) DO UPDATE SET
		   last_compact_trigger = excluded.last_compact_trigger,
		   last_compact_at = excluded.last_compact_at,
		   updated_at = datetime('now')`, id, trigger)
	if err != nil {
		return fmt.Errorf("recording compaction: %w", err)
	}

	return nil
}
