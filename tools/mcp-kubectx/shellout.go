package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Sentinel errors for shell-out diagnostics. Wrapping these makes
// unit tests robust to platform-specific exec error text.
var (
	ErrHostExec    = errors.New("host subcommand")
	ErrHostExecRun = errors.New("invoke host subcommand")
)

// guestEnvVar is the environment variable workmux sets inside its
// sandbox guest. handler.isGuest reads this through envLookup.
const guestEnvVar = "WM_SANDBOX_GUEST"

// hostExecArgs returns the (command, argv) pair to run a `host *`
// subcommand. On the macOS host the binary is invoked at its own
// absolute path. Inside a Lima guest it goes through workmux
// host-exec, which forwards the argv to the host-side mcp-kubectx
// binary (resolved via the workmux host_commands allowlist + PATH).
//
// Reads h.envLookup so tests can flip the guest bit without mutating
// process env. Only handler-bound code paths use this helper; the
// host * subcommand entry points have no handler instance and so
// cannot recurse.
func (h *handler) hostExecArgs(sub string, args []string) (string, []string, error) {
	full := append([]string{"host", sub}, args...)

	if h.isGuest() {
		argv := append([]string{"host-exec", "mcp-kubectx"}, full...)
		return "workmux", argv, nil
	}

	self, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("resolve executable path: %w", err)
	}

	return self, full, nil
}

// isGuest reports whether the serve process is running inside a
// workmux sandbox guest. The decision uses the injected envLookup
// so unit tests can drive both branches deterministically.
func (h *handler) isGuest() bool {
	lookup := h.envLookup
	if lookup == nil {
		lookup = os.Getenv
	}

	return lookup(guestEnvVar) == "1"
}

// runHostFunc is the contract used by handler.list and
// handler.selectCtx to shell out. It is a field on handler so
// tests can substitute a fake that records calls and returns
// canned stdout/stderr without touching the filesystem.
type runHostFunc func(ctx context.Context, sub string, args []string) (stdout []byte, err error)

// defaultRunHost forks the mcp-kubectx binary (or `workmux
// host-exec mcp-kubectx ...` when in a guest) and captures its
// stdout. On non-zero exit the returned error embeds the captured
// stderr so the MCP layer can surface it via toolError.
func (h *handler) defaultRunHost(ctx context.Context, sub string, args []string) ([]byte, error) {
	cmd, argv, err := h.hostExecArgs(sub, args)
	if err != nil {
		return nil, err
	}

	// cmd is either os.Executable() or the literal "workmux"; argv
	// is built from controlled inputs, not user-supplied strings.
	c := exec.CommandContext(ctx, cmd, argv...) //nolint:gosec // see comment

	var stdout, stderr bytes.Buffer

	c.Stdout = &stdout
	c.Stderr = &stderr

	err = c.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.Bytes(), fmt.Errorf(
				"%w %q: exit %d: %s",
				ErrHostExec, sub, exitErr.ExitCode(), bytes.TrimSpace(stderr.Bytes()),
			)
		}

		return stdout.Bytes(), fmt.Errorf("%w %q: %w", ErrHostExecRun, sub, err)
	}

	return stdout.Bytes(), nil
}
