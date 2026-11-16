package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain keeps the git config of the developer from every test. The
// tests run git in their setup and through the handlers, and keys of
// the git config change what git does. For example, a plain git clone
// names its remote after clone.defaultRemoteName. Each git command of
// the tests therefore skips the system config and reads an empty
// global config. A test that needs a global config points
// GIT_CONFIG_GLOBAL at a file of its own, as [setGlobalConfig] does.
func TestMain(m *testing.M) {
	for name, value := range map[string]string{
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   os.DevNull,
	} {
		err := os.Setenv(name, value)
		if err != nil {
			panic(err)
		}
	}

	os.Exit(m.Run())
}

// [TestMain] makes git skip the config file in each place where git
// looks for the system config or the global config of the developer.
// A plain git clone must therefore name its remote origin, whatever
// clone.defaultRemoteName says in that file.
func TestGitSkipsDeveloperConfig(t *testing.T) { //nolint:paralleltest // t.Setenv
	tcs := map[string]struct {
		// env names the variable that leads git to the config file.
		env string
		// file is the path of the config file inside the directory
		// that env names. It is empty when env names the config file.
		file string
	}{
		"system config": {
			env: "GIT_CONFIG_SYSTEM",
		},
		"config in the home directory": {
			env:  "HOME",
			file: ".gitconfig",
		},
		"config in the XDG directory": {
			env:  "XDG_CONFIG_HOME",
			file: filepath.Join("git", "config"),
		},
	}

	for name, tc := range tcs { //nolint:paralleltest // t.Setenv
		t.Run(name, func(t *testing.T) {
			bare := initBareRepo(t)

			path := filepath.Join(t.TempDir(), "config")
			file := filepath.Join(path, tc.file)

			require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
			require.NoError(t, os.WriteFile(file, []byte("[clone]\n\tdefaultRemoteName = upstream\n"), 0o644))
			t.Setenv(tc.env, path)

			work := filepath.Join(t.TempDir(), "work")
			runGitCmds(t, [][]string{{"git", "clone", "-q", bare, work}})

			assert.Equal(t, defaultRemote, gitOut(t, "-C", work, "remote"))
		})
	}
}

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
