package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// writeHook installs an executable hook script in repo's default
// hooks directory and returns the path of the hook file.
func writeHook(t *testing.T, repo, name, script string) string {
	t.Helper()

	return writeHookIn(t, filepath.Join(repo, ".git", "hooks"), name, script)
}

// writeHookIn installs an executable hook script in dir and returns
// the path of the hook file.
func writeHookIn(t *testing.T, dir, name, script string) string {
	t.Helper()

	hook := filepath.Join(dir, name)

	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(
		t,
		os.WriteFile(hook, []byte(script), 0o755),
	) //nolint:gosec // test hook must be executable

	return hook
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

func TestIsLocalURL(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		url  string
		want bool
	}{
		"absolute path":      {url: "/srv/git/repo.git", want: true},
		"relative path":      {url: "../repo.git", want: true},
		"bare name":          {url: "repo", want: true},
		"slash before colon": {url: "./a:b", want: true},
		"file URL":           {url: "file:///srv/git/repo.git", want: true},
		"empty":              {url: "", want: true},
		"https URL":          {url: "https://github.com/o/r.git"},
		"ssh URL":            {url: "ssh://git@github.com/o/r.git"},
		"SCP-style URL":      {url: "git@github.com:o/r.git"},
		"SCP-style host":     {url: "host:repo"},
		"remote helper":      {url: "codecommit::us-east-1://repo"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isLocalURL(tt.url))
		})
	}
}

func TestGitArgs(t *testing.T) {
	t.Parallel()

	hooksOff := []string{"-c", "core.hooksPath=" + os.DevNull}
	fileOff := []string{"-c", "protocol.file.allow=never"}
	recurseOff := []string{"-c", "push.recurseSubmodules=no"}
	recurseCheck := []string{"-c", "push.recurseSubmodules=check"}
	repo := []string{"-C", "/r"}
	fetch := []string{"fetch", "--", "origin"}
	push := []string{"push", "--", "origin"}

	tests := map[string]struct {
		h               *handler
		op              remoteOp
		checkSubmodules bool
		want            []string
	}{
		"fetch": {
			h:    &handler{},
			op:   remoteOp{args: fetch},
			want: slices.Concat(hooksOff, repo, fetch),
		},
		"fetch ignores the submodule check": {
			h:               &handler{},
			op:              remoteOp{args: fetch},
			checkSubmodules: true,
			want:            slices.Concat(hooksOff, repo, fetch),
		},
		"push": {
			h:    &handler{},
			op:   remoteOp{push: true, args: push},
			want: slices.Concat(hooksOff, fileOff, recurseOff, repo, push),
		},
		"push that checks submodules": {
			h:               &handler{},
			op:              remoteOp{push: true, args: push},
			checkSubmodules: true,
			want:            slices.Concat(hooksOff, fileOff, recurseCheck, repo, push),
		},
		"push with file URLs allowed": {
			h:    &handler{allowFileURLs: true},
			op:   remoteOp{push: true, args: push},
			want: slices.Concat(hooksOff, recurseOff, repo, push),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := tt.h.gitArgs(tt.op, "/r", "https://example.invalid/r.git", tt.checkSubmodules)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPushRecursesSubmodules(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// config holds the lines that the test appends to the git
		// config of the repository, in order.
		config string
		want   bool
		err    bool
	}{
		"no config":                        {},
		"push.recurseSubmodules check":     {config: "[push]\n\trecurseSubmodules = check\n", want: true},
		"push.recurseSubmodules on-demand": {config: "[push]\n\trecurseSubmodules = on-demand\n", want: true},
		"push.recurseSubmodules only":      {config: "[push]\n\trecurseSubmodules = only\n", want: true},
		"push.recurseSubmodules no":        {config: "[push]\n\trecurseSubmodules = no\n"},
		"push.recurseSubmodules off":       {config: "[push]\n\trecurseSubmodules = off\n"},
		"push.recurseSubmodules empty":     {config: "[push]\n\trecurseSubmodules =\n"},
		"submodule.recurse true":           {config: "[submodule]\n\trecurse = true\n", want: true},
		"submodule.recurse yes":            {config: "[submodule]\n\trecurse = yes\n", want: true},
		"submodule.recurse 1":              {config: "[submodule]\n\trecurse = 1\n", want: true},
		"submodule.recurse without value":  {config: "[submodule]\n\trecurse\n", want: true},
		"submodule.recurse false":          {config: "[submodule]\n\trecurse = false\n"},
		"key in another case":              {config: "[Push]\n\tRecurseSubmodules = check\n", want: true},
		"submodule.recurse false after push.recurseSubmodules on-demand": {
			config: "[push]\n\trecurseSubmodules = on-demand\n[submodule]\n\trecurse = false\n",
		},
		"push.recurseSubmodules no after submodule.recurse true": {
			config: "[submodule]\n\trecurse = true\n[push]\n\trecurseSubmodules = no\n",
		},
		"push.recurseSubmodules check after submodule.recurse false": {
			config: "[submodule]\n\trecurse = false\n[push]\n\trecurseSubmodules = check\n",
			want:   true,
		},
		"broken config": {config: "[push\n", err: true},
		"other submodule keys": {
			config: "[submodule \"sub\"]\n\tpath = sub\n\turl = https://example.invalid/sub.git\n",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := filepath.Join(t.TempDir(), "repo")
			runGitCmds(t, [][]string{{"git", "init", "-q", repo}})

			config := filepath.Join(repo, ".git", "config")
			f, err := os.OpenFile(config, os.O_APPEND|os.O_WRONLY, 0o644)
			require.NoError(t, err)

			_, err = f.WriteString(tt.config)
			require.NoError(t, err)
			require.NoError(t, f.Close())

			got, err := pushRecursesSubmodules(t.Context(), repo)
			if tt.err {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConfigHooks(t *testing.T) {
	t.Parallel()

	// hook returns the git config commands that define the hook name
	// for event in repo.
	hook := func(repo, name, event string) [][]string {
		return [][]string{
			{"git", "-C", repo, "config", "hook." + name + ".command", "true"},
			{"git", "-C", repo, "config", "hook." + name + ".event", event},
		}
	}

	tests := map[string]struct {
		// setup prepares the clone at work.
		setup func(t *testing.T, work string)
		want  []string
	}{
		"no hook": {
			setup: func(*testing.T, string) {},
		},
		"hook": {
			setup: func(t *testing.T, work string) {
				t.Helper()

				runGitCmds(t, hook(work, "check", "pre-push"))
			},
			want: []string{"check"},
		},
		"hooks in config order": {
			setup: func(t *testing.T, work string) {
				t.Helper()

				runGitCmds(t, hook(work, "lint", "pre-push"))
				runGitCmds(t, hook(work, "check", "pre-push"))
			},
			want: []string{"lint", "check"},
		},
		"disabled hook": {
			setup: func(t *testing.T, work string) {
				t.Helper()

				runGitCmds(t, hook(work, "check", "pre-push"))
				runGitCmds(t, [][]string{
					{"git", "-C", work, "config", "hook.check.enabled", "false"},
				})
			},
		},
		"hook for another event": {
			setup: func(t *testing.T, work string) {
				t.Helper()

				runGitCmds(t, hook(work, "check", "pre-commit"))
			},
		},
		// hookFile reports the hook file.
		"hook file": {
			setup: func(t *testing.T, work string) {
				t.Helper()

				writeHook(t, work, "pre-push", "#!/bin/sh\n")
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			work := initWorkClone(t, initBareRepo(t))
			tt.setup(t, work)

			got, err := configHooks(t.Context(), work, "pre-push")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHookFile(t *testing.T) {
	t.Parallel()

	const script = "#!/bin/sh\n"

	tests := map[string]struct {
		// setup prepares the clone at work. It returns the
		// repository path to ask about and the hook file that the
		// repository holds for pre-push, or "" when it holds none.
		setup func(t *testing.T, work string) (repo, want string)
	}{
		"no hook file": {
			setup: func(_ *testing.T, work string) (string, string) {
				return work, ""
			},
		},
		"executable file": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				return work, writeHook(t, work, "pre-push", script)
			},
		},
		"file for another event": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				writeHook(t, work, "pre-commit", script)

				return work, ""
			},
		},
		"file that is not executable": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				hook := writeHook(t, work, "pre-push", script)
				require.NoError(t, os.Chmod(hook, 0o644))

				return work, ""
			},
		},
		"directory": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				hook := filepath.Join(work, ".git", "hooks", "pre-push")
				require.NoError(t, os.MkdirAll(hook, 0o755))

				return work, ""
			},
		},
		"symlink to an executable file": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				target := writeHookIn(t, t.TempDir(), "check", script)
				hook := filepath.Join(work, ".git", "hooks", "pre-push")
				require.NoError(t, os.MkdirAll(filepath.Dir(hook), 0o755))
				require.NoError(t, os.Symlink(target, hook))

				return work, hook
			},
		},
		"hooksPath in the working tree": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				// The default hooks directory must lose to hooksPath.
				writeHook(t, work, "pre-push", script)
				runGitCmds(t, [][]string{
					{"git", "-C", work, "config", "core.hooksPath", ".husky"},
				})

				return work, writeHookIn(t, filepath.Join(work, ".husky"), "pre-push", script)
			},
		},
		"hooksPath that is not a directory": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				writeHook(t, work, "pre-push", script)
				runGitCmds(t, [][]string{
					{"git", "-C", work, "config", "core.hooksPath", os.DevNull},
				})

				return work, ""
			},
		},
		"subdirectory of the working tree": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				sub := filepath.Join(work, "sub")
				require.NoError(t, os.Mkdir(sub, 0o755))

				return sub, writeHook(t, work, "pre-push", script)
			},
		},
		// git prints the hook path relative to the resolved directory,
		// so its ".." components climb from the target of the symlink.
		"symlink to a subdirectory": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				sub := filepath.Join(work, "a", "b")
				require.NoError(t, os.MkdirAll(sub, 0o755))

				link := filepath.Join(t.TempDir(), "link")
				require.NoError(t, os.Symlink(sub, link))

				return link, writeHook(t, work, "pre-push", script)
			},
		},
		// A linked worktree shares the hooks directory of the main
		// checkout.
		"linked worktree": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				wt := filepath.Join(t.TempDir(), "wt")
				runGitCmds(t, [][]string{
					{"git", "-C", work, "worktree", "add", "-b", "wt-branch", wt},
				})

				return wt, writeHook(t, work, "pre-push", script)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo, want := tt.setup(t, initWorkClone(t, initBareRepo(t)))

			got, err := hookFile(t.Context(), repo, "pre-push")
			require.NoError(t, err)

			if want == "" {
				assert.Empty(t, got)

				return
			}

			// git names the hooks directory of a linked worktree by
			// its resolved path, so compare the hook files and not
			// the spelling of their paths.
			wantInfo, err := os.Stat(want)
			require.NoError(t, err)

			gotInfo, err := os.Stat(got)
			require.NoError(t, err)

			assert.True(t, os.SameFile(wantInfo, gotInfo), "got %s, want %s", got, want)
			assert.True(t, filepath.IsAbs(got), "got %s, want an absolute path", got)
		})
	}

	_, err := hookFile(t.Context(), t.TempDir(), "pre-push")
	require.Error(t, err, "a path outside a repository holds no hook path")
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
