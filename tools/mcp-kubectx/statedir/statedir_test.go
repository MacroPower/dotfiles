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

func TestRunDir(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	assert.Equal(t, filepath.Join(state, "mcp-kubectx-run"), statedir.RunDir())
}

// TestKubeconfigPath pins the directory and the env tag of each
// env. The kubeconfig of every serve lies in [statedir.RunDir],
// which workmux does not mount into a Lima guest. A guest that
// rewrites the kubeconfig of a host-side serve picks the command
// that host-side kubectl runs.
func TestKubeconfigPath(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	tests := map[string]struct {
		want     string
		forGuest bool
	}{
		"host env": {
			forGuest: false,
			want:     filepath.Join(state, "mcp-kubectx-run", "kubeconfig.1234.host.yaml"),
		},
		"guest env": {
			forGuest: true,
			want:     filepath.Join(state, "mcp-kubectx-run", "kubeconfig.1234.guest.yaml"),
		},
	}

	for name, tc := range tests { //nolint:paralleltest // parent uses t.Setenv
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, statedir.KubeconfigPath(1234, tc.forGuest))
		})
	}
}

func TestEnvTag(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "host", statedir.EnvTag(false))
	assert.Equal(t, "guest", statedir.EnvTag(true))
}
