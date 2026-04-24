package main

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// The review_head_sha / review_wt_hash columns now hold the
// fingerprint captured when the user confirms post-implementation
// agents via AskUserQuestion. The names predate the rename and
// are kept to avoid schema churn.
const schema = `
CREATE TABLE IF NOT EXISTS sessions (
    session_id      TEXT PRIMARY KEY,
    exit_plan_count INTEGER NOT NULL DEFAULT 0,
    plan_path       TEXT NOT NULL DEFAULT '',
    base_sha        TEXT NOT NULL DEFAULT '',
    review_head_sha TEXT NOT NULL DEFAULT '',
    review_wt_hash  TEXT NOT NULL DEFAULT '',
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
`

// migrations adds columns that may be missing in databases created before
// the current schema. Each statement is executed individually;
// "duplicate column name" errors are silently ignored so the set can be
// re-run on existing databases before the user_version was introduced.
var migrations = []string{
	`ALTER TABLE sessions ADD COLUMN review_head_sha TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN review_wt_hash TEXT NOT NULL DEFAULT ''`,
}

const (
	busyTimeoutMs = 30000
	schemaVersion = 2
)

// Store manages plan-guard session state in a SQLite database.
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

// ensureSchema creates the schema and runs migrations on a fresh or
// out-of-date database, and is a cheap no-op (one PRAGMA read) on an
// already-current database. The schema version gate keeps the hot path
// free of DDL writes under concurrent load.
//
// Cold-start race: when N processes open a fresh database concurrently,
// all observe user_version == 0 and run CREATE TABLE IF NOT EXISTS plus
// the idempotent ALTERs. The DDL is safe to race on and duplicate-column
// errors are filtered. The final PRAGMA user_version write serializes on
// SQLite's write lock under the busy_timeout window, so readers see the
// version flip atomically and converge on schemaVersion.
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

	for _, m := range migrations {
		_, err = s.db.ExecContext(ctx, m)
		if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("running migration: %w", err)
		}
	}

	// PRAGMA does not accept bound parameters; the version constant is a
	// trusted int, so string interpolation is safe here.
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
	if err != nil {
		return fmt.Errorf("setting schema version: %w", err)
	}

	return nil
}

// MaybePruneStale runs the 24-hour session cleanup with ~5% probability
// per invocation. The probabilistic gate spreads cleanup writes across
// invocations so N concurrent processes don't all contend on the write
// lock at startup. Returns ran=true when the gate passed and the DELETE
// was attempted, plus any error from the delete itself.
func (s *Store) MaybePruneStale(ctx context.Context) (bool, error) {
	// Probabilistic gate, not a security-sensitive choice; weak RNG is fine.
	if rand.IntN(20) != 0 { //nolint:gosec // statistical cleanup gate
		return false, nil
	}

	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE updated_at < datetime('now', '-24 hours')`)
	if err != nil {
		return true, fmt.Errorf("pruning stale sessions: %w", err)
	}

	return true, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
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

// IncrementExitPlanCount atomically increments the counter and returns the new value.
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

// SetAskFingerprint records the git state fingerprint captured when
// a post-implementation AskUserQuestion completes.
func (s *Store) SetAskFingerprint(ctx context.Context, id, headSHA, wtHash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_id, review_head_sha, review_wt_hash)
		 VALUES (?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		   review_head_sha = excluded.review_head_sha,
		   review_wt_hash = excluded.review_wt_hash,
		   updated_at = datetime('now')`, id, headSHA, wtHash)
	if err != nil {
		return fmt.Errorf("setting ask fingerprint: %w", err)
	}

	return nil
}

// AskFingerprint returns the stored git state fingerprint for a session,
// captured when a post-implementation AskUserQuestion completes.
// Returns empty strings when no fingerprint has been recorded.
func (s *Store) AskFingerprint(ctx context.Context, id string) (headSHA, wtHash string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT review_head_sha, review_wt_hash FROM sessions WHERE session_id = ?`, id).
		Scan(&headSHA, &wtHash)
	if err == sql.ErrNoRows {
		return "", "", nil
	}

	if err != nil {
		return "", "", fmt.Errorf("querying ask fingerprint: %w", err)
	}

	return headSHA, wtHash, nil
}

// ResetSession clears plan state for a session (used on EnterPlanMode).
func (s *Store) ResetSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_id)
		 VALUES (?)
		 ON CONFLICT(session_id) DO UPDATE SET
		   exit_plan_count = 0,
		   plan_path = '',
		   base_sha = '',
		   review_head_sha = '',
		   review_wt_hash = '',
		   updated_at = datetime('now')`, id)
	if err != nil {
		return fmt.Errorf("resetting session: %w", err)
	}

	return nil
}

// ClearSession removes a session entirely.
func (s *Store) ClearSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE session_id = ?`, id)
	if err != nil {
		return fmt.Errorf("clearing session: %w", err)
	}

	return nil
}
