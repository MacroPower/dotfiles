package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/hook-router/git"
	"go.jacobcolvin.com/dotfiles/tools/hook-router/gittest"
)

// TestMain keeps the git config and the repository of the developer
// from every test. The tests run git in their setup and through
// [git.Runner], and keys of the git config change what git does. With
// core.hooksPath set in the global config, each git commit of a test
// setup runs the hooks of the developer. git init also copies the
// hooks of the template directory that GIT_TEMPLATE_DIR names. Each git
// command of the tests therefore skips the system config and reads an
// empty global config, and git init copies no template.
//
// git also reads the global ignore file and the global attributes file
// of the developer. When the ignore file names README.md, the git add
// of a test setup skips the file, and the git commit after it fails.
// Each git command of the tests therefore finds neither file.
//
// Variables of the environment point git at a repository. With GIT_DIR
// set, a git command acts on the repository that GIT_DIR names, not on
// the repository of its working directory. The setup of a test then
// commits to the branch of the developer. TestMain therefore unsets
// each variable that [gittest.RepositoryEnv] lists.
func TestMain(m *testing.M) {
	err := gittest.IsolateEnv(context.Background())
	if err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}

// [TestMain] makes git skip the system config, the global config, the
// template directory, the global ignore file, and the global
// attributes file of the developer. The git commands of the tests must
// therefore read no key of that config and no line of those two files.
// They must also run no hook of the developer.
func TestGitSkipsDeveloperConfig(t *testing.T) {
	t.Parallel()

	gittest.CheckConfigEnv(t, func(t *testing.T) {
		t.Helper()

		// The tests create their repositories with these commands.
		// git runs hooks for the git add, the git commit, and the git
		// checkout among them. The git commit signs its commit when
		// the config says so.
		runGit(t, initTestRepo(t), "checkout", "-b", "feat/x")
	})
}

// [TestMain] unsets each variable of the environment that points git
// at a repository. The git commands of the tests must therefore act on
// the repositories of the tests. They must leave the repository that
// the variables name as it was.
func TestGitSkipsEnvironmentRepository(t *testing.T) {
	t.Parallel()

	gittest.CheckRepositoryEnv(t, func(t *testing.T) {
		t.Helper()

		// The tests create their repositories with these commands,
		// which write the git config, the index, a commit, and a
		// branch.
		runGit(t, initTestRepo(t), "checkout", "-b", "feat/x")
	})
}

// A [git.Runner] must answer for the repository of its Dir, whatever
// repository the environment names. A shell that exports GIT_DIR points
// every git command of its processes at one repository, and so does a
// git hook. Each case points variables at a second repository. That
// repository differs from the repository of Dir in its branch, in its
// HEAD commit, in its index, and in its working tree.
func TestRunnerSkipsEnvironmentRepository(t *testing.T) { //nolint:paralleltest // t.Setenv
	names, err := gittest.RepositoryEnv(t.Context())
	require.NoError(t, err)

	// every points each variable that a Runner removes at the git
	// directory. git rejects that value of some of the variables, such
	// as GIT_INDEX_FILE, so a Runner that keeps one of them fails. A
	// Runner keeps the variables that pass config, so every leaves them
	// out.
	every := map[string]string{referenceBackendEnv: ".git"}

	for _, name := range names {
		if !slices.Contains(configEnv, name) {
			every[name] = ".git"
		}
	}

	// Each case maps a variable to a path inside the second repository.
	cases := map[string]map[string]string{
		"git directory": {
			"GIT_DIR": ".git",
		},
		"git directory and working tree": {
			"GIT_DIR":       ".git",
			"GIT_WORK_TREE": ".",
		},
		// git sets these variables for the hooks of a commit in a
		// linked worktree.
		"git directory and index file": {
			"GIT_DIR":        ".git",
			"GIT_INDEX_FILE": filepath.Join(".git", "index"),
		},
		"index file": {
			"GIT_INDEX_FILE": filepath.Join(".git", "index"),
		},
		"working tree": {
			"GIT_WORK_TREE": ".",
		},
		"common directory": {
			"GIT_COMMON_DIR": ".git",
		},
		// git rev-parse --local-env-vars does not print this variable.
		"reference backend": {
			referenceBackendEnv: ".git",
		},
		"every variable that a Runner removes": every,
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := initTestRepo(t)
			runGit(t, dir, "checkout", "-b", "feat/x")

			// The second repository does not hold this commit.
			// initTestRepo creates the same first commit in two
			// repositories when both commits fall in one second.
			require.NoError(t, os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("feat\n"), 0o644))
			runGit(t, dir, "add", ".")
			runGit(t, dir, "commit", "-m", "add feat file")

			want := gitOutput(t, dir, "rev-parse", "HEAD")

			other := initTestRepo(t)
			runGit(t, other, "checkout", "-b", "fix/y")

			require.NoError(t, os.WriteFile(filepath.Join(other, "new.txt"), []byte("new\n"), 0o644))
			runGit(t, other, "add", ".")
			runGit(t, other, "commit", "-m", "add file")

			require.NoError(t, os.WriteFile(filepath.Join(other, "staged.txt"), []byte("staged\n"), 0o644))
			runGit(t, other, "add", ".")

			require.NoError(t, os.WriteFile(filepath.Join(other, "README.md"), []byte("changed\n"), 0o644))

			// Each git command of the setup ran before this point. With
			// the variables set, such a command acts on the second
			// repository.
			for variable, path := range tc {
				value := filepath.Join(other, path)
				if variable == referenceBackendEnv {
					value = "files://" + value
				}

				t.Setenv(variable, value)
			}

			runner := &git.Runner{Dir: dir}

			branch, err := runner.CurrentBranch(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "feat/x", branch)

			sha, err := runner.HeadSHA(t.Context())
			require.NoError(t, err)
			assert.Equal(t, want, sha)

			changed, err := runner.HasChanges(t.Context(), want)
			require.NoError(t, err)
			assert.False(t, changed)
		})
	}
}

// A [git.Runner] removes only the variables that are local to one
// repository, and its commands must keep every other variable of the
// environment. GIT_CEILING_DIRECTORIES is not local to one repository.
// It names a directory where git stops its search for a repository.
// With the root of the working tree as its value, a command in a
// subdirectory finds no repository.
func TestRunnerKeepsOtherEnvironment(t *testing.T) { //nolint:paralleltest // t.Setenv
	dir := initTestRepo(t)

	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))

	runner := &git.Runner{Dir: sub}

	_, err := runner.CurrentBranch(t.Context())
	require.NoError(t, err)

	t.Setenv("GIT_CEILING_DIRECTORIES", dir)

	_, err = runner.CurrentBranch(t.Context())
	require.Error(t, err)
	require.NotErrorIs(t, err, git.ErrNoBranch)
}

// configEnv holds the variables of git rev-parse --local-env-vars that
// pass config to git. A [git.Runner] keeps them.
var configEnv = []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS"}

// referenceBackendEnv names the variable that holds the URI of a ref
// store, such as files:///path/to/repo/.git. git reads HEAD and every
// ref from that store. git rev-parse --local-env-vars does not print
// the name, and a [git.Runner] removes the variable.
const referenceBackendEnv = "GIT_REFERENCE_BACKEND"

// A [git.Runner] keeps the variables that pass config to git, as git
// does when it runs a command in another repository. A parent process
// passes config in them, such as a safe.directory entry that lets git
// read a repository that another user owns. Each case passes that entry
// in one of the variables. GIT_TEST_ASSUME_DIFFERENT_OWNER makes git
// treat each repository as one that another user owns.
func TestRunnerKeepsEnvironmentConfig(t *testing.T) { //nolint:paralleltest // t.Setenv
	cases := map[string]struct {
		env func(dir string) map[string]string
	}{
		"config count": {
			env: func(dir string) map[string]string {
				return map[string]string{
					"GIT_CONFIG_COUNT":   "1",
					"GIT_CONFIG_KEY_0":   "safe.directory",
					"GIT_CONFIG_VALUE_0": dir,
				}
			},
		},
		// git -c safe.directory=<dir> passes this value.
		"config parameters": {
			env: func(dir string) map[string]string {
				return map[string]string{
					"GIT_CONFIG_PARAMETERS": "'safe.directory'='" + dir + "'",
				}
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := initTestRepo(t)
			runGit(t, dir, "checkout", "-b", "feat/x")

			want := gitOutput(t, dir, "rev-parse", "HEAD")

			// HasChanges reports this untracked file.
			require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644))

			// git compares the entry with the path of the repository
			// after it resolves links.
			resolved, err := filepath.EvalSymlinks(dir)
			require.NoError(t, err)

			// The system config and the global config of the developer
			// must not allow the repository.
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
			t.Setenv("GIT_TEST_ASSUME_DIFFERENT_OWNER", "1")

			runner := &git.Runner{Dir: dir}

			// Without the entry, git refuses to read the repository.
			_, err = runner.CurrentBranch(t.Context())
			require.Error(t, err)
			require.NotErrorIs(t, err, git.ErrNoBranch)

			for variable, value := range tc.env(resolved) {
				t.Setenv(variable, value)
			}

			branch, err := runner.CurrentBranch(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "feat/x", branch)

			sha, err := runner.HeadSHA(t.Context())
			require.NoError(t, err)
			assert.Equal(t, want, sha)

			changed, err := runner.HasChanges(t.Context(), want)
			require.NoError(t, err)
			assert.True(t, changed)
		})
	}
}

// git prints the names of the variables that a [git.Runner] removes.
// When git cannot run, each method must return that error. HasChanges
// must not report the directory as one that holds no repository.
func TestRunnerWithoutGit(t *testing.T) { //nolint:paralleltest // t.Setenv
	dir := initTestRepo(t)

	// No directory of PATH holds git.
	t.Setenv("PATH", t.TempDir())

	runner := &git.Runner{Dir: dir}

	_, err := runner.CurrentBranch(t.Context())
	require.ErrorIs(t, err, exec.ErrNotFound)
	require.NotErrorIs(t, err, git.ErrNoBranch)

	_, err = runner.HeadSHA(t.Context())
	require.ErrorIs(t, err, exec.ErrNotFound)

	changed, err := runner.HasChanges(t.Context(), "")
	require.ErrorIs(t, err, exec.ErrNotFound)
	assert.False(t, changed)
}

func initTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v: %s", args, out)
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644))

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "command %v: %s", args, out)
	}

	return dir
}

func TestHeadSHA(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	runner := &git.Runner{Dir: dir}
	ctx := context.Background()

	sha, err := runner.HeadSHA(ctx)
	require.NoError(t, err)
	assert.Len(t, sha, 40)
}

func TestHasChanges_NoChanges(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	runner := &git.Runner{Dir: dir}
	ctx := context.Background()

	sha, err := runner.HeadSHA(ctx)
	require.NoError(t, err)

	changed, err := runner.HasChanges(ctx, sha)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestHasChanges_WithCommit(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	runner := &git.Runner{Dir: dir}
	ctx := context.Background()

	baseSHA, err := runner.HeadSHA(ctx)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644))

	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "add file"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
	}

	changed, err := runner.HasChanges(ctx, baseSHA)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestHasChanges_UncommittedChanges(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	runner := &git.Runner{Dir: dir}
	ctx := context.Background()

	sha, err := runner.HeadSHA(ctx)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644))

	changed, err := runner.HasChanges(ctx, sha)
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestHasChanges_EmptyBaseSHA(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	runner := &git.Runner{Dir: dir}
	ctx := context.Background()

	// No uncommitted changes, empty base SHA.
	changed, err := runner.HasChanges(ctx, "")
	require.NoError(t, err)
	assert.False(t, changed)

	// With uncommitted changes, empty base SHA.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o644))

	changed, err = runner.HasChanges(ctx, "")
	require.NoError(t, err)
	assert.True(t, changed)
}

func TestHasChanges_NotGitRepo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runner := &git.Runner{Dir: dir}
	ctx := context.Background()

	changed, err := runner.HasChanges(ctx, "abc123")
	require.NoError(t, err)
	assert.False(t, changed)
}

// runGit runs one git command in dir and fails the test when the
// command fails.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// gitOutput runs one git command in dir and returns what the command
// prints, without the final newline. It fails the test when the
// command fails.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	require.NoError(t, err, "git %v", args)

	return strings.TrimSuffix(string(out), "\n")
}

func TestCurrentBranch(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		setup func(t *testing.T) string
		err   error
		want  string
	}{
		"branch with a slash in its name": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := initTestRepo(t)
				runGit(t, dir, "checkout", "-b", "feat/x")

				return dir
			},
			want: "feat/x",
		},
		"branch that shares its name with a tag": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := initTestRepo(t)
				runGit(t, dir, "checkout", "-b", "fix/y")
				runGit(t, dir, "tag", "fix/y")

				return dir
			},
			want: "fix/y",
		},
		"branch with no commits": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := t.TempDir()
				runGit(t, dir, "init", "--initial-branch", "docs/readme")

				return dir
			},
			want: "docs/readme",
		},
		"subdirectory of the working tree": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := initTestRepo(t)
				runGit(t, dir, "checkout", "-b", "chore/z")

				sub := filepath.Join(dir, "sub")
				require.NoError(t, os.Mkdir(sub, 0o755))

				return sub
			},
			want: "chore/z",
		},
		"linked worktree reports its own branch": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := initTestRepo(t)
				runGit(t, dir, "checkout", "-b", "feat/x")

				linked := filepath.Join(t.TempDir(), "linked")
				runGit(t, dir, "worktree", "add", "-b", "fix/y", linked)

				return linked
			},
			want: "fix/y",
		},
		"detached HEAD": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := initTestRepo(t)
				runGit(t, dir, "checkout", "--detach")

				return dir
			},
			err: git.ErrNoBranch,
		},
		"HEAD names a ref outside refs/heads": {
			setup: func(t *testing.T) string {
				t.Helper()

				dir := initTestRepo(t)
				runGit(t, dir, "symbolic-ref", "HEAD", "refs/remotes/origin/feat/x")

				return dir
			},
			err: git.ErrNoBranch,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runner := &git.Runner{Dir: tc.setup(t)}

			got, err := runner.CurrentBranch(t.Context())
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCurrentBranch_NotGitRepo(t *testing.T) {
	t.Parallel()

	runner := &git.Runner{Dir: t.TempDir()}

	got, err := runner.CurrentBranch(t.Context())
	require.Error(t, err)
	require.NotErrorIs(t, err, git.ErrNoBranch)
	assert.Empty(t, got)
}

func TestCurrentBranch_MissingDir(t *testing.T) {
	t.Parallel()

	runner := &git.Runner{Dir: filepath.Join(t.TempDir(), "missing")}

	_, err := runner.CurrentBranch(t.Context())
	require.Error(t, err)
	require.NotErrorIs(t, err, git.ErrNoBranch)
}
