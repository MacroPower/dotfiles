//go:build sandbox_integration

package main

// These tests exercise the sandbox network gate end-to-end. The Linux and
// Darwin backends enforce networking with different mechanisms:
//
//   - Linux (bwrap): empty AllowedDomains -> --unshare-net (fresh netns,
//     loopback down). Non-empty -> --share-net (host network, no
//     per-domain filter at this layer).
//
//   - Darwin (sandbox-exec): empty AllowedDomains -> (deny network*).
//     Non-empty -> per-domain network-outbound allows plus an explicit
//     localhost allow.
//
// "Blocks localhost when no domains" therefore means different things per
// platform: on Linux the connect attempt never reaches the host (the
// sandbox netns has its own, down loopback); on Darwin the syscall is
// denied. The shared assertion is "curl exits non-zero".
//
// curl resolution: NixOS hosts (used by the dotfiles dev container) keep
// curl under /nix/store/...; macOS keeps it under /usr/bin. The probe
// realpath must lie under one of the sandbox's bind-mounted roots.

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveCurl returns an absolute curl path whose realpath lies under one
// of the always-bound sandbox roots for the current platform. Skips the
// caller when curl is missing or out of reach.
func resolveCurl(t *testing.T) string {
	t.Helper()

	path, err := exec.LookPath("curl")
	if err != nil {
		t.Skipf("curl not in PATH: %v", err)
	}

	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Skipf("resolving curl symlinks: %v", err)
	}

	roots := curlBoundRoots()
	for _, root := range roots {
		if real == root || strings.HasPrefix(real, root+string(filepath.Separator)) {
			return real
		}
	}

	t.Skipf("curl realpath %q is not under any sandbox-bound root %v", real, roots)

	return ""
}

func curlBoundRoots() []string {
	switch runtime.GOOS {
	case "linux":
		return []string{"/usr", "/bin", "/lib", "/lib64", "/nix", "/run/current-system"}
	case "darwin":
		return []string{"/usr", "/nix"}
	default:
		return nil
	}
}

// startLoopbackServer stands up an HTTP test server bound explicitly to
// 127.0.0.1 and returns a URL targeting that literal IP. Avoiding the
// "localhost" hostname sidesteps systems where the resolver prefers ::1
// while the listener is tcp4-only. The Darwin sandbox-exec
// (remote host "localhost") predicate matches connections to 127.0.0.1
// (and ::1) regardless of how the destination is spelled at the URL
// layer, so the literal IP still satisfies the policy allow.
func startLoopbackServer(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	return "http://" + ln.Addr().String() + "/"
}

// TestSandboxNetworkSanity verifies the test environment itself: curl is
// reachable, the loopback server answers, and no transparent proxy is
// rewriting requests. Failures here are environmental, not sandbox bugs.
func TestSandboxNetworkSanity(t *testing.T) {
	t.Parallel()

	curl := resolveCurl(t)
	url := startLoopbackServer(t)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, curl,
		"--silent", "--connect-timeout", "2", "--max-time", "5",
		"-o", "/dev/null", "-w", "%{http_code}", url,
	)
	cmd.Dir = t.TempDir()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoErrorf(t, cmd.Run(), "stderr: %s", stderr.String())
	assert.Equal(t, "200", stdout.String())
}

// TestSandboxNetworkPolicy probes the sandbox network gate with curl: an
// empty AllowedDomains policy must block connections, any non-empty policy
// must permit localhost.
func TestSandboxNetworkPolicy(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		policy Policy
		wantOK bool
	}{
		"blocks when no domains": {
			policy: Policy{},
			wantOK: false,
		},
		"allows localhost when any domain set": {
			policy: Policy{AllowedDomains: []string{"example.com"}},
			wantOK: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			curl := resolveCurl(t)

			s, err := New(SandboxOn)
			require.NoError(t, err)

			if s.Name() == "noop" {
				t.Skip("sandbox is noop on this host")
			}

			url := startLoopbackServer(t)

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, curl,
				"--silent", "--connect-timeout", "2", "--max-time", "5",
				"-o", "/dev/null", "-w", "%{http_code}", url,
			)
			cmd.Dir = t.TempDir()

			require.NoError(t, s.Wrap(cmd, tc.policy))

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			runErr := cmd.Run()

			if tc.wantOK {
				assert.NoErrorf(t, runErr, "expected curl to succeed; stderr: %s", stderr.String())
				assert.Equal(t, "200", stdout.String())
			} else {
				assert.Errorf(t, runErr, "expected curl to fail; stdout: %q stderr: %q",
					stdout.String(), stderr.String())
			}
		})
	}
}

// TestSandboxInitFetchesProvider runs `tofu init` against a real provider
// download with the registry domains allowed. Gated behind
// TOFU_SANDBOX_LIVE=1 so CI without internet does not flake. Skipped on
// Darwin: the Darwin sandbox pins (remote host) to IPs at policy-load
// time, and CDN-fronted hosts like objects.githubusercontent.com flake by
// design (see policy.go AllowedDomains disclaimer). Linux --share-net has
// no such limitation.
func TestSandboxInitFetchesProvider(t *testing.T) {
	t.Parallel()

	if os.Getenv("TOFU_SANDBOX_LIVE") != "1" {
		t.Skip("set TOFU_SANDBOX_LIVE=1 to run live registry test")
	}

	if runtime.GOOS != "linux" {
		t.Skipf("live registry test is Linux-only (Darwin sandbox pins domains to IPs); GOOS=%s", runtime.GOOS)
	}

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

	policy := Policy{AllowedDomains: []string{
		"registry.opentofu.org",
		"github.com",
		"objects.githubusercontent.com",
		"api.github.com",
	}}

	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()

	stdout, stderr, code, runErr := executor.Run(ctx, dir, policy,
		"init", "-input=false", "-no-color", "-backend=false")
	require.NoErrorf(t, runErr, "stderr: %s", string(stderr))
	assert.Equalf(t, 0, code, "stdout: %s\nstderr: %s", string(stdout), string(stderr))
	assert.DirExists(t, filepath.Join(dir, ".terraform"))
}
