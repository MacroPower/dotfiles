package statefile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statefile"
)

func TestWriteSecure(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sub", "file")

	require.NoError(t, statefile.WriteSecure(path, []byte("data")))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	parent, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), parent.Mode().Perm())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "data", string(data))

	assertOnly(t, filepath.Dir(path), "file")
}

// TestWriteReplacesSymlink pins that neither write writes through a
// symlink at its path. A Lima guest can plant such a symlink in each
// directory that workmux mounts writable into the guest. The write
// replaces the symlink with a regular file and leaves its target as
// it was, or missing.
func TestWriteReplacesSymlink(t *testing.T) {
	t.Parallel()

	writes := map[string]func(path string, data []byte) error{
		"WriteSecure": statefile.WriteSecure,
		"WriteAtomic": func(path string, data []byte) error {
			return statefile.WriteAtomic(path, data, 0o600)
		},
	}

	targets := map[string]struct {
		before []byte
	}{
		"existing target": {before: []byte("keep")},
		"missing target":  {},
	}

	for writeName, write := range writes {
		for targetName, tc := range targets {
			t.Run(writeName+"/"+targetName, func(t *testing.T) {
				t.Parallel()

				outside := t.TempDir()
				target := filepath.Join(outside, "target")

				if tc.before != nil {
					require.NoError(t, os.WriteFile(target, tc.before, 0o600))
				}

				path := filepath.Join(t.TempDir(), "file")
				require.NoError(t, os.Symlink(target, path))

				require.NoError(t, write(path, []byte("new")))

				info, err := os.Lstat(path)
				require.NoError(t, err)
				assert.True(t, info.Mode().IsRegular(), "the write must replace the symlink")

				data, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, "new", string(data))

				data, err = os.ReadFile(target)
				if tc.before == nil {
					require.ErrorIs(t, err, os.ErrNotExist)
					return
				}

				require.NoError(t, err)
				assert.Equal(t, tc.before, data)
			})
		}
	}
}

func TestWriteAtomic(t *testing.T) {
	t.Parallel()

	t.Run("writes content with mode and removes tmp", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")

		require.NoError(t, statefile.WriteAtomic(path, []byte("v1"), 0o600))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "v1", string(data))

		assertOnly(t, filepath.Dir(path), "file")
	})

	// os.CreateTemp creates the tmp file with mode 0600, and a umask
	// such as 022 clears the group and other write bits of 0666. So
	// the file gets mode 0666 only when WriteAtomic sets the mode on
	// the open tmp file.
	t.Run("applies a mode other than the tmp default", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")

		require.NoError(t, statefile.WriteAtomic(path, []byte("v"), 0o666))

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o666), info.Mode().Perm())

		assertOnly(t, filepath.Dir(path), "file")
	})

	t.Run("replaces existing content", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))

		require.NoError(t, statefile.WriteAtomic(path, []byte("new"), 0o600))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "new", string(data))
	})

	// A crash between the create and the rename leaves a tmp file
	// under a random name that ends in .tmp. A later write succeeds
	// and leaves that file in place.
	t.Run("leaves the tmp of a prior crash in place", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "file")
		require.NoError(t, os.WriteFile(path+".123456.tmp", []byte("torn"), 0o600))

		require.NoError(t, statefile.WriteAtomic(path, []byte("fresh"), 0o600))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "fresh", string(data))

		data, err = os.ReadFile(path + ".123456.tmp")
		require.NoError(t, err)
		assert.Equal(t, "torn", string(data))

		assertOnly(t, dir, "file", "file.123456.tmp")
	})

	// The tmp file gets a new random name. So a symlink that another
	// process plants at a name that it can predict, such as
	// <path>.tmp, never receives the data, and WriteAtomic leaves it
	// in place.
	t.Run("leaves a symlink at a predictable tmp name alone", func(t *testing.T) {
		t.Parallel()

		target := filepath.Join(t.TempDir(), "target")
		require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))

		dir := t.TempDir()
		path := filepath.Join(dir, "file")
		require.NoError(t, os.Symlink(target, path+".tmp"))

		require.NoError(t, statefile.WriteAtomic(path, []byte("fresh"), 0o600))

		link, err := os.Readlink(path + ".tmp")
		require.NoError(t, err)
		assert.Equal(t, target, link)

		data, err := os.ReadFile(target)
		require.NoError(t, err)
		assert.Equal(t, "keep", string(data))

		assertOnly(t, dir, "file", "file.tmp")
	})

	// Each write uses a new random tmp name, so no later write removes
	// a tmp file that a failed rename leaves behind. WriteAtomic must
	// remove its own tmp file when the rename fails. A non-empty
	// directory at the path makes the rename fail.
	t.Run("a failed rename leaves no tmp file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "file")
		require.NoError(t, os.MkdirAll(filepath.Join(path, "sub"), 0o700))

		require.Error(t, statefile.WriteAtomic(path, []byte("x"), 0o600))

		assertOnly(t, dir, "file")
	})

	t.Run("missing parent dir surfaces an error", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "absent", "file")

		require.Error(t, statefile.WriteAtomic(path, []byte("x"), 0o600))
	})
}

func TestSymlinkAtomic(t *testing.T) {
	t.Parallel()

	t.Run("creates symlink with parent dirs", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "sub", "link")

		require.NoError(t, statefile.SymlinkAtomic(path, "/target/a"))

		target, err := os.Readlink(path)
		require.NoError(t, err)
		assert.Equal(t, "/target/a", target)
	})

	t.Run("replaces existing symlink", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "link")
		require.NoError(t, os.Symlink("/target/old", path))

		require.NoError(t, statefile.SymlinkAtomic(path, "/target/new"))

		target, err := os.Readlink(path)
		require.NoError(t, err)
		assert.Equal(t, "/target/new", target)
	})

	// SymlinkAtomic uses the fixed tmp name <path>.tmp, but it writes
	// no file data. So the target of a symlink at that name keeps its
	// content.
	t.Run("leaves the target of a symlink at the tmp name alone", func(t *testing.T) {
		t.Parallel()

		outside := filepath.Join(t.TempDir(), "outside")
		require.NoError(t, os.WriteFile(outside, []byte("keep"), 0o600))

		dir := t.TempDir()
		path := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(outside, path+".tmp"))

		require.NoError(t, statefile.SymlinkAtomic(path, "/target/new"))

		target, err := os.Readlink(path)
		require.NoError(t, err)
		assert.Equal(t, "/target/new", target)

		data, err := os.ReadFile(outside)
		require.NoError(t, err)
		assert.Equal(t, "keep", string(data))

		assertOnly(t, dir, "link")
	})

	t.Run("fails on a non-empty directory at the tmp name", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "link")
		require.NoError(t, os.MkdirAll(filepath.Join(path+".tmp", "sub"), 0o700))

		require.Error(t, statefile.SymlinkAtomic(path, "/target/new"))

		_, err := os.Lstat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

// assertOnly asserts that dir holds exactly the entries names, so no
// tmp file of a write remains.
func assertOnly(t *testing.T, dir string, names ...string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}

	assert.ElementsMatch(t, names, got)
}
