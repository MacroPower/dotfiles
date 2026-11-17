package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenLoggerWritesAndAppends(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "run", "kubectx.log")

	for _, run := range []string{"first", "second"} {
		logger, closeLog, err := openLogger(path)
		require.NoError(t, err)

		logger.Info("opened", slog.String("run", run))
		closeLog()
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"run":"first"`)
	assert.Contains(t, string(data), `"run":"second"`)

	dir, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm())
}

// TestOpenLoggerRefusesSymlink pins that serve does not follow a
// symlink at its --log-file path. So a process that can plant the
// symlink cannot make serve append to another file or create one. A
// Lima guest can plant it in each directory that workmux mounts.
func TestOpenLoggerRefusesSymlink(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		before []byte
	}{
		"existing target": {before: []byte("keep\n")},
		"missing target":  {},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			path := filepath.Join(dir, "kubectx.log")

			if tc.before != nil {
				require.NoError(t, os.WriteFile(target, tc.before, 0o600))
			}

			require.NoError(t, os.Symlink(target, path))

			_, _, err := openLogger(path)
			require.ErrorIs(t, err, syscall.ELOOP)

			data, err := os.ReadFile(target)
			if tc.before == nil {
				require.ErrorIs(t, err, os.ErrNotExist)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.before, data)
		})
	}
}
