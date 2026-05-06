package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCall records a single invocation captured by [fakeExecutor].
type fakeCall struct {
	dir  string
	args []string
}

// fakeResponse is one canned reply returned by [fakeExecutor.Run] in order.
type fakeResponse struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

// fakeExecutor is a [tofuExecutor] that returns canned responses and records
// each call. Use it via newFakeExecutor.
type fakeExecutor struct {
	responses []fakeResponse
	calls     []fakeCall
}

func newFakeExecutor(responses ...fakeResponse) *fakeExecutor {
	return &fakeExecutor{responses: responses}
}

func (f *fakeExecutor) Run(ctx context.Context, dir string, args ...string) ([]byte, []byte, int, error) {
	ctxErr := ctx.Err()
	if ctxErr != nil {
		return nil, nil, -1, fmt.Errorf("fakeExecutor: %w", ctxErr)
	}

	f.calls = append(f.calls, fakeCall{dir: dir, args: append([]string(nil), args...)})

	if len(f.calls) > len(f.responses) {
		return nil, nil, -1, errors.New("fakeExecutor: unexpected extra call")
	}

	r := f.responses[len(f.calls)-1]

	return []byte(r.stdout), []byte(r.stderr), r.exitCode, r.err
}

// newTofuTestHandler returns a [*handler] wired with a discard logger and
// the given [*fakeExecutor]. It is shared by the validate, init, and plan
// handler tests.
func newTofuTestHandler(t *testing.T, fake *fakeExecutor) *handler {
	t.Helper()

	return &handler{
		log:  slog.New(slog.DiscardHandler),
		tofu: fake,
	}
}

// validJSON is the canned `tofu validate -json` body for a clean run.
const validJSON = `{
  "format_version": "1.0",
  "valid": true,
  "error_count": 0,
  "warning_count": 0,
  "diagnostics": []
}`

// errorJSON is a `tofu validate -json` body with one error diagnostic.
const errorJSON = `{
  "format_version": "1.0",
  "valid": false,
  "error_count": 1,
  "warning_count": 0,
  "diagnostics": [
    {
      "severity": "error",
      "summary": "Missing required argument",
      "detail": "The argument \"region\" is required, but no definition was found.",
      "range": {
        "filename": "main.tf",
        "start": {"line": 4, "column": 1, "byte": 42},
        "end":   {"line": 4, "column": 10, "byte": 51}
      }
    }
  ]
}`

// warningJSON is a `tofu validate -json` body with one warning diagnostic.
const warningJSON = `{
  "format_version": "1.0",
  "valid": true,
  "error_count": 0,
  "warning_count": 1,
  "diagnostics": [
    {
      "severity": "warning",
      "summary": "Deprecated attribute",
      "detail": "Attribute \"foo\" is deprecated.",
      "range": {
        "filename": "vars.tf",
        "start": {"line": 7, "column": 3, "byte": 80},
        "end":   {"line": 7, "column": 6, "byte": 83}
      }
    }
  ]
}`

func TestHandleValidateSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stdout: validJSON})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.False(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "**Valid**: true")
	assert.Contains(t, text, "succeeded with no issues")

	require.Len(t, fake.calls, 1)
	assert.Equal(t, dir, fake.calls[0].dir)
	assert.Equal(t, []string{"validate", "-json", "-no-color"}, fake.calls[0].args)
}

func TestHandleValidateWithErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stdout: errorJSON, exitCode: 1})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "**Valid**: false")
	assert.Contains(t, text, "**Errors**: 1")
	assert.Contains(t, text, "### Errors")
	assert.Contains(t, text, "`main.tf:4`")
	assert.Contains(t, text, "Missing required argument")
	assert.Contains(t, text, `argument "region" is required`)
}

func TestHandleValidateWithWarnings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stdout: warningJSON})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "**Warnings**: 1")
	assert.Contains(t, text, "### Warnings")
	assert.Contains(t, text, "`vars.tf:7`")
	assert.Contains(t, text, "Deprecated attribute")
	assert.NotContains(t, text, "### Errors")
}

func TestHandleValidateInputErrors(t *testing.T) {
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

			r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{WorkingDirectory: tt.dir})
			require.NoError(t, err)
			require.NotNil(t, r)
			assert.True(t, r.IsError, resultText(t, r))
			assert.Contains(t, resultText(t, r), tt.want)
			assert.Empty(t, fake.calls, "executor must not be invoked for input errors")
		})
	}
}

func TestHandleValidateBinaryNotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{err: exec.ErrNotFound})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError)
	assert.Contains(t, resultText(t, r), "tofu binary not found in PATH")
	assert.Contains(t, resultText(t, r), "--tofu-bin")
}

func TestHandleValidateInitFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stderr:   "Error: Failed to install provider",
		exitCode: 1,
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{
		WorkingDirectory: dir,
		Init:             true,
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.True(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "'tofu init' exited with code 1")
	assert.Contains(t, text, "Failed to install provider")

	require.Len(t, fake.calls, 1, "validate must not be invoked when init fails")
	assert.Equal(t, []string{"init", "-input=false", "-no-color", "-backend=false"}, fake.calls[0].args)
}

func TestHandleValidateInitThenValidate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(
		fakeResponse{stdout: "Initialized!", exitCode: 0},
		fakeResponse{stdout: validJSON, exitCode: 0},
	)
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{
		WorkingDirectory: dir,
		Init:             true,
	})
	require.NoError(t, err)
	require.False(t, r.IsError, resultText(t, r))
	assert.Contains(t, resultText(t, r), "succeeded with no issues")

	require.Len(t, fake.calls, 2)
	assert.Equal(t, []string{"init", "-input=false", "-no-color", "-backend=false"}, fake.calls[0].args)
	assert.Equal(t, []string{"validate", "-json", "-no-color"}, fake.calls[1].args)
	assert.Equal(t, dir, fake.calls[0].dir)
	assert.Equal(t, dir, fake.calls[1].dir)
}

func TestHandleValidateMalformedJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{
		stdout:   "this is not json",
		stderr:   "warning: noisy",
		exitCode: 0,
	})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.False(t, r.IsError, "parse-fallback must not surface as IsError=true")

	text := resultText(t, r)
	assert.Contains(t, text, "Could not parse")
	assert.Contains(t, text, "this is not json")
	assert.Contains(t, text, "warning: noisy")
}

func TestHandleValidateContextCanceled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{err: context.Canceled})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{WorkingDirectory: dir})
	require.Nil(t, r)
	require.ErrorIs(t, err, context.Canceled)
}

func TestHandleValidatePagination(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", defaultMaxLength*2)
	body := `{"format_version":"1.0","valid":false,"error_count":1,"warning_count":0,"diagnostics":[{"severity":"error","summary":"` + long + `","detail":""}]}`

	dir := t.TempDir()
	fake := newFakeExecutor(fakeResponse{stdout: body})
	h := newTofuTestHandler(t, fake)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.False(t, r.IsError)

	text := resultText(t, r)
	assert.Contains(t, text, "Use start_index=5000 to continue reading")
}
