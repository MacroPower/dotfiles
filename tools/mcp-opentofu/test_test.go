package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleTestSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stdout: "tests/example.tftest.hcl... pass\n\nSuccess! 1 passed, 0 failed.",
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleTest(t.Context(), nil, TestInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "## OpenTofu Test:")
	assert.Contains(t, text, "exited with code 0")
	assert.Contains(t, text, "Success! 1 passed, 0 failed.")

	require.Len(t, fake.calls, 1)
	assert.Equal(t, dir, fake.calls[0].dir)
	assert.Equal(t, []string{"test", "-no-color"}, fake.calls[0].args)
}

func TestHandleTestFailureRendered(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stdout:   "tests/example.tftest.hcl... fail\n\nFailure! 0 passed, 1 failed.",
		exitCode: 1,
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleTest(t.Context(), nil, TestInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.False(t, r.IsError,
		"non-zero exit from tofu test must render as data, not surface as IsError=true")

	text := resultText(t, r)
	assert.Contains(t, text, "exited with code 1")
	assert.Contains(t, text, "Failure! 0 passed, 1 failed.")
}

func TestHandleTestFlags(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in       TestInput
		wantArgs []string
	}{
		"default": {
			in:       TestInput{},
			wantArgs: []string{"test", "-no-color"},
		},
		"verbose": {
			in:       TestInput{Verbose: true},
			wantArgs: []string{"test", "-no-color", "-verbose"},
		},
		"test_directory": {
			in:       TestInput{TestDirectory: "tests/integration"},
			wantArgs: []string{"test", "-no-color", "-test-directory=tests/integration"},
		},
		"single filter": {
			in:       TestInput{Filter: []string{"tests/a.tftest.hcl"}},
			wantArgs: []string{"test", "-no-color", "-filter=tests/a.tftest.hcl"},
		},
		"multiple filters": {
			in: TestInput{Filter: []string{"tests/a.tftest.hcl", "tests/b.tftest.hcl"}},
			wantArgs: []string{
				"test", "-no-color",
				"-filter=tests/a.tftest.hcl",
				"-filter=tests/b.tftest.hcl",
			},
		},
		"all combined": {
			in: TestInput{
				TestDirectory: "tests/integration",
				Filter:        []string{"tests/integration/a.tftest.hcl"},
				Verbose:       true,
			},
			wantArgs: []string{
				"test", "-no-color",
				"-test-directory=tests/integration",
				"-filter=tests/integration/a.tftest.hcl",
				"-verbose",
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			fake := newFakeExecutor(fakeResponse{stdout: "ok"})
			h := newTofuTestHandler(t, fake)

			in := tt.in
			in.WorkingDirectory = dir

			r, _, err := h.handleTest(t.Context(), nil, in)
			require.NoError(t, err)
			require.False(t, r.IsError, resultText(t, r))

			require.Len(t, fake.calls, 1)
			assert.Equal(t, tt.wantArgs, fake.calls[0].args)
		})
	}
}

func TestHandleTestStderrOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stderr: "Warning: deprecated provider syntax"})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleTest(t.Context(), nil, TestInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "### Notices (stderr)")
	assert.Contains(t, text, "Warning: deprecated provider syntax")
	assert.NotContains(t, text, "### Output")
}

func TestHandleTestInputErrors(t *testing.T) {
	t.Parallel()

	existingDir := t.TempDir()

	filePath := filepath.Join(existingDir, "main.tf")
	require.NoError(t, os.WriteFile(filePath, []byte("// stub\n"), 0o644))

	missingPath := filepath.Join(existingDir, "does-not-exist")

	tests := map[string]struct {
		dir  string
		want string
	}{
		"empty path": {
			dir:  "",
			want: "working_directory is required",
		},
		"relative path": {
			dir:  "relative/path",
			want: "must be an absolute path",
		},
		"missing path": {
			dir:  missingPath,
			want: "stat working_directory",
		},
		"path is a file": {
			dir:  filePath,
			want: "is not a directory",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeExecutor()
			h := newTofuTestHandler(t, fake)

			r, _, err := h.handleTest(t.Context(), nil, TestInput{WorkingDirectory: tt.dir})
			require.NoError(t, err)
			require.NotNil(t, r)
			assert.True(t, r.IsError, resultText(t, r))
			assert.Contains(t, resultText(t, r), tt.want)
			assert.Empty(t, fake.calls, "executor must not be invoked for input errors")
		})
	}
}

func TestHandleTestBinaryNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{err: exec.ErrNotFound})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleTest(t.Context(), nil, TestInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError)
	assert.Contains(t, resultText(t, r), "tofu binary not found in PATH")
	assert.Contains(t, resultText(t, r), "--tofu-bin")
}

func TestHandleTestMissingBinaryViaCode127(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stderr:   "bwrap: execvp tofu: No such file or directory",
		exitCode: 127,
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleTest(t.Context(), nil, TestInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError,
		"exit 127 + missing-binary stderr must surface as IsError=true, not as a normal test failure")
	assert.Contains(t, resultText(t, r), "tofu binary not found in PATH")
	assert.Contains(t, resultText(t, r), "--tofu-bin")
}

func TestHandleTestInitFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stderr:   "Error: Failed to install provider",
		exitCode: 1,
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleTest(t.Context(), nil, TestInput{
		WorkingDirectory: dir,
		Init:             true,
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "'tofu init' exited with code 1")
	assert.Contains(t, text, "Failed to install provider")

	require.Len(t, fake.calls, 1, "test must not be invoked when init fails")
	assert.Equal(t, []string{"init", "-input=false", "-no-color", "-backend=false"}, fake.calls[0].args)
}

func TestHandleTestInitThenTest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(
		fakeResponse{stdout: "Initialized!", exitCode: 0},
		fakeResponse{stdout: "Success! 1 passed, 0 failed.", exitCode: 0},
	)
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleTest(t.Context(), nil, TestInput{
		WorkingDirectory: dir,
		Init:             true,
	})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))
	assert.Contains(t, resultText(t, r), "Success! 1 passed, 0 failed.")

	require.Len(t, fake.calls, 2)
	assert.Equal(t, []string{"init", "-input=false", "-no-color", "-backend=false"}, fake.calls[0].args)
	assert.Equal(t, []string{"test", "-no-color"}, fake.calls[1].args)
	assert.Equal(t, dir, fake.calls[0].dir)
	assert.Equal(t, dir, fake.calls[1].dir)
}

func TestHandleTestContextCanceled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{err: context.Canceled})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleTest(t.Context(), nil, TestInput{WorkingDirectory: dir})
	require.Nil(t, r)
	require.ErrorIs(t, err, context.Canceled)
}

func TestHandleTestPagination(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", defaultMaxLength*2)

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stdout: long})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleTest(t.Context(), nil, TestInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.False(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "Use start_index=5000 to continue reading")
}
