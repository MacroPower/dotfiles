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

		_, err = os.Stat(path + ".tmp")
		assert.True(t, os.IsNotExist(err), "tmp file must be removed after rename")
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

	t.Run("tolerates leftover tmp from a prior crash", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path+".tmp", []byte("torn"), 0o600))

		require.NoError(t, statefile.WriteAtomic(path, []byte("fresh"), 0o600))

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "fresh", string(data))
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
}
