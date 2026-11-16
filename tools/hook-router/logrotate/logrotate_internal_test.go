package logrotate

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRotateLocked(t *testing.T) {
	t.Parallel()

	// Each case starts after another process rotated a full log of
	// fullLog between the first size check and the lock.
	const fullLog = "full log"

	tcs := map[string]struct {
		before map[string]string
	}{
		"log that another process rotated stays": {
			before: map[string]string{
				"router.log":   "new",
				"router.log.1": fullLog,
			},
		},
		"log that another process moved away": {
			before: map[string]string{
				"router.log.1": fullLog,
			},
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			for file, content := range tc.before {
				require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644))
			}

			r := New(filepath.Join(dir, "router.log"), WithMaxBytes(int64(len(fullLog))))

			rotated, err := r.rotateLocked()
			require.NoError(t, err)

			assert.False(t, rotated)

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)

			files := map[string]string{}

			for _, e := range entries {
				content, err := os.ReadFile(filepath.Join(dir, e.Name()))
				require.NoError(t, err)

				files[e.Name()] = string(content)
			}

			assert.Equal(t, tc.before, files, "the rotated files stay as the other process left them")
		})
	}
}

func TestUnlockReleasesCopiedDescriptor(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "router.log")
	require.NoError(t, os.WriteFile(path, []byte("full"), 0o644))

	r := New(path, WithMaxBytes(1))

	lock, err := r.lock()
	require.NoError(t, err)

	// A process that this process starts holds a copy of the lock
	// descriptor until its exec. The descriptor that dup returns refers
	// to the same open file description as that copy.
	//
	//nolint:gosec // G115: a file descriptor fits in an int.
	copied, err := syscall.Dup(int(lock.Fd()))
	require.NoError(t, err)

	syscall.CloseOnExec(copied)

	t.Cleanup(func() {
		assert.NoError(t, syscall.Close(copied))
	})

	unlock(lock)

	rotated, err := r.Rotate()
	require.NoError(t, err)

	assert.True(t, rotated, "the copy does not hold the lock after the unlock")
}
