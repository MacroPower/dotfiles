package statedir_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/statedir"
)

func TestSubdir(t *testing.T) { //nolint:tparallel // subtests use t.Setenv
	t.Run("honors XDG_STATE_HOME", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		state := t.TempDir()
		t.Setenv("XDG_STATE_HOME", state)

		assert.Equal(t, filepath.Join(state, "sub"), statedir.Subdir("sub"))
	})

	t.Run("falls back to ~/.local/state", func(t *testing.T) {
		// Cannot use t.Parallel with t.Setenv.
		t.Setenv("XDG_STATE_HOME", "")

		home, err := os.UserHomeDir()
		require.NoError(t, err)

		assert.Equal(t, filepath.Join(home, ".local", "state", "sub"), statedir.Subdir("sub"))
	})
}

func TestDir(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	assert.Equal(t, filepath.Join(state, "mcp-kubectx"), statedir.Dir())
}

func TestEnvTag(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "host", statedir.EnvTag(false))
	assert.Equal(t, "guest", statedir.EnvTag(true))
}
