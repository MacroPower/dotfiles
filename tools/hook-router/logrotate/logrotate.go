package logrotate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"syscall"
)

// DefaultMaxBytes is the size limit of a [*Rotator] that [WithMaxBytes]
// does not configure.
const DefaultMaxBytes int64 = 16 << 20

// DefaultBackups is the backup count of a [*Rotator] that [WithBackups]
// does not configure.
const DefaultBackups = 3

// errLocked reports that another process holds the rotation lock.
var errLocked = errors.New("rotation lock is held")

// Rotator renames a log file out of the way when the file reaches a
// size limit, and keeps a fixed number of the renamed files. Construct
// with [New].
type Rotator struct {
	path     string
	maxBytes int64
	backups  int
}

// Option configures a [*Rotator] in [New].
//
//   - [WithMaxBytes]
//   - [WithBackups]
type Option func(*Rotator)

// WithMaxBytes is an [Option] that sets the size in bytes at which
// [*Rotator.Rotate] rotates the log. A size of zero or less turns
// rotation off.
func WithMaxBytes(n int64) Option {
	return func(r *Rotator) { r.maxBytes = n }
}

// WithBackups is an [Option] that sets how many rotated files
// [*Rotator.Rotate] keeps. A count of zero or less keeps none, so a
// rotation removes the log.
func WithBackups(n int) Option {
	return func(r *Rotator) { r.backups = max(n, 0) }
}

// New builds a [*Rotator] for the log at path. Without options it
// rotates at [DefaultMaxBytes] and keeps [DefaultBackups] rotated files.
func New(path string, opts ...Option) *Rotator {
	r := &Rotator{
		path:     path,
		maxBytes: DefaultMaxBytes,
		backups:  DefaultBackups,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Rotate rotates the log when it is a regular file that has reached
// the size limit, and reports whether it did. The log moves to its
// path with ".1" appended, and every rotated file moves one number up.
// Rotate removes each file whose number passes the backup count. The
// caller creates the log again when it opens the log after Rotate
// returns.
//
// Rotate returns false without an error for a missing log and for a
// log smaller than the limit. It never waits for the rotation lock, and
// returns false without an error when another process holds that lock.
func (r *Rotator) Rotate() (bool, error) {
	if r.maxBytes <= 0 {
		return false, nil
	}

	full, err := r.full()
	if err != nil || !full {
		return false, err
	}

	lock, err := r.lock()
	if errors.Is(err, errLocked) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	defer unlock(lock)

	return r.rotateLocked()
}

// rotateLocked rotates the log when it is a regular file that has
// reached the size limit, and reports whether it did. The caller holds
// the rotation lock.
func (r *Rotator) rotateLocked() (bool, error) {
	// Another process might have rotated the log between the first
	// check and the lock. A second rotation moves the log that the
	// other process started into the rotated files, and that move
	// pushes out an older file.
	full, err := r.full()
	if err != nil || !full {
		return false, err
	}

	err = r.shift()
	if err != nil {
		return false, err
	}

	return true, nil
}

// full reports whether the log is a regular file that has reached the
// size limit. A missing log is not full.
func (r *Rotator) full() (bool, error) {
	info, err := os.Stat(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("checking log size: %w", err)
	}

	return info.Mode().IsRegular() && info.Size() >= r.maxBytes, nil
}

// lock opens the lock file and takes the exclusive rotation lock on it
// without waiting. The caller releases the lock with [unlock]. It
// returns [errLocked] when another process holds the lock.
func (r *Rotator) lock() (*os.File, error) {
	f, err := os.OpenFile(r.path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening rotation lock: %w", err)
	}

	//nolint:gosec // G115: a file descriptor fits in an int.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		_ = f.Close() //nolint:errcheck // The lock error is the one to report.

		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLocked
		}

		return nil, fmt.Errorf("taking rotation lock: %w", err)
	}

	return f, nil
}

// unlock releases the rotation lock that [*Rotator.lock] took on f, and
// closes f.
//
// A flock belongs to the open file description. A process that this
// process starts gets a copy of the descriptor of f and keeps the copy
// until its exec. The close of f alone leaves the lock with that copy,
// and a rotation before that exec finds the lock held and skips. The
// explicit unlock releases the lock for every copy.
func unlock(f *os.File) {
	//nolint:errcheck,gosec // Nothing can act on an unlock error here. G115: a file descriptor fits in an int.
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	_ = f.Close() //nolint:errcheck // Nothing can act on a close error here.
}

// shift moves every rotated file one number up, starting from the
// highest number, and then moves the log to number 1. It removes each
// file whose new number passes the backup count. The removal also
// clears the files that a larger backup count left behind.
func (r *Rotator) shift() error {
	// Rotation numbers its files without gaps, so the first missing
	// number ends the count.
	last := 0

	for {
		_, err := os.Lstat(r.backup(last + 1))
		if errors.Is(err, fs.ErrNotExist) {
			break
		}

		if err != nil {
			return fmt.Errorf("checking rotated log: %w", err)
		}

		last++
	}

	for n := last; n >= 1; n-- {
		if n >= r.backups {
			err := os.Remove(r.backup(n))
			if err != nil {
				return fmt.Errorf("removing rotated log: %w", err)
			}

			continue
		}

		err := os.Rename(r.backup(n), r.backup(n+1))
		if err != nil {
			return fmt.Errorf("moving rotated log: %w", err)
		}
	}

	if r.backups == 0 {
		err := os.Remove(r.path)
		if err != nil {
			return fmt.Errorf("removing log: %w", err)
		}

		return nil
	}

	err := os.Rename(r.path, r.backup(1))
	if err != nil {
		return fmt.Errorf("moving log: %w", err)
	}

	return nil
}

// backup returns the path of rotated file number n. Number 1 is the
// most recent.
func (r *Rotator) backup(n int) string {
	return r.path + "." + strconv.Itoa(n)
}
