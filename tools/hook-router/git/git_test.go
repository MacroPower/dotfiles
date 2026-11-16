package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
