package logrotate_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/logrotate"
)

// logName is the file name of the log in each test directory.
const logName = "router.log"

// writeFiles writes each entry of files under dir, keyed by its
// slash-separated path relative to dir. The directory of each entry
// must exist.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644))
	}
}

// readFiles returns the content of every file under dir at any depth,
// keyed by its slash-separated path relative to dir. It leaves out the
// rotation lock, which a rotation creates and never removes.
func readFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		switch {
		case d.Name() == logName+".lock" && d.IsDir():
			return fs.SkipDir
		case d.Name() == logName+".lock", d.IsDir():
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		require.NoError(t, err)

		content, err := os.ReadFile(path)
		require.NoError(t, err)

		files[filepath.ToSlash(rel)] = string(content)

		return nil
	})
	require.NoError(t, err)

	return files
}

func TestRotate(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		before  map[string]string
		want    map[string]string
		opts    []logrotate.Option
		rotated bool
	}{
		"missing log": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(4)},
			want: map[string]string{},
		},
		"log smaller than the limit stays": {
			opts:   []logrotate.Option{logrotate.WithMaxBytes(4)},
			before: map[string]string{logName: "abc"},
			want:   map[string]string{logName: "abc"},
		},
		"log at the limit moves to number 1": {
			opts:    []logrotate.Option{logrotate.WithMaxBytes(4)},
			before:  map[string]string{logName: "abcd"},
			want:    map[string]string{logName + ".1": "abcd"},
			rotated: true,
		},
		"log past the limit moves to number 1": {
			opts:    []logrotate.Option{logrotate.WithMaxBytes(4)},
			before:  map[string]string{logName: "abcdefgh"},
			want:    map[string]string{logName + ".1": "abcdefgh"},
			rotated: true,
		},
		"rotated files move one number up": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(3)},
			before: map[string]string{
				logName:        "current",
				logName + ".1": "first",
				logName + ".2": "second",
			},
			want: map[string]string{
				logName + ".1": "current",
				logName + ".2": "first",
				logName + ".3": "second",
			},
			rotated: true,
		},
		"the file at the backup count goes": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(3)},
			before: map[string]string{
				logName:        "current",
				logName + ".1": "first",
				logName + ".2": "second",
				logName + ".3": "third",
			},
			want: map[string]string{
				logName + ".1": "current",
				logName + ".2": "first",
				logName + ".3": "second",
			},
			rotated: true,
		},
		"files that a larger backup count left go": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(2)},
			before: map[string]string{
				logName:        "current",
				logName + ".1": "first",
				logName + ".2": "second",
				logName + ".3": "third",
				logName + ".4": "fourth",
				logName + ".5": "fifth",
			},
			want: map[string]string{
				logName + ".1": "current",
				logName + ".2": "first",
			},
			rotated: true,
		},
		"zero backups remove the log": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(0)},
			before: map[string]string{
				logName:        "current",
				logName + ".1": "first",
			},
			want:    map[string]string{},
			rotated: true,
		},
		"negative backups keep none": {
			opts:    []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(-2)},
			before:  map[string]string{logName: "current"},
			want:    map[string]string{},
			rotated: true,
		},
		"zero limit turns rotation off": {
			opts:   []logrotate.Option{logrotate.WithMaxBytes(0)},
			before: map[string]string{logName: "current"},
			want:   map[string]string{logName: "current"},
		},
		"negative limit turns rotation off": {
			opts:   []logrotate.Option{logrotate.WithMaxBytes(-1)},
			before: map[string]string{logName: "current"},
			want:   map[string]string{logName: "current"},
		},
		"file that only resembles a rotated file stays": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(1)},
			before: map[string]string{
				logName:          "current",
				logName + ".bak": "kept",
			},
			want: map[string]string{
				logName + ".1":   "current",
				logName + ".bak": "kept",
			},
			rotated: true,
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			writeFiles(t, dir, tc.before)

			rotated, err := logrotate.New(filepath.Join(dir, logName), tc.opts...).Rotate()
			require.NoError(t, err)

			assert.Equal(t, tc.rotated, rotated)
			assert.Equal(t, tc.want, readFiles(t, dir))
		})
	}
}

func TestRotateDefaults(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		size    int64
		rotated bool
	}{
		"one byte short of the default limit": {size: logrotate.DefaultMaxBytes - 1},
		"at the default limit":                {size: logrotate.DefaultMaxBytes, rotated: true},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, logName)

			// One rotated file more than the default count, so the
			// rotation shows how many files it keeps.
			before := map[string]string{logName: ""}
			for n := 1; n <= logrotate.DefaultBackups+1; n++ {
				before[logName+"."+strconv.Itoa(n)] = "old"
			}

			writeFiles(t, dir, before)
			require.NoError(t, os.Truncate(path, tc.size))

			rotated, err := logrotate.New(path).Rotate()
			require.NoError(t, err)
			require.Equal(t, tc.rotated, rotated)

			files := readFiles(t, dir)

			if !tc.rotated {
				assert.Len(t, files, len(before))

				return
			}

			assert.NotContains(t, files, logName)
			assert.Len(t, files, logrotate.DefaultBackups)
		})
	}
}

func TestRotateSkipsDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, logName)

	require.NoError(t, os.Mkdir(path, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(path, "child"), []byte("data"), 0o644))

	rotated, err := logrotate.New(path, logrotate.WithMaxBytes(1)).Rotate()
	require.NoError(t, err)

	assert.False(t, rotated)
	assert.DirExists(t, path)
	assert.NoFileExists(t, path+".1")
}

// makeReadOnly makes dir read-only, so a rotation in dir cannot move or
// remove a file. It first writes the rotation lock of the log, because
// the rotator cannot create the lock in a read-only directory. It skips
// the test for root, which ignores directory permissions.
func makeReadOnly(t *testing.T, dir string) {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	writeFiles(t, dir, map[string]string{logName + ".lock": ""})
	require.NoError(t, os.Chmod(dir, 0o555))

	t.Cleanup(func() {
		//nolint:errcheck // The temporary directory removal reports a failed restore.
		_ = os.Chmod(dir, 0o755)
	})
}

func TestRotateErrors(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		// setup prepares dir and returns the path of the log.
		setup func(t *testing.T, dir string) string
		want  map[string]string
		err   string
		opts  []logrotate.Option
	}{
		"lock file that cannot open": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1)},
			setup: func(t *testing.T, dir string) string {
				t.Helper()

				path := filepath.Join(dir, logName)

				// A directory at the lock path fails the open of the lock.
				writeFiles(t, dir, map[string]string{logName: "current"})
				require.NoError(t, os.Mkdir(path+".lock", 0o755))

				return path
			},
			want: map[string]string{logName: "current"},
			err:  "opening rotation lock",
		},
		"log whose size cannot be read": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1)},
			setup: func(t *testing.T, dir string) string {
				t.Helper()

				// A regular file in place of the log directory fails the
				// stat of the log with ENOTDIR.
				writeFiles(t, dir, map[string]string{"file": "data"})

				return filepath.Join(dir, "file", logName)
			},
			want: map[string]string{"file": "data"},
			err:  "checking log size",
		},
		"log that cannot move": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(1)},
			setup: func(t *testing.T, dir string) string {
				t.Helper()

				writeFiles(t, dir, map[string]string{logName: "current"})
				makeReadOnly(t, dir)

				return filepath.Join(dir, logName)
			},
			want: map[string]string{logName: "current"},
			err:  "moving log",
		},
		"log that cannot be removed": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(0)},
			setup: func(t *testing.T, dir string) string {
				t.Helper()

				writeFiles(t, dir, map[string]string{logName: "current"})
				makeReadOnly(t, dir)

				return filepath.Join(dir, logName)
			},
			want: map[string]string{logName: "current"},
			err:  "removing log",
		},
		"rotated file that cannot move": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(2)},
			setup: func(t *testing.T, dir string) string {
				t.Helper()

				writeFiles(t, dir, map[string]string{
					logName:        "current",
					logName + ".1": "first",
				})
				makeReadOnly(t, dir)

				return filepath.Join(dir, logName)
			},
			want: map[string]string{
				logName:        "current",
				logName + ".1": "first",
			},
			err: "moving rotated log",
		},
		"rotated file that cannot be removed": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(1)},
			setup: func(t *testing.T, dir string) string {
				t.Helper()

				// The rotator cannot remove a directory that holds a
				// child, so the removal of number 1 fails.
				require.NoError(t, os.Mkdir(filepath.Join(dir, logName+".1"), 0o755))
				writeFiles(t, dir, map[string]string{
					logName:              "current",
					logName + ".1/child": "data",
				})

				return filepath.Join(dir, logName)
			},
			want: map[string]string{
				logName:              "current",
				logName + ".1/child": "data",
			},
			err: "removing rotated log",
		},
		"file past the backup count that cannot be removed": {
			opts: []logrotate.Option{logrotate.WithMaxBytes(1), logrotate.WithBackups(1)},
			setup: func(t *testing.T, dir string) string {
				t.Helper()

				// Number 2 is a directory that holds a child, so its
				// removal fails before the rotation touches number 1.
				require.NoError(t, os.Mkdir(filepath.Join(dir, logName+".2"), 0o755))
				writeFiles(t, dir, map[string]string{
					logName:              "current",
					logName + ".1":       "first",
					logName + ".2/child": "data",
				})

				return filepath.Join(dir, logName)
			},
			want: map[string]string{
				logName:              "current",
				logName + ".1":       "first",
				logName + ".2/child": "data",
			},
			err: "removing rotated log",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := tc.setup(t, dir)

			rotated, err := logrotate.New(path, tc.opts...).Rotate()
			require.ErrorContains(t, err, tc.err)

			assert.False(t, rotated)
			assert.Equal(t, tc.want, readFiles(t, dir))
		})
	}
}

func TestRotateSkipsWhenLockHeld(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, logName)

	writeFiles(t, dir, map[string]string{logName: "current"})

	// A flock belongs to the open file description, so this descriptor
	// holds the lock against the one that Rotate opens, as a second
	// process does.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)

	t.Cleanup(func() {
		//nolint:errcheck // The second close of the lock file reports an error that the test ignores.
		_ = lock.Close()
	})

	//nolint:gosec // G115: a file descriptor fits in an int.
	require.NoError(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_EX))

	r := logrotate.New(path, logrotate.WithMaxBytes(1))

	rotated, err := r.Rotate()
	require.NoError(t, err)

	assert.False(t, rotated)
	assert.Equal(t, map[string]string{logName: "current"}, readFiles(t, dir))

	// A process that a parallel test starts might hold a copy of the
	// descriptor, and that copy keeps the lock through the close. The
	// explicit unlock releases the lock for every copy.
	//
	//nolint:gosec // G115: a file descriptor fits in an int.
	require.NoError(t, syscall.Flock(int(lock.Fd()), syscall.LOCK_UN))
	require.NoError(t, lock.Close())

	rotated, err = r.Rotate()
	require.NoError(t, err)

	assert.True(t, rotated)
	assert.Equal(t, map[string]string{logName + ".1": "current"}, readFiles(t, dir))
}

func TestRotateWhileProcessesStart(t *testing.T) {
	t.Parallel()

	// A started process copies the descriptors of the test process and
	// keeps the copies until its exec. Only a start in the middle of a
	// rotation copies the lock descriptor. This count of starts makes a
	// run without such a copy improbable.
	const starts = 50

	path := filepath.Join(t.TempDir(), logName)
	r := logrotate.New(path, logrotate.WithMaxBytes(1), logrotate.WithBackups(1))

	var (
		wg        sync.WaitGroup
		stop      atomic.Bool
		rotations int
		skipped   int
		rotateErr error
	)

	// The goroutine fills the log and rotates it until the test has
	// started every process. No other process rotates this log, so each
	// rotation must find the lock free.
	wg.Go(func() {
		for !stop.Load() {
			rotateErr = os.WriteFile(path, []byte("full"), 0o644)
			if rotateErr != nil {
				return
			}

			var rotated bool

			rotated, rotateErr = r.Rotate()
			if rotateErr != nil {
				return
			}

			rotations++

			if !rotated {
				skipped++
			}
		}
	})

	var startErr error

	for range starts {
		startErr = exec.CommandContext(t.Context(), "true").Run()
		if startErr != nil {
			break
		}
	}

	stop.Store(true)
	wg.Wait()

	require.NoError(t, startErr)
	require.NoError(t, rotateErr)

	assert.Zerof(t, skipped, "%d of %d rotations found the lock held", skipped, rotations)
}

func TestRotateConcurrent(t *testing.T) {
	t.Parallel()

	const writers = 16

	dir := t.TempDir()
	path := filepath.Join(dir, logName)
	full := strings.Repeat("x", 1024)

	writeFiles(t, dir, map[string]string{logName: full})

	// Each writer acts as one process. It rotates, then opens the log
	// and appends a record. The records together stay far below the
	// limit, so only the full log that the test starts with is due.
	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		rotated = make([]bool, writers)
		errs    = make([]error, writers)
	)

	for i := range writers {
		wg.Go(func() {
			<-start

			rotated[i], errs[i] = logrotate.New(path, logrotate.WithMaxBytes(1024)).Rotate()
			if errs[i] != nil {
				return
			}

			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
			if err != nil {
				errs[i] = err

				return
			}

			_, err = f.WriteString("record\n")
			if err != nil {
				errs[i] = err
			}

			err = f.Close()
			if err != nil {
				errs[i] = err
			}
		})
	}

	close(start)
	wg.Wait()

	rotations := 0

	for i := range writers {
		require.NoError(t, errs[i])

		if rotated[i] {
			rotations++
		}
	}

	assert.Equal(t, 1, rotations, "the full log rotates one time")

	// A writer that opened the log before the rotation appends to the
	// rotated file, so every record is in one of the two files.
	files := readFiles(t, dir)
	require.Len(t, files, 2)

	assert.True(t, strings.HasPrefix(files[logName+".1"], full), "number 1 holds the full log")
	assert.Equal(t, writers, strings.Count(files[logName]+files[logName+".1"], "record\n"))
}
