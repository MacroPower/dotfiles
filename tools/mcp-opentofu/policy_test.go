package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	t.Parallel()

	d := Defaults()

	for _, tool := range []string{toolValidate, toolInit, toolPlan} {
		p, ok := d[tool]
		require.True(t, ok, "expected default policy for %q", tool)
		assert.Empty(t, p.AllowedDomains)
		assert.Empty(t, p.AllowRead)
		assert.Empty(t, p.AllowWrite)
		assert.Empty(t, p.AllowUnixSockets)
	}
}

func TestLoadFile(t *testing.T) {
	t.Parallel()

	t.Run("ok", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "policy.json")
		body := `{
			"init":     {"allowed_domains": ["registry.opentofu.org"], "allow_read": [], "allow_write": []},
			"validate": {"allowed_domains": [], "allow_read": [], "allow_write": []},
			"plan":     {"allowed_domains": [], "allow_read": [], "allow_write": []}
		}`
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

		got, err := LoadFile(path)
		require.NoError(t, err)
		require.Contains(t, got, "init")
		assert.Equal(t, []string{"registry.opentofu.org"}, got["init"].AllowedDomains)
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()

		_, err := LoadFile(filepath.Join(t.TempDir(), "nope.json"))
		require.ErrorIs(t, err, ErrPolicy)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("malformed", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "policy.json")
		require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))

		_, err := LoadFile(path)
		require.ErrorIs(t, err, ErrPolicy)
	})
}

func TestValidateExtraPath(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(rootDir)
	require.NoError(t, err)

	outsideDir := t.TempDir()
	resolvedOutside, err := filepath.EvalSymlinks(outsideDir)
	require.NoError(t, err)

	require.NoError(t, os.Mkdir(filepath.Join(resolvedRoot, ".ssh"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(resolvedRoot, ".ssh", "id_rsa"), []byte("x"), 0o600))

	share := filepath.Join(resolvedRoot, "share")
	require.NoError(t, os.Mkdir(share, 0o755))

	link := filepath.Join(resolvedRoot, "link")
	require.NoError(t, os.Symlink(share, link))

	tests := map[string]struct {
		in      string
		want    string
		wantErr string
	}{
		"absolute under root": {
			in:   share,
			want: share,
		},
		"symlink resolves to allowed target": {
			in:   link,
			want: share,
		},
		"empty": {
			in:      "",
			wantErr: "is empty",
		},
		"relative": {
			in:      "share",
			wantErr: "must be absolute",
		},
		"outside root": {
			in:      resolvedOutside,
			wantErr: "outside allow root",
		},
		"credentials deny set": {
			in:      filepath.Join(resolvedRoot, ".ssh"),
			wantErr: "credentials deny set",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := validateExtraPath(tt.in, resolvedRoot)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrPolicy)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestResolveAllowRootSymlink confirms that a symlinked allow-root is
// resolved to its real path so [validateExtraPath] callers get a stable
// prefix to compare against. Cannot run in parallel because it uses
// t.Setenv to point [allowRootEnv] at the symlink.
func TestResolveAllowRootSymlink(t *testing.T) {
	target := t.TempDir()
	resolvedTarget, err := filepath.EvalSymlinks(target)
	require.NoError(t, err)

	rootDir := t.TempDir()
	rootLink := filepath.Join(rootDir, "root")
	require.NoError(t, os.Symlink(resolvedTarget, rootLink))

	t.Setenv(allowRootEnv, rootLink)
	t.Setenv("HOME", resolvedTarget)

	got, err := resolveAllowRoot()
	require.NoError(t, err)
	assert.Equal(t, resolvedTarget, got)

	share := filepath.Join(resolvedTarget, "share")
	require.NoError(t, os.Mkdir(share, 0o755))

	resolved, err := validateExtraPath(share, got)
	require.NoError(t, err)
	assert.Equal(t, share, resolved)
}

func TestMergeAllowRead(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		base   []string
		extras []string
		want   []string
	}{
		"empty extras": {
			base: []string{"/a"},
			want: []string{"/a"},
		},
		"dedup": {
			base:   []string{"/a", "/b"},
			extras: []string{"/b", "/c"},
			want:   []string{"/a", "/b", "/c"},
		},
		"empty base": {
			extras: []string{"/a"},
			want:   []string{"/a"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := mergeAllowRead(tt.base, tt.extras)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMergeAllowReadDoesNotAliasBase(t *testing.T) {
	t.Parallel()

	base := []string{"/a"}
	got := mergeAllowRead(base, nil)
	got = append(got, "/extra")

	assert.Equal(t, []string{"/a"}, base, "base must not see the appended entry")
}
