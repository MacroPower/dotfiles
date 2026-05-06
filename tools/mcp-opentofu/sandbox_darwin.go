//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// newPlatformSandbox returns the Darwin sandbox backend (sandbox-exec).
// See [New] for the high-level mode contract.
func newPlatformSandbox(mode SandboxMode) (Sandbox, error) {
	if mode == SandboxOff {
		return noopSandbox{}, nil
	}

	path, err := exec.LookPath("sandbox-exec")
	if err != nil {
		if mode == SandboxOn {
			return nil, fmt.Errorf("%w: sandbox-exec not found in PATH: %w", ErrSandbox, err)
		}

		return noopSandbox{}, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("%w: resolving home directory: %w", ErrSandbox, err)
	}

	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}

	s := &darwinSandbox{
		bin:         path,
		tmp:         tmp,
		terraformrc: filepath.Join(home, ".terraformrc"),
		terraformd:  filepath.Join(home, ".terraform.d"),
		pluginCache: filepath.Join(home, ".terraform.d", "plugin-cache"),
	}

	s.staticProfile = s.renderStaticProfile()

	return s, nil
}

// darwinSandbox is the macOS [Sandbox] backed by sandbox-exec. The profile
// is generated per-call so per-tool [Policy] entries can vary; the
// static prefix (system-read allowlist, syscall allows, ~/.terraform.d
// binds) is rendered once at construction.
type darwinSandbox struct {
	bin           string
	tmp           string
	terraformrc   string
	terraformd    string
	pluginCache   string
	staticProfile string
}

// Wrap rewrites cmd to run under sandbox-exec with a freshly built
// profile. The original command path becomes the profile's first
// argument and original args follow.
func (s *darwinSandbox) Wrap(cmd *exec.Cmd, policy Policy) error {
	if cmd.Dir == "" {
		return fmt.Errorf("%w: darwin sandbox requires cmd.Dir", ErrSandbox)
	}

	if !filepath.IsAbs(cmd.Dir) {
		return fmt.Errorf("%w: cmd.Dir %q must be absolute", ErrSandbox, cmd.Dir)
	}

	profile := s.buildProfile(cmd.Dir, policy)

	newArgs := []string{"sandbox-exec", "-p", profile, cmd.Path}
	if len(cmd.Args) > 1 {
		newArgs = append(newArgs, cmd.Args[1:]...)
	}

	cmd.Path = s.bin
	cmd.Args = newArgs

	return nil
}

// Name returns "seatbelt".
func (s *darwinSandbox) Name() string { return "seatbelt" }

// renderStaticProfile builds the policy-independent prefix once at
// construction so per-call [*darwinSandbox.buildProfile] only emits the
// pieces that vary by [Policy].
func (s *darwinSandbox) renderStaticProfile() string {
	var b strings.Builder

	b.WriteString(staticProfileHeader)

	systemReads := []string{
		"/usr", "/System", "/Library",
		"/private/etc", "/private/var/db/dyld",
		"/nix",
	}
	for _, p := range systemReads {
		fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", quote(p))
	}

	fmt.Fprintf(&b, "(allow file-read* (literal %s))\n", quote(s.terraformrc))
	fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", quote(s.terraformd))
	fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", quote(s.pluginCache))
	fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", quote(s.tmp))

	return b.String()
}

// buildProfile renders the sandbox-exec profile for a single tofu
// invocation. The structure is intentionally explicit (no clever loops
// over heterogeneous lists) so the generated S-expression is greppable
// when debugging a permission denial.
func (s *darwinSandbox) buildProfile(workdir string, policy Policy) string {
	var b strings.Builder

	b.WriteString(s.staticProfile)

	fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", quote(workdir))

	for _, p := range policy.AllowWrite {
		fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", quote(p))
	}

	for _, p := range policy.AllowRead {
		fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", quote(p))
	}

	if len(policy.AllowedDomains) == 0 {
		b.WriteString("(deny network*)\n")
	} else {
		b.WriteString("(allow network-outbound (remote host \"localhost\"))\n")
		for _, d := range policy.AllowedDomains {
			fmt.Fprintf(&b, "(allow network-outbound (remote host %s))\n", quote(d))
		}
		b.WriteString("(allow network-bind (local ip \"localhost:*\"))\n")
	}

	// AllowUnixSockets must follow the network deny so its allows are not
	// shadowed by the (deny network*) clause emitted in the no-domains
	// branch. The right operator for unix sockets is
	// network-outbound (literal ...), not network*.
	for _, p := range policy.AllowUnixSockets {
		fmt.Fprintf(&b, "(allow network-outbound (literal %s))\n", quote(p))
	}

	return b.String()
}

// staticProfileHeader is the policy-independent prefix of every
// generated sandbox-exec profile: the version pragma, default-deny, and
// syscall allows that every tofu invocation needs.
const staticProfileHeader = `(version 1)
(deny default)
(allow process-fork)
(allow process-exec)
(allow signal (target self))
(allow mach-lookup)
(allow ipc-posix-shm)
(allow sysctl-read)
(allow file-read-metadata)
`

// quoteReplacer escapes the two characters that need escaping inside a
// sandbox-exec string literal: backslash and double quote.
var quoteReplacer = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// quote produces a sandbox-exec-safe quoted string. The profile language
// is a Scheme-style S-expression and only requires escaping double
// quotes and backslashes inside string literals.
func quote(s string) string {
	return `"` + quoteReplacer.Replace(s) + `"`
}
