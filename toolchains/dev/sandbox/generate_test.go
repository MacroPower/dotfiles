package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"
)

func TestGenerate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		setup   func(t *testing.T) string
		err     error
		wantMsg string
	}{
		"missing file": {
			setup: func(t *testing.T) string {
				t.Helper()

				return "/nonexistent/path/config.yaml"
			},
			wantMsg: "reading config",
		},
		"invalid YAML": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				path := filepath.Join(dir, "config.yaml")
				require.NoError(t, os.WriteFile(path, []byte(":\n  :\n  - [invalid"), 0o644))

				return path
			},
			wantMsg: "parsing config",
		},
		"empty FQDN selector": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				path := filepath.Join(dir, "config.yaml")
				cfg := "egress:\n  - toFQDNs:\n      - {}\n"
				require.NoError(t, os.WriteFile(path, []byte(cfg), 0o644))

				return path
			},
			err:     sandbox.ErrFQDNSelectorEmpty,
			wantMsg: "parsing config",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := tt.setup(t)

			err := sandbox.Generate(path)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantMsg)

			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
		})
	}
}
