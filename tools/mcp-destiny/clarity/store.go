package clarity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/cachefile"
)

const (
	// recheck is how long a [*Store] serves its database after a check
	// of the version that succeeded.
	recheck = time.Hour

	// retry is how long a [*Store] waits after a check that did not
	// succeed. A site that does not answer therefore delays one tool
	// call in that time, and not each of them.
	retry = 5 * time.Minute

	// cacheFile names the cache file of a [*Store]. The digit changes
	// when the fields of [cacheContent] change, so a process never
	// decodes a stale layout.
	cacheFile = "clarity-v1.json"

	// cachePattern matches the cache file of every layout.
	cachePattern = "clarity-v*.json"
)

// ErrUnavailable marks the reason that a [*Store] has no database. The
// output of a tool prints the text of the error that wraps it after the
// word Clarity.
var ErrUnavailable = errors.New("descriptions unavailable")

// Source serves the two files of the Clarity site. [*Client] implements
// it.
type Source interface {
	Version(ctx context.Context) (string, error)
	Descriptions(ctx context.Context) ([]byte, error)
}

// State is the database that a [*Store] holds, or the reason that it
// holds none. Exactly one field is set, and Err wraps [ErrUnavailable].
type State struct {
	Database *Database
	Err      error
}

// StoreOption configures a [*Store] via [NewStore].
//
// Functions of this type:
//   - [WithClock]
//   - [WithLogger]
type StoreOption func(*Store)

// WithClock overrides the clock that schedules the checks of the
// version. It is a [StoreOption].
func WithClock(now func() time.Time) StoreOption {
	return func(s *Store) { s.now = now }
}

// WithLogger sets the logger for the checks that do not succeed and for
// cache failures. It is a [StoreOption].
func WithLogger(log *slog.Logger) StoreOption {
	return func(s *Store) { s.log = log }
}

// Store keeps the Clarity database of the process up to date.
//
// The site names the version of its descriptions file in a small versions
// file. The store downloads the descriptions file only when neither the
// store nor its cache file holds the version that the site names. It
// saves each download in the cache file, so a new process downloads only
// the versions file. The stores of several processes can share the cache
// directory, and each one reads the download of another.
//
// The store checks the version at most one time each hour. A check that
// does not succeed leaves the held database in place, and the store
// checks again after five minutes. Clarity is an optional source, so the
// store serves a database of an older version in place of none.
//
// The store has no lock, so the caller serializes the calls to Load.
type Store struct {
	source   Source
	now      func() time.Time
	log      *slog.Logger
	db       *Database
	err      error
	checked  time.Time
	cacheDir string
}

// NewStore returns a [*Store] that reads the Clarity site through source
// and keeps its cache file in cacheDir. An empty cacheDir turns the cache
// file off.
func NewStore(source Source, cacheDir string, opts ...StoreOption) *Store {
	s := &Store{
		source:   source,
		cacheDir: cacheDir,
		now:      time.Now,
		log:      slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Load returns the [State] of the store at the time of the call.
//
// The first call checks the version that the site publishes. A later
// call checks the version again when an hour passed after a check that
// succeeded, or five minutes after a check that did not. Every other
// call sends no request.
//
// A check that finds the version of the held database changes nothing.
// Otherwise the check reads the cache file, and it downloads the
// descriptions file when the cache file holds another version. A check
// that does not succeed keeps the held database, and a store without one
// then holds the database of the cache file. The state holds that
// database, and the store logs the reason. Without a database, the state
// holds the reason of the last check.
//
// A check that ends because the caller canceled ctx says nothing about
// the site. The store then keeps the time and the result of the check
// before it, so the next call checks again. The state of the canceled
// call holds the database that the store holds, or the reason of the
// canceled check.
func (s *Store) Load(ctx context.Context) State {
	now := s.now()

	wait := recheck
	if s.err != nil {
		wait = retry
	}

	if s.checked.IsZero() || now.Sub(s.checked) >= wait {
		err := s.refresh(ctx)
		if err != nil && ctx.Err() != nil {
			return s.state(err)
		}

		s.err = err
		s.checked = now

		if err != nil {
			s.log.WarnContext(ctx, "refreshing Clarity descriptions", slog.Any("error", err))
		}
	}

	return s.state(s.err)
}

// state returns the [State] of the held database. A store without a
// database returns reason in the state.
func (s *Store) state(reason error) State {
	if s.db == nil {
		return State{Err: fmt.Errorf("%w: %w", ErrUnavailable, reason)}
	}

	return State{Database: s.db}
}

// refresh makes the store hold the database of the version that the site
// publishes. It reads the cache file at most one time, and it downloads
// the descriptions file only when the cache file holds another version.
// It replaces the held database only with a file that decodes. A store
// without a database holds the database of the cache file after a
// refresh that does not succeed.
func (s *Store) refresh(ctx context.Context) error {
	version, err := s.source.Version(ctx)
	if err != nil {
		if s.db == nil {
			s.db = s.readCache(ctx)
		}

		return fmt.Errorf("reading the version: %w", err)
	}

	if s.db != nil && s.db.Version == version {
		return nil
	}

	// The store of another process might have saved the version. A
	// store without a database takes the cache file of any version, so
	// a download that does not succeed leaves it that database.
	cached := s.readCache(ctx)
	if cached != nil && (s.db == nil || cached.Version == version) {
		s.db = cached
	}

	if s.db != nil && s.db.Version == version {
		return nil
	}

	raw, err := s.source.Descriptions(ctx)
	if err != nil {
		return fmt.Errorf("downloading the descriptions: %w", err)
	}

	db, err := Decode(version, raw)
	if err != nil {
		return err
	}

	s.db = db
	s.writeCache(ctx, cacheContent{Version: version, Descriptions: raw})

	return nil
}

// cacheContent is the content of the cache file of a [*Store].
// Descriptions holds the descriptions file as the site served it, and
// Version is the version that the site gave that file.
type cacheContent struct {
	Version      string          `json:"version"`
	Descriptions json.RawMessage `json:"descriptions"`
}

// readCache returns the database of the cache file. It returns nil when
// the store has no cache file that decodes.
func (s *Store) readCache(ctx context.Context) *Database {
	if s.cacheDir == "" {
		return nil
	}

	path := filepath.Join(s.cacheDir, cacheFile)

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var content cacheContent

	err = json.Unmarshal(raw, &content)
	if err == nil && content.Version == "" {
		err = errNoVersion
	}

	var db *Database
	if err == nil {
		db, err = Decode(content.Version, content.Descriptions)
	}

	if err != nil {
		s.log.WarnContext(ctx, "ignoring unreadable Clarity cache",
			slog.String("path", path),
			slog.Any("error", err),
		)

		return nil
	}

	return db
}

// writeCache saves content as the cache file and deletes the cache files
// of other layouts. A cache failure costs only the next process a
// download, so writeCache logs it and continues.
func (s *Store) writeCache(ctx context.Context, content cacheContent) {
	if s.cacheDir == "" {
		return
	}

	path := filepath.Join(s.cacheDir, cacheFile)

	err := cachefile.Save(s.cacheDir, path, content)
	if err != nil {
		s.log.WarnContext(ctx, "writing Clarity cache",
			slog.String("path", path),
			slog.Any("error", err),
		)

		return
	}

	cachefile.RemoveOthers(ctx, s.log, filepath.Join(s.cacheDir, cachePattern), path)
}
