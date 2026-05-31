package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSessionEnd(t *testing.T) { //nolint:tparallel,paralleltest // subtests use t.Setenv
	logger := slog.New(slog.DiscardHandler)

	t.Run("removes existing CLAUDE_KUBECTX_DIR", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		parent := t.TempDir()
		dir := filepath.Join(parent, "claude-kubectx.42")
		require.NoError(t, os.MkdirAll(dir, 0o700))

		nested := filepath.Join(dir, "kubeconfig")
		require.NoError(t, os.WriteFile(nested, []byte("hi"), 0o600))

		t.Setenv("CLAUDE_KUBECTX_DIR", dir)

		err := handleSessionEnd(t.Context(), logger)
		require.NoError(t, err)

		_, err = os.Stat(dir)
		assert.True(t, os.IsNotExist(err), "session-end must remove the kubectx dir")
	})

	t.Run("env unset: noop", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_DIR", "")

		err := handleSessionEnd(t.Context(), logger)
		require.NoError(t, err)
	})

	t.Run("missing dir: noop (no error)", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_DIR", filepath.Join(t.TempDir(), "claude-kubectx.42"))

		err := handleSessionEnd(t.Context(), logger)
		require.NoError(t, err)
	})

	t.Run("refuses unrecognized path shape", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		// Sentinel guards against a rogue env value pointing at e.g. $HOME.
		parent := t.TempDir()
		dir := filepath.Join(parent, "not-claude-kubectx")
		require.NoError(t, os.MkdirAll(dir, 0o700))

		marker := filepath.Join(dir, "important.txt")
		require.NoError(t, os.WriteFile(marker, []byte("keep me"), 0o600))

		t.Setenv("CLAUDE_KUBECTX_DIR", dir)

		err := handleSessionEnd(t.Context(), logger)
		require.NoError(t, err)

		_, err = os.Stat(marker)
		assert.NoError(t, err, "unrecognized path must not be removed")
	})

	t.Run("refuses missing PID suffix", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		parent := t.TempDir()
		dir := filepath.Join(parent, "claude-kubectx.")
		require.NoError(t, os.MkdirAll(dir, 0o700))

		t.Setenv("CLAUDE_KUBECTX_DIR", dir)

		err := handleSessionEnd(t.Context(), logger)
		require.NoError(t, err)

		_, err = os.Stat(dir)
		assert.NoError(t, err, "empty PID suffix must not be treated as a session dir")
	})

	t.Run("refuses non-numeric PID suffix", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		parent := t.TempDir()
		dir := filepath.Join(parent, "claude-kubectx.notanumber")
		require.NoError(t, os.MkdirAll(dir, 0o700))

		t.Setenv("CLAUDE_KUBECTX_DIR", dir)

		err := handleSessionEnd(t.Context(), logger)
		require.NoError(t, err)

		_, err = os.Stat(dir)
		assert.NoError(t, err, "non-numeric suffix must not be treated as a session dir")
	})
}

func TestIsClaudeKubectxDir(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		path string
		want bool
	}{
		"valid pid suffix":     {path: "/tmp/claude-kubectx.42", want: true},
		"valid xdg parent":     {path: "/run/user/1000/claude-kubectx.99999", want: true},
		"missing prefix":       {path: "/tmp/foo.42", want: false},
		"missing pid suffix":   {path: "/tmp/claude-kubectx.", want: false},
		"non-numeric suffix":   {path: "/tmp/claude-kubectx.abc", want: false},
		"prefix only basename": {path: "/tmp/claude-kubectx", want: false},
		"empty path":           {path: "", want: false},
		"signed pid suffix":    {path: "/tmp/claude-kubectx.-1", want: false},
		"plus-signed suffix":   {path: "/tmp/claude-kubectx.+5", want: false},
		"leading-zero suffix":  {path: "/tmp/claude-kubectx.007", want: false},
		"zero pid suffix":      {path: "/tmp/claude-kubectx.0", want: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, isClaudeKubectxDir(tc.path))
		})
	}
}

func TestSweepKubectxDirs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	t.Run("removes orphan but preserves live and unrelated dirs", func(t *testing.T) {
		t.Parallel()

		parent := t.TempDir()

		// Live PID: our own process. The sweep must not delete this.
		liveDir := filepath.Join(parent, "claude-kubectx."+strconv.Itoa(os.Getpid()))
		require.NoError(t, os.MkdirAll(liveDir, 0o700))

		// Orphan PID: a value that we are confident is not a running
		// process on this host. 0x7FFFFFFF is the kernel-defined max
		// for pid_t on Linux/macOS and is never assigned to a real
		// process.
		orphanPID := 0x7FFFFFFF
		orphanDir := filepath.Join(parent, "claude-kubectx."+strconv.Itoa(orphanPID))
		require.NoError(t, os.MkdirAll(orphanDir, 0o700))

		// Unrelated dir: must be ignored.
		unrelatedDir := filepath.Join(parent, "other-dir")
		require.NoError(t, os.MkdirAll(unrelatedDir, 0o700))

		// Malformed PID suffix: must be ignored.
		malformedDir := filepath.Join(parent, "claude-kubectx.notapid")
		require.NoError(t, os.MkdirAll(malformedDir, 0o700))

		sweepKubectxDirs(parent, logger)

		_, err := os.Stat(liveDir)
		assert.NoError(t, err, "live PID dir must be preserved")

		_, err = os.Stat(orphanDir)
		assert.True(t, os.IsNotExist(err), "orphan PID dir must be removed")

		_, err = os.Stat(unrelatedDir)
		assert.NoError(t, err, "unrelated dir must be preserved")

		_, err = os.Stat(malformedDir)
		assert.NoError(t, err, "malformed PID suffix must not match the sweep")
	})

	t.Run("missing parent dir: noop", func(t *testing.T) {
		t.Parallel()

		// Confirms the sweep tolerates a clean host that has not yet
		// run any Claude sessions.
		sweepKubectxDirs(filepath.Join(t.TempDir(), "never-created"), logger)
	})
}

func TestPidAlive(t *testing.T) {
	t.Parallel()

	t.Run("own pid is alive", func(t *testing.T) {
		t.Parallel()

		assert.True(t, pidAlive(os.Getpid()))
	})

	t.Run("max pid is not alive", func(t *testing.T) {
		t.Parallel()

		assert.False(t, pidAlive(0x7FFFFFFF))
	})
}

func TestKubectxSweepParent(t *testing.T) { //nolint:tparallel,paralleltest // subtests use t.Setenv
	t.Run("derives parent from CLAUDE_KUBECTX_DIR when set", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv. The wrapper baked the
		// resolved location here, so the sweep root is its parent even
		// when hook-router's own XDG_RUNTIME_DIR points elsewhere.
		t.Setenv("XDG_RUNTIME_DIR", "/run/user/99")
		t.Setenv("CLAUDE_KUBECTX_DIR", "/run/user/42/claude-kubectx.123")

		assert.Equal(t, "/run/user/42", kubectxSweepParent())
	})

	t.Run("uses XDG_RUNTIME_DIR when CLAUDE_KUBECTX_DIR unset", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_DIR", "")
		t.Setenv("XDG_RUNTIME_DIR", "/run/user/42")

		assert.Equal(t, "/run/user/42", kubectxSweepParent())
	})

	t.Run("falls back to /tmp when unset", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_DIR", "")
		t.Setenv("XDG_RUNTIME_DIR", "")

		assert.Equal(t, "/tmp", kubectxSweepParent())
	})
}

// Sanity check that the sweep is wired up to remove dirs whose
// basename matches the launcher wrapper's PID-suffixed shape (not
// just any directory inside parent).
func TestSweepKubectxDirs_OnlyMatchingPrefix(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()

	// A dir whose name happens to start with "claude" but is not
	// the wrapper prefix.
	siblingPrefixed := filepath.Join(parent, "claude-something.42")
	require.NoError(t, os.MkdirAll(siblingPrefixed, 0o700))

	sweepKubectxDirs(parent, slog.New(slog.DiscardHandler))

	_, err := os.Stat(siblingPrefixed)
	assert.NoError(t, err, "directory must not be removed unless basename has the kubectx prefix")

	// Defensive: explicitly confirm the test pinned the right shape.
	require.True(t, strings.HasPrefix(filepath.Base(siblingPrefixed), "claude-"))
}
