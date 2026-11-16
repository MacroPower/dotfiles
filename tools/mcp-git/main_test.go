package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testGitEnv holds each variable that [TestMain] sets for every test,
// with its value.
var testGitEnv = map[string]string{
	"GIT_CONFIG_NOSYSTEM": "1",
	"GIT_CONFIG_GLOBAL":   os.DevNull,
	// A count of zero makes git skip each GIT_CONFIG_KEY_<n> and
	// GIT_CONFIG_VALUE_<n> variable of the developer.
	"GIT_CONFIG_COUNT": "0",
	// git -c passes its config in this variable to each command
	// that git starts, such as the command of git rebase --exec.
	"GIT_CONFIG_PARAMETERS": "",
	// The variable names the template directory, and it takes
	// precedence over the init.templateDir key of the config. With
	// an empty value, git init and git clone copy no file.
	"GIT_TEMPLATE_DIR": "",
	// git looks for the files git/ignore and git/attributes in the
	// directory that this variable names. It looks in the .config
	// directory of the home directory only when the variable is unset
	// or empty. No file exists below the null device, so git finds
	// neither file.
	"XDG_CONFIG_HOME": os.DevNull,
	// git reads a system attributes file for every repository, such
	// as /etc/gitattributes, and GIT_CONFIG_NOSYSTEM does not stop
	// that read. With this variable set to 1, git skips the file.
	"GIT_ATTR_NOSYSTEM": "1",
}

// TestMain keeps the git config, the template directory, the global
// ignore file, the global attributes file, and the repository of the
// developer from every test. It also keeps the system attributes file
// of the machine from every test. The tests run git in their setup and
// through the handlers. The setup of most tests copies a repository
// that the test binary builds with git one time, as fixture_test.go
// describes. The commands that build it take the environment that
// TestMain leaves, except their global config.
//
// Keys of the git config change what git does. For example, a plain
// git clone names its remote after clone.defaultRemoteName. Each git
// command of the tests therefore skips the system config and reads no
// config from the environment. Each one reads an empty global config,
// except the commands that build a template. Their global config sets
// only maintenance.auto to false, as [templateEnv] describes. Each git
// config command of the tests reads and writes the config of the
// repository, not a file that the environment names.
//
// A test that needs a global config points GIT_CONFIG_GLOBAL at a file
// of its own, as [setGlobalConfig] does. A test that needs config from
// the environment sets GIT_CONFIG_COUNT and its numbered variables, as
// [TestReadUserHooks] does.
//
// git init and git clone copy the files of the template directory,
// such as its hooks, into each repository that they create. The git
// commit of a test setup then runs those hooks. The init.templateDir
// key of the config names the template directory, and the
// GIT_TEMPLATE_DIR variable takes precedence over the key. TestMain
// sets the variable to an empty value, so git init and git clone copy
// no file. Each repository of the tests therefore starts without a
// hooks directory. A test that needs that directory creates it, as
// [makeHooksDir] and [writeHookIn] do. A test that needs a template
// directory sets GIT_TEMPLATE_DIR itself, as
// [TestHandleCloneSkipsHooks] does.
//
// git also reads a global ignore file and a global attributes file for
// every repository. The core.excludesFile key and the
// core.attributesFile key name those files, and the empty global
// config sets neither key. git then looks for the files in the
// directory that XDG_CONFIG_HOME names, or in the .config directory of
// the home directory. When the ignore file names README, the git add
// of a test setup skips the file, and the git commit after it fails.
// TestMain therefore points XDG_CONFIG_HOME at the null device, and
// git finds neither file.
//
// git also reads a system attributes file for every repository, such
// as /etc/gitattributes, and GIT_CONFIG_NOSYSTEM does not stop that
// read. TestMain therefore sets GIT_ATTR_NOSYSTEM, and git skips the
// file. The variables of TestMain do not reach git http-backend, so
// [gitBackend] sets the variable for it.
//
// Variables of the environment also point git at a repository. With
// GIT_DIR set, git -C <dir> acts on the repository that GIT_DIR names.
// The setup of a test then commits to the branch of the developer. git
// sets GIT_DIR and GIT_INDEX_FILE for the hooks of a commit in a
// linked worktree. A commit hook that runs the tests passes both
// variables on. TestMain therefore unsets each variable that
// [repositoryEnv] lists and that [testGitEnv] does not hold. Each git
// command of the tests then acts on the repository that git finds from
// the directory of the command. A test that needs one of the variables
// sets it for a call of the server alone, as [setRepositoryEnv]
// describes.
//
// Variables of the environment also select the formats of the
// repositories that git init creates. With GIT_DEFAULT_HASH set to
// sha256, an object name has 64 characters. git update-index then
// rejects the SHA-1 object name of 40 characters that the setup of
// [TestHandlePushRunsNoSubmoduleFetch] passes to it. TestMain
// therefore unsets each variable that [formatEnv] lists, and git init
// creates each repository in the default formats of the installed git.
func TestMain(m *testing.M) {
	for name, value := range testGitEnv {
		err := os.Setenv(name, value)
		if err != nil {
			panic(err)
		}
	}

	// git reads the system config and the global config before it
	// prints the names. The variables of testGitEnv are therefore in
	// place before repositoryEnv runs git.
	names, err := repositoryEnv(context.Background())
	if err != nil {
		panic(err)
	}

	for _, name := range slices.Concat(names, formatEnv()) {
		if _, ok := testGitEnv[name]; ok {
			continue
		}

		// git reads an empty value of some of these variables. For
		// example, a git config command without a file option reads
		// and writes the file that GIT_CONFIG names in place of the
		// config of the repository. An empty value names no file, and
		// git config then fails to write. git init reads the format
		// that a variable of formatEnv names, and it fails when the
		// value names no format. TestMain therefore unsets each
		// variable.
		err := os.Unsetenv(name)
		if err != nil {
			panic(err)
		}
	}

	// The templates take the environment that the steps before this
	// one leave.
	removeTemplates, err := setUpTemplates()
	if err != nil {
		panic(err)
	}

	code := m.Run()

	removeTemplates()
	os.Exit(code)
}

// formatEnv returns the name of each variable that [TestMain] unsets
// and that [repositoryEnv] does not list. Each variable selects a
// format of the repositories that git init creates. GIT_DEFAULT_HASH
// names the object format, such as sha256, and it takes precedence
// over the init.defaultObjectFormat key of the config.
// GIT_DEFAULT_REF_FORMAT names the format of the ref storage, such as
// reftable, and it takes precedence over the init.defaultRefFormat
// key. The empty global config sets neither key.
//
// GIT_REFERENCE_BACKEND names the ref storage of every repository that
// git opens, and it takes precedence over the extensions.refStorage key
// of the repository. git init creates the ref storage that the
// variable names, whatever format GIT_DEFAULT_REF_FORMAT names. A
// value such as files:///path/to/repo/.git also points git at the refs
// of another repository.
func formatEnv() []string {
	return []string{
		"GIT_DEFAULT_HASH",
		"GIT_DEFAULT_REF_FORMAT",
		"GIT_REFERENCE_BACKEND",
	}
}

// repositoryEnv returns the name of each variable of the environment
// that is local to one repository, such as GIT_DIR, GIT_WORK_TREE, and
// GIT_INDEX_FILE. git rev-parse --local-env-vars prints the names, and
// it does not need a repository to print them.
func repositoryEnv(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		return nil, fmt.Errorf("listing the repository variables of git: %w", err)
	}

	return strings.Fields(string(out)), nil
}

// repositoryPaths maps variables of the environment that point git at
// a repository to a path inside the working tree of that repository.
// A variable with that path points git at the part of the repository
// that the variable names, such as its index. The empty path names the
// working tree.
//
// The result holds each variable that the function names, and each
// variable of names. A test sets every variable of the result at one
// time. The code under test removes each variable on its own. When the
// code stops removing one variable, the test therefore leaves that
// variable set alone. For example, with only GIT_INDEX_FILE set, git
// finds the repository from the directory of the command. It then
// reads and writes the index of the other repository.
//
// A test must not take names from the list of the code under test. The
// test then stops setting a variable when the code stops removing it.
func repositoryPaths(names []string) map[string]string {
	paths := map[string]string{
		"GIT_DIR":              ".git",
		"GIT_COMMON_DIR":       ".git",
		"GIT_WORK_TREE":        "",
		"GIT_INDEX_FILE":       filepath.Join(".git", "index"),
		"GIT_OBJECT_DIRECTORY": filepath.Join(".git", "objects"),
	}

	for _, name := range names {
		if _, ok := paths[name]; !ok {
			paths[name] = ".git"
		}
	}

	return paths
}

// serverRepositoryPaths returns the result of [repositoryPaths] for
// each variable that the server must remove. git rev-parse
// --local-env-vars prints these variables, except
// GIT_REFERENCE_BACKEND. The server keeps the two variables of that
// list that pass config to every git command.
func serverRepositoryPaths(t *testing.T) map[string]string {
	t.Helper()

	names, err := repositoryEnv(t.Context())
	require.NoError(t, err)

	names = slices.DeleteFunc(names, func(name string) bool {
		return name == "GIT_CONFIG_PARAMETERS" || name == "GIT_CONFIG_COUNT"
	})

	return repositoryPaths(append(names, "GIT_REFERENCE_BACKEND"))
}

// setRepositoryEnv sets each variable of env to its path inside the
// working tree at repo, as [repositoryPaths] gives it. git reads the
// URI of a ref store from GIT_REFERENCE_BACKEND, so setRepositoryEnv
// gives that variable the path with the files:// scheme. It returns a
// function that unsets the variables, and t.Setenv restores them when
// the test ends.
//
// The git commands of the helpers read the variables too. A test
// therefore calls setRepositoryEnv after it creates its repositories,
// and it calls the returned function before it reads a repository.
func setRepositoryEnv(t *testing.T, repo string, env map[string]string) func() {
	t.Helper()

	for name, path := range env {
		value := filepath.Join(repo, path)
		if name == "GIT_REFERENCE_BACKEND" {
			value = "files://" + value
		}

		t.Setenv(name, value)
	}

	return func() {
		for name := range env {
			require.NoError(t, os.Unsetenv(name))
		}
	}
}

// readTree returns the mode of each file and each directory under
// root, by path. The entry of a regular file also holds the SHA-256
// hash of its content. A command that adds, removes, or changes a file
// under root between two calls therefore changes the result.
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()

	tree := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)

		info, err := d.Info()
		require.NoError(t, err)

		tree[path] = info.Mode().String()

		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			require.NoError(t, err)

			tree[path] += fmt.Sprintf(" %x", sha256.Sum256(content))
		}

		return nil
	})
	require.NoError(t, err)

	return tree
}

// readTrees returns the entries of [readTree] for every root.
func readTrees(t *testing.T, roots ...string) map[string]string {
	t.Helper()

	trees := map[string]string{}

	for _, root := range roots {
		maps.Copy(trees, readTree(t, root))
	}

	return trees
}

// requireUnchanged fails the test when a file or a directory under a
// root differs from before, the result of [readTrees] for the same
// roots. The message names each path that changed.
//
// A call that the server refuses before it runs the git command of the
// tool must change nothing. A test of such a refusal therefore checks
// the repositories of the call with requireUnchanged. A git command that
// fails can still write under the git directory. For example, a pull
// writes the index before it connects to the remote. A test of a git
// command that fails therefore checks the commits instead, as
// [remoteTool.requireNotLanded] does.
func requireUnchanged(t *testing.T, before map[string]string, roots ...string) {
	t.Helper()

	after := readTrees(t, roots...)

	var changed []string

	for path, entry := range before {
		if after[path] != entry {
			changed = append(changed, path)
		}
	}

	for path := range after {
		if _, ok := before[path]; !ok {
			changed = append(changed, path)
		}
	}

	slices.Sort(changed)
	require.Empty(t, changed, "the call must change no file of the repositories")
}

// plainCloneRemote makes a plain git clone of a new repository, and it
// returns the name that git gave the remote of the clone. Both git
// commands run in the environment of the test, so the repository is
// no copy of a template.
func plainCloneRemote(t *testing.T) string {
	t.Helper()

	bare := filepath.Join(t.TempDir(), "bare.git")
	work := filepath.Join(t.TempDir(), "work")

	runGitCmds(t, [][]string{
		{"git", "init", "-q", "--bare", bare},
		{"git", "clone", "-q", bare, work},
	})

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

// [TestMain] gives git an empty template directory. git init and git
// clone must therefore copy no hook of the template directory that
// GIT_TEMPLATE_DIR names. The git commands of the tests must run no
// hook of that directory.
//
// TestMain sets the variable when the test binary starts, so a test
// that sets it names a template directory again. The test therefore
// starts the test binary again in an environment that names a template
// directory with hooks, as the shell of a developer can. The new
// process runs only this test, and probeEnv makes the test run the
// setup of a test instead of starting a process.
func TestGitSkipsTemplateDirectory(t *testing.T) {
	t.Parallel()

	// probeEnv marks the process that this test started.
	const probeEnv = "MCP_GIT_TEST_TEMPLATE_PROBE"

	if os.Getenv(probeEnv) != "" {
		// The setup of most tests copies the repositories of these
		// helpers. The first call of each helper builds them with git
		// commands. The commands create a bare repository and clones
		// of it, and they commit and push.
		bare := initBareRepo(t)
		work := initWorkClone(t, bare)

		// The setup fires only some of the events of git. git copies
		// the hooks of a template directory into the hooks directory
		// of a new repository. A repository without that directory
		// therefore holds no hook of any event.
		assert.NoDirExists(t, filepath.Join(bare, "hooks"))
		assert.NoDirExists(t, filepath.Join(work, ".git", "hooks"))

		return
	}

	binary, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	template := filepath.Join(dir, "template")
	ran := filepath.Join(dir, "ran")

	// git fires these events for the commands of a test setup, such
	// as git add, git commit, git clone, and git push. A push fires
	// events in the clone and in the bare repository that receives
	// the push.
	events := []string{
		"pre-commit",
		"prepare-commit-msg",
		"commit-msg",
		"post-commit",
		"post-checkout",
		"post-index-change",
		"reference-transaction",
		"pre-push",
		"pre-receive",
		"update",
		"post-receive",
		"post-update",
	}

	// Each hook creates the file ran.
	for _, event := range events {
		writeHookIn(t, filepath.Join(template, "hooks"), event, "#!/bin/sh\ntouch '"+ran+"'\n")
	}

	cmd := exec.CommandContext(t.Context(), binary, "-test.run=^"+t.Name()+"$")
	cmd.Env = slices.Concat(os.Environ(), []string{"GIT_TEMPLATE_DIR=" + template, probeEnv + "=1"})

	out, err := cmd.CombinedOutput()

	// A process that fails can still run a hook, so the test reports
	// both.
	assert.NoFileExists(t, ran, "git ran a hook of the template directory")
	require.NoError(t, err, "%s", out)
}

// [TestMain] makes git find no global ignore file and no global
// attributes file of the developer. The git commands of the tests must
// therefore ignore no path and set no attribute that those files name.
// The setup of a test must commit the file that it writes.
//
// TestMain sets XDG_CONFIG_HOME when the test binary starts, so a test
// that sets the variable leads git to the files again. Each case
// therefore starts the test binary again. The environment of the new
// process leads git to an ignore file and to an attributes file of the
// case, as the environment of a developer does. The new process runs
// only this test. probeEnv makes the test check a path in a new
// repository and run the setup of a test instead of starting a
// process. The check of the ignore file and the check of the
// attributes file each fail on their own.
func TestGitSkipsDeveloperIgnoreAndAttributes(t *testing.T) {
	t.Parallel()

	// probeEnv marks a process that a case of this test started.
	const probeEnv = "MCP_GIT_TEST_IGNORE_PROBE"

	// attribute names an attribute that only the attributes file of a
	// case sets.
	const attribute = "mcp-git-test-developer"

	if os.Getenv(probeEnv) != "" {
		// git check-ignore and git check-attr run only in a
		// repository. A new repository ignores no path and sets no
		// attribute, so only a file of a case changes what the two
		// commands report.
		repo := t.TempDir()

		runGitCmds(t, [][]string{{"git", "-C", repo, "init", "--quiet"}})

		// git check-ignore exits 1 when no pattern ignores the path.
		// Otherwise it prints the file that holds the pattern.
		cmd := exec.CommandContext(t.Context(), "git", "-C", repo, "check-ignore", "--verbose", "README")

		var exitErr *exec.ExitError

		out, err := cmd.Output()
		require.ErrorAs(t, err, &exitErr, "git check-ignore found %q", out)
		assert.Equal(t, 1, exitErr.ExitCode())

		// git check-attr prints unspecified as the value of an
		// attribute that no attributes file sets for the path.
		got := gitOut(t, "-C", repo, "check-attr", attribute, "--", "README")
		assert.Equal(t, "README: "+attribute+": unspecified", got)

		// The setup of most tests copies the repositories of these
		// helpers. The first call of each helper builds them with git
		// commands. The commands write the README file, and their git
		// add adds every file that git does not ignore.
		work := initWorkClone(t, initBareRepo(t))
		assert.Equal(t, "init", gitOut(t, "-C", work, "log", "--format=%s"))

		return
	}

	binary, err := os.Executable()
	require.NoError(t, err)

	run := "-test.run=^" + t.Name() + "$"

	// This process holds the XDG_CONFIG_HOME of TestMain, and each new
	// process inherits the environment of this process. git looks in
	// the home directory only when the variable is unset or empty, so
	// the environment of each case starts without the variable.
	environ := slices.DeleteFunc(os.Environ(), func(entry string) bool {
		return strings.HasPrefix(entry, "XDG_CONFIG_HOME=")
	})

	// ignore ignores every path, and attributes sets attribute on
	// every path.
	const (
		ignore     = "*\n"
		attributes = "* " + attribute + "\n"
	)

	tcs := map[string]struct {
		// env names the variable that leads git to the files.
		env string
		// dir is the path of the directory of the files inside the
		// directory that env names.
		dir string
	}{
		"files in the XDG directory": {
			env: "XDG_CONFIG_HOME",
			dir: "git",
		},
		"files in the home directory": {
			env: "HOME",
			dir: filepath.Join(".config", "git"),
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			files := filepath.Join(dir, tc.dir)

			require.NoError(t, os.MkdirAll(files, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(files, "ignore"), []byte(ignore), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(files, "attributes"), []byte(attributes), 0o644))

			cmd := exec.CommandContext(t.Context(), binary, run)
			cmd.Env = slices.Concat(environ, []string{tc.env + "=" + dir, probeEnv + "=1"})

			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
		})
	}
}

// [TestMain] makes git skip the system attributes file of the machine.
// The git commands of the tests must therefore read no attributes file
// from outside their repositories.
//
// git has no variable that names another path for the system
// attributes file, so no case leads git to a file of its own. Each
// case runs git var GIT_ATTR_SYSTEM in the environment of some git
// commands of the tests. The command prints the path of the system
// attributes file, also when no file exists at that path. When
// GIT_ATTR_NOSYSTEM makes git skip the file, the command prints
// nothing, and it exits 1. git var knows the variable GIT_ATTR_SYSTEM
// in git 2.42.0 and later.
func TestGitSkipsSystemAttributes(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		// env returns the environment of the git command. With a nil
		// environment, the command inherits the environment of the
		// test process.
		env func(t *testing.T) []string
	}{
		"git command of a test": {
			env: func(*testing.T) []string {
				return nil
			},
		},
		"git command of a handler": {
			env: func(*testing.T) []string {
				return (&handler{}).commandEnv()
			},
		},
		// The commands that build the templates of fixture_test.go run
		// with an environment of their own.
		"git command that builds a template": {
			env: func(*testing.T) []string {
				return templateEnv
			},
		},
		// The variables of [TestMain] do not reach git http-backend.
		// The case runs git with the Env field of the handler alone.
		"git http-backend": {
			env: func(t *testing.T) []string {
				t.Helper()

				return gitBackend(t, t.TempDir()).Env
			},
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cmd := exec.CommandContext(t.Context(), "git", "-C", t.TempDir(), "var", "GIT_ATTR_SYSTEM")
			cmd.Env = tc.env(t)

			var exitErr *exec.ExitError

			out, err := cmd.Output()
			require.ErrorAs(t, err, &exitErr, "git reads the system attributes file %s", out)
			assert.Equal(t, 1, exitErr.ExitCode(), "%s", exitErr.Stderr)
		})
	}
}

// [TestMain] unsets each variable of the environment that points git
// at a repository. The git commands of the tests must therefore act
// on the repositories of the tests. They must leave the repository
// that the variables name as it was.
//
// TestMain unsets the variables when the test binary starts, so a
// test that sets them points git at that repository again. The test
// therefore starts the test binary again in an environment that names
// the repository of a developer, as the environment of a commit hook
// does. git sets GIT_DIR and GIT_INDEX_FILE for the hooks of a commit
// in a linked worktree. The new process runs only this test. probeEnv
// makes the test run the setup of a test and check its own environment
// instead of starting a process.
func TestGitSkipsEnvironmentRepository(t *testing.T) {
	t.Parallel()

	// probeEnv marks the process that this test started.
	const probeEnv = "MCP_GIT_TEST_REPOSITORY_PROBE"

	names, err := repositoryEnv(t.Context())
	require.NoError(t, err)

	if os.Getenv(probeEnv) != "" {
		// The setup of most tests copies the repositories of these
		// helpers. The first call of each helper builds them with git
		// commands. The commands write the git config, the index, and
		// a commit.
		work := initWorkClone(t, initBareRepo(t))
		assert.Equal(t, "init", gitOut(t, "-C", work, "log", "--format=%s"))

		// Some of the variables change only what git reads, such as
		// GIT_GRAFT_FILE, and the setup succeeds with them. Of the
		// variables of the list, the environment must therefore hold
		// only the variables of testGitEnv.
		for _, name := range names {
			want, wantSet := testGitEnv[name]
			got, gotSet := os.LookupEnv(name)

			assert.Equal(t, wantSet, gotSet, "%s is set", name)
			assert.Equal(t, want, got, name)
		}

		return
	}

	binary, err := os.Executable()
	require.NoError(t, err)

	// dev stands for the repository of the developer. Its setup does
	// not run git commit in dev. A commit starts git maintenance in
	// the background, and maintenance writes under the git directory.
	dev := initWorkClone(t, initBareRepo(t))
	before := readTree(t, dev)

	// The environment points every variable that git lists at dev.
	// TestMain gives the variables of testGitEnv their own values.
	env := []string{probeEnv + "=1"}
	for variable, path := range repositoryPaths(names) {
		env = append(env, variable+"="+filepath.Join(dev, path))
	}

	cmd := exec.CommandContext(t.Context(), binary, "-test.run=^"+t.Name()+"$")
	cmd.Env = slices.Concat(os.Environ(), env)

	out, err := cmd.CombinedOutput()

	// A process that fails can still write to the repository, so the
	// test reports both.
	assert.Equal(t, before, readTree(t, dev))
	require.NoError(t, err, "%s", out)
}

// [TestMain] unsets each variable of the environment that selects a
// format of the repositories that git init creates. The git init of a
// test setup must therefore read none of those variables, and it must
// create its repository whatever they say.
//
// TestMain unsets the variables when the test binary starts, so a test
// that sets them selects a format again. The test therefore starts the
// test binary again in an environment that sets every one of the
// variables, as the shell of a developer can. The value names no
// format, so a git init that reads one of the variables fails,
// whatever formats the installed git creates by default. The new
// process runs only this test. probeEnv makes the test check its own
// environment and run the setup of a test instead of starting a
// process. That check names each variable that TestMain left set.
func TestGitSkipsEnvironmentFormat(t *testing.T) {
	t.Parallel()

	// probeEnv marks the process that this test started.
	const probeEnv = "MCP_GIT_TEST_FORMAT_PROBE"

	// noFormat names no object format and no format of the ref
	// storage.
	const noFormat = "mcp-git-test-no-format"

	// variables names each variable that selects a format.
	variables := []string{
		"GIT_DEFAULT_HASH",
		"GIT_DEFAULT_REF_FORMAT",
		"GIT_REFERENCE_BACKEND",
	}

	if os.Getenv(probeEnv) != "" {
		for _, variable := range variables {
			_, set := os.LookupEnv(variable)
			assert.False(t, set, "TestMain left %s set", variable)
		}

		// The setup of most tests copies the repositories of these
		// helpers. The first call of each helper builds them with git
		// commands. The commands create a bare repository with git
		// init, and they commit to a clone of it.
		work := initWorkClone(t, initBareRepo(t))
		assert.Equal(t, "init", gitOut(t, "-C", work, "log", "--format=%s"))

		return
	}

	binary, err := os.Executable()
	require.NoError(t, err)

	env := []string{probeEnv + "=1"}
	for _, variable := range variables {
		env = append(env, variable+"="+noFormat)
	}

	cmd := exec.CommandContext(t.Context(), binary, "-test.run=^"+t.Name()+"$")
	cmd.Env = slices.Concat(os.Environ(), env)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
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
		"missing sibling of the target of a link": {
			// The ".." leads to the parent of the target of the link
			// also when the directory after it does not exist.
			dir:      deep + sep + ".." + sep + "missing",
			want:     filepath.Join(target, "missing"),
			skipDest: true,
		},
		"link after the parent of a missing directory": {
			dir:  root + sep + "missing" + sep + ".." + sep + "link" + sep + "gone",
			want: filepath.Join(target, "gone"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := mustResolveAllowDirs(t, tt.dir)
			require.Equal(t, []string{tt.want}, got)

			// The handler allows a destination under the directory
			// that the flag names.
			h := &handler{allowDirs: got}
			require.NoError(t, h.checkDest(filepath.Join(tt.want, "repo")))

			if tt.skipDest {
				return
			}

			// The handler allows a destination that the caller names
			// through the directory as the flag gave it.
			require.NoError(t, h.checkDest(filepath.Join(tt.dir, "repo")))
		})
	}
}

// An --allow-dir can pass through a symbolic link whose target does
// not exist when the server starts. The allowed directory stays the
// same after someone creates that target, both for the running server
// and after a restart.
func TestResolveAllowDirsLinkWithoutTarget(t *testing.T) {
	t.Parallel()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	target := filepath.Join(root, "target")
	later := filepath.Join(target, "later")
	link := filepath.Join(root, "link")

	require.NoError(t, os.Mkdir(target, 0o755))
	require.NoError(t, os.Symlink(later, link))

	sep := string(filepath.Separator)
	dirs := []string{filepath.Join(link, "repos"), link + sep + ".." + sep + "repos"}
	want := []string{filepath.Join(later, "repos"), filepath.Join(target, "repos")}

	before := mustResolveAllowDirs(t, dirs...)
	require.Equal(t, want, before)

	h := &handler{allowDirs: before[:1]}
	require.NoError(t, h.checkDest(filepath.Join(link, "repos", "repo")))

	require.NoError(t, os.Mkdir(later, 0o755))

	assert.NoError(t, h.checkDest(filepath.Join(link, "repos", "repo")))
	assert.Equal(t, want, mustResolveAllowDirs(t, dirs...))
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
