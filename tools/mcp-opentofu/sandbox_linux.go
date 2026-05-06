//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// newPlatformSandbox returns the Linux sandbox backend (bwrap). See [New]
// for the high-level mode contract.
func newPlatformSandbox(mode SandboxMode) (Sandbox, error) {
	if mode == SandboxOff {
		return noopSandbox{}, nil
	}

	path, err := exec.LookPath("bwrap")
	if err != nil {
		if mode == SandboxOn {
			return nil, fmt.Errorf("%w: bwrap not found in PATH: %w", ErrSandbox, err)
		}

		return noopSandbox{}, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("%w: resolving home directory: %w", ErrSandbox, err)
	}

	s := &linuxSandbox{
		bin:         path,
		home:        home,
		terraformrc: filepath.Join(home, ".terraformrc"),
		terraformd:  filepath.Join(home, ".terraform.d"),
		pluginCache: filepath.Join(home, ".terraform.d", "plugin-cache"),
	}

	s.staticPrefix = s.renderStaticPrefix()

	return s, nil
}

// passthroughEnv lists environment variable names whose values are
// forwarded into the sandbox when set in the parent process. The
// selection covers tofu-specific knobs and the common CA-bundle
// indirections used by Nix-built binaries; --clearenv erases the rest.
var passthroughEnv = []string{
	"TF_DATA_DIR",
	"TF_PLUGIN_CACHE_DIR",
	"TF_CLI_CONFIG_FILE",
	"TF_LOG",
	"TF_LOG_PATH",
	"CHECKPOINT_DISABLE",
	"LD_LIBRARY_PATH",
	"NIX_SSL_CERT_FILE",
	"SSL_CERT_FILE",
	"CURL_CA_BUNDLE",
	"GIT_SSL_CAINFO",
	"REQUESTS_CA_BUNDLE",
}

// linuxSandbox is the Linux [Sandbox] backed by bwrap.
type linuxSandbox struct {
	bin          string
	home         string
	terraformrc  string
	terraformd   string
	pluginCache  string
	staticPrefix []string
}

// Wrap rewrites cmd to run under bwrap. Original args follow the bwrap
// argv after a `--` separator.
func (s *linuxSandbox) Wrap(cmd *exec.Cmd, policy Policy) error {
	if cmd.Dir == "" {
		return fmt.Errorf("%w: linux sandbox requires cmd.Dir", ErrSandbox)
	}

	if !filepath.IsAbs(cmd.Dir) {
		return fmt.Errorf("%w: cmd.Dir %q must be absolute", ErrSandbox, cmd.Dir)
	}

	args := s.buildArgs(cmd.Dir, cmd.Path, cmd.Args, policy)

	cmd.Path = s.bin
	cmd.Args = args

	return nil
}

// Name returns "bwrap".
func (s *linuxSandbox) Name() string { return "bwrap" }

// renderStaticPrefix builds the policy-independent argv prefix once at
// construction. The selection of `--setenv`, `--ro-bind*`, `--proc`,
// `--dev`, `--tmpfs`, and the ~/.terraform.d binds is fixed for the
// process lifetime; only the workdir, per-policy paths, and tail
// arguments vary per call.
func (s *linuxSandbox) renderStaticPrefix() []string {
	a := []string{
		"bwrap", "--clearenv",
		"--setenv", "HOME", s.home,
		"--setenv", "PATH", "/usr/bin:/bin:/usr/local/bin:/run/current-system/sw/bin",
		"--setenv", "USER", currentUser(),
		"--setenv", "TMPDIR", "/tmp",
		"--setenv", "LANG", "C.UTF-8",
	}

	for _, name := range passthroughEnv {
		val, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		a = append(a, "--setenv", name, val)
	}

	a = append(a,
		"--ro-bind", "/usr", "/usr",
		"--ro-bind-try", "/etc", "/etc",
		"--ro-bind-try", "/nix", "/nix",
		"--ro-bind-try", "/run/current-system", "/run/current-system",
		"--ro-bind-try", "/bin", "/bin",
		"--ro-bind-try", "/lib", "/lib",
		"--ro-bind-try", "/lib64", "/lib64",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		// ~/.terraform.d ro must precede the rw plugin-cache bind so the
		// later mount shadows the cache subpath without shadowing the
		// rest of the directory.
		"--ro-bind-try", s.terraformrc, s.terraformrc,
		"--ro-bind-try", s.terraformd, s.terraformd,
		"--bind-try", s.pluginCache, s.pluginCache,
	)

	return a
}

// buildArgs assembles the bwrap argv. The static prefix is reused from
// [*linuxSandbox.renderStaticPrefix]; only workdir, per-policy paths,
// and tail args are appended per call.
func (s *linuxSandbox) buildArgs(workdir, origPath string, origArgs []string, policy Policy) []string {
	a := append([]string(nil), s.staticPrefix...)
	a = append(a, "--bind", workdir, workdir)

	for _, p := range policy.AllowWrite {
		a = append(a, "--bind", p, p)
	}

	for _, p := range policy.AllowRead {
		a = append(a, "--ro-bind", p, p)
	}

	for _, p := range policy.AllowUnixSockets {
		a = append(a, "--bind-try", p, p)
	}

	a = append(a, "--unshare-pid", "--unshare-user", "--unshare-uts", "--unshare-ipc", "--unshare-cgroup-try")
	if len(policy.AllowedDomains) == 0 {
		a = append(a, "--unshare-net")
	} else {
		a = append(a, "--share-net")
	}

	a = append(a, "--die-with-parent", "--chdir", workdir, "--", origPath)
	if len(origArgs) > 1 {
		a = append(a, origArgs[1:]...)
	}

	return a
}

// currentUser returns the value bwrap should advertise as USER inside the
// sandbox. The bind mounts assume the host's home path layout so
// preserving the host username avoids surprises in tools that expand
// ~ via $USER.
func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}

	return "user"
}
