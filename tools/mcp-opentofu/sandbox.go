package main

import (
	"errors"
	"fmt"
	"os/exec"
)

// ErrSandbox is returned for sandbox setup failures: an unsupported
// [SandboxMode], missing platform tools (such as bwrap on Linux), or a
// platform that has no sandbox backend.
var ErrSandbox = errors.New("sandbox")

// SandboxMode controls how [New] picks a [Sandbox] implementation.
type SandboxMode string

const (
	// SandboxAuto picks the platform-appropriate backend (seatbelt on
	// darwin, bwrap on linux) and falls back to [noopSandbox] elsewhere
	// with a startup warning.
	SandboxAuto SandboxMode = "auto"

	// SandboxOn forces a real sandbox backend. [New] returns an error
	// when no backend is available for the current platform.
	SandboxOn SandboxMode = "on"

	// SandboxOff disables sandboxing entirely. Intended for debugging
	// the binary by hand; the Nix-built wrapper never sets this.
	SandboxOff SandboxMode = "off"
)

// Sandbox wraps an [*exec.Cmd] so its eventual [*exec.Cmd.Run] runs under
// the platform's process-level sandbox. Implementations must rewrite the
// command's Path and Args (and may set Env) in place; subsequent
// configuration of the cmd by the caller (Stdout/Stderr/Cancel/WaitDelay)
// must remain effective.
//
// The policy parameter carries the per-tool [Policy] merged with any
// per-call additions. Implementations may assume every entry in
// policy.AllowRead, policy.AllowWrite, and policy.AllowUnixSockets is
// already an absolute, symlink-resolved path.
type Sandbox interface {
	// Wrap rewrites cmd in place so it runs under the platform sandbox
	// using policy. The command's working directory (cmd.Dir) is treated
	// as a read-write bind into the sandbox.
	Wrap(cmd *exec.Cmd, policy Policy) error

	// Name returns a short identifier ("seatbelt", "bwrap", "noop") used
	// in startup logs.
	Name() string
}

// ParseSandboxMode validates s and returns the canonical [SandboxMode].
// Unknown values produce an error listing the accepted modes.
func ParseSandboxMode(s string) (SandboxMode, error) {
	switch SandboxMode(s) {
	case SandboxAuto, SandboxOn, SandboxOff:
		return SandboxMode(s), nil
	default:
		return "", fmt.Errorf("%w: unknown sandbox mode %q (want auto, on, or off)", ErrSandbox, s)
	}
}

// New returns a [Sandbox] for the given [SandboxMode]. The platform-specific
// constructors live in sandbox_darwin.go, sandbox_linux.go, and
// sandbox_other.go; they share this entry point so the rest of the binary
// remains platform-agnostic.
func New(mode SandboxMode) (Sandbox, error) {
	switch mode {
	case SandboxAuto, SandboxOn, SandboxOff:
		return newPlatformSandbox(mode)

	default:
		return nil, fmt.Errorf("%w: unknown sandbox mode %q (want auto, on, or off)", ErrSandbox, mode)
	}
}

// noopSandbox is the unsandboxed [Sandbox] used for --sandbox=off and as
// the auto fallback on platforms with no real backend. It leaves cmd
// untouched.
type noopSandbox struct{}

// Wrap is a no-op.
func (noopSandbox) Wrap(_ *exec.Cmd, _ Policy) error { return nil }

// Name returns "noop".
func (noopSandbox) Name() string { return "noop" }
