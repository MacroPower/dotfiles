package main

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

// gitOut returns the trimmed stdout of a git command, failing the
// test on error.
func gitOut(t *testing.T, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // test helper
	out, err := cmd.Output()
	require.NoError(t, err, "git %v", args)

	return strings.TrimSpace(string(out))
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

// gitStandIn puts a directory that holds only the script git on PATH,
// so that the server runs the script in place of git. An empty script
// leaves PATH without git. gitStandIn sets an environment variable, so
// the test must not run in parallel.
func gitStandIn(t *testing.T, script string) {
	t.Helper()

	bin := t.TempDir()
	if script != "" {
		writeHookIn(t, bin, "git", script)
	}

	t.Setenv("PATH", bin)
}

// markerScript writes an executable script that creates a marker file
// and then fails. It returns the path of the script and the path of
// the marker. A test sets a command that git must not run to the
// script. The marker then shows that git ran the command, and the
// failure of the script also fails most git commands that run it.
func markerScript(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := writeHookIn(t, dir, "command", "#!/bin/sh\ntouch '"+marker+"'\nexit 1\n")

	return script, marker
}

// requireNotRun fails the test when the script of [markerScript] ran.
func requireNotRun(t *testing.T, marker string) {
	t.Helper()

	_, err := os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist, "git must not run a command that the git config names")
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

// unparseableConfig is git config text that holds a section header
// without its closing bracket. git cannot parse that line, so git
// cannot open a repository with that config.
const unparseableConfig = "[remote \"origin\"\n"

// git exits with status 128 when it finds no repository from a
// directory. It exits with the same status when it finds a repository
// that it cannot open. gitCommonDir and absoluteGitDir must report a
// missing repository only for the first failure. For every other
// failure, the error must say that git could not open the repository,
// and it must hold the message of git.
func TestRepoPathErrors(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		// dir prepares the directory of the call and returns its
		// path.
		dir func(t *testing.T) string

		// want is text that the message of git holds.
		want string

		// err is set when the error must report a missing repository.
		err error
	}{
		"directory outside every repository": {
			dir: func(t *testing.T) string {
				t.Helper()

				return t.TempDir()
			},
			err: ErrNotRepo,
		},
		"config that git cannot parse": {
			dir: func(t *testing.T) string {
				t.Helper()

				return initConfigRepo(t, unparseableConfig)
			},
			want: "bad config line",
		},
		"directory in a repository whose config git cannot parse": {
			dir: func(t *testing.T) string {
				t.Helper()

				dir := filepath.Join(initConfigRepo(t, unparseableConfig), "dir")
				require.NoError(t, os.Mkdir(dir, 0o755))

				return dir
			},
			want: "bad config line",
		},
		"directory that does not exist": {
			dir: func(t *testing.T) string {
				t.Helper()

				return filepath.Join(t.TempDir(), "missing")
			},
			want: "cannot change to",
		},
	}

	paths := map[string]func(ctx context.Context, dir string) (string, error){
		"gitCommonDir":   gitCommonDir,
		"absoluteGitDir": absoluteGitDir,
	}

	for pathName, path := range paths {
		for name, tc := range tcs {
			t.Run(pathName+"/"+name, func(t *testing.T) {
				t.Parallel()

				dir := tc.dir(t)

				_, err := path(t.Context(), dir)
				require.Error(t, err)

				if tc.err != nil {
					require.ErrorIs(t, err, tc.err)
					assert.EqualError(t, err, "not a git repository: "+dir)

					return
				}

				require.NotErrorIs(t, err, ErrNotRepo)

				message := err.Error()
				assert.Contains(t, message, "opening the repository at "+dir+": running git: exit status 128: fatal: ")
				assert.Contains(t, message, tc.want)
				assert.NotContains(t, message, ErrNotRepo.Error())
			})
		}
	}
}

// git translates its messages into the language of the locale, and
// gitCommonDir reads the message of git to find a missing repository.
// It must report a directory outside every repository as one, whatever
// the locale of the server is.
func TestGitCommonDirIgnoresLocale(t *testing.T) { //nolint:paralleltest // t.Setenv
	// LANGUAGE names the language of the messages in every locale
	// except the C locale.
	t.Setenv("LANGUAGE", "de")
	t.Setenv("LC_ALL", "en_US.UTF-8")

	dir := t.TempDir()

	// The check needs a git that prints a translated message here. git
	// does not translate when it has no German messages, or when the
	// host lacks the locale that LC_ALL names.
	cmd := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "--git-dir") //nolint:gosec // test helper

	out, err := cmd.CombinedOutput()
	require.Error(t, err)

	if strings.Contains(string(out), noRepoMessage) {
		t.Skip("git does not translate its messages in this environment")
	}

	_, err = gitCommonDir(t.Context(), dir)
	require.ErrorIs(t, err, ErrNotRepo)
}

// git can exit with an error and print nothing, and the server can
// fail to start git. gitCommonDir must then report neither a missing
// repository nor an empty message of git.
func TestGitCommonDirWithoutMessage(t *testing.T) { //nolint:paralleltest // t.Setenv
	tcs := map[string]struct {
		// git is the script that stands in for git. An empty script
		// leaves PATH without git.
		git string

		// want is the cause that the error names after the path.
		want string
	}{
		"git exits with an error and prints nothing": {
			git:  "#!/bin/sh\nexit 128\n",
			want: "running git: exit status 128",
		},
		"git cannot start": {
			want: `running git: exec: "git": executable file not found in $PATH`,
		},
	}

	for name, tc := range tcs { //nolint:paralleltest // t.Setenv
		t.Run(name, func(t *testing.T) {
			gitStandIn(t, tc.git)

			dir := t.TempDir()

			_, err := gitCommonDir(t.Context(), dir)
			require.NotErrorIs(t, err, ErrNotRepo)
			assert.EqualError(t, err, "opening the repository at "+dir+": "+tc.want)
		})
	}
}

// The working tree that core.worktree names can hold a repository of
// its own. checkWorkTree must say that git finds no git directory only
// when git finds no repository from the working tree. For every other
// failure of git, the error must hold the message of git.
func TestCheckWorkTreeErrors(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		// tree prepares the working tree and returns its path.
		tree func(t *testing.T) string

		// want returns text that the error must hold for the working
		// tree at tree.
		want func(tree string) string
	}{
		"directory outside every repository": {
			tree: func(t *testing.T) string {
				t.Helper()

				return t.TempDir()
			},
			want: func(tree string) string {
				return ": git finds no git directory from " + tree
			},
		},
		"checkout whose config git cannot parse": {
			tree: func(t *testing.T) string {
				t.Helper()

				return initConfigRepo(t, unparseableConfig)
			},
			want: func(tree string) string {
				return ": opening the repository at " + tree + ": running git: exit status 128: fatal: bad config line"
			},
		},
		// git prints the value of core.worktree as the working tree,
		// whether or not a directory exists there.
		"directory that does not exist": {
			tree: func(t *testing.T) string {
				t.Helper()

				return filepath.Join(t.TempDir(), "missing")
			},
			want: func(tree string) string {
				return ": opening the repository at " + tree + ": running git: exit status 128: fatal: cannot change to"
			},
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := initConfigRepo(t, "")
			tree := tc.tree(t)

			err := (&handler{}).checkWorkTree(t.Context(), repo, tree)
			require.ErrorIs(t, err, ErrDeniedWorkTree)
			assert.ErrorContains(t, err, ErrDeniedWorkTree.Error()+tc.want(tree))
		})
	}
}

func TestGitWorkTree(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	want, err := filepath.EvalSymlinks(work)
	require.NoError(t, err)

	got, err := gitWorkTree(t.Context(), work)
	require.NoError(t, err)
	assert.Equal(t, want, got)

	sub := filepath.Join(work, "dir")
	require.NoError(t, os.Mkdir(sub, 0o755))

	got, err = gitWorkTree(t.Context(), sub)
	require.NoError(t, err)
	assert.Equal(t, want, got, "a directory of the working tree resolves to its root")

	wt := filepath.Join(t.TempDir(), "wt")
	runGitCmds(t, [][]string{
		{"git", "-C", work, "worktree", "add", "-b", "wt-branch", wt},
	})

	wantWt, err := filepath.EvalSymlinks(wt)
	require.NoError(t, err)

	got, err = gitWorkTree(t.Context(), wt)
	require.NoError(t, err)
	assert.Equal(t, wantWt, got)

	got, err = gitWorkTree(t.Context(), bare)
	require.NoError(t, err)
	assert.Empty(t, got, "a bare repository has no working tree")

	// core.worktree moves the working tree away from the path of the
	// repository.
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	setWorkTree(t, work, outside)

	got, err = gitWorkTree(t.Context(), work)
	require.NoError(t, err)
	assert.Equal(t, outside, got)

	// git takes a repository that sets core.bare for one that has no
	// working tree, and it prints a warning about core.worktree before
	// it says so.
	setConfig(t, work, "core.bare", "true")

	got, err = gitWorkTree(t.Context(), work)
	require.NoError(t, err)
	assert.Empty(t, got, "a repository that sets core.bare has no working tree")
}

// git translates its messages into the language of the locale.
// gitWorkTree reads the message of git to find a repository that has
// no working tree. It must report a bare repository as one, whatever
// the locale of the server is.
func TestGitWorkTreeIgnoresLocale(t *testing.T) { //nolint:paralleltest // t.Setenv
	// LANGUAGE names the language of the messages in every locale
	// except the C locale.
	t.Setenv("LANGUAGE", "de")
	t.Setenv("LC_ALL", "en_US.UTF-8")

	bare := initBareRepo(t)

	// The check needs a git that prints a translated message here. git
	// does not translate when it has no German messages, or when the
	// host lacks the locale that LC_ALL names.
	args := []string{"-C", bare, "rev-parse", "--show-toplevel"}
	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // test helper

	out, err := cmd.CombinedOutput()
	require.Error(t, err)

	if strings.Contains(string(out), noWorkTreeMessage) {
		t.Skip("git does not translate its messages in this environment")
	}

	got, err := gitWorkTree(t.Context(), bare)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// A repository has no working tree only when git says so. git can
// also fail for another reason. For example, the kernel kills git to
// free memory, or the server cannot start git. gitWorkTree must then
// return the failure, because [handler.runRemote] skips the check of
// the working tree for a repository that has none.
func TestGitWorkTreeFailure(t *testing.T) { //nolint:paralleltest // t.Setenv
	tcs := map[string]struct {
		// git is the script that stands in for git. An empty script
		// leaves PATH without git.
		git string

		// want is the cause that the error names after the path.
		want string
	}{
		"signal kills git": {
			git:  "#!/bin/sh\nkill -KILL $$\n",
			want: "running git: signal: killed",
		},
		"git exits with an error and prints nothing": {
			git:  "#!/bin/sh\nexit 128\n",
			want: "running git: exit status 128",
		},
		"git prints another message": {
			git:  "#!/bin/sh\necho \"fatal: cannot chdir to '': No such file or directory\" >&2\nexit 128\n",
			want: "running git: exit status 128: fatal: cannot chdir to '': No such file or directory",
		},
		"git cannot start": {
			want: `running git: exec: "git": executable file not found in $PATH`,
		},
	}

	for name, tc := range tcs { //nolint:paralleltest // t.Setenv
		t.Run(name, func(t *testing.T) {
			gitStandIn(t, tc.git)

			dir := t.TempDir()

			got, err := gitWorkTree(t.Context(), dir)
			require.EqualError(t, err, "resolving the working tree of "+dir+": "+tc.want)
			assert.Empty(t, got)
		})
	}
}

func TestEnclosingGitDir(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)
	gitDir := filepath.Join(work, ".git")

	wt := filepath.Join(t.TempDir(), "wt")
	runGitCmds(t, [][]string{{"git", "-C", work, "worktree", "add", "-b", "wt-branch", wt}})

	// git keeps the HEAD and the commondir file of a linked worktree
	// in a directory of the common git directory.
	wtGitDir := gitOut(t, "-C", wt, "rev-parse", "--absolute-git-dir")

	// A caller that writes a git directory can move its HEAD aside,
	// or replace HEAD with a symbolic link that git accepts.
	headless := filepath.Join(initRepo(t, t.TempDir()), ".git")
	head := filepath.Join(headless, "HEAD")
	require.NoError(t, os.Rename(head, head+".aside"))

	missingRef := filepath.Join(t.TempDir(), "missing.git")
	initEmptyBare(t, missingRef)
	linkHead(t, missingRef, "refs/heads/nope")

	refsDir := filepath.Join(t.TempDir(), "dir.git")
	initEmptyBare(t, refsDir)
	linkHead(t, refsDir, "refs/heads")

	named := filepath.Join(t.TempDir(), ".git")
	upper := filepath.Join(t.TempDir(), ".GIT")

	tests := map[string]struct {
		dir  string
		want string
	}{
		"working tree":                          {dir: work},
		"directory of the working tree":         {dir: filepath.Join(work, "dir")},
		"linked worktree":                       {dir: wt},
		"plain directory":                       {dir: t.TempDir()},
		"git directory":                         {dir: gitDir, want: gitDir},
		"hooks directory":                       {dir: makeHooksDir(t, gitDir), want: gitDir},
		"bare repository":                       {dir: bare, want: bare},
		"git directory of a linked worktree":    {dir: wtGitDir, want: wtGitDir},
		"hooks directory, HEAD moved aside":     {dir: makeHooksDir(t, headless), want: headless},
		"directory named .git":                  {dir: named, want: named},
		"directory inside .GIT":                 {dir: filepath.Join(upper, "hooks"), want: upper},
		"bare repository, HEAD links to no ref": {dir: missingRef, want: missingRef},
		"bare repository, HEAD links to a dir":  {dir: makeHooksDir(t, refsDir), want: refsDir},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, enclosingGitDir(tt.dir))
		})
	}
}

func TestConfigValue(t *testing.T) {
	t.Parallel()

	repo := initRepo(t, t.TempDir())

	setConfig(t, repo,
		"mcp-git.name", "value",
		"mcp-git.switch", "yes",
		"branch.feat/x.merge", "refs/heads/feat/x",
	)

	tests := map[string]struct {
		key     string
		options []string
		want    string
		err     bool
	}{
		"key that the config sets":    {key: "mcp-git.name", want: "value"},
		"key in another case":         {key: "MCP-Git.Name", want: "value"},
		"key with a subsection":       {key: "branch.feat/x.merge", want: "refs/heads/feat/x"},
		"key that the config lacks":   {key: "mcp-git.nosuch"},
		"boolean":                     {key: "mcp-git.switch", options: []string{"--type=bool"}, want: "true"},
		"boolean that the key is not": {key: "mcp-git.name", options: []string{"--type=bool"}, err: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := configValue(t.Context(), repo, tt.key, tt.options...)
			if tt.err {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// fetchOnlyOrigin is git config text that defines the origin remote
// with a fetch refspec and without a URL.
const fetchOnlyOrigin = "[remote \"origin\"]\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"

// git uses the name of a remote without a URL as its URL. The git
// config can also store that name as a URL of the remote, and a
// rewrite can change the name. remoteLacksURL must return true only
// for a remote that has no URL to connect to.
func TestRemoteLacksURL(t *testing.T) {
	t.Parallel()

	const (
		fetchURL = aliasPrefix + "a.git"
		pushURL  = aliasPrefix + "p.git"

		// namedPushURL stores the name of the origin remote as its
		// push URL, and no URL.
		namedPushURL = "[remote \"origin\"]\n\tpushurl = origin\n"

		// rewrite and pushRewrite change the name origin to a URL
		// under aliasPrefix.
		rewrite     = "[url \"" + aliasPrefix + "\"]\n\tinsteadOf = ori\n"
		pushRewrite = "[url \"" + aliasPrefix + "\"]\n\tpushInsteadOf = ori\n"
	)

	// originURLs returns git config text that stores a URL and a push
	// URL for the origin remote. An empty value clears the URLs that
	// the git config stores before it, in git 2.46 and later.
	originURLs := func(url, pushURL string) string {
		return "[remote \"origin\"]\n\turl = " + url + "\n\tpushurl = " + pushURL + "\n"
	}

	tcs := map[string]struct {
		config string
		remote string
		push   bool
		want   bool
	}{
		"URL": {
			config: remoteNamed("origin", fetchURL),
		},
		"URL, for a push": {
			config: remoteNamed("origin", fetchURL),
			push:   true,
		},
		"push URL and no URL": {
			config: pushOnlyOrigin,
			want:   true,
		},
		"push URL and no URL, for a push": {
			config: pushOnlyOrigin,
			push:   true,
		},
		"fetch refspec and no URL": {
			config: fetchOnlyOrigin,
			want:   true,
		},
		"fetch refspec and no URL, for a push": {
			config: fetchOnlyOrigin,
			push:   true,
			want:   true,
		},
		"empty value clears the URL": {
			config: remoteNamed("origin", fetchURL) + remoteNamed("origin", ""),
			want:   true,
		},
		"empty values clear the URL and the push URL, for a push": {
			config: originURLs(fetchURL, pushURL) + originURLs("", ""),
			push:   true,
			want:   true,
		},
		"empty value clears only the URL, for a push": {
			config: originURLs(fetchURL, pushURL) + remoteNamed("origin", ""),
			push:   true,
		},
		"empty value clears only the push URL, for a push": {
			config: originURLs(fetchURL, pushURL) + "[remote \"origin\"]\n\tpushurl =\n",
			push:   true,
		},
		"URL is the name of the remote": {
			config: remoteNamed("origin", "origin"),
		},
		"URL is the name of the remote, for a push": {
			config: remoteNamed("origin", "origin"),
			push:   true,
		},
		"push URL is the name of the remote": {
			config: namedPushURL,
			want:   true,
		},
		"push URL is the name of the remote, for a push": {
			config: namedPushURL,
			push:   true,
		},
		"rewrite changes the name": {
			config: fetchOnlyOrigin + rewrite,
		},
		"rewrite changes the name, for a push": {
			config: fetchOnlyOrigin + rewrite,
			push:   true,
		},
		"push rewrite changes the name": {
			config: fetchOnlyOrigin + pushRewrite,
			want:   true,
		},
		"push rewrite changes the name, for a push": {
			config: fetchOnlyOrigin + pushRewrite,
			push:   true,
		},
		"name with a dot": {
			config: "[remote \"up.stream\"]\n\tfetch = +refs/heads/*:refs/remotes/up.stream/*\n",
			remote: "up.stream",
			want:   true,
		},
		"name with a dot, for a push": {
			config: "[remote \"up.stream\"]\n\tpushurl = " + pushURL + "\n",
			remote: "up.stream",
			push:   true,
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := initConfigRepo(t, tc.config)
			remote := remoteOrDefault(tc.remote)

			urls, err := remoteURLs(t.Context(), repo, remote, tc.push)
			require.NoError(t, err)

			got, err := remoteLacksURL(t.Context(), repo, remote, urls[0], tc.push)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// valuelessOriginURL is git config text that gives the URL of the
// origin remote no value. git opens a repository with that config, and
// git remote get-url rejects the config.
const valuelessOriginURL = "[remote \"origin\"]\n\turl\n"

// git remote get-url exits with status 2 only when it finds no remote
// of the name. remoteURLs must report an unknown remote only then. The
// git config can define a remote that git cannot read. The error must
// then say that remoteURLs could not read the remote.
func TestRemoteURLsErrors(t *testing.T) {
	t.Parallel()

	const (
		originWithURL = "[remote \"origin\"]\n\turl = " + aliasPrefix + "a.git\n"

		// valuelessPushURL gives the push URL of the origin remote no
		// value, and it stores a URL for the remote.
		valuelessPushURL = originWithURL + "[remote \"origin\"]\n\tpushurl\n"
	)

	tcs := map[string]struct {
		config string
		remote string
		push   bool

		// err is set when the error must report an unknown remote.
		err error
	}{
		"missing remote": {
			remote: "nosuch",
			err:    ErrUnknownRemote,
		},
		"missing remote, for a push": {
			remote: "nosuch",
			push:   true,
			err:    ErrUnknownRemote,
		},
		"missing remote beside another remote": {
			config: originWithURL,
			remote: "nosuch",
			err:    ErrUnknownRemote,
		},
		"URL without a value": {
			config: valuelessOriginURL,
		},
		"URL without a value, for a push": {
			config: valuelessOriginURL,
			push:   true,
		},
		"push URL without a value": {
			config: valuelessPushURL,
		},
		"push URL without a value, for a push": {
			config: valuelessPushURL,
			push:   true,
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := initConfigRepo(t, tc.config)
			remote := remoteOrDefault(tc.remote)

			_, err := remoteURLs(t.Context(), repo, remote, tc.push)
			require.Error(t, err)

			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
				assert.EqualError(t, err, "unknown remote: "+remote)

				return
			}

			require.NotErrorIs(t, err, ErrUnknownRemote)
			assert.ErrorContains(t, err, "reading the "+remote+" remote of "+repo+": ")
		})
	}
}

func TestGitOptions(t *testing.T) {
	t.Parallel()

	guard := []string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "core.alternateRefsCommand=true",
		"-c", "maintenance.auto=false",
	}
	push := []string{
		"-c", "push.recurseSubmodules=no",
		"-c", "push.pushOption=",
		"-c", "remote.up.mirror=false",
	}
	cred := []string{
		"-c", "credential.https://github.com.helper=",
		"-c", "credential.https://github.com.helper=" + githubCredentialHelper,
	}
	repo := []string{"-C", "/r"}

	tests := map[string]struct {
		token    string
		op       remoteOp
		workTree string
		hooks    []string
		want     []string
	}{
		"fetch": {
			want: slices.Concat(guard, repo),
		},
		// The credential options name no URL of the remote, so every
		// command takes them.
		"token": {
			token: "ghp_test123",
			want:  slices.Concat(cred, guard, repo),
		},
		"push with a token": {
			token: "ghp_test123",
			op:    remoteOp{remote: "up", push: true},
			want:  slices.Concat(cred, guard, push, repo),
		},
		"push": {
			op:   remoteOp{remote: "up", push: true},
			want: slices.Concat(guard, push, repo),
		},
		"working tree": {
			workTree: "/w",
			want:     slices.Concat(guard, []string{"--work-tree=/w"}, repo),
		},
		// git reads the value of each key from the environment, so
		// the name of a hook can hold '='.
		"hooks": {
			hooks: []string{"check", "a.b c", "x=y", ""},
			want: slices.Concat(guard, []string{
				"--config-env=hook.check.enabled=" + hookSwitchEnv,
				"--config-env=hook.a.b c.enabled=" + hookSwitchEnv,
				"--config-env=hook.x=y.enabled=" + hookSwitchEnv,
				"--config-env=hook..enabled=" + hookSwitchEnv,
			}, repo),
		},
		"push with a hook and a working tree": {
			op:       remoteOp{remote: "up", push: true},
			workTree: "/w",
			hooks:    []string{"check"},
			want: slices.Concat(
				guard,
				[]string{"--config-env=hook.check.enabled=" + hookSwitchEnv},
				push,
				[]string{"--work-tree=/w"},
				repo,
			),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := (&handler{token: tt.token}).gitOptions(tt.op, "/r", tt.workTree, tt.hooks)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCommandEnv(t *testing.T) { //nolint:paralleltest // t.Setenv
	// The environment of the test process might already hold a value
	// that the server sets. The test therefore gives each of these
	// variables another value first, so that each assertion passes
	// only when the server sets the variable.
	t.Setenv("GIT_ALLOW_PROTOCOL", "ext")
	t.Setenv(hookSwitchEnv, "true")
	t.Setenv("GIT_TERMINAL_PROMPT", "1")

	tests := map[string]struct {
		h    *handler
		want string
	}{
		"default":         {h: &handler{}, want: "https:ssh"},
		"allow insecure":  {h: &handler{allowInsecure: true}, want: "https:ssh:http:git"},
		"allow file URLs": {h: &handler{allowFileURLs: true}, want: "https:ssh:file"},
		"allow both": {
			h:    &handler{allowInsecure: true, allowFileURLs: true},
			want: "https:ssh:http:git:file",
		},
	}

	for name, tt := range tests { //nolint:paralleltest // t.Setenv
		t.Run(name, func(t *testing.T) {
			env := tt.h.commandEnv()

			// The process runs git with the last value of a variable,
			// so the values of the server must come last.
			assert.Equal(t, "GIT_ALLOW_PROTOCOL="+tt.want, lastEnv(env, "GIT_ALLOW_PROTOCOL"))
			assert.Equal(t, hookSwitchEnv+"=false", lastEnv(env, hookSwitchEnv))
			assert.Equal(t, "GIT_TERMINAL_PROMPT=0", lastEnv(env, "GIT_TERMINAL_PROMPT"))
		})
	}
}

// lastEnv returns the last entry of env that sets the variable name,
// or "" when env does not set it.
//
// A test must assert on the entry that lastEnv returns, never on the
// whole of env. testify prints the values of a failed assertion, and
// env holds the environment of the test process with its secrets,
// such as GH_TOKEN. A failed assertion on env therefore writes those
// secrets to the test log.
//
// A test must also set or remove each variable that it asserts on
// before it builds env, with t.Setenv or unsetEnv. The environment of
// the test process might already hold the wanted value, such as
// GIT_EDITOR=true. The assertion then passes whether or not the
// server sets the variable. To check a value that the server sets,
// give the variable another value first.
func lastEnv(env []string, name string) string {
	var last string

	for _, entry := range env {
		if strings.HasPrefix(entry, name+"=") {
			last = entry
		}
	}

	return last
}

func TestConfigHooks(t *testing.T) {
	t.Parallel()

	// hook defines the hook name for event in the git config of repo.
	hook := func(t *testing.T, repo, name, event string) {
		t.Helper()

		setConfig(t, repo,
			"hook."+name+".command", "true",
			"hook."+name+".event", event,
		)
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

				hook(t, work, "check", "pre-push")
			},
			want: []string{"check"},
		},
		"hooks in config order": {
			setup: func(t *testing.T, work string) {
				t.Helper()

				hook(t, work, "lint", "pre-push")
				hook(t, work, "check", "pre-push")
			},
			want: []string{"lint", "check"},
		},
		"disabled hook": {
			setup: func(t *testing.T, work string) {
				t.Helper()

				hook(t, work, "check", "pre-push")
				setConfig(t, work, "hook.check.enabled", "false")
			},
		},
		"hook for another event": {
			setup: func(t *testing.T, work string) {
				t.Helper()

				hook(t, work, "check", "pre-commit")
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
				setConfig(t, work, "core.hooksPath", ".husky")

				return work, writeHookIn(t, filepath.Join(work, ".husky"), "pre-push", script)
			},
		},
		"hooksPath that is not a directory": {
			setup: func(t *testing.T, work string) (string, string) {
				t.Helper()

				writeHook(t, work, "pre-push", script)
				setConfig(t, work, "core.hooksPath", os.DevNull)

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

	h := &handler{allowDirs: mustResolveAllowDirs(t, sub)}

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
	copyCloneTo(t, baseRepos(t).work, inside, bare)

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

	h := &handler{allowDirs: mustResolveAllowDirs(t, allowDir)}

	_, err := h.runRemote(t.Context(), remoteOp{
		repo:   evil,
		remote: "origin",
		args:   []string{"fetch", "--", "origin"},
	})
	require.ErrorIs(t, err, ErrDeniedDest)
}

func TestRunGitCapturesOutput(t *testing.T) {
	t.Parallel()

	h := &handler{}

	out, err := h.runGit(t.Context(), "version")
	require.NoError(t, err)
	assert.Contains(t, out, "git version")

	// Failure output (git writes it to stderr) must be captured and
	// folded into the error, never streamed to the process stdout.
	out, err = h.runGit(t.Context(), "-C", t.TempDir(), "rev-parse", "--git-dir")
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
	// The environment of the test process might already hold a value
	// that gitEnv sets, such as GIT_EDITOR=true. The test therefore
	// gives each of these variables another value first, so that each
	// assertion passes only when gitEnv sets the variable.
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GIT_EDITOR", "vi")
	t.Setenv("GIT_SEQUENCE_EDITOR", "vi")

	t.Setenv("GIT_SSH_COMMAND", "")

	env := gitEnv()
	assert.Equal(t, "GIT_TERMINAL_PROMPT=0", lastEnv(env, "GIT_TERMINAL_PROMPT"))
	assert.Equal(t, "GIT_EDITOR=true", lastEnv(env, "GIT_EDITOR"))
	assert.Equal(t, "GIT_SEQUENCE_EDITOR=true", lastEnv(env, "GIT_SEQUENCE_EDITOR"))
	assert.Equal(t, "GIT_SSH_COMMAND=ssh -o BatchMode=yes", lastEnv(env, "GIT_SSH_COMMAND"))
}

func TestGitEnvPreservesSSHCommand(t *testing.T) { //nolint:paralleltest // t.Setenv
	t.Setenv("GIT_SSH_COMMAND", "ssh -i /custom/key")

	// gitEnv appends each entry that it adds. The last entry is
	// therefore the inherited entry only when gitEnv adds no default.
	assert.Equal(t, "GIT_SSH_COMMAND=ssh -i /custom/key", lastEnv(gitEnv(), "GIT_SSH_COMMAND"))
}

// unsetEnv removes the variable name from the environment for the
// rest of the test. t.Setenv restores the variable when the test
// ends.
func unsetEnv(t *testing.T, name string) {
	t.Helper()

	t.Setenv(name, "")
	require.NoError(t, os.Unsetenv(name))
}

// git reads core.askPass and core.gitProxy only when the environment
// does not set the variable, so gitEnv sets each variable.
func TestGitEnvSetsConfigCommands(t *testing.T) { //nolint:paralleltest // t.Setenv
	unsetEnv(t, "GIT_ASKPASS")
	unsetEnv(t, "SSH_ASKPASS")
	unsetEnv(t, "GIT_PROXY_COMMAND")

	env := gitEnv()
	assert.Equal(t, "GIT_ASKPASS=", lastEnv(env, "GIT_ASKPASS"))
	assert.Equal(t, "GIT_PROXY_COMMAND=", lastEnv(env, "GIT_PROXY_COMMAND"))
}

// git reads SSH_ASKPASS after core.askPass, so gitEnv moves its value
// to GIT_ASKPASS, which git reads first.
func TestGitEnvKeepsSSHAskpass(t *testing.T) { //nolint:paralleltest // t.Setenv
	unsetEnv(t, "GIT_ASKPASS")
	t.Setenv("SSH_ASKPASS", "/usr/bin/askpass")

	assert.Equal(t, "GIT_ASKPASS=/usr/bin/askpass", lastEnv(gitEnv(), "GIT_ASKPASS"))
}

func TestGitEnvPreservesConfigCommands(t *testing.T) { //nolint:paralleltest // t.Setenv
	t.Setenv("GIT_ASKPASS", "/custom/askpass")
	t.Setenv("SSH_ASKPASS", "/usr/bin/askpass")
	t.Setenv("GIT_PROXY_COMMAND", "/custom/proxy")

	env := gitEnv()
	assert.Equal(t, "GIT_ASKPASS=/custom/askpass", lastEnv(env, "GIT_ASKPASS"))
	assert.Equal(t, "GIT_PROXY_COMMAND=/custom/proxy", lastEnv(env, "GIT_PROXY_COMMAND"))
}

// A variable with an empty value already keeps git from reading the
// config key.
func TestGitEnvPreservesEmptyConfigCommands(t *testing.T) { //nolint:paralleltest // t.Setenv
	t.Setenv("GIT_ASKPASS", "")
	t.Setenv("SSH_ASKPASS", "/usr/bin/askpass")
	t.Setenv("GIT_PROXY_COMMAND", "")

	env := gitEnv()
	assert.Equal(t, "GIT_ASKPASS=", lastEnv(env, "GIT_ASKPASS"))
	assert.Equal(t, "GIT_PROXY_COMMAND=", lastEnv(env, "GIT_PROXY_COMMAND"))
}

// setConfigFileEnv points GIT_CONFIG at a new file that holds text,
// for the rest of the test. A git config command without a file
// option then reads and writes only that file, and so does each git
// config command that a test runs itself. A test therefore calls
// setConfigFileEnv after it runs those commands.
func setConfigFileEnv(t *testing.T, text string) {
	t.Helper()

	file := filepath.Join(t.TempDir(), "config")

	require.NoError(t, os.WriteFile(file, []byte(text), 0o644))
	t.Setenv("GIT_CONFIG", file)
}

// git config reads only the file that GIT_CONFIG names, and every
// other git command ignores the variable. gitEnv therefore removes
// the variable, whatever its value is. It keeps the variables that
// every git command reads, whose names start with GIT_CONFIG_.
func TestGitEnvRemovesConfigFile(t *testing.T) { //nolint:paralleltest // t.Setenv
	// values maps the name of a case to the value of GIT_CONFIG.
	values := map[string]string{
		"file": filepath.Join(t.TempDir(), "config"),
		// git reads an empty value as the name of a file, so git
		// config fails with it.
		"empty value": "",
	}

	for name, value := range values { //nolint:paralleltest // t.Setenv
		t.Run(name, func(t *testing.T) {
			t.Setenv("GIT_CONFIG", value)
			t.Setenv("GIT_CONFIG_GLOBAL", "/custom/gitconfig")
			t.Setenv("GIT_CONFIG_COUNT", "1")

			envs := map[string][]string{
				"gitEnv":     gitEnv(),
				"commandEnv": (&handler{}).commandEnv(),
			}

			for source, env := range envs {
				assert.Empty(t, lastEnv(env, "GIT_CONFIG"), source)
				assert.Equal(t, "GIT_CONFIG_GLOBAL=/custom/gitconfig", lastEnv(env, "GIT_CONFIG_GLOBAL"), source)
				assert.Equal(t, "GIT_CONFIG_COUNT=1", lastEnv(env, "GIT_CONFIG_COUNT"), source)
			}
		})
	}
}

// git rev-parse --local-env-vars prints the variables that point git
// at one repository. gitEnv must remove each of them, whatever its
// value is. It must keep the two variables of the list that pass
// config to every git command.
//
// The list comes from the git that runs the tests. The test therefore
// fails when that git prints a variable that repositoryVariables does
// not hold.
func TestGitEnvRemovesRepositoryVariables(t *testing.T) { //nolint:paralleltest // t.Setenv
	listed, err := repositoryEnv(t.Context())
	require.NoError(t, err)
	require.Contains(t, listed, "GIT_DIR")

	kept := []string{"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT"}

	for _, name := range kept {
		assert.NotContains(t, repositoryVariables, name)
	}

	for _, name := range listed {
		if !slices.Contains(kept, name) {
			assert.Contains(t, repositoryVariables, name, "git lists the variable")
		}
	}

	// values maps the name of a case to the value of each variable.
	values := map[string]string{
		"path": filepath.Join(t.TempDir(), "repo", ".git"),
		// git reads an empty GIT_DIR as the path of a git directory,
		// and it finds no repository there.
		"empty value": "",
	}

	for name, value := range values { //nolint:paralleltest // t.Setenv
		t.Run(name, func(t *testing.T) {
			for _, name := range slices.Concat(repositoryVariables, kept) {
				t.Setenv(name, value)
			}

			envs := map[string][]string{
				"gitEnv":     gitEnv(),
				"commandEnv": (&handler{}).commandEnv(),
			}

			for source, env := range envs {
				for _, name := range repositoryVariables {
					assert.Empty(t, lastEnv(env, name), source)
				}

				for _, name := range kept {
					assert.Equal(t, name+"="+value, lastEnv(env, name), source)
				}
			}
		})
	}
}

// The environment of the server can set GIT_CONFIG. configValue must
// still read the config of the repository, which the git commands of
// the server read. It must not read the file that the variable names.
func TestConfigValueSkipsConfigFileVariable(t *testing.T) { //nolint:paralleltest // t.Setenv
	repo := initRepo(t, t.TempDir())

	setConfig(t, repo, "mcp-git.name", "value")
	setConfigFileEnv(t, "[mcp-git]\n\tname = other\n\textra = other\n")

	got, err := configValue(t.Context(), repo, "mcp-git.name")
	require.NoError(t, err)
	assert.Equal(t, "value", got)

	got, err = configValue(t.Context(), repo, "mcp-git.extra")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestRemoteOrDefault(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "origin", remoteOrDefault(""))
	assert.Equal(t, "upstream", remoteOrDefault("upstream"))
}

func TestResolveExistingPrefix(t *testing.T) {
	t.Parallel()

	// The temporary directory can lie behind a symbolic link of its
	// own, as it does on macOS. The expected paths therefore start
	// from its resolved path.
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	deep := filepath.Join(root, "deep")
	relative := filepath.Join(root, "relative")
	dangling := filepath.Join(root, "dangling")
	pending := filepath.Join(root, "pending")
	chain := filepath.Join(root, "chain")
	loop := filepath.Join(root, "loop")

	require.NoError(t, os.MkdirAll(filepath.Join(target, "existing"), 0o755))
	require.NoError(t, os.Symlink(target, link))
	require.NoError(t, os.Symlink(filepath.Join(target, "existing"), deep))
	require.NoError(t, os.Symlink(filepath.Join("target", "existing"), relative))
	require.NoError(t, os.Symlink(filepath.Join(root, "nowhere"), dangling))
	require.NoError(t, os.Symlink(filepath.Join(target, "new"), pending))
	require.NoError(t, os.Symlink(pending, chain))
	require.NoError(t, os.Symlink(loop, loop))

	sep := string(filepath.Separator)

	// filepath.Join cleans the path, so concat joins the elements of
	// a path that keeps a "..", a ".", or an empty element.
	concat := func(elems ...string) string {
		return strings.Join(elems, sep)
	}

	tests := map[string]struct {
		path string
		want string
	}{
		"root": {
			path: sep,
			want: sep,
		},
		"existing directory": {
			path: filepath.Join(target, "existing"),
			want: filepath.Join(target, "existing"),
		},
		"missing nested directory": {
			path: filepath.Join(target, "missing", "nested"),
			want: filepath.Join(target, "missing", "nested"),
		},
		"link": {
			path: link,
			want: target,
		},
		"missing directory behind a link": {
			path: filepath.Join(link, "missing"),
			want: filepath.Join(target, "missing"),
		},
		"missing directory after a dot and empty elements": {
			path: concat(root, "", "link", ".", "missing", ""),
			want: filepath.Join(target, "missing"),
		},
		"link without a target": {
			path: filepath.Join(dangling, "repo"),
			want: filepath.Join(root, "nowhere", "repo"),
		},
		"link to a link without a target": {
			path: filepath.Join(chain, "repo"),
			want: filepath.Join(target, "new", "repo"),
		},
		"link with a relative target": {
			path: filepath.Join(relative, "missing"),
			want: filepath.Join(target, "existing", "missing"),
		},
		"loop of links": {
			path: filepath.Join(loop, "repo"),
			want: filepath.Join(loop, "repo"),
		},
		"parent of the target of a link": {
			path: concat(deep, ".."),
			want: target,
		},
		"existing sibling of the target of a link": {
			path: concat(deep, "..", "existing"),
			want: filepath.Join(target, "existing"),
		},
		"missing sibling of the target of a link": {
			path: concat(deep, "..", "missing"),
			want: filepath.Join(target, "missing"),
		},
		"missing nested directory beside the target of a link": {
			path: concat(deep, "..", "missing", "nested"),
			want: filepath.Join(target, "missing", "nested"),
		},
		"missing sibling of the target of a link without a target": {
			path: concat(pending, "..", "missing"),
			want: filepath.Join(target, "missing"),
		},
		"missing sibling of the relative target of a link": {
			path: concat(relative, "..", "missing"),
			want: filepath.Join(target, "missing"),
		},
		"parent of a loop of links": {
			path: concat(loop, "..", "missing"),
			want: filepath.Join(root, "missing"),
		},
		"missing directory after two parents of the target of a link": {
			path: concat(deep, "..", "..", "missing"),
			want: filepath.Join(root, "missing"),
		},
		"parent of a missing directory": {
			path: concat(root, "missing", ".."),
			want: root,
		},
		"parent of a missing directory behind a link": {
			path: concat(link, "missing", ".."),
			want: target,
		},
		"link after the parent of a missing directory": {
			path: concat(root, "missing", "..", "link", "gone"),
			want: filepath.Join(target, "gone"),
		},
		"parent of the target of a link after the parent of a missing directory": {
			path: concat(deep, "missing", "..", "..", "gone"),
			want: filepath.Join(target, "gone"),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, resolveExistingPrefix(tt.path))
		})
	}
}
