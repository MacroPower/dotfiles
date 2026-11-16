package gittest_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/gittest"
)

// leakEnv marks a process whose [TestMain] leaves the environment as
// it is. runLeaking starts such a process.
const leakEnv = "HOOK_ROUTER_GITTEST_LEAK"

// TestMain keeps the git config and the repository of the developer
// from every test, as the documentation of package gittest describes.
func TestMain(m *testing.M) {
	if os.Getenv(leakEnv) == "" {
		err := gittest.IsolateEnv(context.Background())
		if err != nil {
			panic(err)
		}
	}

	os.Exit(m.Run())
}

func TestRepositoryEnv(t *testing.T) {
	t.Parallel()

	names, err := gittest.RepositoryEnv(t.Context())
	require.NoError(t, err)

	assert.Subset(t, names, []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"})
}

func TestIsolateEnv(t *testing.T) { //nolint:paralleltest // t.Setenv
	names, err := gittest.RepositoryEnv(t.Context())
	require.NoError(t, err)

	// git does not list this variable, because it is not local to one
	// repository.
	const other = "GIT_AUTHOR_NAME"

	require.NotContains(t, names, other)

	for _, name := range slices.Concat(names, []string{other}) {
		t.Setenv(name, "value")
	}

	// The shell of a developer can name a global config file, turn the
	// system config on, and name a template directory and an XDG
	// directory.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// The shell of a developer can also select the formats of the
	// repositories that git init creates.
	formats := map[string]string{
		"GIT_DEFAULT_HASH":       "sha256",
		"GIT_DEFAULT_REF_FORMAT": "reftable",
		"GIT_REFERENCE_BACKEND":  "reftable",
	}

	for name, value := range formats {
		t.Setenv(name, value)
	}

	require.NoError(t, gittest.IsolateEnv(t.Context()))

	for _, name := range names {
		_, set := os.LookupEnv(name)
		assert.False(t, set, "%s is set", name)
	}

	// git init fails when one of these variables holds an empty
	// value, so IsolateEnv must unset each of them.
	for name := range formats {
		_, set := os.LookupEnv(name)
		assert.False(t, set, "%s is set", name)
	}

	assert.Equal(t, "value", os.Getenv(other))
	assert.Equal(t, os.DevNull, os.Getenv("GIT_CONFIG_GLOBAL"))
	assert.Equal(t, "1", os.Getenv("GIT_CONFIG_NOSYSTEM"))

	// git init falls back to the init.templateDir key and then to the
	// template directory of the git installation when the variable is
	// unset. An empty value makes it copy no template at all.
	template, set := os.LookupEnv("GIT_TEMPLATE_DIR")
	assert.True(t, set, "GIT_TEMPLATE_DIR is unset")
	assert.Empty(t, template)

	// git looks for the global ignore file and the global attributes
	// file below the home directory when the variable is unset or
	// empty.
	assert.Equal(t, os.DevNull, os.Getenv("XDG_CONFIG_HOME"))
}

// git reads the global config before it lists the variables, and it
// stops at a config file that it cannot parse. [gittest.IsolateEnv]
// must therefore skip the config of the developer before it runs git.
func TestIsolateEnvWithBrokenConfig(t *testing.T) { //nolint:paralleltest // t.Setenv
	file := filepath.Join(t.TempDir(), "gitconfig")

	require.NoError(t, os.WriteFile(file, []byte("[broken\n"), 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", file)

	var exitErr *exec.ExitError

	_, err := gittest.RepositoryEnv(t.Context())
	require.ErrorAs(t, err, &exitErr)

	require.NoError(t, gittest.IsolateEnv(t.Context()))

	assert.Equal(t, os.DevNull, os.Getenv("GIT_CONFIG_GLOBAL"))
}

// When git cannot list the variables, [gittest.IsolateEnv] returns the
// error and unsets nothing. A TestMain then stops before the tests
// run, and no test runs git with GIT_DIR still set.
func TestIsolateEnvWithoutGit(t *testing.T) { //nolint:paralleltest // t.Setenv
	t.Setenv("GIT_DIR", "value")

	// No directory of PATH holds git.
	t.Setenv("PATH", t.TempDir())

	_, err := gittest.RepositoryEnv(t.Context())
	require.ErrorIs(t, err, exec.ErrNotFound)

	err = gittest.IsolateEnv(t.Context())
	require.ErrorIs(t, err, exec.ErrNotFound)

	assert.Equal(t, "value", os.Getenv("GIT_DIR"))
}

// commitRepository creates a repository with one commit. Its commands
// write the git config, the index, and a commit.
func commitRepository(t *testing.T) {
	t.Helper()

	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644))

	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		{"add", "."},
		{"commit", "--quiet", "--message", "initial"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
}

// [TestMain] unsets each variable of the environment that points git
// at a repository, so the check passes.
func TestCheckRepositoryEnv(t *testing.T) {
	t.Parallel()

	gittest.CheckRepositoryEnv(t, commitRepository)
}

// [gittest.CheckRepositoryEnv] must fail every one of its cases when
// TestMain leaves the variables to the environment. Together the
// failures must report GIT_DIR in the environment of the process of a
// case and a changed file in the repository of a case. The case that
// sets every variable that git lists reports neither. git rejects the
// path that GIT_CONFIG_COUNT holds, so that case fails in the setup of
// its process.
func TestCheckRepositoryEnvFindsLeak(t *testing.T) {
	t.Parallel()

	const checked = "TestCheckRepositoryEnv"

	out := runLeaking(t, checked)

	assert.Contains(t, out, "--- FAIL: "+checked+"/")
	assert.NotContains(t, out, "--- PASS: "+checked+"/")
	assert.Contains(t, out, "TestMain left GIT_DIR set")
	assert.Contains(t, out, "a file of the repository changed")
}

// [gittest.CheckRepositoryEnv] must stop before its first case when
// TestMain leaves a variable to the environment. Each case runs git to
// create its repository, and the variable points those commands at the
// repository of the developer.
func TestCheckRepositoryEnvStopsBeforeCases(t *testing.T) {
	t.Parallel()

	const checked = "TestCheckRepositoryEnv"

	dev := gittest.InitRepository(t)
	before := gittest.ReadTree(t, dev)

	out := runLeaking(t, checked, "GIT_DIR="+filepath.Join(dev, ".git"))

	assert.Contains(t, out, "TestMain left GIT_DIR set")
	assert.NotContains(t, out, checked+"/")
	assert.Equal(t, before, gittest.ReadTree(t, dev), "a file of the repository changed")
}

// [TestMain] makes git skip the system config, the global config, and
// the template directory of the developer. It hides the global ignore
// file and the global attributes file. It also unsets the variables
// that select a format of a new repository, so the check passes.
func TestCheckConfigEnv(t *testing.T) {
	t.Parallel()

	gittest.CheckConfigEnv(t, commitRepository)
}

// [gittest.CheckConfigEnv] must fail every one of its cases when
// TestMain leaves the environment as it is. git then reads the system
// config, the global config, the template directory, the global ignore
// file, and the global attributes file of each case. Together the
// failures must report a key that git config read from the config file
// of a case and a hook that git ran from the hooks directory of a
// case. They must also report a path that the ignore file of a case
// ignores and an attribute that the attributes file of a case sets.
// They must report each variable of a case that selects a format of a
// new repository.
func TestCheckConfigEnvFindsLeak(t *testing.T) {
	t.Parallel()

	const checked = "TestCheckConfigEnv"

	out := runLeaking(t, checked)

	assert.Contains(t, out, "--- FAIL: "+checked+"/")
	assert.NotContains(t, out, "--- PASS: "+checked+"/")
	assert.Contains(t, out, "git read the config of the developer")
	assert.Contains(t, out, "git ran a hook of the developer")
	assert.Contains(t, out, "git read the ignore file of the developer")
	assert.Contains(t, out, "git read the attributes file of the developer")
	assert.Contains(t, out, "TestMain left GIT_DEFAULT_HASH set")
	assert.Contains(t, out, "TestMain left GIT_DEFAULT_REF_FORMAT set")
	assert.Contains(t, out, "TestMain left GIT_REFERENCE_BACKEND set")
}

// runLeaking starts the test binary again with leakEnv and env set,
// and the new process runs the test that checked names. The [TestMain]
// of that process and of each process that it starts then leaves the
// environment as it is. runLeaking fails t when the process passes,
// and it returns the output of the process.
func runLeaking(t *testing.T, checked string, env ...string) string {
	t.Helper()

	binary, err := os.Executable()
	require.NoError(t, err)

	// With -test.v, the output names each case that runs.
	cmd := exec.CommandContext(t.Context(), binary, "-test.run=^"+checked+"$", "-test.v")
	cmd.Env = slices.Concat(os.Environ(), []string{leakEnv + "=1"}, env)

	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError

	require.ErrorAs(t, err, &exitErr, "%s", out)

	return string(out)
}
