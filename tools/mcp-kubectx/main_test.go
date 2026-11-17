package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

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

// TestServeOutputUsage runs `serve --help` of this test binary as a
// child process and reads the usage of --output. A Lima-guest serve
// removes its kubeconfig through the writable bind mount that
// home/claude.nix declares. So the usage must name that mount as a
// need of the default path.
func TestServeOutputUsage(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, self, subServe, "--help")

	var stderr bytes.Buffer

	cmd.Env = append(os.Environ(), runMainEnv+"=1")
	cmd.Stderr = &stderr

	var exitErr *exec.ExitError

	require.ErrorAs(t, cmd.Run(), &exitErr, "serve exits 1 after --help")

	_, entry, found := strings.Cut(stderr.String(), "\n  -output string\n")
	require.True(t, found, "usage lists --output: %s", stderr.String())

	entry, _, _ = strings.Cut(entry, "\n  -")

	assert.Contains(
		t,
		entry,
		"writable bind mount of <host's $XDG_STATE_HOME>/mcp-kubectx that home/claude.nix declares",
	)
	assert.Contains(t, entry, "a Lima-guest serve needs that mount")
	assert.NotContains(t, entry, "does not need a writable mount")
}
