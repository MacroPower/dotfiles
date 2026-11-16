package main

import (
	"io"
	"net"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, result.Content)

	tc, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])

	return tc.Text
}

func TestBuildCloneArgs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input CloneInput
		want  []string
	}{
		"minimal": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/b"},
			want:  []string{"clone", "-q", "--origin", "origin", "--", "https://github.com/a/b", "/tmp/b"},
		},
		"with depth": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/b", Depth: 1},
			want: []string{
				"clone", "-q", "--origin", "origin", "--depth", "1",
				"--", "https://github.com/a/b", "/tmp/b",
			},
		},
		"with branch": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/b", Branch: "main"},
			want: []string{
				"clone", "-q", "--origin", "origin", "--branch", "main",
				"--", "https://github.com/a/b", "/tmp/b",
			},
		},
		"with single branch": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/b", SingleBranch: true},
			want: []string{
				"clone", "-q", "--origin", "origin", "--single-branch",
				"--", "https://github.com/a/b", "/tmp/b",
			},
		},
		"all options": {
			input: CloneInput{
				URL: "https://github.com/a/b", Dest: "/tmp/b",
				Depth: 1, Branch: "dev", SingleBranch: true,
			},
			want: []string{
				"clone", "-q", "--origin", "origin", "--depth", "1", "--branch", "dev", "--single-branch",
				"--", "https://github.com/a/b", "/tmp/b",
			},
		},
		"with sparse bool": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/b", Sparse: true},
			want: []string{
				"clone", "-q", "--origin", "origin", "--sparse", "--filter=blob:none",
				"--", "https://github.com/a/b", "/tmp/b",
			},
		},
		"with sparse paths": {
			input: CloneInput{
				URL: "https://github.com/a/b", Dest: "/tmp/b",
				SparsePaths: []string{"src", "docs"},
			},
			want: []string{
				"clone", "-q", "--origin", "origin", "--sparse", "--filter=blob:none",
				"--", "https://github.com/a/b", "/tmp/b",
			},
		},
		"sparse with depth": {
			input: CloneInput{
				URL: "https://github.com/a/b", Dest: "/tmp/b",
				Sparse: true, Depth: 1,
			},
			want: []string{
				"clone", "-q", "--origin", "origin", "--depth", "1", "--sparse",
				"--", "https://github.com/a/b", "/tmp/b",
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := buildCloneArgs(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAcquireLock(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "repo")
	lockPath := dest + ".lock"

	cleanup, err := acquireLock(dest)
	require.NoError(t, err)

	_, statErr := os.Stat(lockPath)
	require.NoError(t, statErr, "lock file should exist")

	cleanup()

	_, statErr = os.Stat(lockPath)
	require.ErrorIs(t, statErr, os.ErrNotExist, "lock file should be removed after cleanup")
}

func TestCheckDest(t *testing.T) {
	t.Parallel()

	h := &handler{
		allowDirs: []string{"/tmp/git", "/private/tmp/git"},
	}

	tests := map[string]struct {
		dest string
		err  error
	}{
		"allowed": {
			dest: "/tmp/git/owner/repo",
		},
		"allowed private": {
			dest: "/private/tmp/git/owner/repo",
		},
		"allowed exact": {
			dest: "/tmp/git",
		},
		"allowed first name starts with two dots": {
			dest: "/tmp/git/..owner/repo",
		},
		"allowed first name of three dots": {
			dest: "/tmp/git/...",
		},
		"allowed later name starts with two dots": {
			dest: "/tmp/git/owner/..repo",
		},
		"denied": {
			dest: "/home/user/repo",
			err:  ErrDeniedDest,
		},
		"denied traversal": {
			dest: "/tmp/git/../../etc/passwd",
			err:  ErrDeniedDest,
		},
		"denied parent": {
			dest: "/tmp/git/..",
			err:  ErrDeniedDest,
		},
		"denied sibling": {
			dest: "/tmp/git/../..owner/repo",
			err:  ErrDeniedDest,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := h.checkDest(tt.dest)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckDestNoRestrictions(t *testing.T) {
	t.Parallel()

	h := &handler{}

	require.NoError(t, h.checkDest("/anywhere/at/all"))
}

func TestHandleValidation(t *testing.T) {
	t.Parallel()

	h := &handler{}

	tests := map[string]struct {
		input CloneInput
		want  string
	}{
		"missing url": {
			input: CloneInput{Dest: "/tmp/x"},
			want:  ErrMissingURL.Error(),
		},
		"missing dest": {
			input: CloneInput{URL: "https://github.com/a/b"},
			want:  ErrMissingDest.Error(),
		},
		"denied ref": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/x", Ref: "--upload-pack=evil"},
			want:  ErrDeniedRef.Error(),
		},
		"ref and branch conflict": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/x", Branch: "main", Ref: "v1"},
			want:  ErrRefConflict.Error(),
		},
		"sparse path with dash": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/x", SparsePaths: []string{"-evil"}},
			want:  "sparse path is invalid: \"-evil\" starts with '-'",
		},
		"sparse path with dotdot": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/x", SparsePaths: []string{"../etc"}},
			want:  "sparse path is invalid: \"../etc\" contains '..'",
		},
		"sparse path absolute": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/x", SparsePaths: []string{"/etc/passwd"}},
			want:  "sparse path is invalid: \"/etc/passwd\" is absolute",
		},
		"sparse path empty string": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/x", SparsePaths: []string{""}},
			want:  "sparse path is invalid: empty path",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, _, err := h.handleClone(t.Context(), nil, tt.input)
			require.NoError(t, err)
			require.True(t, result.IsError)
			assert.Equal(t, tt.want, resultText(t, result))
		})
	}
}

// initBareRepo creates a bare git repository with one commit for testing.
func initBareRepo(t *testing.T) string {
	t.Helper()

	bare := filepath.Join(t.TempDir(), "bare.git")
	work := filepath.Join(t.TempDir(), "work")

	for _, args := range [][]string{
		{"git", "init", "--bare", bare},
		{"git", "clone", bare, work},
		{"git", "-C", work, "config", "user.email", "test@test.com"},
		{"git", "-C", work, "config", "user.name", "Test"},
	} {
		cmd := exec.CommandContext(t.Context(), args[0], args[1:]...) //nolint:gosec // test helper
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run(), "setup command failed: %v", args)
	}

	require.NoError(t, os.WriteFile(filepath.Join(work, "README"), []byte("hello"), 0o644))

	for _, args := range [][]string{
		{"git", "-C", work, "add", "."},
		{"git", "-C", work, "commit", "-m", "init"},
		{"git", "-C", work, "push"},
	} {
		cmd := exec.CommandContext(t.Context(), args[0], args[1:]...) //nolint:gosec // test helper
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run(), "setup command failed: %v", args)
	}

	return bare
}

func TestCredentialArgs(t *testing.T) {
	t.Parallel()

	wantArgs := []string{
		"-c", "credential.https://github.com.helper=",
		"-c", `credential.https://github.com.helper=!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f`,
	}

	tests := map[string]struct {
		token string
		want  []string
	}{
		"token": {
			token: "ghp_test123",
			want:  wantArgs,
		},
		"no token": {},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := (&handler{token: tt.token}).credentialArgs()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckURL(t *testing.T) {
	t.Parallel()

	h := &handler{}

	tests := map[string]struct {
		url string
		err error
	}{
		"https": {
			url: "https://github.com/a/b",
		},
		"denied http": {
			url: "http://github.com/a/b",
			err: ErrDeniedURL,
		},
		"ssh": {
			url: "ssh://git@github.com/a/b",
		},
		"denied git": {
			url: "git://github.com/a/b",
			err: ErrDeniedURL,
		},
		"scp-style": {
			url: "git@github.com:a/b",
		},
		"denied ext": {
			url: "ext::sh -c 'echo pwned'%s",
			err: ErrDeniedURL,
		},
		"denied file": {
			url: "file:///etc/passwd",
			err: ErrDeniedURL,
		},
		"denied local absolute": {
			url: "/tmp/some/repo",
			err: ErrDeniedURL,
		},
		"denied local relative": {
			url: "../some/repo",
			err: ErrDeniedURL,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := h.checkURL(tt.url)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckURLAllowInsecure(t *testing.T) {
	t.Parallel()

	h := &handler{allowInsecure: true}

	require.NoError(t, h.checkURL("http://github.com/a/b"))
	require.NoError(t, h.checkURL("git://github.com/a/b"))
}

func TestCheckURLAllowFile(t *testing.T) {
	t.Parallel()

	h := &handler{allowFileURLs: true}

	require.NoError(t, h.checkURL("/tmp/some/repo"))
	require.NoError(t, h.checkURL("file:///tmp/some/repo"))
}

func TestCheckDestSymlink(t *testing.T) {
	t.Parallel()

	allowDir := t.TempDir()
	outside := t.TempDir()

	// Create a symlink inside the allowed dir pointing outside.
	link := filepath.Join(allowDir, "escape")
	require.NoError(t, os.Symlink(outside, link))

	h := &handler{allowDirs: mustResolveAllowDirs(t, allowDir)}

	// A path through the symlink should be denied.
	err := h.checkDest(filepath.Join(link, "repo"))
	require.ErrorIs(t, err, ErrDeniedDest)

	// A normal path under the allowed dir should still work.
	require.NoError(t, h.checkDest(filepath.Join(allowDir, "repo")))
}

func TestCheckSparsePaths(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		paths []string
		err   error
	}{
		"nil":            {},
		"empty slice":    {paths: []string{}},
		"valid single":   {paths: []string{"src"}},
		"valid nested":   {paths: []string{"src/pkg/foo"}},
		"valid multiple": {paths: []string{"src", "docs"}},
		"dash prefix": {
			paths: []string{"-x"},
			err:   ErrDeniedSparsePath,
		},
		"dotdot": {
			paths: []string{"a/../b"},
			err:   ErrDeniedSparsePath,
		},
		"absolute": {
			paths: []string{"/x"},
			err:   ErrDeniedSparsePath,
		},
		"empty string": {
			paths: []string{""},
			err:   ErrDeniedSparsePath,
		},
		"valid then invalid": {
			paths: []string{"src", ""},
			err:   ErrDeniedSparsePath,
		},
		"null byte": {
			paths: []string{"src\x00evil"},
			err:   ErrDeniedSparsePath,
		},
		"newline": {
			paths: []string{"src\nevil"},
			err:   ErrDeniedSparsePath,
		},
		"carriage return": {
			paths: []string{"src\revil"},
			err:   ErrDeniedSparsePath,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := checkSparsePaths(tt.paths)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDeniedBranch(t *testing.T) {
	t.Parallel()

	h := &handler{allowFileURLs: true}

	result, _, err := h.handleClone(t.Context(), nil, CloneInput{
		URL:    "/tmp/repo",
		Dest:   filepath.Join(t.TempDir(), "x"),
		Branch: "--upload-pack=evil",
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "must not start with")
}

func TestDeniedDest(t *testing.T) {
	t.Parallel()

	h := &handler{allowFileURLs: true}

	result, _, err := h.handleClone(t.Context(), nil, CloneInput{
		URL:  "/tmp/repo",
		Dest: "--upload-pack=evil",
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "must not start with")
}

func TestHandleClone(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	h := &handler{allowFileURLs: true}

	dest := filepath.Join(t.TempDir(), "cloned")

	// First call: clone.
	result, _, err := h.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Cloned")

	_, statErr := os.Stat(filepath.Join(dest, ".git"))
	require.NoError(t, statErr, ".git directory should exist after clone")

	// Second call: pull.
	result, _, err = h.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Pulled")
}

// Each case names one destination with another spelling, and
// [filepath.Abs] returns the plain path for every spelling.
func TestHandleCloneDestSpelling(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		suffix string
	}{
		"plain":            {},
		"trailing slash":   {suffix: "/"},
		"repeated slashes": {suffix: "//"},
		"dot segment":      {suffix: "/."},
		"dot dot segment":  {suffix: "/sub/.."},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bare := initBareRepo(t)
			h := &handler{allowFileURLs: true}

			parent := t.TempDir()
			dest := filepath.Join(parent, "cloned")
			lockPath := dest + ".lock"
			input := CloneInput{URL: bare, Dest: dest + tc.suffix}

			// Two calls exclude each other only when they lock the
			// same file. A directory at the path of the lock file
			// makes the lock fail, and the error names the path that
			// the call locks.
			require.NoError(t, os.Mkdir(lockPath, 0o755))

			_, _, err := h.handleClone(t.Context(), nil, input)
			require.ErrorContains(t, err, "creating lock file: open "+lockPath+":")

			require.NoError(t, os.Remove(lockPath))

			result, _, err := h.handleClone(t.Context(), nil, input)
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))
			assert.Equal(t, "Cloned "+bare+" into "+dest, resultText(t, result))

			// The call leaves no lock file beside the clone or
			// inside it.
			assert.Equal(t, []string{"cloned"}, dirEntries(t, parent))
			assert.Equal(t, []string{".git", "README"}, dirEntries(t, dest))

			result, _, err = h.handleClone(t.Context(), nil, input)
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))
			assert.Contains(t, resultText(t, result), "Pulled latest changes in "+dest)

			assert.Equal(t, []string{"cloned"}, dirEntries(t, parent))
			assert.Equal(t, []string{".git", "README"}, dirEntries(t, dest))
		})
	}
}

// A directory name can start with two dots. Such a name is not a ".."
// segment, so a dest under that directory stays under the allowed
// directory. The second call pulls, and the pull checks the path of
// the repository as git_fetch, git_pull, and git_push check it.
func TestHandleCloneDestNameStartsWithTwoDots(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	allowDir := t.TempDir()

	h := &handler{allowFileURLs: true, allowDirs: mustResolveAllowDirs(t, allowDir)}

	dest := filepath.Join(allowDir, "..owner", "repo")
	input := CloneInput{URL: bare, Dest: dest}

	result, _, err := h.handleClone(t.Context(), nil, input)
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Equal(t, "Cloned "+bare+" into "+dest, resultText(t, result))
	assert.Equal(t, []string{".git", "README"}, dirEntries(t, dest))

	result, _, err = h.handleClone(t.Context(), nil, input)
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Pulled latest changes in "+dest)
}

// [handler.checkDest] removes a ".." segment and the segment before
// it. The OS resolves a symbolic link in that earlier segment first,
// so the uncleaned path names a directory beside the target of the
// link. The clone must take the path that checkDest allows.
func TestHandleCloneDotDotAfterSymlink(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)

	root := t.TempDir()
	allowDir := filepath.Join(root, "allowed")
	outside := filepath.Join(root, "outside")

	require.NoError(t, os.Mkdir(allowDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "target"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(outside, "target"), filepath.Join(allowDir, "link")))

	h := &handler{allowFileURLs: true, allowDirs: mustResolveAllowDirs(t, allowDir)}

	result, _, err := h.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: allowDir + "/link/../cloned",
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	_, statErr := os.Stat(filepath.Join(allowDir, "cloned", ".git"))
	require.NoError(t, statErr, "the clone must take the path that checkDest allows")

	assert.Equal(t, []string{"target"}, dirEntries(t, outside))
}

// [handler.checkDest] reads a relative dest against the working
// directory that [os.Getwd] reports. Getwd returns $PWD when $PWD
// names the working directory, and $PWD can pass through a symbolic
// link. The OS resolves a relative path against the physical working
// directory instead. The clone must take the path that checkDest
// allows.
func TestHandleCloneRelativeDestThroughSymlink(t *testing.T) { //nolint:paralleltest // t.Chdir, t.Setenv
	bare := initBareRepo(t)

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	allowDir := filepath.Join(root, "allowed")
	outside := filepath.Join(root, "outside")
	target := filepath.Join(outside, "target")
	link := filepath.Join(allowDir, "link")

	require.NoError(t, os.Mkdir(allowDir, 0o755))
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.Symlink(target, link))

	t.Chdir(target)
	t.Setenv("PWD", link)

	wd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, link, wd, "Getwd must report the working directory through the link")

	h := &handler{allowFileURLs: true, allowDirs: mustResolveAllowDirs(t, allowDir)}

	result, _, err := h.handleClone(t.Context(), nil, CloneInput{URL: bare, Dest: "../cloned"})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	dest := filepath.Join(allowDir, "cloned")
	assert.Equal(t, "Cloned "+bare+" into "+dest, resultText(t, result))
	assert.Equal(t, []string{".git", "README"}, dirEntries(t, dest))

	// The call leaves no lock file in either directory.
	assert.Equal(t, []string{"cloned", "link"}, dirEntries(t, allowDir))
	assert.Equal(t, []string{"target"}, dirEntries(t, outside))
	assert.Empty(t, dirEntries(t, target))
}

// [filepath.Abs] reads the working directory for a relative dest, and
// [os.Getwd] fails when someone deleted that directory. git_clone then
// returns the error of Getwd.
func TestHandleCloneDestWithoutWorkingDirectory(t *testing.T) { //nolint:paralleltest // t.Chdir
	if runtime.GOOS != "linux" {
		t.Skip("the getcwd of Linux fails with ENOENT for a deleted working directory")
	}

	bare := initBareRepo(t)

	dir := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(dir, 0o755))

	t.Chdir(dir)
	require.NoError(t, os.Remove(dir))

	h := &handler{allowFileURLs: true}

	result, _, err := h.handleClone(t.Context(), nil, CloneInput{URL: bare, Dest: "cloned"})
	require.ErrorContains(t, err, "resolving dest path: ")
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Nil(t, result)
}

func TestHandleCloneWithToken(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	h := &handler{allowFileURLs: true, token: "ghp_unused"}

	dest := filepath.Join(t.TempDir(), "cloned")

	// Clone with token set. git applies the credential options only to an
	// https connection to github.com, so the clone of a file URL asks for
	// no credential.
	result, _, err := h.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Cloned")

	// Pull with token set.
	result, _, err = h.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Pulled")
}

func TestHandleCloneRef(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)

	// Create a tag in the bare repo via a temporary clone.
	tmp := initWorkClone(t, bare)

	runGitCmds(t, [][]string{
		{"git", "-C", tmp, "tag", "v1.0.0"},
		{"git", "-C", tmp, "push", "--tags"},
	})

	h := &handler{allowFileURLs: true}
	dest := filepath.Join(t.TempDir(), "cloned")

	result, _, err := h.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
		Ref:  "v1.0.0",
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Cloned")

	// Verify HEAD is at the tagged commit.
	cmd := exec.CommandContext(t.Context(), "git", "-C", dest, "describe", "--tags", "--exact-match", "HEAD")
	out, descErr := cmd.Output()
	require.NoError(t, descErr)
	assert.Equal(t, "v1.0.0", strings.TrimSpace(string(out)))
}

func TestHandleCloneSparse(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)

	// Add files in src/ and docs/ directories.
	tmp := initWorkClone(t, bare)

	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "src", "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "docs", "README"), []byte("docs"), 0o644))

	runGitCmds(t, [][]string{
		{"git", "-C", tmp, "add", "."},
		{"git", "-C", tmp, "commit", "-m", "add dirs"},
		{"git", "-C", tmp, "push"},
	})

	h := &handler{allowFileURLs: true}
	dest := filepath.Join(t.TempDir(), "cloned")

	// Clone with sparse checkout.
	result, _, err := h.handleClone(t.Context(), nil, CloneInput{
		URL:         bare,
		Dest:        dest,
		SparsePaths: []string{"src"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "sparse: src")

	// src/ should exist, docs/ should not.
	_, statErr := os.Stat(filepath.Join(dest, "src", "main.go"))
	require.NoError(t, statErr, "src/main.go should exist")

	_, statErr = os.Stat(filepath.Join(dest, "docs", "README"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "docs/README should not exist in sparse checkout")

	// Pull should still work.
	result, _, err = h.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Pulled")
}

// The caller can write the git config of a new clone before git_clone
// sets its sparse checkout. core.worktree in that config names a
// directory outside the destination.
func TestSparseCheckoutArgsKeepWorkTree(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	work := initWorkClone(t, bare)

	require.NoError(t, os.MkdirAll(filepath.Join(work, "docs"), 0o755))
	commitFile(t, work, "docs/README", "docs", "add docs")
	runGitCmds(t, [][]string{{"git", "-C", work, "push"}})

	h := &handler{allowFileURLs: true}
	dest := filepath.Join(t.TempDir(), "cloned")
	outside := t.TempDir()
	opts := slices.Concat(guardOptions(), hookOptions(nil))
	paths := []string{"docs"}

	out, err := h.runGit(t.Context(), slices.Concat(opts, buildCloneArgs(CloneInput{
		URL:         bare,
		Dest:        dest,
		SparsePaths: paths,
	}))...)
	require.NoError(t, err, out)

	runGitCmds(t, [][]string{{"git", "-C", dest, "config", "core.worktree", outside}})

	out, err = h.runGit(t.Context(), sparseCheckoutArgs(opts, dest, paths)...)
	require.NoError(t, err, out)

	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries)

	assert.FileExists(t, filepath.Join(dest, "docs", "README"))
}

func TestHandleOriginMismatch(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	h := &handler{allowFileURLs: true}

	dest := filepath.Join(t.TempDir(), "cloned")

	// Clone the repo.
	result, _, err := h.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Try to pull with a different URL.
	result, _, err = h.handleClone(t.Context(), nil, CloneInput{
		URL:  "/tmp/different-repo",
		Dest: dest,
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "origin URL mismatch")
}

// A destination can hold a repository that has no origin remote, such
// as the repository that git init creates. The error must name the
// destination and the missing remote, and git_clone must leave the
// destination in place.
func TestHandleCloneDestWithoutOrigin(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		setup func(t *testing.T, bare, dest string)
	}{
		"repository of git init": {
			setup: func(t *testing.T, _, dest string) {
				t.Helper()

				runGitCmds(t, [][]string{{"git", "init", "-q", dest}})
			},
		},
		"clone that lost its origin": {
			setup: func(t *testing.T, bare, dest string) {
				t.Helper()

				runGitCmds(t, [][]string{
					{"git", "clone", "-q", bare, dest},
					{"git", "-C", dest, "remote", "remove", "origin"},
				})
			},
		},
		"repository with another remote": {
			setup: func(t *testing.T, bare, dest string) {
				t.Helper()

				runGitCmds(t, [][]string{
					{"git", "init", "-q", dest},
					{"git", "-C", dest, "remote", "add", "upstream", bare},
				})
			},
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bare := initBareRepo(t)
			dest := filepath.Join(t.TempDir(), "cloned")

			tc.setup(t, bare, dest)

			result, _, err := (&handler{allowFileURLs: true}).handleClone(t.Context(), nil, CloneInput{
				URL:  bare,
				Dest: dest,
			})
			require.NoError(t, err)
			require.True(t, result.IsError)

			text := resultText(t, result)
			assert.Contains(t, text, ErrNoOrigin.Error()+": "+dest)
			assert.NotContains(t, text, ErrOriginMismatch.Error())
			assert.NotContains(t, text, "exit status")

			assert.DirExists(t, filepath.Join(dest, ".git"))

			asides, err := filepath.Glob(dest + ".corrupt-*")
			require.NoError(t, err)
			assert.Empty(t, asides)
		})
	}
}

// aliasPrefix is the URL prefix that the rewrite of [rewriteConfig]
// replaces.
const aliasPrefix = "https://example.invalid/"

// aliasURL returns the URL under aliasPrefix that git rewrites to bare
// when the git config holds the rewrite of [rewriteConfig].
func aliasURL(bare string) string {
	return aliasPrefix + filepath.Base(bare)
}

// rewriteConfig returns git config text with a url.<base>.insteadOf
// rewrite. The rewrite replaces aliasPrefix in a URL with the
// directory that holds bare.
func rewriteConfig(bare string) string {
	return "[url \"" + filepath.Dir(bare) + "/\"]\n\tinsteadOf = " + aliasPrefix + "\n"
}

// addRewrite adds the rewrite of [rewriteConfig] to the git config of
// the repository at dest.
func addRewrite(t *testing.T, bare, dest string) {
	t.Helper()

	appendGitConfig(t, dest, rewriteConfig(bare))
}

// aliasOrigin adds the rewrite of [rewriteConfig] to the git config of
// the repository at dest. It also stores the URL of [aliasURL] as the
// URL of origin. git clone stores that URL when the caller clones the
// alias.
func aliasOrigin(t *testing.T, bare, dest string) {
	t.Helper()

	addRewrite(t, bare, dest)
	runGitCmds(t, [][]string{{"git", "-C", dest, "remote", "set-url", "origin", aliasURL(bare)}})
}

// setGlobalRewrite points the global git config at a file that holds
// the rewrite of [rewriteConfig]. It sets an environment variable, so
// the test must not run in parallel.
func setGlobalRewrite(t *testing.T, bare string) {
	t.Helper()

	setGlobalConfig(t, rewriteConfig(bare))
}

// setGlobalConfig points the global git config at a file that holds
// text. It sets an environment variable, so the test must not run in
// parallel.
func setGlobalConfig(t *testing.T, text string) {
	t.Helper()

	global := filepath.Join(t.TempDir(), "gitconfig")

	require.NoError(t, os.WriteFile(global, []byte(text), 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", global)
}

func TestCheckOrigin(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		setup func(t *testing.T, bare, dest string)
		url   func(bare string) string
		err   error
	}{
		"origin has the URL": {
			url: func(bare string) string { return bare },
		},
		"URL without the .git suffix": {
			url: func(bare string) string { return strings.TrimSuffix(bare, ".git") },
		},
		"origin has another URL": {
			url: func(bare string) string { return bare + "-other" },
			err: ErrOriginMismatch,
		},
		// git clone stores the url input in origin, so a second
		// git_clone call finds the alias in both places.
		"alias in the URL and in the origin": {
			setup: aliasOrigin,
			url:   aliasURL,
		},
		"alias in the URL": {
			setup: addRewrite,
			url:   aliasURL,
		},
		"alias in the origin": {
			setup: aliasOrigin,
			url:   func(bare string) string { return bare },
		},
		"alias of another URL": {
			setup: aliasOrigin,
			url:   func(bare string) string { return aliasURL(bare + "-other") },
			err:   ErrOriginMismatch,
		},
		"no origin": {
			setup: func(t *testing.T, _, dest string) {
				t.Helper()

				runGitCmds(t, [][]string{{"git", "-C", dest, "remote", "remove", "origin"}})
			},
			url: func(bare string) string { return bare },
			err: ErrNoOrigin,
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bare := initBareRepo(t)
			dest := initWorkClone(t, bare)

			if tc.setup != nil {
				tc.setup(t, bare, dest)
			}

			err := (&handler{}).checkOrigin(t.Context(), tc.url(bare), dest)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)

				return
			}

			require.NoError(t, err)
		})
	}
}

// The mismatch error names the URL that git reads for origin, and it
// names the url input without the rewrites. It names each URL without
// a trailing ".git" suffix.
func TestCheckOriginMismatchNamesURLInput(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	dest := initWorkClone(t, bare)

	aliasOrigin(t, bare, dest)

	err := (&handler{}).checkOrigin(t.Context(), aliasPrefix+"other.git", dest)
	require.ErrorIs(t, err, ErrOriginMismatch)

	want := "origin URL mismatch: got " + strings.TrimSuffix(bare, ".git") + ", want " + aliasPrefix + "other"

	assert.EqualError(t, err, want)
}

// credentialPrefix is aliasPrefix with a user and a password.
const credentialPrefix = "https://user:s3cret@example.invalid/"

// credentialRewrite is git config text with a url.<base>.insteadOf
// rewrite. The rewrite adds the user and the password of
// credentialPrefix to each URL under aliasPrefix.
const credentialRewrite = "[url \"" + credentialPrefix + "\"]\n\tinsteadOf = " + aliasPrefix + "\n"

// initOriginRepo creates a repository whose origin remote has the URL
// origin, and returns its path. It adds config to the git config of
// the repository.
func initOriginRepo(t *testing.T, origin, config string) string {
	t.Helper()

	dest := filepath.Join(t.TempDir(), "cloned")

	runGitCmds(t, [][]string{
		{"git", "init", "-q", dest},
		{"git", "-C", dest, "remote", "add", "origin", origin},
	})

	appendGitConfig(t, dest, config)

	return dest
}

// The tool result holds the mismatch error, so the error must name
// each URL without the credential that the URL embeds.
func TestCheckOriginMismatchOmitsCredential(t *testing.T) {
	t.Parallel()

	const differ = ": the URLs differ only in a user or a password that this message leaves out"

	tcs := map[string]struct {
		config string
		origin string
		url    string
		want   string
	}{
		"rewrite adds a credential to the origin": {
			config: credentialRewrite,
			origin: aliasPrefix + "a.git",
			url:    aliasPrefix + "b.git",
			want:   "origin URL mismatch: got https://example.invalid/a, want https://example.invalid/b",
		},
		"origin stores a credential": {
			origin: credentialPrefix + "a.git",
			url:    aliasPrefix + "b.git",
			want:   "origin URL mismatch: got https://example.invalid/a, want https://example.invalid/b",
		},
		"url input holds a credential": {
			origin: aliasPrefix + "a.git",
			url:    credentialPrefix + "b.git",
			want:   "origin URL mismatch: got https://example.invalid/a, want https://example.invalid/b",
		},
		"origin stores a credential that the url input lacks": {
			origin: credentialPrefix + "a.git",
			url:    aliasPrefix + "a.git",
			want:   "origin URL mismatch: got https://example.invalid/a, want https://example.invalid/a" + differ,
		},
		"rewrite adds another credential than the one of the url input": {
			config: credentialRewrite,
			origin: aliasPrefix + "a.git",
			url:    "https://other@example.invalid/a.git",
			want:   "origin URL mismatch: got https://example.invalid/a, want https://example.invalid/a" + differ,
		},
		"ssh URLs differ only in the password": {
			origin: "ssh://git:s3cret@example.invalid/a.git",
			url:    "ssh://git@example.invalid/a.git",
			want:   "origin URL mismatch: got ssh://git@example.invalid/a, want ssh://git@example.invalid/a" + differ,
		},
		// The user of an ssh URL is a login name, and the caller needs
		// it to see how the two URLs differ.
		"ssh URLs differ only in the user": {
			origin: "ssh://git@example.invalid/a.git",
			url:    "ssh://other@example.invalid/a.git",
			want:   "origin URL mismatch: got ssh://git@example.invalid/a, want ssh://other@example.invalid/a",
		},
		"SCP-style URLs": {
			origin: "git@example.invalid:org/a.git",
			url:    "git@example.invalid:org/b.git",
			want:   "origin URL mismatch: got git@example.invalid:org/a, want git@example.invalid:org/b",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dest := initOriginRepo(t, tc.origin, tc.config)

			err := (&handler{}).checkOrigin(t.Context(), tc.url, dest)
			require.ErrorIs(t, err, ErrOriginMismatch)
			assert.EqualError(t, err, tc.want)
		})
	}
}

// The global git config can hold a rewrite whose base embeds a
// credential. git applies the rewrite to the URL of origin, and the
// result of git_clone must not hold that credential.
func TestHandleCloneMismatchOmitsCredential(t *testing.T) { //nolint:paralleltest // t.Setenv
	setGlobalConfig(t, credentialRewrite)

	dest := initOriginRepo(t, aliasPrefix+"a.git", "")

	result, _, err := (&handler{}).handleClone(t.Context(), nil, CloneInput{
		URL:  aliasPrefix + "b.git",
		Dest: dest,
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Equal(t,
		"origin URL mismatch: got https://example.invalid/a, want https://example.invalid/b",
		resultText(t, result),
	)
}

func TestStripCredential(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		url  string
		want string
	}{
		"https with a user and a password": {
			url:  "https://user:s3cret@example.invalid/org/repo",
			want: "https://example.invalid/org/repo",
		},
		"https with a token as the user": {
			url:  "https://s3cret@example.invalid/org/repo",
			want: "https://example.invalid/org/repo",
		},
		"https with a port": {
			url:  "https://user:s3cret@example.invalid:8443/org/repo",
			want: "https://example.invalid:8443/org/repo",
		},
		"https with an at sign in the password": {
			url:  "https://user:s3@cret@example.invalid/org/repo",
			want: "https://example.invalid/org/repo",
		},
		"https with a user and an at sign in the path": {
			url:  "https://user:s3cret@example.invalid/org/repo@v2",
			want: "https://example.invalid/org/repo@v2",
		},
		"https without a path": {
			url:  "https://user:s3cret@example.invalid",
			want: "https://example.invalid",
		},
		"https without a user": {
			url:  "https://example.invalid/org/repo",
			want: "https://example.invalid/org/repo",
		},
		"https with an at sign in the path": {
			url:  "https://example.invalid/org/repo@v2",
			want: "https://example.invalid/org/repo@v2",
		},
		"https with an at sign in the query": {
			url:  "https://example.invalid?ref=a@b",
			want: "https://example.invalid?ref=a@b",
		},
		"https with an at sign in the fragment": {
			url:  "https://example.invalid#a@b",
			want: "https://example.invalid#a@b",
		},
		"http with a user and a password": {
			url:  "http://user:s3cret@example.invalid/org/repo",
			want: "http://example.invalid/org/repo",
		},
		"ssh with a user": {
			url:  "ssh://git@example.invalid:2222/org/repo",
			want: "ssh://git@example.invalid:2222/org/repo",
		},
		"ssh with a user and a password": {
			url:  "ssh://git:s3cret@example.invalid:2222/org/repo",
			want: "ssh://git@example.invalid:2222/org/repo",
		},
		"ssh with an at sign in the password": {
			url:  "ssh://git:s3@cret@example.invalid/org/repo",
			want: "ssh://git@example.invalid/org/repo",
		},
		"ssh without a user": {
			url:  "ssh://example.invalid/org/repo",
			want: "ssh://example.invalid/org/repo",
		},
		"file with an at sign in the path": {
			url:  "file:///srv/user@host/repo",
			want: "file:///srv/user@host/repo",
		},
		"SCP-style with a user": {
			url:  "git@example.invalid:org/repo",
			want: "git@example.invalid:org/repo",
		},
		"SCP-style with a user and a password": {
			url:  "git:s3cret@example.invalid:org/repo",
			want: "git@example.invalid:org/repo",
		},
		"SCP-style with a user and an IPv6 host": {
			url:  "git@[::1]:org/repo",
			want: "git@[::1]:org/repo",
		},
		"SCP-style with a user and an at sign in the path": {
			url:  "git@example.invalid:org/repo@v2",
			want: "git@example.invalid:org/repo@v2",
		},
		"SCP-style without a user": {
			url:  "example.invalid:org/repo",
			want: "example.invalid:org/repo",
		},
		"SCP-style with an at sign in the path": {
			url:  "example.invalid:org/repo@v2",
			want: "example.invalid:org/repo@v2",
		},
		"local path": {
			url:  "/srv/git/repo",
			want: "/srv/git/repo",
		},
		"local path with a colon on each side of an at sign": {
			url:  "/srv/a:b@host:git/repo",
			want: "/srv/a:b@host:git/repo",
		},
		"relative path with an at sign": {
			url:  "user@host/repo",
			want: "user@host/repo",
		},
		"empty": {},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, stripCredential(tc.url))
		})
	}
}

// git rejects an empty URL when checkOrigin applies the rewrites to
// it. checkOrigin compared no URL then, so the error must not report
// another URL.
func TestCheckOriginUnresolvedURL(t *testing.T) {
	t.Parallel()

	dest := initWorkClone(t, initBareRepo(t))

	err := (&handler{}).checkOrigin(t.Context(), "", dest)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrOriginMismatch)
	assert.ErrorContains(t, err, "applying the URL rewrites of the git config of "+dest)
}

// git can open a repository whose config gives the URL of origin no
// value, but git remote get-url rejects that config. The remote
// exists, so the error must not report a missing remote or another
// URL.
func TestCheckOriginUnreadableRemote(t *testing.T) {
	t.Parallel()

	dest := initWorkClone(t, initBareRepo(t))

	runGitCmds(t, [][]string{{"git", "-C", dest, "remote", "remove", "origin"}})
	appendGitConfig(t, dest, "[remote \"origin\"]\n\turl\n")

	err := (&handler{}).checkOrigin(t.Context(), "https://example.invalid/r.git", dest)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoOrigin)
	require.NotErrorIs(t, err, ErrOriginMismatch)
	assert.ErrorContains(t, err, "reading the origin remote of "+dest)
}

// The git config can rewrite the URL of a clone to a transport that
// the server denies, after checkURL accepts the URL. git itself must
// refuse the transport.
func TestHandleCloneRefusesRewrittenURL(t *testing.T) { //nolint:paralleltest // t.Setenv
	bare := initBareRepo(t)

	setGlobalRewrite(t, bare)

	input := CloneInput{
		URL:  aliasURL(bare),
		Dest: filepath.Join(t.TempDir(), "cloned"),
	}

	result, _, err := (&handler{}).handleClone(t.Context(), nil, input)
	require.NoError(t, err)
	require.True(t, result.IsError, "git must refuse the file transport")
	assert.Contains(t, resultText(t, result), "transport 'file' not allowed")

	// The rewrite reaches the bare repository, so the refusal is what
	// kept git from cloning it.
	result, _, err = (&handler{allowFileURLs: true}).handleClone(t.Context(), nil, input)
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
}

// git clone stores the url input as the URL of origin. git applies the
// rewrite of the global config each time it reads that URL. A second
// git_clone call with the same inputs must still pull.
func TestHandleClonePullsRewrittenURL(t *testing.T) { //nolint:paralleltest // t.Setenv
	bare := initBareRepo(t)

	setGlobalRewrite(t, bare)

	h := &handler{allowFileURLs: true}

	input := CloneInput{
		URL:  aliasURL(bare),
		Dest: filepath.Join(t.TempDir(), "cloned"),
	}

	result, _, err := h.handleClone(t.Context(), nil, input)
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Cloned")

	assert.Equal(t, input.URL, gitOut(t, "-C", input.Dest, "config", "--get", "remote.origin.url"),
		"git clone must store the URL without the rewrite")

	result, _, err = h.handleClone(t.Context(), nil, input)
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Pulled")
}

// git clone names the remote of a new clone after
// clone.defaultRemoteName of the git config. git_clone pulls from
// origin, so the remote of its clone must have that name whatever the
// key says. A second git_clone call with the same inputs must then
// pull. A sparse clone without a depth leaves out each blob that its
// checkout does not hold. git fetches each blob that the checkout
// needs from the remote of the clone, both when it sets the sparse
// paths and when the second call pulls.
func TestHandleCloneIgnoresDefaultRemoteName(t *testing.T) { //nolint:paralleltest // t.Setenv
	tcs := map[string]struct {
		sparsePaths []string
		// partial reports whether the clone lacks the blob of
		// other/x, a file outside the sparse paths.
		partial bool
	}{
		"full clone":   {},
		"sparse clone": {sparsePaths: []string{"docs"}, partial: true},
	}

	for name, tc := range tcs { //nolint:paralleltest // setGlobalConfig calls t.Setenv.
		t.Run(name, func(t *testing.T) {
			bare := initBareRepo(t)
			runGitCmds(t, [][]string{{"git", "-C", bare, "config", "uploadpack.allowFilter", "true"}})

			work := initWorkClone(t, bare)

			require.NoError(t, os.MkdirAll(filepath.Join(work, "docs"), 0o755))
			require.NoError(t, os.MkdirAll(filepath.Join(work, "other"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(work, "other", "x"), []byte("other"), 0o644))
			commitFile(t, work, filepath.Join("docs", "guide"), "guide", "add docs")
			runGitCmds(t, [][]string{{"git", "-C", work, "push", "-q"}})

			setGlobalConfig(t, "[clone]\n\tdefaultRemoteName = upstream\n")

			h := &handler{allowFileURLs: true}

			// git ignores --filter in a clone from a local path, so the
			// clone reads the bare repository through a file:// URL.
			input := CloneInput{
				URL:         "file://" + bare,
				Dest:        filepath.Join(t.TempDir(), "cloned"),
				SparsePaths: tc.sparsePaths,
			}

			result, _, err := h.handleClone(t.Context(), nil, input)
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))
			assert.Contains(t, resultText(t, result), "Cloned")

			assert.Equal(t, defaultRemote, gitOut(t, "-C", input.Dest, "remote"))
			assert.FileExists(t, filepath.Join(input.Dest, "docs", "guide"))

			blob := gitOut(t, "-C", work, "rev-parse", "HEAD:other/x")
			objects := gitOut(t, "-C", input.Dest, "rev-list", "--objects", "--missing=print", "HEAD")

			want := blob
			if tc.partial {
				want = "?" + blob
			}

			assert.Contains(t, strings.Fields(objects), want)

			commitFile(t, work, filepath.Join("docs", "guide"), "guide v2", "update docs")
			runGitCmds(t, [][]string{{"git", "-C", work, "push", "-q"}})

			result, _, err = h.handleClone(t.Context(), nil, input)
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))
			assert.Contains(t, resultText(t, result), "Pulled")

			got, err := os.ReadFile(filepath.Join(input.Dest, "docs", "guide"))
			require.NoError(t, err)
			assert.Equal(t, "guide v2", string(got))
		})
	}
}

// authProxy is an HTTP proxy that tunnels each CONNECT request to one
// https server. The server records each credential that it receives,
// and it passes each request to the backend of [startAuthProxy].
type authProxy struct {
	// url is the URL of the proxy.
	url   string
	hosts []string
	creds []string
	mu    sync.Mutex
}

// unauthorized answers every request with 401. git then asks the
// credential helpers for a credential, sends the request again with
// the credential, and fails.
var unauthorized http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
	w.WriteHeader(http.StatusUnauthorized)
})

// requireCredential passes each request that carries cred, as
// user:password, to next. It answers every other request with 401.
func requireCredential(cred string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user+":"+password != cred {
			unauthorized.ServeHTTP(w, r)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// requireHostCredential passes each request that carries the
// credential of its host to next. creds maps the name of each host to
// that credential, as user:password. It answers every other request
// with 401.
func requireHostCredential(creds map[string]string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cred, ok := creds[r.Host]
		if !ok {
			unauthorized.ServeHTTP(w, r)

			return
		}

		requireCredential(cred, next).ServeHTTP(w, r)
	})
}

// gitBackend serves each bare repository under root over smart HTTP
// with git http-backend. The repository at <root>/a/b.git answers for
// the path /a/b.git.
func gitBackend(t *testing.T, root string) http.Handler {
	t.Helper()

	git, err := exec.LookPath("git")
	require.NoError(t, err)

	return &cgi.Handler{
		Path: git,
		Args: []string{"http-backend"},
		Env: []string{
			"GIT_PROJECT_ROOT=" + root,
			"GIT_HTTP_EXPORT_ALL=1",
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=" + os.DevNull,
			"HOME=" + root,
		},
	}
}

// startAuthProxy starts an [authProxy] for the lifetime of the test.
// Its server passes each request to backend.
func startAuthProxy(t *testing.T, backend http.Handler) *authProxy {
	t.Helper()

	p := &authProxy{}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if ok {
			p.mu.Lock()
			p.creds = append(p.creds, user+":"+password)
			p.mu.Unlock()
		}

		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "the proxy serves only CONNECT", http.StatusMethodNotAllowed)

			return
		}

		p.mu.Lock()
		p.hosts = append(p.hosts, r.Host)
		p.mu.Unlock()

		upstream, err := (&net.Dialer{}).DialContext(r.Context(), "tcp", server.Listener.Addr().String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)

			return
		}
		defer upstream.Close() //nolint:errcheck // The tunnel ends when either side closes.

		conn, buf, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck // The tunnel ends when either side closes.

		_, err = conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		if err != nil {
			return
		}

		// Each copy ends when one side closes its connection, and its
		// error reports only that close.
		copied := make(chan error, 2)

		go func() {
			_, copyErr := io.Copy(upstream, buf)

			copied <- copyErr
		}()

		go func() {
			_, copyErr := io.Copy(conn, upstream)

			copied <- copyErr
		}()

		<-copied
	}))
	t.Cleanup(proxy.Close)

	p.url = proxy.URL

	return p
}

// requests returns each host that git asked the proxy to connect to,
// with each repeat removed, and each credential that the server
// received, as user:password.
func (p *authProxy) requests() ([]string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return slices.Compact(slices.Clone(p.hosts)), slices.Clone(p.creds)
}

// config returns git config text that sends each https connection
// through the proxy and trusts the certificate of its server. The
// curloptResolve keys point github.com and gitlab.com at the loopback
// address, so git cannot reach either host when it skips the proxy.
func (p *authProxy) config() string {
	return "[http]\n" +
		"\tproxy = " + p.url + "\n" +
		"\tsslVerify = false\n" +
		"\tcurloptResolve = github.com:443:127.0.0.1\n" +
		"\tcurloptResolve = gitlab.com:443:127.0.0.1\n"
}

// setProxyEnv sets GH_TOKEN to token, and it points the global git
// config at a file that holds config. It clears each variable of the
// environment that changes how git reaches a host or where git finds a
// credential. It sets environment variables, so the test must not run
// in parallel.
func setProxyEnv(t *testing.T, token, config string) {
	t.Helper()

	t.Setenv("GH_TOKEN", token)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	// An empty GIT_ASKPASS keeps git from asking a program of the
	// environment for a credential. gitEnv keeps the empty value, so
	// git also skips the program that SSH_ASKPASS names.
	t.Setenv("GIT_ASKPASS", "")
	// gitEnv keeps a GIT_SSH_COMMAND that is set. git then fails
	// without a connection to a host when no rewrite changes an
	// SCP-style URL to an https URL.
	t.Setenv("GIT_SSH_COMMAND", "false")
	// git reads config from these variables after the global config.
	// A credential helper or a proxy that they set then overrides the
	// test config.
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_CONFIG_PARAMETERS", "")
	setGlobalConfig(t, config)
}

// githubHTTPSRewrite is git config text with a url.<base>.insteadOf
// rewrite that turns an SCP-style URL of GitHub into an https URL.
const githubHTTPSRewrite = "[url \"https://github.com/\"]\n\tinsteadOf = git@github.com:\n"

// globalHelper is git config text with a credential helper that
// answers for every host.
const globalHelper = "[credential]\n\thelper = \"!f() { echo username=global; echo password=secret; }; f\"\n"

// git clone connects to the url input after every rewrite of the git
// config. The token must reach each https connection to github.com,
// and the credential helpers of the git config must answer for every
// other host.
func TestHandleCloneCredential(t *testing.T) { //nolint:paralleltest // t.Setenv
	const (
		token      = "ghp_test123"
		tokenCred  = "x-access-token:" + token
		globalCred = "global:secret"
		github     = "github.com:443"
		gitlab     = "gitlab.com:443"
	)

	tests := map[string]struct {
		token string
		url   string
		// config is text that the test adds to the global config.
		config string
		// include is the text of a config file that the global config
		// includes only for a git directory under the parent of dest.
		include string
		host    string
		creds   []string
	}{
		"https URL of GitHub": {
			token: token,
			url:   "https://github.com/a/b",
			host:  github,
			creds: []string{tokenCred},
		},
		"SCP-style URL that the global config rewrites": {
			token:  token,
			url:    "git@github.com:a/b",
			config: githubHTTPSRewrite,
			host:   github,
			creds:  []string{tokenCred},
		},
		"SCP-style URL that a conditional include rewrites": {
			token:   token,
			url:     "git@github.com:a/b",
			include: githubHTTPSRewrite,
			host:    github,
			creds:   []string{tokenCred},
		},
		"GitHub with a helper in the global config": {
			token:  token,
			url:    "https://github.com/a/b",
			config: globalHelper,
			host:   github,
			creds:  []string{tokenCred},
		},
		"another host with a helper in the global config": {
			token:  token,
			url:    "https://gitlab.com/a/b",
			config: globalHelper,
			host:   gitlab,
			creds:  []string{globalCred},
		},
		"another host": {
			token: token,
			url:   "https://gitlab.com/a/b",
			host:  gitlab,
		},
		"no token": {
			url:    "git@github.com:a/b",
			config: githubHTTPSRewrite + globalHelper,
			host:   github,
			creds:  []string{globalCred},
		},
	}

	for name, tt := range tests { //nolint:paralleltest // setProxyEnv calls t.Setenv.
		t.Run(name, func(t *testing.T) {
			proxy := startAuthProxy(t, unauthorized)

			// git matches the pattern of an includeIf gitdir condition
			// against the git directory with each symbolic link
			// resolved. The temporary directory of macOS lies under
			// such a link.
			parent, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)

			config := proxy.config() + tt.config

			if tt.include != "" {
				include := filepath.Join(t.TempDir(), "include")
				require.NoError(t, os.WriteFile(include, []byte(tt.include), 0o644))

				config += "[includeIf \"gitdir:" + parent + "/\"]\n\tpath = " + include + "\n"
			}

			setProxyEnv(t, tt.token, config)

			input := CloneInput{URL: tt.url, Dest: filepath.Join(parent, "dest")}

			result, _, err := (&handler{token: tt.token}).handleClone(t.Context(), nil, input)
			require.NoError(t, err)
			require.True(t, result.IsError, resultText(t, result))

			hosts, creds := proxy.requests()
			assert.Equal(t, []string{tt.host}, hosts)
			assert.Equal(t, tt.creds, creds)
		})
	}
}

// A sparse clone without a depth leaves out each blob that its
// checkout does not hold. git sparse-checkout set then fetches the
// blobs of the sparse paths from origin, and that fetch must take the
// token as well.
func TestHandleCloneSparseCredential(t *testing.T) { //nolint:paralleltest // t.Setenv
	const token = "ghp_test123"

	root := t.TempDir()
	bare := filepath.Join(root, "a", "b.git")

	runGitCmds(t, [][]string{
		{"git", "init", "-q", "--bare", bare},
		{"git", "-C", bare, "config", "uploadpack.allowFilter", "true"},
	})

	work := initWorkClone(t, bare)

	require.NoError(t, os.MkdirAll(filepath.Join(work, "dir"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(work, "other"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(work, "other", "g.txt"), []byte("other"), 0o644))
	commitFile(t, work, "dir/f.txt", "dir", "add dirs")
	runGitCmds(t, [][]string{{"git", "-C", work, "push", "-q", "origin", "HEAD"}})

	proxy := startAuthProxy(t, requireCredential("x-access-token:"+token, gitBackend(t, root)))
	setProxyEnv(t, token, proxy.config()+githubHTTPSRewrite)

	dest := filepath.Join(t.TempDir(), "cloned")

	result, _, err := (&handler{token: token}).handleClone(t.Context(), nil, CloneInput{
		URL:         "git@github.com:a/b.git",
		Dest:        dest,
		SparsePaths: []string{"dir"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	assert.FileExists(t, filepath.Join(dest, "dir", "f.txt"))
	assert.NoFileExists(t, filepath.Join(dest, "other", "g.txt"))

	// The clone lacks the blob of a path outside the sparse checkout.
	// git clone therefore left out the blobs, and git sparse-checkout
	// set fetched the blob of dir/f.txt.
	blob := gitOut(t, "-C", work, "rev-parse", "HEAD:other/g.txt")
	missing := gitOut(t, "-C", dest, "rev-list", "--objects", "--missing=print", "HEAD")
	assert.Contains(t, strings.Fields(missing), "?"+blob)
}

// A new clone holds the hook files of the template directory, and git
// fires hooks when it checks out the clone and when it sets the sparse
// checkout.
func TestHandleCloneSkipsHooks(t *testing.T) { //nolint:paralleltest // t.Setenv
	bare := initBareRepo(t)
	script, marker := markerScript(t)
	template := t.TempDir()

	for _, event := range hookEvents {
		writeHookIn(t, filepath.Join(template, "hooks"), event, "#!/bin/sh\nexec '"+script+"'\n")
	}

	t.Setenv("GIT_TEMPLATE_DIR", template)

	dest := filepath.Join(t.TempDir(), "cloned")

	result, _, err := (&handler{allowFileURLs: true}).handleClone(t.Context(), nil, CloneInput{
		URL:         bare,
		Dest:        dest,
		SparsePaths: []string{"docs"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	requireNotRun(t, marker)
	assert.FileExists(t, filepath.Join(dest, ".git", "hooks", "post-checkout"),
		"the clone must hold the hook files of the template")
}

// A hook of the global config runs in the new clone, and the remote
// chose each file of that clone. git fires hooks when it checks out
// the clone and when it sets the sparse checkout.
func TestHandleCloneSkipsGlobalHooks(t *testing.T) { //nolint:paralleltest // t.Setenv
	bare := initBareRepo(t)
	script, marker := markerScript(t)

	global := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(global, []byte(hookConfig(script, "check", "x=y")), 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", global)

	result, _, err := (&handler{allowFileURLs: true}).handleClone(t.Context(), nil, CloneInput{
		URL:         bare,
		Dest:        filepath.Join(t.TempDir(), "cloned"),
		SparsePaths: []string{"docs"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	requireNotRun(t, marker)
}

// git clone reads no config of a repository that holds its working
// directory, so [readUserHooks] does not list the hooks of such a
// repository. The caller can write that config when the server starts
// in a repository of the caller.
func TestHandleCloneIgnoresConfigOfWorkingDirectory(t *testing.T) { //nolint:paralleltest // t.Chdir
	bare := initBareRepo(t)
	script, marker := markerScript(t)

	cwd := filepath.Join(t.TempDir(), "repo")
	runGitCmds(t, [][]string{
		{"git", "init", "-q", cwd},
		{"git", "-C", cwd, "config", "core.fsmonitor", script},
	})
	appendGitConfig(t, cwd, hookConfig(script, "check"))

	t.Chdir(cwd)

	result, _, err := (&handler{allowFileURLs: true}).handleClone(t.Context(), nil, CloneInput{
		URL:         bare,
		Dest:        filepath.Join(t.TempDir(), "cloned"),
		SparsePaths: []string{"docs"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	requireNotRun(t, marker)
}

// A second git_clone call pulls into the existing checkout, and the
// caller can write the git config of that checkout.
func TestHandleClonePullRefusesConfigCommands(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	h := &handler{allowFileURLs: true}

	input := CloneInput{URL: bare, Dest: filepath.Join(t.TempDir(), "cloned")}

	result, _, err := h.handleClone(t.Context(), nil, input)
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	script, marker := markerScript(t)

	runGitCmds(t, [][]string{{"git", "-C", input.Dest, "config", "filter.x.smudge", script}})

	result, _, err = h.handleClone(t.Context(), nil, input)
	require.NoError(t, err)
	require.True(t, result.IsError, "the pull must refuse the checkout")
	assert.Contains(t, resultText(t, result), ErrDeniedConfig.Error()+": the repository sets filter.x.smudge.")

	requireNotRun(t, marker)
}

func TestHandleCloneCorruptDest(t *testing.T) {
	t.Parallel()

	tcs := map[string]struct {
		remove []string
	}{
		"missing HEAD and config": {remove: []string{"HEAD", "config"}},
		"missing HEAD":            {remove: []string{"HEAD"}},
		"missing config":          {remove: []string{"config"}},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bare := initBareRepo(t)
			h := &handler{allowFileURLs: true}

			// A repository encloses dest, so the check must not
			// walk up and accept the parent as the checkout.
			parent := t.TempDir()
			runGitCmds(t, [][]string{{"git", "init", "-q", parent}})

			dest := filepath.Join(parent, "cloned")

			result, _, err := h.handleClone(t.Context(), nil, CloneInput{URL: bare, Dest: dest})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))

			for _, f := range tc.remove {
				require.NoError(t, os.Remove(filepath.Join(dest, ".git", f)))
			}

			require.NoError(t, os.WriteFile(filepath.Join(dest, "local.txt"), []byte("keep"), 0o644))

			result, _, err = h.handleClone(t.Context(), nil, CloneInput{URL: bare, Dest: dest})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))
			assert.Contains(t, resultText(t, result), "Cloned")

			asides, err := filepath.Glob(dest + ".corrupt-*")
			require.NoError(t, err)
			require.Len(t, asides, 1)
			assert.Contains(t, resultText(t, result), asides[0])

			kept, err := os.ReadFile(filepath.Join(asides[0], "local.txt"))
			require.NoError(t, err)
			assert.Equal(t, "keep", string(kept))

			// The new checkout is a working repository again.
			result, _, err = h.handleClone(t.Context(), nil, CloneInput{URL: bare, Dest: dest})
			require.NoError(t, err)
			require.False(t, result.IsError, resultText(t, result))
			assert.Contains(t, resultText(t, result), "Pulled")
		})
	}
}

func TestHandleCloneCorruptDestOutsideAllowDir(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "cloned")

	// dest is the allowed directory itself, so its sibling is not
	// an allowed path and the checkout must stay in place.
	h := &handler{allowFileURLs: true, allowDirs: mustResolveAllowDirs(t, dest)}

	result, _, err := h.handleClone(t.Context(), nil, CloneInput{URL: bare, Dest: dest})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	require.NoError(t, os.Remove(filepath.Join(dest, ".git", "HEAD")))

	result, _, err = h.handleClone(t.Context(), nil, CloneInput{URL: bare, Dest: dest})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), ErrCorruptDest.Error())

	_, statErr := os.Stat(filepath.Join(dest, ".git"))
	require.NoError(t, statErr)
}

func TestEffectiveTimeout(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		handlerTimeout time.Duration
		perCallSecs    int
		want           time.Duration
	}{
		"per-call wins over default": {
			handlerTimeout: time.Minute,
			perCallSecs:    5,
			want:           5 * time.Second,
		},
		"default used when per-call zero": {
			handlerTimeout: time.Minute,
			perCallSecs:    0,
			want:           time.Minute,
		},
		"zero everywhere": {
			handlerTimeout: 0,
			perCallSecs:    0,
			want:           0,
		},
		"large per-call over small default": {
			handlerTimeout: time.Second,
			perCallSecs:    600,
			want:           10 * time.Minute,
		},
		"per-call when default disabled": {
			handlerTimeout: 0,
			perCallSecs:    10,
			want:           10 * time.Second,
		},
		"negative per-call falls back to default": {
			handlerTimeout: time.Minute,
			perCallSecs:    -1,
			want:           time.Minute,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &handler{timeout: tt.handlerTimeout}
			assert.Equal(t, tt.want, h.effectiveTimeout(tt.perCallSecs))
		})
	}
}

func TestHandleCloneTimeout(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	h := &handler{allowFileURLs: true, timeout: time.Nanosecond}

	dest := filepath.Join(t.TempDir(), "cloned")

	result, _, err := h.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "timed out")
}

func TestHandleClonePullTimeout(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "cloned")

	// First call clones with no timeout configured.
	cloneH := &handler{allowFileURLs: true}

	result, _, err := cloneH.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	// Second call takes the pull branch and trips the timeout.
	pullH := &handler{allowFileURLs: true, timeout: time.Nanosecond}

	result, _, err = pullH.handleClone(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "timed out")
}
