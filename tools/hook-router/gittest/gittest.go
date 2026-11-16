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

// UnsetRepositoryEnv unsets each variable that [RepositoryEnv] lists,
// for the whole process. A TestMain calls it before it runs the tests.
//
// The list also holds variables that pass config to git, such as
// GIT_CONFIG_COUNT and GIT_CONFIG_PARAMETERS. The git commands of the
// process then read no config from those variables. They still read
// the system config and the global config of the developer.
func UnsetRepositoryEnv(ctx context.Context) error {
	names, err := RepositoryEnv(ctx)
	if err != nil {
		return err
	}

	for _, name := range names {
		// git reads an empty value of some of these variables. For
		// example, a git config command writes the file that
		// GIT_CONFIG names, and it fails when the value names no file.
		// UnsetRepositoryEnv therefore unsets each variable.
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
