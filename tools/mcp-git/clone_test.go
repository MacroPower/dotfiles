package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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
			want:  []string{"clone", "-q", "--", "https://github.com/a/b", "/tmp/b"},
		},
		"with depth": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/b", Depth: 1},
			want:  []string{"clone", "-q", "--depth", "1", "--", "https://github.com/a/b", "/tmp/b"},
		},
		"with branch": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/b", Branch: "main"},
			want:  []string{"clone", "-q", "--branch", "main", "--", "https://github.com/a/b", "/tmp/b"},
		},
		"with single branch": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/b", SingleBranch: true},
			want:  []string{"clone", "-q", "--single-branch", "--", "https://github.com/a/b", "/tmp/b"},
		},
		"all options": {
			input: CloneInput{
				URL: "https://github.com/a/b", Dest: "/tmp/b",
				Depth: 1, Branch: "dev", SingleBranch: true,
			},
			want: []string{
				"clone", "-q", "--depth", "1", "--branch", "dev", "--single-branch",
				"--", "https://github.com/a/b", "/tmp/b",
			},
		},
		"with sparse bool": {
			input: CloneInput{URL: "https://github.com/a/b", Dest: "/tmp/b", Sparse: true},
			want: []string{
				"clone", "-q", "--sparse", "--filter=blob:none",
				"--", "https://github.com/a/b", "/tmp/b",
			},
		},
		"with sparse paths": {
			input: CloneInput{
				URL: "https://github.com/a/b", Dest: "/tmp/b",
				SparsePaths: []string{"src", "docs"},
			},
			want: []string{
				"clone", "-q", "--sparse", "--filter=blob:none",
				"--", "https://github.com/a/b", "/tmp/b",
			},
		},
		"sparse with depth": {
			input: CloneInput{
				URL: "https://github.com/a/b", Dest: "/tmp/b",
				Sparse: true, Depth: 1,
			},
			want: []string{
				"clone", "-q", "--depth", "1", "--sparse",
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
		"denied": {
			dest: "/home/user/repo",
			err:  ErrDeniedDest,
		},
		"denied traversal": {
			dest: "/tmp/git/../../etc/passwd",
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
		"-c", "credential.helper=",
		"-c", `credential.https://github.com.helper=!f() { echo username=x-access-token; echo password=$GH_TOKEN; }; f`,
	}

	tests := map[string]struct {
		token string
		url   string
		want  []string
	}{
		"no token": {
			url: "https://github.com/a/b",
		},
		"github https": {
			token: "ghp_test123",
			url:   "https://github.com/a/b",
			want:  wantArgs,
		},
		"gitlab https": {
			token: "ghp_test123",
			url:   "https://gitlab.com/a/b",
		},
		"github ssh": {
			token: "ghp_test123",
			url:   "ssh://git@github.com/a/b",
		},
		"github scp": {
			token: "ghp_test123",
			url:   "git@github.com:a/b",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := &handler{token: tt.token}
			got := h.credentialArgs(tt.url)
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

	h := &handler{allowDirs: []string{allowDir}}

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

func TestHandleCloneWithToken(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	h := &handler{allowFileURLs: true, token: "ghp_unused"}

	dest := filepath.Join(t.TempDir(), "cloned")

	// Clone with token set (file URL, so credential args are not injected).
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

// The git config can rewrite the URL of a clone to a transport that
// the server denies, after checkURL accepts the URL. git itself must
// refuse the transport.
func TestHandleCloneRefusesRewrittenURL(t *testing.T) { //nolint:paralleltest // t.Setenv
	bare := initBareRepo(t)

	global := filepath.Join(t.TempDir(), "gitconfig")
	rewrite := "[url \"" + filepath.Dir(bare) + "/\"]\n\tinsteadOf = https://example.invalid/\n"

	require.NoError(t, os.WriteFile(global, []byte(rewrite), 0o644))
	t.Setenv("GIT_CONFIG_GLOBAL", global)

	input := CloneInput{
		URL:  "https://example.invalid/" + filepath.Base(bare),
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
	h := &handler{allowFileURLs: true, allowDirs: resolveAllowDirs([]string{dest})}

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
