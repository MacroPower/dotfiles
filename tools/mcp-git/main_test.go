package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain keeps the git config of the developer from every test. The
// tests run git in their setup and through the handlers, and keys of
// the git config change what git does. For example, a plain git clone
// names its remote after clone.defaultRemoteName. Each git command of
// the tests therefore skips the system config, reads an empty global
// config, and reads no config from the environment. Each git config
// command of the tests reads and writes the config of the repository,
// not a file that the environment names. A test that needs a global
// config points GIT_CONFIG_GLOBAL at a file of its own, as
// [setGlobalConfig] does. A test that needs config from the
// environment sets GIT_CONFIG_COUNT and its numbered variables, as
// [TestReadUserHooks] does.
func TestMain(m *testing.M) {
	for name, value := range map[string]string{
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		// A count of zero makes git skip each GIT_CONFIG_KEY_<n> and
		// GIT_CONFIG_VALUE_<n> variable of the developer.
		"GIT_CONFIG_COUNT": "0",
		// git -c passes its config in this variable to each command
		// that git starts, such as the command of git rebase --exec.
		"GIT_CONFIG_PARAMETERS": "",
	} {
		err := os.Setenv(name, value)
		if err != nil {
			panic(err)
		}
	}

	// A git config command without a file option reads and writes the
	// file that GIT_CONFIG names in place of the config of the
	// repository. An empty value names no file, and git config then
	// fails to write, so TestMain unsets the variable.
	err := os.Unsetenv("GIT_CONFIG")
	if err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

// plainCloneRemote makes a plain git clone of a new repository, and it
// returns the name that git gave the remote of the clone.
func plainCloneRemote(t *testing.T) string {
	t.Helper()

	bare := initBareRepo(t)
	work := filepath.Join(t.TempDir(), "work")

	runGitCmds(t, [][]string{{"git", "clone", "-q", bare, work}})

	return gitOut(t, "-C", work, "remote")
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
			path := filepath.Join(t.TempDir(), "config")
			file := filepath.Join(path, tc.file)

			require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
			require.NoError(t, os.WriteFile(file, []byte("[clone]\n\tdefaultRemoteName = upstream\n"), 0o644))
			t.Setenv(tc.env, path)

			assert.Equal(t, defaultRemote, plainCloneRemote(t))
		})
	}
}

// [TestMain] makes git read no config from the variables of the
// environment that pass config to git or that name the config file of
// git config. A plain git clone must therefore name its remote origin,
// and git config must find no clone.defaultRemoteName, whatever those
// variables say.
//
// TestMain resets the variables when the test binary starts, so a test
// that sets them puts the config back. Each case therefore starts the
// test binary again in an environment that sets the key, as the shell
// of a developer can. The new process runs only this test, and
// probeEnv makes the test check a clone and the config instead of
// starting a process.
func TestGitSkipsEnvironmentConfig(t *testing.T) {
	t.Parallel()

	// probeEnv marks a process that a case of this test started.
	const probeEnv = "MCP_GIT_TEST_CLONE_PROBE"

	if os.Getenv(probeEnv) != "" {
		assert.Equal(t, defaultRemote, plainCloneRemote(t))

		// git config exits 1 when no config sets the key. The command
		// runs outside every repository, so only the config of the
		// developer can set the key.
		cmd := exec.CommandContext(t.Context(), "git", "-C", t.TempDir(), "config", "--get", "clone.defaultRemoteName")

		var exitErr *exec.ExitError

		out, err := cmd.Output()
		require.ErrorAs(t, err, &exitErr, "git config found %q", out)
		assert.Equal(t, 1, exitErr.ExitCode())

		return
	}

	binary, err := os.Executable()
	require.NoError(t, err)

	run := "-test.run=^" + t.Name() + "$"

	file := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(file, []byte("[clone]\n\tdefaultRemoteName = upstream\n"), 0o644))

	tcs := map[string][]string{
		"config file variable": {
			"GIT_CONFIG=" + file,
		},
		"numbered variables": {
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=clone.defaultRemoteName",
			"GIT_CONFIG_VALUE_0=upstream",
		},
		"parameters variable": {
			"GIT_CONFIG_PARAMETERS='clone.defaultRemoteName'='upstream'",
		},
	}

	for name, env := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cmd := exec.CommandContext(t.Context(), binary, run)
			cmd.Env = slices.Concat(os.Environ(), env, []string{probeEnv + "=1"})

			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
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
