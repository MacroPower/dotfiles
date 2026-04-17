package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

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
// the current schema. Each statement is executed individually; "duplicate
// column name" errors are silently ignored.
var migrations = []string{
	`ALTER TABLE sessions ADD COLUMN review_head_sha TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN review_wt_hash TEXT NOT NULL DEFAULT ''`,
}

// Store manages plan-guard session state in a SQLite database.
type Store struct {
	db *sql.DB
}

// OpenStore opens (or creates) the SQLite database at path and applies
// the schema. It enables WAL mode and a busy timeout for safe concurrent
// access, and prunes sessions older than 24 hours.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating store directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()

			return nil, fmt.Errorf("setting %s: %w", pragma, err)
		}
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()

		return nil, fmt.Errorf("creating schema: %w", err)
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			// Ignore "duplicate column name" errors from re-running migrations.
			if !strings.Contains(err.Error(), "duplicate column name") {
				db.Close()

				return nil, fmt.Errorf("running migration: %w", err)
			}
		}
	}

	// Clean up stale sessions (>24h).
	_, _ = db.Exec(`DELETE FROM sessions WHERE updated_at < datetime('now', '-24 hours')`)

	return &Store{db: db}, nil
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

// SetReviewFingerprint records the git state fingerprint captured when
// a reviewer agent is spawned.
func (s *Store) SetReviewFingerprint(ctx context.Context, id, headSHA, wtHash string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (session_id, review_head_sha, review_wt_hash)
		 VALUES (?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		   review_head_sha = excluded.review_head_sha,
		   review_wt_hash = excluded.review_wt_hash,
		   updated_at = datetime('now')`, id, headSHA, wtHash)
	if err != nil {
		return fmt.Errorf("setting review fingerprint: %w", err)
	}

	return nil
}

// ReviewFingerprint returns the stored git state fingerprint for a session.
// Returns empty strings when no fingerprint has been recorded.
func (s *Store) ReviewFingerprint(ctx context.Context, id string) (headSHA, wtHash string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT review_head_sha, review_wt_hash FROM sessions WHERE session_id = ?`, id).
		Scan(&headSHA, &wtHash)
	if err == sql.ErrNoRows {
		return "", "", nil
	}

	if err != nil {
		return "", "", fmt.Errorf("querying review fingerprint: %w", err)
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
