package kubeconfig_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kubeconfig"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("round-trips the modeled subset", func(t *testing.T) {
		t.Parallel()

		cfg := kubeconfig.Config{
			APIVersion:     "v1",
			Kind:           "Config",
			CurrentContext: "prod",
			Clusters: []kubeconfig.NamedCluster{
				{Name: "prod-cluster", Cluster: map[string]any{"server": "https://prod.example.com"}},
			},
			Contexts: []kubeconfig.NamedContext{
				{Name: "prod", Context: kubeconfig.Context{Cluster: "prod-cluster", User: "admin", Namespace: "ns"}},
			},
			Users: []kubeconfig.NamedUser{
				{Name: "admin", User: map[string]any{"token": "admin-token"}},
			},
		}

		data, err := cfg.Marshal()
		require.NoError(t, err)

		path := filepath.Join(t.TempDir(), "kubeconfig")
		require.NoError(t, os.WriteFile(path, data, 0o600))

		got, err := kubeconfig.Load(path)
		require.NoError(t, err)
		assert.Equal(t, &cfg, got)
	})

	t.Run("missing file wraps ErrLoad", func(t *testing.T) {
		t.Parallel()

		_, err := kubeconfig.Load(filepath.Join(t.TempDir(), "absent"))
		require.ErrorIs(t, err, kubeconfig.ErrLoad)
	})

	t.Run("malformed yaml wraps ErrLoad", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "bad")
		require.NoError(t, os.WriteFile(path, []byte("{not yaml"), 0o600))

		_, err := kubeconfig.Load(path)
		require.ErrorIs(t, err, kubeconfig.ErrLoad)
	})
}

func TestServerHost(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cluster any
		want    string
		wantErr bool
	}{
		"plain host": {
			cluster: map[string]any{"server": "https://prod.example.com"},
			want:    "prod.example.com",
		},
		"host with port": {
			cluster: map[string]any{"server": "https://prod.example.com:6443"},
			want:    "prod.example.com",
		},
		"not an object": {
			cluster: "https://prod.example.com",
			wantErr: true,
		},
		"missing server": {
			cluster: map[string]any{},
			wantErr: true,
		},
		"empty host": {
			cluster: map[string]any{"server": "https://"},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := kubeconfig.ServerHost(tc.cluster)
			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
