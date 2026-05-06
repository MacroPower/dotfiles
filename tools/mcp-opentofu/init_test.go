package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleInitSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stdout: "Initializing the backend...\nOpenTofu has been successfully initialized!",
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleInit(t.Context(), nil, InitInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "OpenTofu initialization succeeded")
	assert.Contains(t, text, "OpenTofu has been successfully initialized!")

	require.Len(t, fake.calls, 1)
	assert.Equal(t, dir, fake.calls[0].dir)
	assert.Equal(t, []string{"init", "-input=false", "-no-color", "-backend=false"}, fake.calls[0].args)
}

func TestHandleInitFlags(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in       InitInput
		wantArgs []string
	}{
		"default": {
			in:       InitInput{},
			wantArgs: []string{"init", "-input=false", "-no-color", "-backend=false"},
		},
		"backend": {
			in:       InitInput{Backend: true},
			wantArgs: []string{"init", "-input=false", "-no-color", "-backend=true"},
		},
		"upgrade": {
			in:       InitInput{Upgrade: true},
			wantArgs: []string{"init", "-input=false", "-no-color", "-backend=false", "-upgrade"},
		},
		"backend+upgrade": {
			in:       InitInput{Backend: true, Upgrade: true},
			wantArgs: []string{"init", "-input=false", "-no-color", "-backend=true", "-upgrade"},
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

			r, _, err := h.handleInit(t.Context(), nil, in)
			require.NoError(t, err)
			require.False(t, r.IsError, resultText(t, r))

			require.Len(t, fake.calls, 1)
			assert.Equal(t, tt.wantArgs, fake.calls[0].args)
		})
	}
}

func TestHandleInitInputErrors(t *testing.T) {
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

			r, _, err := h.handleInit(t.Context(), nil, InitInput{WorkingDirectory: tt.dir})
			require.NoError(t, err)
			require.NotNil(t, r)
			assert.True(t, r.IsError, resultText(t, r))
			assert.Contains(t, resultText(t, r), tt.want)
			assert.Empty(t, fake.calls, "executor must not be invoked for input errors")
		})
	}
}

func TestHandleInitRenderEmptyStderr(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stdout: "Initializing the backend..."})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleInit(t.Context(), nil, InitInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "### Output")
	assert.NotContains(t, text, "### Notices")
}

func TestHandleInitRenderStderrOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stderr: "Warning: deprecated module path"})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleInit(t.Context(), nil, InitInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "### Notices (stderr)")
	assert.Contains(t, text, "Warning: deprecated module path")
	assert.NotContains(t, text, "### Output")
}

func TestHandleInitFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stderr:   "Error: Failed to install provider",
		exitCode: 1,
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleInit(t.Context(), nil, InitInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "'tofu init' exited with code 1")
	assert.Contains(t, text, "Failed to install provider")
}

func TestHandleInitBinaryNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{err: exec.ErrNotFound})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleInit(t.Context(), nil, InitInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError)
	assert.Contains(t, resultText(t, r), "tofu binary not found in PATH")
	assert.Contains(t, resultText(t, r), "--tofu-bin")
}

func TestHandleInitContextCanceled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{err: context.Canceled})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleInit(t.Context(), nil, InitInput{WorkingDirectory: dir})
	require.Nil(t, r)
	require.ErrorIs(t, err, context.Canceled)
}
