//go:build sandbox_integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSandboxValidateNoNetwork drives a real sandbox-exec / bwrap against a
// fixture workdir with `tofu validate -json`. The test is gated behind
// the sandbox_integration build tag so the unit suite stays
// platform-portable; run with `go test -tags sandbox_integration ./...`.
func TestSandboxValidateNoNetwork(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Minimal valid HCL: an empty terraform block parses without
	// downloading anything.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.tf"),
		[]byte(`terraform {}`+"\n"), 0o644))

	sandbox, err := New(SandboxOn)
	require.NoError(t, err)

	tofuPath := os.Getenv("TOFU_BIN")
	if tofuPath == "" {
		tofuPath = "tofu"
	}

	executor := newExecTofu(tofuPath, sandbox)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	stdout, stderr, code, err := executor.Run(ctx, dir, Policy{}, "validate", "-json", "-no-color")
	require.NoError(t, err, "stderr: %s", string(stderr))
	assert.Equal(t, 0, code, "stdout: %s\nstderr: %s", string(stdout), string(stderr))
}

func TestSandboxRejectsNetworkDeniedInit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.tf"),
		[]byte(`terraform { required_providers { random = { source = "hashicorp/random" } } }`+"\n"),
		0o644))

	sandbox, err := New(SandboxOn)
	require.NoError(t, err)

	tofuPath := os.Getenv("TOFU_BIN")
	if tofuPath == "" {
		tofuPath = "tofu"
	}

	executor := newExecTofu(tofuPath, sandbox)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	_, stderr, code, _ := executor.Run(ctx, dir, Policy{},
		"init", "-input=false", "-no-color", "-backend=false")
	assert.NotEqual(t, 0, code, "init must fail when no domains are allowed; stderr: %s", string(stderr))
}
