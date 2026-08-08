package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runGitCmds runs each command, failing the test on any error.
func runGitCmds(t *testing.T, cmds [][]string) {
	t.Helper()

	for _, args := range cmds {
		cmd := exec.CommandContext(t.Context(), args[0], args[1:]...) //nolint:gosec // test helper
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run(), "setup command failed: %v", args)
	}
}

// initWorkClone clones bare into a fresh temp dir and configures a
// test identity.
func initWorkClone(t *testing.T, bare string) string {
	t.Helper()

	work := filepath.Join(t.TempDir(), "work")

	runGitCmds(t, [][]string{
		{"git", "clone", bare, work},
		{"git", "-C", work, "config", "user.email", "test@test.com"},
		{"git", "-C", work, "config", "user.name", "Test"},
	})

	return work
}

// gitOut returns the trimmed stdout of a git command, failing the
// test on error.
func gitOut(t *testing.T, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // test helper
	out, err := cmd.Output()
	require.NoError(t, err, "git %v", args)

	return strings.TrimSpace(string(out))
}

// currentBranch returns the checked-out branch of repo.
func currentBranch(t *testing.T, repo string) string {
	t.Helper()

	return gitOut(t, "-C", repo, "symbolic-ref", "--short", "HEAD")
}

// commitFile writes content to name in repo and commits it.
func commitFile(t *testing.T, repo, name, content, msg string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644))
	runGitCmds(t, [][]string{
		{"git", "-C", repo, "add", "."},
		{"git", "-C", repo, "commit", "-m", msg},
	})
}

func TestCheckRemoteName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		remote string
		err    error
	}{
		"origin":       {remote: "origin"},
		"upstream":     {remote: "upstream"},
		"dashed":       {remote: "my-fork"},
		"dotted":       {remote: "fork.2"},
		"empty":        {remote: "", err: ErrDeniedRemote},
		"dash prefix":  {remote: "-x", err: ErrDeniedRemote},
		"slash":        {remote: "a/b", err: ErrDeniedRemote},
		"https url":    {remote: "https://evil/x", err: ErrDeniedRemote},
		"scp url":      {remote: "git@h:p", err: ErrDeniedRemote},
		"space":        {remote: "a b", err: ErrDeniedRemote},
		"newline":      {remote: "a\nb", err: ErrDeniedRemote},
		"null byte":    {remote: "a\x00b", err: ErrDeniedRemote},
		"option-alike": {remote: "--upload-pack=evil", err: ErrDeniedRemote},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := checkRemoteName(tt.remote)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckRefName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		ref string
		err error
	}{
		"branch":            {ref: "main"},
		"tag":               {ref: "v1.0.0"},
		"nested":            {ref: "feature/x"},
		"release":           {ref: "release-1.2"},
		"empty":             {ref: "", err: ErrDeniedRefName},
		"dash prefix":       {ref: "-x", err: ErrDeniedRefName},
		"delete refspec":    {ref: "main:evil", err: ErrDeniedRefName},
		"force refspec":     {ref: "+main", err: ErrDeniedRefName},
		"dotdot":            {ref: "a..b", err: ErrDeniedRefName},
		"reflog syntax":     {ref: "HEAD@{1}", err: ErrDeniedRefName},
		"lock suffix":       {ref: "x.lock", err: ErrDeniedRefName},
		"trailing slash":    {ref: "feature/", err: ErrDeniedRefName},
		"trailing dot":      {ref: "x.", err: ErrDeniedRefName},
		"space":             {ref: "a b", err: ErrDeniedRefName},
		"control character": {ref: "a\nb", err: ErrDeniedRefName},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := checkRefName(tt.ref)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckRemoteInputs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		repo   string
		remote string
		ref    string
		err    error
	}{
		"valid": {repo: "/tmp/x", remote: "origin"},
		"valid with ref": {
			repo: "/tmp/x", remote: "origin", ref: "main",
		},
		"missing repo": {remote: "origin", err: ErrMissingRepo},
		"dash repo": {
			repo: "--upload-pack=evil", remote: "origin",
			err: ErrDeniedRepoPrefix,
		},
		"bad remote": {
			repo: "/tmp/x", remote: "https://evil/x",
			err: ErrDeniedRemote,
		},
		"bad ref": {
			repo: "/tmp/x", remote: "origin", ref: ":main",
			err: ErrDeniedRefName,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := checkRemoteInputs(tt.repo, tt.remote, tt.ref)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGitCommonDir(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	want, err := filepath.EvalSymlinks(filepath.Join(work, ".git"))
	require.NoError(t, err)

	got, err := gitCommonDir(t.Context(), work)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	// A linked worktree's .git is a file pointing back at the main
	// checkout; the common dir must resolve to the main git dir.
	wt := filepath.Join(t.TempDir(), "wt")
	runGitCmds(t, [][]string{
		{"git", "-C", work, "worktree", "add", "-b", "wt-branch", wt},
	})

	got, err = gitCommonDir(t.Context(), wt)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	_, err = gitCommonDir(t.Context(), t.TempDir())
	require.ErrorIs(t, err, ErrNotRepo)
}

func TestRunRemoteEscapeAboveAllowDir(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	// Allow only a subdirectory of the repo; rev-parse walks upward
	// to the repo's git dir, which is outside the allow dir.
	sub := filepath.Join(work, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))

	h := &handler{allowDirs: []string{sub}}

	_, err := h.runRemote(t.Context(), remoteOp{
		repo:   sub,
		remote: "origin",
		args:   []string{"fetch", "--", "origin"},
	})
	require.ErrorIs(t, err, ErrDeniedDest)
}

func TestRunRemoteDeniedRepoPath(t *testing.T) {
	t.Parallel()

	allowDir := t.TempDir()

	bare := initBareRepo(t)

	inside := filepath.Join(allowDir, "repo")
	runGitCmds(t, [][]string{
		{"git", "clone", bare, inside},
	})

	// A gitdir file outside the allow dirs pointing at an allowed
	// git directory must still be denied: git -C would mutate the
	// outside working tree.
	evil := filepath.Join(t.TempDir(), "evil")
	require.NoError(t, os.Mkdir(evil, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(evil, ".git"),
		[]byte("gitdir: "+filepath.Join(inside, ".git")+"\n"),
		0o644,
	))

	h := &handler{allowDirs: []string{allowDir}}

	_, err := h.runRemote(t.Context(), remoteOp{
		repo:   evil,
		remote: "origin",
		args:   []string{"fetch", "--", "origin"},
	})
	require.ErrorIs(t, err, ErrDeniedDest)
}

func TestRunGitCapturesOutput(t *testing.T) {
	t.Parallel()

	out, err := runGit(t.Context(), "version")
	require.NoError(t, err)
	assert.Contains(t, out, "git version")

	// Failure output (git writes it to stderr) must be captured and
	// folded into the error, never streamed to the process stdout.
	out, err = runGit(t.Context(), "-C", t.TempDir(), "rev-parse", "--git-dir")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
	assert.Contains(t, out, "not a git repository")
}

func TestGitValueDiscardsStderr(t *testing.T) {
	t.Parallel()

	out, err := gitValue(t.Context(), "version")
	require.NoError(t, err)
	assert.Contains(t, out, "git version")

	// gitValue errors must not embed captured output: callers use it
	// for values (remote URLs) that may hold credentials.
	_, err = gitValue(t.Context(), "-C", t.TempDir(), "rev-parse", "--git-dir")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "not a git repository")
}

func TestTruncateOutput(t *testing.T) {
	t.Parallel()

	short := "hello"
	assert.Equal(t, short, truncateOutput(short))

	long := strings.Repeat("x", maxOutputBytes) + "TAIL"
	got := truncateOutput(long)
	assert.True(t, strings.HasPrefix(got, "[output truncated]\n"))
	assert.True(t, strings.HasSuffix(got, "TAIL"))
	assert.LessOrEqual(t, len(got), maxOutputBytes+len("[output truncated]\n"))
}

func TestGitEnvDefaultSSHCommand(t *testing.T) { //nolint:paralleltest // t.Setenv
	t.Setenv("GIT_SSH_COMMAND", "")

	env := gitEnv()
	assert.Contains(t, env, "GIT_TERMINAL_PROMPT=0")
	assert.Contains(t, env, "GIT_EDITOR=true")
	assert.Contains(t, env, "GIT_SEQUENCE_EDITOR=true")
	assert.Contains(t, env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
}

func TestGitEnvPreservesSSHCommand(t *testing.T) { //nolint:paralleltest // t.Setenv
	t.Setenv("GIT_SSH_COMMAND", "ssh -i /custom/key")

	env := gitEnv()
	assert.Contains(t, env, "GIT_SSH_COMMAND=ssh -i /custom/key")
	assert.NotContains(t, env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
}

func TestRemoteOrDefault(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "origin", remoteOrDefault(""))
	assert.Equal(t, "upstream", remoteOrDefault("upstream"))
}
