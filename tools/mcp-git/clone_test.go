package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
		"-c", `credential.https://github.com.helper=!f() { echo username=x-access-token; echo password=$GITHUB_TOKEN; }; f`,
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
