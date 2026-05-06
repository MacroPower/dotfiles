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

func TestHandlePlanNoChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stdout:   "No changes. Your infrastructure matches the configuration.",
		exitCode: 0,
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handlePlan(t.Context(), nil, PlanInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "**Changes pending**: false")
	assert.NotContains(t, text, "**Mode**:")
	assert.Contains(t, text, "No changes")

	require.Len(t, fake.calls, 1)
	assert.Equal(t, dir, fake.calls[0].dir)
	assert.Equal(t, []string{"plan", "-input=false", "-no-color", "-detailed-exitcode"}, fake.calls[0].args)
}

func TestHandlePlanWithChanges(t *testing.T) {
	t.Parallel()

	const planOut = `OpenTofu will perform the following actions:

  # null_resource.demo will be created
  + resource "null_resource" "demo" {
      + id = (known after apply)
    }

Plan: 1 to add, 0 to change, 0 to destroy.`

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stdout: planOut, exitCode: 2})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handlePlan(t.Context(), nil, PlanInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "**Changes pending**: true")
	assert.Contains(t, text, `+ resource "null_resource" "demo"`)
	assert.Contains(t, text, "Plan: 1 to add")
}

func TestHandlePlanRefreshOnlyDrift(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stdout:   "Note: Objects have changed outside of OpenTofu",
		exitCode: 2,
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handlePlan(t.Context(), nil, PlanInput{
		WorkingDirectory: dir,
		RefreshOnly:      true,
	})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "**Mode**: refresh-only")
	assert.Contains(t, text, "**Drift detected**: true")
	assert.NotContains(t, text, "**Changes pending**")

	require.Len(t, fake.calls, 1)
	assert.Equal(t,
		[]string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-refresh-only"},
		fake.calls[0].args,
	)
}

func TestHandlePlanFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stderr:   "Error: Reference to undeclared input variable",
		exitCode: 1,
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handlePlan(t.Context(), nil, PlanInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "'tofu plan' exited with code 1")
	assert.Contains(t, text, "Reference to undeclared input variable")
}

func TestHandlePlanFlags(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in       PlanInput
		wantArgs []string
	}{
		"default": {
			in:       PlanInput{},
			wantArgs: []string{"plan", "-input=false", "-no-color", "-detailed-exitcode"},
		},
		"destroy": {
			in:       PlanInput{Destroy: true},
			wantArgs: []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-destroy"},
		},
		"refresh_only": {
			in:       PlanInput{RefreshOnly: true},
			wantArgs: []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-refresh-only"},
		},
		"destroy+refresh_only": {
			in:       PlanInput{Destroy: true, RefreshOnly: true},
			wantArgs: []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-destroy", "-refresh-only"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			fake := newFakeExecutor(fakeResponse{stdout: "No changes."})
			h := newTofuTestHandler(t, fake)

			in := tt.in
			in.WorkingDirectory = dir

			r, _, err := h.handlePlan(t.Context(), nil, in)
			require.NoError(t, err)
			require.False(t, r.IsError, resultText(t, r))

			require.Len(t, fake.calls, 1)
			assert.Equal(t, tt.wantArgs, fake.calls[0].args)
		})
	}
}

func TestHandlePlanInputErrors(t *testing.T) {
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

			r, _, err := h.handlePlan(t.Context(), nil, PlanInput{WorkingDirectory: tt.dir})
			require.NoError(t, err)
			require.NotNil(t, r)
			assert.True(t, r.IsError, resultText(t, r))
			assert.Contains(t, resultText(t, r), tt.want)
			assert.Empty(t, fake.calls, "executor must not be invoked for input errors")
		})
	}
}

func TestHandlePlanInitFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stderr:   "Error: Failed to install provider",
		exitCode: 1,
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handlePlan(t.Context(), nil, PlanInput{
		WorkingDirectory: dir,
		Init:             true,
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "'tofu init' exited with code 1")
	assert.Contains(t, text, "Failed to install provider")

	require.Len(t, fake.calls, 1, "plan must not be invoked when init fails")
	assert.Equal(t, []string{"init", "-input=false", "-no-color", "-backend=false"}, fake.calls[0].args)
}

func TestHandlePlanInitThenPlan(t *testing.T) {
	t.Parallel()

	const initStderr = "Warning: deprecated module path"

	dir := t.TempDir()
	fake := newFakeExecutor(
		fakeResponse{stdout: "Initialized!", stderr: initStderr, exitCode: 0},
		fakeResponse{stdout: "No changes.", exitCode: 0},
	)
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handlePlan(t.Context(), nil, PlanInput{
		WorkingDirectory: dir,
		Init:             true,
	})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "**Changes pending**: false")
	assert.NotContains(t, text, initStderr,
		"init's stderr must not leak into the rendered plan output")
	assert.NotContains(t, text, "Initialized!",
		"init's stdout must not leak into the rendered plan output")

	require.Len(t, fake.calls, 2)
	assert.Equal(t, []string{"init", "-input=false", "-no-color", "-backend=false"}, fake.calls[0].args)
	assert.Equal(t, []string{"plan", "-input=false", "-no-color", "-detailed-exitcode"}, fake.calls[1].args)
	assert.Equal(t, dir, fake.calls[0].dir)
	assert.Equal(t, dir, fake.calls[1].dir)
}

func TestHandlePlanBinaryNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{err: exec.ErrNotFound})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handlePlan(t.Context(), nil, PlanInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError)
	assert.Contains(t, resultText(t, r), "tofu binary not found in PATH")
	assert.Contains(t, resultText(t, r), "--tofu-bin")
}

func TestHandlePlanContextCanceled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{err: context.Canceled})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handlePlan(t.Context(), nil, PlanInput{WorkingDirectory: dir})
	require.Nil(t, r)
	require.ErrorIs(t, err, context.Canceled)
}

func TestHandlePlanPagination(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", defaultMaxLength*2)

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stdout: long, exitCode: 2})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handlePlan(t.Context(), nil, PlanInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.False(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "Use start_index=5000 to continue reading")
}
