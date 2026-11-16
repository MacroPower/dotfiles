package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveAllowDirs(t *testing.T) {
	t.Parallel()

	// The temporary directory can lie behind a symbolic link of its
	// own, as it does on macOS. The expected paths therefore start
	// from its resolved path.
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	deep := filepath.Join(root, "deep")

	require.NoError(t, os.MkdirAll(filepath.Join(target, "existing"), 0o755))
	require.NoError(t, os.Symlink(target, link))
	require.NoError(t, os.Symlink(filepath.Join(target, "existing"), deep))

	sep := string(filepath.Separator)

	tests := map[string]struct {
		dir  string
		want string
		// skipDest skips the check of a destination under dir.
		// checkDest cleans the destination before it resolves the
		// links, so it drops a ".." that follows a link.
		skipDest bool
	}{
		"link": {
			dir:  link,
			want: target,
		},
		"existing directory behind a link": {
			dir:  filepath.Join(link, "existing"),
			want: filepath.Join(target, "existing"),
		},
		"missing directory behind a link": {
			dir:  filepath.Join(link, "missing"),
			want: filepath.Join(target, "missing"),
		},
		"missing nested directory behind a link": {
			dir:  filepath.Join(link, "missing", "nested"),
			want: filepath.Join(target, "missing", "nested"),
		},
		"missing directory with a final separator": {
			dir:  filepath.Join(link, "missing") + string(filepath.Separator),
			want: filepath.Join(target, "missing"),
		},
		"parent of a missing directory behind a link": {
			// filepath.Join cleans the path, so a concatenation
			// keeps the "..".
			dir:  link + sep + "missing" + sep + "..",
			want: target,
		},
		"missing directory without a link": {
			dir:  filepath.Join(target, "missing"),
			want: filepath.Join(target, "missing"),
		},
		"parent of the target of a link": {
			// filepath.Join cleans the path, so a concatenation
			// keeps the "..".
			dir:      deep + sep + ".." + sep + "existing",
			want:     filepath.Join(target, "existing"),
			skipDest: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := resolveAllowDirs([]string{tt.dir})
			require.Equal(t, []string{tt.want}, got)

			if tt.skipDest {
				return
			}

			// The handler allows a destination that the caller names
			// through the directory as the flag gave it.
			h := &handler{allowDirs: got}
			require.NoError(t, h.checkDest(filepath.Join(tt.dir, "repo")))
		})
	}
}
