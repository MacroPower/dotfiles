package main

import (
	"os"
	"os/exec"
	"path/filepath"
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

	h := &cloneHandler{
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

	h := &cloneHandler{}

	require.NoError(t, h.checkDest("/anywhere/at/all"))
}

func TestHandleValidation(t *testing.T) {
	t.Parallel()

	h := &cloneHandler{}

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

			result, _, err := h.handle(t.Context(), nil, tt.input)
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

			h := &cloneHandler{token: tt.token}
			got := h.credentialArgs(tt.url)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckURL(t *testing.T) {
	t.Parallel()

	h := &cloneHandler{}

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

	h := &cloneHandler{allowInsecure: true}

	require.NoError(t, h.checkURL("http://github.com/a/b"))
	require.NoError(t, h.checkURL("git://github.com/a/b"))
}

func TestCheckURLAllowFile(t *testing.T) {
	t.Parallel()

	h := &cloneHandler{allowFileURLs: true}

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

	h := &cloneHandler{allowDirs: []string{allowDir}}

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

	h := &cloneHandler{allowFileURLs: true}

	result, _, err := h.handle(t.Context(), nil, CloneInput{
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

	h := &cloneHandler{allowFileURLs: true}

	result, _, err := h.handle(t.Context(), nil, CloneInput{
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
	h := &cloneHandler{allowFileURLs: true}

	dest := filepath.Join(t.TempDir(), "cloned")

	// First call: clone.
	result, _, err := h.handle(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Cloned")

	_, statErr := os.Stat(filepath.Join(dest, ".git"))
	require.NoError(t, statErr, ".git directory should exist after clone")

	// Second call: pull.
	result, _, err = h.handle(t.Context(), nil, CloneInput{
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
	h := &cloneHandler{allowFileURLs: true, token: "ghp_unused"}

	dest := filepath.Join(t.TempDir(), "cloned")

	// Clone with token set (file URL, so credential args are not injected).
	result, _, err := h.handle(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Contains(t, resultText(t, result), "Cloned")

	// Pull with token set.
	result, _, err = h.handle(t.Context(), nil, CloneInput{
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
	tmp := filepath.Join(t.TempDir(), "tag-work")

	for _, args := range [][]string{
		{"git", "clone", bare, tmp},
		{"git", "-C", tmp, "config", "user.email", "test@test.com"},
		{"git", "-C", tmp, "config", "user.name", "Test"},
		{"git", "-C", tmp, "tag", "v1.0.0"},
		{"git", "-C", tmp, "push", "--tags"},
	} {
		cmd := exec.CommandContext(t.Context(), args[0], args[1:]...) //nolint:gosec // test helper
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run(), "setup command failed: %v", args)
	}

	h := &cloneHandler{allowFileURLs: true}
	dest := filepath.Join(t.TempDir(), "cloned")

	result, _, err := h.handle(t.Context(), nil, CloneInput{
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
	tmp := filepath.Join(t.TempDir(), "sparse-work")

	for _, args := range [][]string{
		{"git", "clone", bare, tmp},
		{"git", "-C", tmp, "config", "user.email", "test@test.com"},
		{"git", "-C", tmp, "config", "user.name", "Test"},
	} {
		cmd := exec.CommandContext(t.Context(), args[0], args[1:]...) //nolint:gosec // test helper
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run(), "setup command failed: %v", args)
	}

	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "src"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "src", "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "docs", "README"), []byte("docs"), 0o644))

	for _, args := range [][]string{
		{"git", "-C", tmp, "add", "."},
		{"git", "-C", tmp, "commit", "-m", "add dirs"},
		{"git", "-C", tmp, "push"},
	} {
		cmd := exec.CommandContext(t.Context(), args[0], args[1:]...) //nolint:gosec // test helper
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run(), "setup command failed: %v", args)
	}

	h := &cloneHandler{allowFileURLs: true}
	dest := filepath.Join(t.TempDir(), "cloned")

	// Clone with sparse checkout.
	result, _, err := h.handle(t.Context(), nil, CloneInput{
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
	result, _, err = h.handle(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))
	assert.Contains(t, resultText(t, result), "Pulled")
}

func TestHandleOriginMismatch(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	h := &cloneHandler{allowFileURLs: true}

	dest := filepath.Join(t.TempDir(), "cloned")

	// Clone the repo.
	result, _, err := h.handle(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Try to pull with a different URL.
	result, _, err = h.handle(t.Context(), nil, CloneInput{
		URL:  "/tmp/different-repo",
		Dest: dest,
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "origin URL mismatch")
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

			h := &cloneHandler{timeout: tt.handlerTimeout}
			assert.Equal(t, tt.want, h.effectiveTimeout(tt.perCallSecs))
		})
	}
}

func TestHandleCloneTimeout(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	h := &cloneHandler{allowFileURLs: true, timeout: time.Nanosecond}

	dest := filepath.Join(t.TempDir(), "cloned")

	result, _, err := h.handle(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "timed out")
}

func TestHandlePullTimeout(t *testing.T) {
	t.Parallel()

	bare := initBareRepo(t)
	dest := filepath.Join(t.TempDir(), "cloned")

	// First call clones with no timeout configured.
	cloneH := &cloneHandler{allowFileURLs: true}

	result, _, err := cloneH.handle(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.False(t, result.IsError, resultText(t, result))

	// Second call takes the pull branch and trips the timeout.
	pullH := &cloneHandler{allowFileURLs: true, timeout: time.Nanosecond}

	result, _, err = pullH.handle(t.Context(), nil, CloneInput{
		URL:  bare,
		Dest: dest,
	})
	require.NoError(t, err)
	require.True(t, result.IsError)
	assert.Contains(t, resultText(t, result), "timed out")
}
