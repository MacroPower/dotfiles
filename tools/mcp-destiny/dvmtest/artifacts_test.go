package dvmtest_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvmtest"
)

func TestArtifacts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		file string
		want string
	}{
		"stored file": {
			file: "core-0.json",
			want: "[]",
		},
		"file that the test did not store": {
			file: "core-1.json",
			err:  dvm.ErrService,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// NewArtifacts returns maps that a test can fill.
			a := dvmtest.NewArtifacts("245114.26/1")
			a.Index["core"] = []string{"core-0.json", "core-1.json"}
			a.Files["core-0.json"] = "[]"

			cfg, err := a.Config(t.Context())
			require.NoError(t, err)
			assert.Equal(t, dvm.ArtifactIndex{
				ManifestVersion: "245114.26/1",
				Files:           map[string][]string{"core": {"core-0.json", "core-1.json"}},
			}, cfg.Artifacts)

			body, err := a.Artifact(t.Context(), tc.file)
			assert.Equal(t, []string{tc.file}, a.Fetched)

			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)

			raw, err := io.ReadAll(body)
			require.NoError(t, err)
			require.NoError(t, body.Close())
			assert.Equal(t, tc.want, string(raw))
		})
	}
}
