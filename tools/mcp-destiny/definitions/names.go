package definitions

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
)

const (
	// namesCachePrefix names the cache files of a [*NameStore]. The digit
	// changes when the fields of [Lookup] that the file stores change, so
	// a process never decodes a stale layout.
	namesCachePrefix = "names-v1-"

	// namesLockFile names the lock file of the cache files of a
	// [*NameStore] in the cache directory.
	namesLockFile = "names.lock"

	// defaultLookupLimit is the number of lookups that one call of
	// [*NameStore.Name] sends at most when no [WithLookupLimit] sets it.
	defaultLookupLimit = 20

	// defaultLookupTimeout bounds the lookups of one call of
	// [*NameStore.Name] when no [WithLookupTimeout] sets it.
	defaultLookupTimeout = 10 * time.Second

	// lookupWorkers is the number of lookups that one call of
	// [*NameStore.Name] runs at a time.
	lookupWorkers = 4
)

// ItemSource looks up one item definition in the live Destiny manifest of
// Bungie. A caller adapts [*bungie.Client] to it and adds the credentials
// to each lookup. A [*NameStore] runs several lookups at a time, so the
// source must be safe for concurrent use.
type ItemSource interface {
	ItemDefinition(ctx context.Context, hash uint32) (*bungie.ItemDefinition, error)
}

// Lookup is the outcome of the lookup of one item hash in the live
// manifest of Bungie. Err holds the reason of a lookup that did not get an
// answer. Skipped marks a hash that the store did not look up, because the
// call reached the lookup limit first.
//
// The other fields hold the answer of Bungie. Defined reports whether
// Bungie defines an item with the hash, and the zero answer therefore
// means that it defines none. Name is the name of the definition, and it
// is empty for a definition without a name. Redacted marks a definition
// that Bungie does not show yet, which the game shows as a classified
// item.
type Lookup struct {
	Err      error  `json:"-"`
	Name     string `json:"name,omitempty"`
	Defined  bool   `json:"defined,omitempty"`
	Redacted bool   `json:"redacted,omitempty"`
	Skipped  bool   `json:"-"`
}

// Answered reports whether Bungie answered the lookup.
func (l Lookup) Answered() bool {
	return l.Err == nil && !l.Skipped
}

// NameOption configures a [*NameStore] via [NewNameStore].
//
// Functions of this type:
//   - [WithLookupLimit]
//   - [WithLookupTimeout]
//   - [WithNameLogger]
type NameOption func(*NameStore)

// WithLookupLimit sets the number of lookups that one call of
// [*NameStore.Name] sends at most. The limit defaults to 20. It is a
// [NameOption].
func WithLookupLimit(n int) NameOption {
	return func(s *NameStore) { s.limit = n }
}

// WithLookupTimeout sets the time that the lookups of one call of
// [*NameStore.Name] take at most. A lookup that runs out of the time
// fails. The timeout defaults to 10 seconds. It is a [NameOption].
func WithLookupTimeout(d time.Duration) NameOption {
	return func(s *NameStore) { s.timeout = d }
}

// WithNameLogger sets the logger for failed lookups and cache failures.
// It is a [NameOption].
func WithNameLogger(log *slog.Logger) NameOption {
	return func(s *NameStore) { s.log = log }
}

// NameStore names the item hashes that the definitions do not name. It
// looks each such hash up in the live manifest of Bungie. The live
// manifest defines the items that the service withholds, and the items
// that Bungie added after the manifest version of the definitions.
//
// The store keeps each answer of Bungie for the manifest version of the
// definitions, in memory and in a cache file per version. The stores of
// several processes can share the cache directory. Before a store looks up
// a hash without a kept answer, it reads the answers that the other stores
// saved. It saves its own answers together with those of the file, so no
// store drops the answers of another. A hash therefore costs one answered
// lookup for each manifest version, unless two stores look it up at the
// same time. The store keeps no failed lookup, so the next call that needs
// the hash looks it up again.
type NameStore struct {
	source   ItemSource
	log      *slog.Logger
	answers  map[uint32]Lookup
	version  string
	cacheDir string
	limit    int
	timeout  time.Duration
	mu       sync.Mutex
}

// NewNameStore returns a [*NameStore] that looks hashes up through source
// and keeps its cache files in cacheDir. An empty cacheDir turns the cache
// files off.
func NewNameStore(source ItemSource, cacheDir string, opts ...NameOption) *NameStore {
	s := &NameStore{
		source:   source,
		cacheDir: cacheDir,
		limit:    defaultLookupLimit,
		timeout:  defaultLookupTimeout,
		log:      slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Name returns the definitions defs with the names that Bungie gives the
// item hashes in hashes that defs do not name. It returns defs itself when
// defs name every hash. Otherwise it returns a copy whose Lookups field
// holds the outcome for each hash that defs do not name. The
// [*Defs.ItemName] method of the copy reads the names of Bungie.
//
// Name looks up only the hashes without a kept answer, at most the lookup
// limit of them, in the order of hashes. It marks the rest as skipped.
// Before the lookups, it keeps the answers that the cache file holds. A
// lookup that fails leaves the hash without a name, and the Err field of
// its outcome gives the reason.
func (s *NameStore) Name(ctx context.Context, defs *Defs, hashes []uint32) *Defs {
	var misses []uint32

	seen := map[uint32]bool{}

	for _, hash := range hashes {
		if !seen[hash] && defs.ItemName(hash) == "" {
			misses = append(misses, hash)
		}

		seen[hash] = true
	}

	if len(misses) == 0 {
		return defs
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.use(defs.Version)

	for _, hash := range misses {
		if _, ok := s.answers[hash]; !ok {
			// The store of another process might have saved the answer.
			s.merge(s.readCache(ctx))

			break
		}
	}

	lookups := maps.Clone(defs.Lookups)
	if lookups == nil {
		lookups = map[uint32]Lookup{}
	}

	var queue []uint32

	for _, hash := range misses {
		answer, ok := s.answers[hash]

		switch {
		case ok:
			lookups[hash] = answer
		case len(queue) < s.limit:
			queue = append(queue, hash)
		default:
			lookups[hash] = Lookup{Skipped: true}
		}
	}

	answered := false

	for i, lookup := range s.fetch(ctx, queue) {
		hash := queue[i]
		lookups[hash] = lookup

		if lookup.Err != nil {
			s.log.WarnContext(ctx, "looking up item name",
				slog.Uint64("hash", uint64(hash)),
				slog.Any("error", lookup.Err),
			)

			continue
		}

		s.answers[hash] = lookup
		answered = true
	}

	if answered {
		s.writeCache(ctx)
	}

	out := *defs
	out.Lookups = lookups

	return &out
}

// fetch looks up each hash of hashes and returns the outcomes in the
// order of hashes. It runs [lookupWorkers] lookups at a time, and the
// lookup timeout bounds them all.
func (s *NameStore) fetch(ctx context.Context, hashes []uint32) []Lookup {
	out := make([]Lookup, len(hashes))
	if len(hashes) == 0 {
		return out
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	next := make(chan int)

	var wg sync.WaitGroup

	for range min(lookupWorkers, len(hashes)) {
		wg.Go(func() {
			for i := range next {
				out[i] = s.lookup(ctx, hashes[i])
			}
		})
	}

	for i := range hashes {
		next <- i
	}

	close(next)
	wg.Wait()

	return out
}

// lookup looks up one hash and returns its outcome.
func (s *NameStore) lookup(ctx context.Context, hash uint32) Lookup {
	def, err := s.source.ItemDefinition(ctx, hash)
	if err != nil {
		return Lookup{Err: err}
	}

	if def == nil {
		return Lookup{}
	}

	return Lookup{Name: def.DisplayProperties.Name, Defined: true, Redacted: def.Redacted}
}

// namesFile is the content of a cache file of a [*NameStore]. Answers
// maps each item hash to the answer of Bungie.
type namesFile struct {
	Answers map[uint32]Lookup `json:"answers"`
	Version string            `json:"version"`
}

// use makes the store keep the answers of the manifest version. It drops
// the kept answers when they belong to another version.
func (s *NameStore) use(version string) {
	if s.answers != nil && s.version == version {
		return
	}

	s.version = version
	s.answers = map[uint32]Lookup{}
}

// merge keeps each answer of answers whose hash has no kept answer.
func (s *NameStore) merge(answers map[uint32]Lookup) {
	for hash, answer := range answers {
		if _, ok := s.answers[hash]; !ok {
			s.answers[hash] = answer
		}
	}
}

// readCache returns the answers of the cache file of the kept manifest
// version. It returns nil when the store has no readable file of the
// version.
func (s *NameStore) readCache(ctx context.Context) map[uint32]Lookup {
	if s.cacheDir == "" {
		return nil
	}

	path := cachePath(s.cacheDir, namesCachePrefix, s.version)

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var file namesFile

	err = json.Unmarshal(raw, &file)
	if err != nil || file.Version != s.version {
		s.log.WarnContext(ctx, "ignoring unreadable item name cache",
			slog.String("path", path),
			slog.Any("error", err),
		)

		return nil
	}

	return file.Answers
}

// writeCache saves the kept answers and deletes the cache files of other
// manifest versions and other layouts. A cache failure costs only the
// next process a lookup of each hash, so writeCache logs it and
// continues.
func (s *NameStore) writeCache(ctx context.Context) {
	if s.cacheDir == "" {
		return
	}

	path := cachePath(s.cacheDir, namesCachePrefix, s.version)

	err := s.saveAnswers(ctx, path)
	if err != nil {
		s.log.WarnContext(ctx, "writing item name cache",
			slog.String("path", path),
			slog.Any("error", err),
		)

		return
	}

	removeCaches(ctx, s.log, filepath.Join(s.cacheDir, "names-v*.json"), path)
}

// saveAnswers keeps the answers of the cache file at path and saves the
// kept answers to it. It holds the lock of the name cache files from the
// read to the write. Without the lock, another process can save its
// answers between the two, and the write drops them.
func (s *NameStore) saveAnswers(ctx context.Context, path string) error {
	unlock, err := lockCache(s.cacheDir)
	if err != nil {
		return err
	}
	defer unlock()

	s.merge(s.readCache(ctx))

	return saveCache(s.cacheDir, path, namesFile{Version: s.version, Answers: s.answers})
}

// lockCache takes the exclusive advisory lock of the name cache files in
// dir. The lock serializes the writes of the processes that share dir. The
// caller runs the returned function to release the lock.
func lockCache(dir string) (func(), error) {
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}

	f, err := os.OpenFile(filepath.Join(dir, namesLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening cache lock: %w", err)
	}

	//nolint:gosec // G115: a file descriptor fits in an int.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	if err != nil {
		_ = f.Close() //nolint:errcheck // The lock error is the one to report.

		return nil, fmt.Errorf("locking cache: %w", err)
	}

	return func() {
		// Closing the descriptor releases the lock.
		_ = f.Close() //nolint:errcheck // Nothing can act on a close error here.
	}, nil
}
