package main

import (
	"os"
	"path/filepath"
	"runtime"
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

// mustResolveAllowDirs resolves dirs as the server does when it
// starts.
func mustResolveAllowDirs(t *testing.T, dirs ...string) []string {
	t.Helper()

	resolved, err := resolveAllowDirs(dirs)
	require.NoError(t, err)

	return resolved
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

			got := mustResolveAllowDirs(t, tt.dir)
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

// The server reads a relative --allow-dir against its working
// directory, as [handler.checkDest] reads a relative path. The handler
// then allows a path under the directory by its relative spelling and
// by its absolute spelling.
func TestResolveAllowDirsRelative(t *testing.T) { //nolint:paralleltest // t.Chdir
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	wd := filepath.Join(root, "wd")
	target := filepath.Join(root, "target")

	require.NoError(t, os.MkdirAll(filepath.Join(wd, "allowed"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(target, "existing"), 0o755))
	require.NoError(t, os.Symlink(target, filepath.Join(wd, "link")))
	require.NoError(t, os.Symlink(filepath.Join(target, "existing"), filepath.Join(wd, "deep")))

	t.Chdir(wd)

	sep := string(filepath.Separator)

	tests := map[string]struct {
		dir  string
		want string
	}{
		"directory": {
			dir:  "allowed",
			want: filepath.Join(wd, "allowed"),
		},
		"directory after a dot": {
			dir:  "." + sep + "allowed",
			want: filepath.Join(wd, "allowed"),
		},
		"directory with a final separator": {
			dir:  "allowed" + sep,
			want: filepath.Join(wd, "allowed"),
		},
		"working directory": {
			dir:  ".",
			want: wd,
		},
		"parent of the working directory": {
			dir:  "..",
			want: root,
		},
		"sibling of the working directory": {
			dir:  filepath.Join("..", "target"),
			want: target,
		},
		"missing directory": {
			dir:  "missing",
			want: filepath.Join(wd, "missing"),
		},
		"missing nested directory": {
			dir:  filepath.Join("missing", "nested"),
			want: filepath.Join(wd, "missing", "nested"),
		},
		"missing sibling of the working directory": {
			dir:  filepath.Join("..", "missing"),
			want: filepath.Join(root, "missing"),
		},
		"link": {
			dir:  "link",
			want: target,
		},
		"missing directory behind a link": {
			dir:  filepath.Join("link", "missing"),
			want: filepath.Join(target, "missing"),
		},
		"parent of a link": {
			// filepath.Join cleans the path, so a concatenation
			// keeps the "..". The server cleans a relative
			// --allow-dir as text before it resolves the links, as
			// checkDest cleans a relative path. The ".." therefore
			// drops the link and leads back to the working directory.
			dir:  "deep" + sep + ".." + sep + "existing",
			want: filepath.Join(wd, "existing"),
		},
	}

	for name, tt := range tests { //nolint:paralleltest // t.Chdir
		t.Run(name, func(t *testing.T) {
			got := mustResolveAllowDirs(t, tt.dir)
			require.Equal(t, []string{tt.want}, got)

			h := &handler{allowDirs: got}

			require.NoError(t, h.checkDest(filepath.Join(tt.want, "repo")))
			require.NoError(t, h.checkDest(tt.dir+sep+"repo"))
			require.ErrorIs(t, h.checkDest(filepath.Join(outside, "repo")), ErrDeniedDest)
		})
	}
}

// [os.Getwd] returns $PWD when $PWD names the working directory, and
// $PWD can pass through a symbolic link. [handler.checkDest] reads a
// relative path against $PWD with [filepath.Abs], and the server reads
// a relative --allow-dir the same way. A ".." at the start of the
// directory therefore leads to the parent of $PWD, which is not the
// parent of the physical working directory.
func TestResolveAllowDirsRelativeThroughSymlink(t *testing.T) { //nolint:paralleltest // t.Chdir, t.Setenv
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	// The physical working directory is root/parent/target, and $PWD
	// is root/link. Both parents hold a directory named existing, so
	// each case tells the two parents apart.
	parent := filepath.Join(root, "parent")
	target := filepath.Join(parent, "target")
	link := filepath.Join(root, "link")

	require.NoError(t, os.MkdirAll(filepath.Join(parent, "existing"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(root, "existing"), 0o755))
	require.NoError(t, os.Mkdir(target, 0o755))
	require.NoError(t, os.Symlink(target, link))

	t.Chdir(target)
	t.Setenv("PWD", link)

	wd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, link, wd, "Getwd must report the working directory through the link")

	tests := map[string]struct {
		dir  string
		want string
	}{
		"working directory": {
			dir:  ".",
			want: target,
		},
		"missing directory": {
			dir:  "missing",
			want: filepath.Join(target, "missing"),
		},
		"parent of the working directory": {
			dir:  "..",
			want: root,
		},
		"sibling of the working directory": {
			dir:  filepath.Join("..", "existing"),
			want: filepath.Join(root, "existing"),
		},
		"missing sibling of the working directory": {
			dir:  filepath.Join("..", "missing"),
			want: filepath.Join(root, "missing"),
		},
	}

	for name, tt := range tests { //nolint:paralleltest // t.Chdir
		t.Run(name, func(t *testing.T) {
			got := mustResolveAllowDirs(t, tt.dir)
			require.Equal(t, []string{tt.want}, got)

			h := &handler{allowDirs: got}
			require.NoError(t, h.checkDest(filepath.Join(tt.dir, "repo")))
		})
	}
}

// [os.Getwd] fails when someone deleted the working directory. The
// server then cannot read a relative --allow-dir, and the error names
// the value of the flag.
func TestResolveAllowDirsWithoutWorkingDirectory(t *testing.T) { //nolint:paralleltest // t.Chdir
	if runtime.GOOS != "linux" {
		t.Skip("the getcwd of Linux fails with ENOENT for a deleted working directory")
	}

	abs, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(dir, 0o755))

	t.Chdir(dir)
	require.NoError(t, os.Remove(dir))

	got, err := resolveAllowDirs([]string{abs, "repos"})
	require.ErrorContains(t, err, `resolving --allow-dir "repos": `)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, got)

	// An absolute directory does not depend on the working directory.
	require.Equal(t, []string{abs}, mustResolveAllowDirs(t, abs))
}

func TestResolveAllowDirsEmpty(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		dirs []string
	}{
		"only value": {
			dirs: []string{""},
		},
		"value after a directory": {
			dirs: []string{t.TempDir(), ""},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveAllowDirs(tt.dirs)
			require.ErrorIs(t, err, ErrEmptyAllowDir)
			require.Nil(t, got)
		})
	}
}
