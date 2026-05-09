//go:build e2e

package main

import (
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tofuBinaryE2E resolves the tofu binary path for e2e tests. TOFU_BIN wins;
// otherwise the path comes from [exec.LookPath]. The test is skipped when
// neither is set so the suite stays runnable on hosts without OpenTofu.
func tofuBinaryE2E(t *testing.T) string {
	t.Helper()

	if p := os.Getenv("TOFU_BIN"); p != "" {
		return p
	}

	p, err := exec.LookPath("tofu")
	if err != nil {
		t.Skip("tofu binary not available; set TOFU_BIN or install tofu")
	}

	return p
}

// copyTestdataE2E recursively copies testdata/<name>/ into a fresh
// [testing.T.TempDir]. tofu init writes .terraform/ and .terraform.lock.hcl
// into the working directory, so each test must operate on its own copy of
// the fixture.
func copyTestdataE2E(t *testing.T, name string) string {
	t.Helper()

	src := filepath.Join("testdata", name)
	dst := t.TempDir()

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()

		_, err = io.Copy(out, in)
		return err
	})
	require.NoError(t, err, "copying testdata/%s", name)

	return dst
}

// newE2EHandler builds a [*handler] wired with a real [*execTofu] under
// [SandboxOff]. The sandbox layer is exercised separately by
// sandbox_integration_test.go; here we want to drive the handler glue
// against a real tofu binary.
//
// allowRoot is left zero: it is only consulted by [validateExtraPath] when
// the caller passes [InitInput.AllowedPaths] / [ValidateInput.AllowedPaths]
// / [TestInput.AllowedPaths], and the e2e tests do not. The
// MCP_OPENTOFU_ALLOW_ROOT environment variable in the developer's shell is
// likewise irrelevant because we skip [resolveAllowRoot] entirely.
func newE2EHandler(t *testing.T, tofuPath string) *handler {
	t.Helper()

	sb, err := New(SandboxOff)
	require.NoError(t, err)

	return &handler{
		log:      slog.New(slog.DiscardHandler),
		tofu:     newExecTofu(tofuPath, sb),
		policies: Defaults(),
	}
}

func TestE2EHandleInit(t *testing.T) {
	t.Parallel()

	tofuPath := tofuBinaryE2E(t)
	dir := copyTestdataE2E(t, "basic")
	h := newE2EHandler(t, tofuPath)

	r, _, err := h.handleInit(t.Context(), nil, InitInput{WorkingDirectory: dir})
	require.NoError(t, err)
	require.NotNil(t, r)
	require.False(t, r.IsError, resultText(t, r))

	assert.Contains(t, resultText(t, r), "OpenTofu initialization succeeded")
}

func TestE2EHandleValidate(t *testing.T) {
	t.Parallel()

	tofuPath := tofuBinaryE2E(t)
	dir := copyTestdataE2E(t, "basic")
	h := newE2EHandler(t, tofuPath)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{
		WorkingDirectory: dir,
		Init:             true,
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	require.False(t, r.IsError, resultText(t, r))

	assert.Contains(t, resultText(t, r), "OpenTofu validation succeeded with no issues.")
}

func TestE2EHandleTest(t *testing.T) {
	t.Parallel()

	tofuPath := tofuBinaryE2E(t)
	dir := copyTestdataE2E(t, "basic")
	h := newE2EHandler(t, tofuPath)

	r, _, err := h.handleTest(t.Context(), nil, TestInput{
		WorkingDirectory: dir,
		Init:             true,
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	require.False(t, r.IsError, resultText(t, r))

	assert.Contains(t, resultText(t, r), "OpenTofu test exited with code 0.")
}

// TestE2EValidateInvalidConfig writes a config that parses but references an
// undeclared variable. tofu validate -json exits non-zero with parseable
// JSON; the handler renders the diagnostics as data (not a tool error)
// because classifyMissingBinary only matches missing-binary stderr.
func TestE2EValidateInvalidConfig(t *testing.T) {
	t.Parallel()

	tofuPath := tofuBinaryE2E(t)
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "main.tf"),
		[]byte(`output "x" { value = var.does_not_exist }`+"\n"),
		0o644,
	))

	h := newE2EHandler(t, tofuPath)

	r, _, err := h.handleValidate(t.Context(), nil, ValidateInput{
		WorkingDirectory: dir,
		Init:             true,
	})
	require.NoError(t, err)
	require.NotNil(t, r)
	require.False(t, r.IsError, resultText(t, r))

	text := resultText(t, r)
	assert.Contains(t, text, "**Valid**: false")
	assert.Contains(t, text, "### Errors")
	assert.Contains(t, text, "Reference to undeclared")
}
