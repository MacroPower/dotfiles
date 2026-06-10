package kubectx_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/kubectx"
)

// writeContextsConfig writes a minimal kubeconfig with the given
// current-context and context names, returning its path.
func writeContextsConfig(t *testing.T, currentContext string, names ...string) string {
	t.Helper()

	var b strings.Builder

	b.WriteString("apiVersion: v1\nkind: Config\n")

	if currentContext != "" {
		b.WriteString("current-context: " + currentContext + "\n")
	}

	if len(names) > 0 {
		b.WriteString("contexts:\n")

		for _, n := range names {
			b.WriteString("- name: " + n + "\n  context:\n    cluster: " + n + "\n    user: " + n + "\n")
		}
	}

	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))

	return path
}

// TestKubectxSelected pins the bash-gate selection check, including
// the union-membership rule that lets a guest context
// ($CLAUDE_KUBECTX_GUEST_CONFIG) count as selected even though
// current-context is read from local.yaml only.
func TestKubectxSelected(t *testing.T) { //nolint:tparallel,paralleltest // subtests use t.Setenv
	t.Run("local context selected", func(t *testing.T) {
		local := writeContextsConfig(t, "kind-dev", "kind-dev")
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", "")
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

		assert.Equal(t, local, kubectx.Selected())
	})

	t.Run("guest context selected via union", func(t *testing.T) {
		local := writeContextsConfig(t, "tald")
		guest := writeContextsConfig(t, "", "tald")
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", guest)
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

		assert.Equal(t, local, kubectx.Selected())
	})

	t.Run("foreign current-context denied", func(t *testing.T) {
		local := writeContextsConfig(t, "admin@main")
		guest := writeContextsConfig(t, "", "tald")
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", guest)
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

		assert.Empty(t, kubectx.Selected())
	})

	t.Run("empty current-context denied", func(t *testing.T) {
		local := writeContextsConfig(t, "", "kind-dev")
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", "")
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

		assert.Empty(t, kubectx.Selected())
	})

	t.Run("guest var unset falls back to local-only", func(t *testing.T) {
		// current-context names a context that exists only in a guest
		// config the gate cannot see, so without the var it denies.
		local := writeContextsConfig(t, "tald")
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", "")
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

		assert.Empty(t, kubectx.Selected())
	})

	t.Run("missing guest file is no error and denies", func(t *testing.T) {
		local := writeContextsConfig(t, "tald")
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", filepath.Join(t.TempDir(), "absent"))
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", "")

		assert.Empty(t, kubectx.Selected())
	})

	t.Run("external sidecar present selects", func(t *testing.T) {
		local := writeContextsConfig(t, "admin@main")
		sidecar := filepath.Join(t.TempDir(), "kubeconfig")
		require.NoError(t, os.WriteFile(sidecar, []byte("apiVersion: v1\nkind: Config\n"), 0o600))
		t.Setenv("CLAUDE_KUBECTX_LOCAL", local)
		t.Setenv("CLAUDE_KUBECTX_GUEST_CONFIG", "")
		t.Setenv("CLAUDE_KUBECTX_SIDECAR", sidecar)

		assert.Equal(t, local, kubectx.Selected())
	})

	t.Run("CLAUDE_KUBECTX_LOCAL unset denies", func(t *testing.T) {
		t.Setenv("CLAUDE_KUBECTX_LOCAL", "")

		assert.Empty(t, kubectx.Selected())
	})
}

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

		err := kubectx.RemoveSessionDir(logger)
		require.NoError(t, err)

		_, err = os.Stat(dir)
		assert.True(t, os.IsNotExist(err), "session-end must remove the kubectx dir")
	})

	t.Run("env unset: noop", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_DIR", "")

		err := kubectx.RemoveSessionDir(logger)
		require.NoError(t, err)
	})

	t.Run("missing dir: noop (no error)", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_DIR", filepath.Join(t.TempDir(), "claude-kubectx.42"))

		err := kubectx.RemoveSessionDir(logger)
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

		err := kubectx.RemoveSessionDir(logger)
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

		err := kubectx.RemoveSessionDir(logger)
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

		err := kubectx.RemoveSessionDir(logger)
		require.NoError(t, err)

		_, err = os.Stat(dir)
		assert.NoError(t, err, "non-numeric suffix must not be treated as a session dir")
	})
}

// TestRemoveSessionDir_BasenameShapeGuard pins the containment guard:
// only a basename with the exact claude-kubectx.<pid> shape (canonical
// positive decimal PID) is eligible for removal, so a rogue
// CLAUDE_KUBECTX_DIR value cannot point the hook at an arbitrary path.
func TestRemoveSessionDir_BasenameShapeGuard(t *testing.T) { //nolint:tparallel,paralleltest // subtests use t.Setenv
	logger := slog.New(slog.DiscardHandler)

	cases := map[string]struct {
		basename    string
		wantRemoved bool
	}{
		"valid pid suffix":     {basename: "claude-kubectx.42", wantRemoved: true},
		"valid large pid":      {basename: "claude-kubectx.99999", wantRemoved: true},
		"missing prefix":       {basename: "foo.42", wantRemoved: false},
		"missing pid suffix":   {basename: "claude-kubectx.", wantRemoved: false},
		"non-numeric suffix":   {basename: "claude-kubectx.abc", wantRemoved: false},
		"prefix only basename": {basename: "claude-kubectx", wantRemoved: false},
		"signed pid suffix":    {basename: "claude-kubectx.-1", wantRemoved: false},
		"plus-signed suffix":   {basename: "claude-kubectx.+5", wantRemoved: false},
		"leading-zero suffix":  {basename: "claude-kubectx.007", wantRemoved: false},
		"zero pid suffix":      {basename: "claude-kubectx.0", wantRemoved: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), tc.basename)
			require.NoError(t, os.MkdirAll(dir, 0o700))
			t.Setenv("CLAUDE_KUBECTX_DIR", dir)

			require.NoError(t, kubectx.RemoveSessionDir(logger))

			_, err := os.Stat(dir)
			if tc.wantRemoved {
				assert.True(t, os.IsNotExist(err), "dir matching the session shape must be removed")
			} else {
				assert.NoError(t, err, "dir not matching the session shape must be preserved")
			}
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

		kubectx.SweepOrphans(parent, logger)

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
		kubectx.SweepOrphans(filepath.Join(t.TempDir(), "never-created"), logger)
	})
}

func TestKubectxSweepParent(t *testing.T) { //nolint:tparallel,paralleltest // subtests use t.Setenv
	t.Run("derives parent from CLAUDE_KUBECTX_DIR when set", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv. The wrapper baked the
		// resolved location here, so the sweep root is its parent even
		// when hook-router's own XDG_RUNTIME_DIR points elsewhere.
		t.Setenv("XDG_RUNTIME_DIR", "/run/user/99")
		t.Setenv("CLAUDE_KUBECTX_DIR", "/run/user/42/claude-kubectx.123")

		assert.Equal(t, "/run/user/42", kubectx.SweepParent())
	})

	t.Run("uses XDG_RUNTIME_DIR when CLAUDE_KUBECTX_DIR unset", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_DIR", "")
		t.Setenv("XDG_RUNTIME_DIR", "/run/user/42")

		assert.Equal(t, "/run/user/42", kubectx.SweepParent())
	})

	t.Run("falls back to /tmp when unset", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("CLAUDE_KUBECTX_DIR", "")
		t.Setenv("XDG_RUNTIME_DIR", "")

		assert.Equal(t, "/tmp", kubectx.SweepParent())
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

	kubectx.SweepOrphans(parent, slog.New(slog.DiscardHandler))

	_, err := os.Stat(siblingPrefixed)
	assert.NoError(t, err, "directory must not be removed unless basename has the kubectx prefix")

	// Defensive: explicitly confirm the test pinned the right shape.
	require.True(t, strings.HasPrefix(filepath.Base(siblingPrefixed), "claude-"))
}
