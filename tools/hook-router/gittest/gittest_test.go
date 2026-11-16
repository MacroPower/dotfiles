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

// TestMain keeps the repository of the developer from every test, as
// the documentation of package gittest describes.
func TestMain(m *testing.M) {
	if os.Getenv(leakEnv) == "" {
		err := gittest.UnsetRepositoryEnv(context.Background())
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

func TestUnsetRepositoryEnv(t *testing.T) { //nolint:paralleltest // t.Setenv
	names, err := gittest.RepositoryEnv(t.Context())
	require.NoError(t, err)

	// git does not list this variable, because it is not local to one
	// repository.
	const other = "GIT_AUTHOR_NAME"

	require.NotContains(t, names, other)

	for _, name := range slices.Concat(names, []string{other}) {
		t.Setenv(name, "value")
	}

	require.NoError(t, gittest.UnsetRepositoryEnv(t.Context()))

	for _, name := range names {
		_, set := os.LookupEnv(name)
		assert.False(t, set, "%s is set", name)
	}

	assert.Equal(t, "value", os.Getenv(other))
}

// When git cannot list the variables, [gittest.UnsetRepositoryEnv]
// returns the error and unsets nothing. A TestMain then stops before
// the tests run, and no test runs git with GIT_DIR still set.
func TestUnsetRepositoryEnvWithoutGit(t *testing.T) { //nolint:paralleltest // t.Setenv
	t.Setenv("GIT_DIR", "value")

	// No directory of PATH holds git.
	t.Setenv("PATH", t.TempDir())

	_, err := gittest.RepositoryEnv(t.Context())
	require.ErrorIs(t, err, exec.ErrNotFound)

	err = gittest.UnsetRepositoryEnv(t.Context())
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

	out := runLeaking(t)

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

	dev := gittest.InitRepository(t)
	before := gittest.ReadTree(t, dev)

	out := runLeaking(t, "GIT_DIR="+filepath.Join(dev, ".git"))

	assert.Contains(t, out, "TestMain left GIT_DIR set")
	assert.NotContains(t, out, checked+"/")
	assert.Equal(t, before, gittest.ReadTree(t, dev), "a file of the repository changed")
}

// checked names the test that runLeaking runs.
const checked = "TestCheckRepositoryEnv"

// runLeaking starts the test binary again with leakEnv and env set,
// and the new process runs [TestCheckRepositoryEnv]. The [TestMain] of
// that process and of each process that it starts then unsets nothing.
// runLeaking fails t when the process passes, and it returns the output
// of the process.
func runLeaking(t *testing.T, env ...string) string {
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
