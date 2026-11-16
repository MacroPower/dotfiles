package gittest

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// probeEnv marks a process that [CheckRepositoryEnv] started.
const probeEnv = "HOOK_ROUTER_GITTEST_PROBE"

// configProbeEnv marks a process that [CheckConfigEnv] started.
const configProbeEnv = "HOOK_ROUTER_GITTEST_CONFIG_PROBE"

// canaryKey names a config key that only the config file of a case of
// [CheckConfigEnv] sets.
const canaryKey = "gittest.developerConfig"

// canaryAttribute names an attribute that only the attributes file of
// a case of [CheckConfigEnv] sets.
const canaryAttribute = "gittest-developer"

// canaryPath names the path that a process of a case of
// [CheckConfigEnv] passes to git check-ignore and to git check-attr.
// The file does not exist.
const canaryPath = "canary.txt"

// noFormat is the value that a case of [CheckConfigEnv] gives to a
// variable that [formatEnv] lists. It names no format, so a git init
// that reads the variable fails.
const noFormat = "gittest-no-format"

// configEnv holds each variable that [IsolateEnv] sets, with its
// value. With GIT_CONFIG_NOSYSTEM set, git skips the system config.
// With GIT_CONFIG_GLOBAL set, git reads the file that the variable
// names as the global config. It then reads neither the .gitconfig
// file of the home directory nor the config file of the XDG directory.
//
// GIT_TEMPLATE_DIR names the template directory, and it takes
// precedence over the init.templateDir key of the config. git init and
// git clone copy the files of the template directory, such as its
// hooks, into each repository that they create. With the variable set
// to an empty value, they copy no file.
//
// git reads a global ignore file and a global attributes file for
// every repository. The core.excludesFile key and the
// core.attributesFile key of the config name those files, and the
// empty global config sets neither key. git then looks for the files
// git/ignore and git/attributes in the directory that XDG_CONFIG_HOME
// names. It looks in the .config directory of the home directory only
// when the variable is unset or empty. With the variable set to the
// null device, git finds neither file, because no file exists below
// the null device.
//
// git also reads a system attributes file for every repository, such
// as /etc/gitattributes. GIT_CONFIG_NOSYSTEM does not stop that read.
// With GIT_ATTR_NOSYSTEM set to 1, git skips the system attributes
// file.
var configEnv = map[string]string{
	"GIT_CONFIG_NOSYSTEM": "1",
	"GIT_CONFIG_GLOBAL":   os.DevNull,
	"GIT_TEMPLATE_DIR":    "",
	"XDG_CONFIG_HOME":     os.DevNull,
	"GIT_ATTR_NOSYSTEM":   "1",
}

// formatEnv returns the name of each variable that [IsolateEnv] unsets
// and that [RepositoryEnv] does not list. Each variable selects a
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
//
// Without the variables, git init creates each repository in the
// default formats of the installed git.
func formatEnv() []string {
	return []string{
		"GIT_DEFAULT_HASH",
		"GIT_DEFAULT_REF_FORMAT",
		"GIT_REFERENCE_BACKEND",
	}
}

// RepositoryEnv returns the name of each variable of the environment
// that is local to one repository, such as GIT_DIR, GIT_WORK_TREE, and
// GIT_INDEX_FILE. git rev-parse --local-env-vars prints the names, so
// the list follows the installed git. The command does not need a
// repository.
func RepositoryEnv(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		return nil, fmt.Errorf("listing the repository variables of git: %w", err)
	}

	return strings.Fields(string(out)), nil
}

// IsolateEnv keeps the git config and the repository of the developer
// from each git command of the process. A TestMain calls it before it
// runs the tests.
//
// IsolateEnv makes git skip the system config and read an empty global
// config. It sets an empty template directory, so git init and git
// clone copy no hook into a new repository. It also unsets each
// variable that [RepositoryEnv] lists. The list holds the variables
// that pass config to git, such as GIT_CONFIG_COUNT and
// GIT_CONFIG_PARAMETERS. A git command of the process therefore reads
// only the config of the repository that it acts on.
//
// IsolateEnv also points XDG_CONFIG_HOME at the null device. git then
// finds no global ignore file and no global attributes file. A git add
// of a test setup therefore adds each file that the setup wrote. Every
// other program of the process that reads XDG_CONFIG_HOME finds no
// config file either.
//
// IsolateEnv also makes git skip the system attributes file of the
// machine, such as /etc/gitattributes. A git command of the process
// therefore reads no attributes file from outside the repository that
// it acts on.
//
// IsolateEnv also unsets GIT_DEFAULT_HASH, GIT_DEFAULT_REF_FORMAT, and
// GIT_REFERENCE_BACKEND. git init then creates each repository in the
// default formats of the installed git. With GIT_DEFAULT_HASH set to
// sha256, an object name has 64 characters, and a test that expects
// the 40 characters of a SHA-1 object name fails.
func IsolateEnv(ctx context.Context) error {
	// git reads the system config and the global config before it
	// prints the names of RepositoryEnv. It stops at a config file
	// that it cannot parse. The variables of configEnv are therefore
	// in place before RepositoryEnv runs git.
	for name, value := range configEnv {
		err := os.Setenv(name, value)
		if err != nil {
			return fmt.Errorf("setting %s: %w", name, err)
		}
	}

	names, err := RepositoryEnv(ctx)
	if err != nil {
		return err
	}

	for _, name := range slices.Concat(names, formatEnv()) {
		// git reads an empty value of some of these variables. For
		// example, a git config command writes the file that
		// GIT_CONFIG names, and it fails when the value names no file.
		// git init reads the format that a variable of formatEnv
		// names, and it fails when the value names no format.
		// IsolateEnv therefore unsets each variable.
		err := os.Unsetenv(name)
		if err != nil {
			return fmt.Errorf("unsetting %s: %w", name, err)
		}
	}

	return nil
}

// CheckRepositoryEnv fails t when the TestMain of its test package
// leaves a variable of [RepositoryEnv] to the environment. One
// top-level test of each test package that runs git calls it. setup
// runs git commands that the tests of the package run, such as the
// commands that create a repository with one commit.
//
// TestMain unsets the variables when the test binary starts, so a test
// that sets them points git at the other repository again.
// CheckRepositoryEnv therefore starts the test binary again for each
// of its cases. The environment of the new process names a repository
// of the case, as the environment of a commit hook names the
// repository of a developer. The new process runs only the test that
// called CheckRepositoryEnv. In that process, CheckRepositoryEnv runs
// setup, and it checks that the environment holds none of the
// variables. The case then checks that no file of its repository
// changed. The cases run git to create their repositories. So
// CheckRepositoryEnv fails before the first case when the test process
// holds one of the variables.
func CheckRepositoryEnv(t *testing.T, setup func(t *testing.T)) {
	t.Helper()

	names, err := RepositoryEnv(t.Context())
	require.NoError(t, err)

	if os.Getenv(probeEnv) != "" {
		setup(t)

		// Some of the variables change only what git reads, such as
		// GIT_GRAFT_FILE, and setup succeeds with them. The check of
		// the environment finds those variables.
		for _, name := range names {
			_, set := os.LookupEnv(name)
			assert.False(t, set, "TestMain left %s set", name)
		}

		return
	}

	// The cases run git to create their repositories. A variable that
	// TestMain left set points those commands at the repository of the
	// developer, so the test stops before the first of them.
	for _, name := range names {
		_, set := os.LookupEnv(name)
		require.False(t, set, "TestMain left %s set", name)
	}

	binary, err := os.Executable()
	require.NoError(t, err)

	run := "-test.run=^" + t.Name() + "$"

	// every points each variable of the list at the git directory.
	every := map[string]string{}
	for _, name := range names {
		every[name] = ".git"
	}

	// Each case maps a variable to a path inside the repository that
	// stands for the repository of a developer.
	tcs := map[string]map[string]string{
		"git directory": {
			"GIT_DIR": ".git",
		},
		// git sets these variables for the hooks of a commit in a
		// linked worktree.
		"git directory and index file": {
			"GIT_DIR":        ".git",
			"GIT_INDEX_FILE": filepath.Join(".git", "index"),
		},
		// With this variable alone, a git add in another repository
		// succeeds, and it writes the index of the developer.
		"index file": {
			"GIT_INDEX_FILE": filepath.Join(".git", "index"),
		},
		"common directory": {
			"GIT_COMMON_DIR": ".git",
		},
		"every variable that git lists": every,
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dev := initRepository(t)
			before := readTree(t, dev)

			env := []string{probeEnv + "=1"}
			for variable, path := range tc {
				env = append(env, variable+"="+filepath.Join(dev, path))
			}

			//nolint:gosec // G204: binary is the test binary itself.
			cmd := exec.CommandContext(t.Context(), binary, run)
			cmd.Env = slices.Concat(os.Environ(), env)

			out, err := cmd.CombinedOutput()

			// A process that fails can still write to the repository,
			// so the case reports both.
			assert.Equal(t, before, readTree(t, dev), "a file of the repository changed")
			require.NoError(t, err, "%s", out)
		})
	}
}

// CheckConfigEnv fails t when the TestMain of its test package leaves
// git to read the system config, the global config, or the template
// directory of the developer. It also fails t when git still finds the
// global ignore file or the global attributes file of the developer,
// or when git still reads the system attributes file of the machine.
// It fails t as well when TestMain leaves a variable to the
// environment that selects a format of the repositories that git init
// creates. One top-level test of each test package that runs git calls
// it. setup runs git commands that the tests of the package run, such
// as the commands that create a repository with one commit.
//
// TestMain sets its variables when the test binary starts, so a test
// that sets them again undoes the work of TestMain. CheckConfigEnv
// therefore starts the test binary again for each of its cases. The
// new process runs only the test that called CheckConfigEnv.
//
// In the new process, CheckConfigEnv checks that the environment holds
// no variable that selects a format. It checks that git reads no
// system attributes file. It checks that git config does not find a
// key of the config file of the case. It also checks that git ignores
// no path and sets no attribute of the case in a new repository. It
// then runs setup. The case then checks that git ran no hook of the
// case.
//
// The environment of a format case gives GIT_DEFAULT_HASH,
// GIT_DEFAULT_REF_FORMAT, or GIT_REFERENCE_BACKEND a value that names
// no format. A git init that reads the variable fails, whatever
// formats the installed git creates by default.
//
// git has no variable that names another path for the system
// attributes file. No case therefore leads git to a system attributes
// file of its own. The environment of the system attributes case gives
// GIT_ATTR_NOSYSTEM the value 0. git then reads the system attributes
// file of the machine, as it does without the variable. The new
// process runs git var GIT_ATTR_SYSTEM, which prints the path of the
// file that git reads. The command prints no path when git skips the
// file. When the command prints a path, the new process stops before a
// git command reads the file.
//
// The environment of every other case leads git to a file or to a
// template directory of the case. A config file sits in one of the
// places where git looks for the system config or the global config.
// It names a directory of hooks in core.hooksPath, and it makes git
// commit sign each commit with a program that fails. A template
// directory holds the hooks in its hooks directory, and git init
// copies them into each repository that it creates. An ignore file or
// an attributes file sits in one of the places where git looks for the
// global ignore file or the global attributes file. The ignore file
// ignores every path, so a git add of setup adds no file. The
// attributes file sets an attribute of the case on every path.
//
// [CheckRepositoryEnv] covers the variables that pass config to git,
// because [RepositoryEnv] lists them.
func CheckConfigEnv(t *testing.T, setup func(t *testing.T)) {
	t.Helper()

	if os.Getenv(configProbeEnv) != "" {
		// setup stops the test when a git command fails, so the
		// checks of the environment and of the files of the case run
		// first. checkSystemAttributes stops the test as well, so it
		// runs after the check of the environment.
		checkFormatEnv(t)
		checkSystemAttributes(t)
		checkDeveloperFiles(t)
		setup(t)

		return
	}

	binary, err := os.Executable()
	require.NoError(t, err)

	run := "-test.run=^" + t.Name() + "$"

	// ignore ignores every path, and attributes sets canaryAttribute
	// on every path.
	const (
		ignore     = "*\n"
		attributes = "* " + canaryAttribute + "\n"
	)

	tcs := map[string]struct {
		// env names the variable that leads git to the file or to the
		// template directory of the case, that selects a format, or
		// that makes git read the system attributes file.
		env string
		// file is the path of the file inside the directory that env
		// names. It is empty when env names the file.
		file string
		// content is the content of the ignore file or of the
		// attributes file of the case. It is empty when the case
		// writes a config file or a template directory.
		content string
		// template is true when env names a template directory. The
		// case then writes only the hooks of that directory, and no
		// config file, ignore file, or attributes file.
		template bool
		// value is the value that the case gives to env. It is empty
		// when env names the file or the template directory of the
		// case. A case with a value writes no file.
		value string
	}{
		"system config": {
			env: "GIT_CONFIG_SYSTEM",
		},
		"global config variable": {
			env: "GIT_CONFIG_GLOBAL",
		},
		"config in the home directory": {
			env:  "HOME",
			file: ".gitconfig",
		},
		"config in the XDG directory": {
			env:  "XDG_CONFIG_HOME",
			file: filepath.Join("git", "config"),
		},
		// git reads the init.templateDir key from the config, which
		// the other cases cover. The variable names the template
		// directory outside the config.
		"template directory": {
			env:      "GIT_TEMPLATE_DIR",
			template: true,
		},
		// git reads the core.excludesFile key and the
		// core.attributesFile key from the config, which the config
		// cases cover. Without the keys, git reads the files of these
		// cases.
		"ignore file in the XDG directory": {
			env:     "XDG_CONFIG_HOME",
			file:    filepath.Join("git", "ignore"),
			content: ignore,
		},
		"ignore file in the home directory": {
			env:     "HOME",
			file:    filepath.Join(".config", "git", "ignore"),
			content: ignore,
		},
		"attributes file in the XDG directory": {
			env:     "XDG_CONFIG_HOME",
			file:    filepath.Join("git", "attributes"),
			content: attributes,
		},
		"attributes file in the home directory": {
			env:     "HOME",
			file:    filepath.Join(".config", "git", "attributes"),
			content: attributes,
		},
		// git reads the init.defaultObjectFormat key and the
		// init.defaultRefFormat key from the config, which the config
		// cases cover. The variables of these cases select the
		// formats outside the config.
		"object format": {
			env:   "GIT_DEFAULT_HASH",
			value: noFormat,
		},
		"ref format": {
			env:   "GIT_DEFAULT_REF_FORMAT",
			value: noFormat,
		},
		"ref backend": {
			env:   "GIT_REFERENCE_BACKEND",
			value: noFormat,
		},
		// git has no variable that names another path for the system
		// attributes file. With the value 0, git reads the system
		// attributes file of the machine, as it does without the
		// variable.
		"system attributes file": {
			env:   "GIT_ATTR_NOSYSTEM",
			value: "0",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dev := t.TempDir()
			home := filepath.Join(dev, "home")
			xdg := filepath.Join(dev, "xdg")
			ran := filepath.Join(dev, "ran")
			path := filepath.Join(dev, "developer")

			for _, dir := range []string{home, xdg} {
				require.NoError(t, os.MkdirAll(dir, 0o755))
			}

			// value is the value of the variable that env names.
			value := path

			switch {
			case tc.value != "":
				value = tc.value
			case tc.template:
				writeHooks(t, filepath.Join(path, "hooks"), ran)
			case tc.content != "":
				file := filepath.Join(path, tc.file)

				require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
				require.NoError(t, os.WriteFile(file, []byte(tc.content), 0o644))

			default:
				hooks := filepath.Join(dev, "hooks")

				writeHooks(t, hooks, ran)
				writeConfig(t, filepath.Join(path, tc.file), hooks)
			}

			// Each case names a home directory and a system config of
			// its own. The process inherits no template directory, so
			// only the template directory case names one. When
			// TestMain leaves git to read the files and the template
			// directory of the developer, the process therefore reads
			// those of the case. It runs no hook of the developer who
			// runs the tests.
			//
			// No case names a system attributes file of its own, so
			// each case makes git skip the system attributes file of
			// the machine. Only the system attributes case then gives
			// GIT_ATTR_NOSYSTEM another value.
			env := []string{
				"HOME=" + home,
				"GIT_CONFIG_SYSTEM=" + os.DevNull,
				"GIT_ATTR_NOSYSTEM=1",
			}

			// git reads the .config directory of the home directory
			// only when XDG_CONFIG_HOME is unset or empty. The process
			// inherits no XDG_CONFIG_HOME, and a case that names the
			// home directory leaves the variable unset. Every other
			// case names an XDG directory of its own.
			if tc.env != "HOME" {
				env = append(env, "XDG_CONFIG_HOME="+xdg)
			}

			env = append(env, tc.env+"="+value, configProbeEnv+"=1")

			//nolint:gosec // G204: binary is the test binary itself.
			cmd := exec.CommandContext(t.Context(), binary, run)
			cmd.Env = slices.Concat(environWithoutConfigEnv(), env)

			out, err := cmd.CombinedOutput()

			// A process that fails can still run a hook, so the case
			// reports both.
			assert.NoFileExists(t, ran, "git ran a hook of the developer")
			require.NoError(t, err, "%s", out)
		})
	}
}

// checkFormatEnv fails t when the environment holds a variable that
// [formatEnv] lists. It runs in the process that a case of
// [CheckConfigEnv] started.
func checkFormatEnv(t *testing.T) {
	t.Helper()

	for _, name := range formatEnv() {
		_, set := os.LookupEnv(name)
		assert.False(t, set, "TestMain left %s set", name)
	}
}

// checkSystemAttributes stops t when git reads the system attributes
// file of the machine. It runs in the process that a case of
// [CheckConfigEnv] started.
//
// git var GIT_ATTR_SYSTEM prints the path of the system attributes
// file, also when no file exists at that path. The command does not
// read the file. When GIT_ATTR_NOSYSTEM makes git skip the file, the
// command prints nothing, and it exits 1. git var knows the variable
// GIT_ATTR_SYSTEM in git 2.42.0 and later.
func checkSystemAttributes(t *testing.T) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", "var", "GIT_ATTR_SYSTEM")
	cmd.Dir = t.TempDir()

	out, err := cmd.Output()

	// Some git commands after this check read attributes, such as
	// git check-attr and the git add of setup. The check therefore
	// stops the test before one of them reads the file of the machine.
	var exitErr *exec.ExitError

	require.ErrorAs(t, err, &exitErr, "git reads the system attributes file %s", out)
	require.Equal(t, 1, exitErr.ExitCode(), "%s", exitErr.Stderr)
}

// checkDeveloperFiles fails t when git reads the config file, the
// ignore file, or the attributes file of a case of [CheckConfigEnv].
// It runs in the process that the case started.
func checkDeveloperFiles(t *testing.T) {
	t.Helper()

	// git config exits 1 when no config sets the key, and only the
	// config file of a case sets canaryKey.
	cmd := exec.CommandContext(t.Context(), "git", "config", "--get", canaryKey)
	cmd.Dir = t.TempDir()

	_, err := cmd.Output()

	var configErr *exec.ExitError
	if assert.ErrorAs(t, err, &configErr, "git read the config of the developer") {
		assert.Equal(t, 1, configErr.ExitCode(), "%s", configErr.Stderr)
	}

	// git check-ignore and git check-attr run only in a repository.
	// A new repository ignores no path and sets no attribute, so only
	// a file of a case changes what the two commands report.
	dir := t.TempDir()

	cmd = exec.CommandContext(t.Context(), "git", "init", "--quiet")
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%v: %s", cmd.Args, out)

	// git check-ignore exits 1 when no pattern ignores the path.
	// Otherwise it prints the file that holds the pattern.
	cmd = exec.CommandContext(t.Context(), "git", "check-ignore", "--verbose", canaryPath)
	cmd.Dir = dir

	out, err = cmd.Output()

	var ignoreErr *exec.ExitError
	if assert.ErrorAs(t, err, &ignoreErr, "git read the ignore file of the developer: %s", out) {
		assert.Equal(t, 1, ignoreErr.ExitCode(), "%s", ignoreErr.Stderr)
	}

	// git check-attr prints unspecified as the value of an attribute
	// that no attributes file sets for the path.
	cmd = exec.CommandContext(t.Context(), "git", "check-attr", canaryAttribute, "--", canaryPath)
	cmd.Dir = dir

	out, err = cmd.Output()
	require.NoError(t, err, "%v", cmd.Args)

	want := canaryPath + ": " + canaryAttribute + ": unspecified\n"
	assert.Equal(t, want, string(out), "git read the attributes file of the developer")
}

// environWithoutConfigEnv returns the environment of the process
// without the variables of configEnv. The process holds them after its
// TestMain called [IsolateEnv]. A process that inherits them skips the
// files and the template directory of each case, whatever its own
// TestMain does.
func environWithoutConfigEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		_, ok := configEnv[name]

		return ok
	})
}

// writeHooks creates the directory dir and writes hooks to it. Each
// hook appends its path to the file ran when git runs it.
func writeHooks(t *testing.T, dir, ran string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o755))

	// git runs these hooks for commands of a test setup, such as git
	// add, git commit, and git checkout.
	names := []string{
		"pre-commit",
		"prepare-commit-msg",
		"commit-msg",
		"post-commit",
		"post-checkout",
		"post-index-change",
		"reference-transaction",
	}

	script := "#!/bin/sh\necho \"$0\" >> '" + ran + "'\n"

	for _, name := range names {
		//nolint:gosec // G306: git runs a hook only when the file is executable.
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755))
	}
}

// writeConfig writes the config of a developer to file, and it creates
// the directory of the file. The config sets core.hooksPath to the
// directory hooks, and it sets canaryKey. It also makes git commit sign
// each commit with a program that fails. git config writes the file,
// so git quotes each value.
func writeConfig(t *testing.T, file, hooks string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))

	for _, setting := range [][2]string{
		{"core.hooksPath", hooks},
		{"commit.gpgSign", "true"},
		{"gpg.program", "false"},
		{canaryKey, "true"},
	} {
		//nolint:gosec // G204: the arguments name a file and a setting of the case.
		cmd := exec.CommandContext(t.Context(), "git", "config", "--file", file, setting[0], setting[1])

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%v: %s", cmd.Args, out)
	}
}

// initRepository creates a repository with one staged file, and it
// returns the path of the repository. It does not run git commit. A
// commit starts git maintenance in the background, and maintenance
// still changes files under the git directory after the commit
// returns.
func initRepository(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("notes\n"), 0o644))

	for _, cmd := range []*exec.Cmd{
		exec.CommandContext(t.Context(), "git", "init", "--quiet"),
		exec.CommandContext(t.Context(), "git", "add", "notes.txt"),
	} {
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%v: %s", cmd.Args, out)
	}

	return dir
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
