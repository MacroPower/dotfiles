package archive_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/archive"
)

func TestOutputArchiveAnnotate(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	t.Run("writes the raw original verbatim and appends a pointer", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		a := archive.New(dir)

		// original carries ANSI and repeated lines; compacted is the
		// already-shorter result. Annotate must persist original verbatim
		// (ANSI + repeats intact), not the compacted form.
		original := "\x1b[31m" + strings.Repeat("repeated line\n", 400)
		compacted := "repeated line\n    [hook-router: +399 identical lines]"

		annotated, ok := a.Annotate("sess-1", "stdout", original, compacted, logger)
		require.True(t, ok)

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.Len(t, entries, 1, "exactly one archived file")

		path := filepath.Join(dir, entries[0].Name())

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, original, string(got), "file must hold the raw uncompacted stream")
		assert.NotEqual(t, compacted, string(got), "the compacted form must not be what is archived")

		// Absolute path under dir.
		assert.True(t, filepath.IsAbs(path), "archived path must be absolute")

		rel, err := filepath.Rel(dir, path)
		require.NoError(t, err)
		assert.False(t, strings.HasPrefix(rel, ".."), "archived path must stay under dir")

		// Returned string is compacted + "\n" + marker naming that file.
		wantMarker := archive.PointerMarker("stdout", path, len(original))
		assert.Equal(t, compacted+"\n"+wantMarker, annotated)
	})

	t.Run("gate before write: pointer would not net-shorten, no file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		a := archive.New(dir)

		// compacted is only marginally shorter than original; the pointer
		// marker (which embeds the abs path) pushes the annotated length
		// back over the original, so Annotate must decline before writing.
		original := strings.Repeat("x", 60)
		compacted := strings.Repeat("x", 55)

		annotated, ok := a.Annotate("s", "stdout", original, compacted, logger)
		assert.False(t, ok)
		assert.Empty(t, annotated)

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Empty(t, entries, "a gate-miss must not create any file")
	})

	t.Run("disabled archive declines and writes nothing", func(t *testing.T) {
		t.Parallel()

		original := strings.Repeat("x", 5000)

		t.Run("nil receiver", func(t *testing.T) {
			t.Parallel()

			var a *archive.Archive

			assert.True(t, a.Empty())
			assert.Equal(t, "", a.Dir())

			annotated, ok := a.Annotate("s", "stdout", original, "short", logger)
			assert.False(t, ok)
			assert.Empty(t, annotated)
		})

		t.Run("empty dir", func(t *testing.T) {
			t.Parallel()

			a := archive.New("")

			assert.True(t, a.Empty())
			assert.Equal(t, "", a.Dir())

			annotated, ok := a.Annotate("s", "stdout", original, "short", logger)
			assert.False(t, ok)
			assert.Empty(t, annotated)
		})
	})

	t.Run("save failure falls back without a file", func(t *testing.T) {
		t.Parallel()

		// dir lives under a regular file, so MkdirAll fails. The fixture
		// is sized so the gate passes first -- otherwise Annotate
		// short-circuits at the gate and never reaches the failing
		// MkdirAll.
		base := t.TempDir()
		notDir := filepath.Join(base, "not-a-dir")
		require.NoError(t, os.WriteFile(notDir, []byte("x"), 0o644))

		a := archive.New(filepath.Join(notDir, "outputs"))

		var buf bytes.Buffer

		warnLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

		annotated, ok := a.Annotate("s", "stdout", strings.Repeat("x", 5000), "short", warnLogger)
		assert.False(t, ok)
		assert.Empty(t, annotated)
		assert.Contains(t, strings.ToLower(buf.String()), "compaction output dir", "MkdirAll failure must warn")
	})

	t.Run("distinct paths for the same session and stream", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		a := archive.New(dir)
		original := strings.Repeat("x", 5000)

		a1, ok1 := a.Annotate("sess", "stdout", original, "short", logger)
		require.True(t, ok1)

		a2, ok2 := a.Annotate("sess", "stdout", original, "short", logger)
		require.True(t, ok2)

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Len(t, entries, 2, "two calls must produce two distinct files")
		assert.NotEqual(t, a1, a2, "the pointer markers must name distinct files")
	})
}

// TestAnnotateSanitizesSessionID pins the filename produced for
// hostile session IDs: every byte outside [A-Za-z0-9_-] maps to '_',
// an empty ID becomes "nosession", and the result always stays a
// single path component directly under the archive dir.
func TestAnnotateSanitizesSessionID(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	tests := map[string]struct {
		in   string
		want string
	}{
		"slash":       {in: "a/b", want: "a_b"},
		"dotdot":      {in: "..", want: "__"},
		"empty":       {in: "", want: "nosession"},
		"clean":       {in: "abc-123_XYZ", want: "abc-123_XYZ"},
		"traversal":   {in: "../../etc/passwd", want: "______etc_passwd"},
		"mixed bytes": {in: "a b.c:d", want: "a_b_c_d"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			a := archive.New(dir)

			_, ok := a.Annotate(tt.in, "stdout", strings.Repeat("x", 5000), "short", logger)
			require.True(t, ok)

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "the archived file must land directly under dir")

			got := entries[0].Name()
			assert.True(t, strings.HasPrefix(got, tt.want+"-stdout-"),
				"filename %q must start with sanitized prefix %q", got, tt.want+"-stdout-")
			assert.Equal(t, got, filepath.Base(got), "filename must be a single path component")
		})
	}
}

func TestCompactionPointerMarker(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		"    [hook-router: uncompacted stdout saved to /x/y.log (42 bytes)]",
		archive.PointerMarker("stdout", "/x/y.log", 42))
	assert.Equal(t,
		"    [hook-router: uncompacted stderr saved to /x/y.log (7 bytes)]",
		archive.PointerMarker("stderr", "/x/y.log", 7))
}

func TestSweepCompactionOutputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	t.Run("removes files older than the TTL, keeps fresh ones", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		oldFile := filepath.Join(dir, "old.log")
		require.NoError(t, os.WriteFile(oldFile, []byte("old"), 0o644))

		freshFile := filepath.Join(dir, "fresh.log")
		require.NoError(t, os.WriteFile(freshFile, []byte("fresh"), 0o644))

		const ttl = time.Hour

		// Push the old file's mtime well past the TTL; leave fresh at now.
		// os.Chtimes is deterministic -- no sleeps, hermetic check-phase.
		past := time.Now().Add(-2 * ttl)
		require.NoError(t, os.Chtimes(oldFile, past, past))

		archive.Sweep(dir, ttl, logger)

		_, err := os.Stat(oldFile)
		assert.True(t, os.IsNotExist(err), "a file older than the TTL must be removed")

		_, err = os.Stat(freshFile)
		assert.NoError(t, err, "a fresh file must be kept")
	})

	t.Run("missing dir is a silent no-op", func(t *testing.T) {
		t.Parallel()

		archive.Sweep(filepath.Join(t.TempDir(), "never-created"), time.Hour, logger)
	})

	t.Run("empty dir string is a silent no-op", func(t *testing.T) {
		t.Parallel()

		archive.Sweep("", time.Hour, logger)
	})
}
