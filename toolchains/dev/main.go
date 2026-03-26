// Reusable development container functions powered by nix home-manager.
// Provides pre-configured dev environments with tools and shell
// configuration derived from the dotfiles flake's homeConfigurations
// output.

package main

import (
	"context"
	"fmt"
	"strings"

	"dagger/dev/internal/dagger"
)

const (
	// nixImage is the pinned nixos/nix container image.
	nixImage = "nixos/nix:2.34.1@sha256:1d59121e0c361076b4f23c158d236702f2f045b3b477b51075b81ceb6188d34a"

	// devCacheNamespace is the namespace prefix for dev-specific cache volumes.
	devCacheNamespace = "go.jacobcolvin.com/dotfiles/toolchains/dev"

	// homeConfigFmt is the format string for home-manager configuration names.
	// The placeholder is filled with the Nix system string (e.g. "aarch64-linux").
	homeConfigFmt = "dev@%s"

	// golangciLintVersion is the golangci-lint image tag used by [Dev.CheckLint].
	golangciLintVersion = "v2.11" // renovate: datasource=github-releases depName=golangci/golangci-lint

	// username is the non-root user created inside the dev container.
	username = "dev"

	// homeDir is the home directory for the dev container user.
	homeDir = "/home/dev"

	// uid is the numeric user ID for the sandbox user.
	uid = "1000"

	// gid is the numeric group ID for the sandbox user.
	gid = "1000"

	// terrariumConfigPath is the XDG config path where terrarium
	// looks for its configuration YAML.
	terrariumConfigPath = homeDir + "/.config/terrarium/config.yaml"
)

// Dev provides reusable development container functions powered by
// nix home-manager. Create instances with [New].
type Dev struct {
	// Dotfiles source directory containing flake.nix, used to build
	// the nix home-manager environment.
	Source *dagger.Directory
}

// New creates a [Dev] module with the dotfiles source directory.
func New(
	// Dotfiles source directory containing flake.nix.
	// +defaultPath="/"
	// +ignore=["result", ".git", "toolchains/"]
	source *dagger.Directory,
) *Dev {
	return &Dev{Source: source}
}

// lintBase returns a golangci-lint container with source, caches, and
// the repo's linter configuration. When mod is non-empty and not ".",
// the container's working directory is set to the module subdirectory.
func (m *Dev) lintBase(mod string) *dagger.Container {
	src := dag.CurrentModule().Source()
	ctr := dag.Container().
		From("golangci/golangci-lint:"+golangciLintVersion).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume(devCacheNamespace+":modules")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume(devCacheNamespace+":build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithMountedDirectory("/src", src).
		WithFile("/src/.golangci.yaml", m.Source.File(".golangci.yaml")).
		WithWorkdir("/src").
		WithMountedCache("/root/.cache/golangci-lint",
			dag.CacheVolume(devCacheNamespace+":golangci-lint-"+golangciLintVersion))
	if mod != "" && mod != "." {
		ctr = ctr.WithWorkdir("/src/" + mod)
	}

	return ctr
}

// CheckLint verifies that golangci-lint passes on all Go modules in the
// toolchain.
//
// +check
func (m *Dev) CheckLint(ctx context.Context) error {
	cmd := []string{"golangci-lint", "run"}

	_, err := m.lintBase(".").WithExec(cmd).Sync(ctx)
	if err != nil {
		return fmt.Errorf("linting: %w", err)
	}

	return nil
}

// Format runs golangci-lint --fix across all Go modules in the toolchain
// and returns the merged changeset of auto-fixed source files. Changeset
// paths are prefixed with the module's location within the repo so that
// dagger generate applies fixes at the correct location (see
// dagger/dagger#11160).
//
// +generate
func (m *Dev) Format() *dagger.Changeset {
	return m.formatModule(".")
}

// formatModule runs golangci-lint --fix on a single module directory and
// returns the changeset with repo-root-relative paths.
func (m *Dev) formatModule(mod string) *dagger.Changeset {
	src := dag.CurrentModule().Source()

	outDir := "/src"
	if mod != "" && mod != "." {
		outDir = "/src/" + mod
	}

	// The modernize/newexpr pass panics with --fix on some packages
	// (golang.org/x/tools bug); disable it until golangci-lint ships
	// a fixed version. ReturnTypeAny tolerates non-zero exits from
	// unfixable lint issues while still applying available fixes.
	fixed := m.lintBase(mod).
		WithExec(
			[]string{"golangci-lint", "run", "--fix"},
			dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny},
		).
		Directory(outDir).
		// lintBase copies .golangci.yaml from the repo root into /src;
		// strip it so it does not appear as a new file in the changeset.
		WithoutFile(".golangci.yaml")

	if mod != "" && mod != "." {
		src = src.WithDirectory(mod, fixed)
	} else {
		src = fixed
	}

	// moduleSubpath is the module's location within the repo root.
	// Wrapping both sides at this prefix produces context-root-relative
	// changeset paths so dagger generate writes to the correct directory.
	const moduleSubpath = "toolchains/dev"

	original := dag.Directory().WithDirectory(moduleSubpath, dag.CurrentModule().Source())
	updated := dag.Directory().WithDirectory(moduleSubpath, src)

	return updated.Changes(original)
}

// nixSystem maps a Dagger container platform to a Nix system string.
func nixSystem(ctx context.Context, ctr *dagger.Container) (string, error) {
	plat, err := ctr.Platform(ctx)
	if err != nil {
		return "", fmt.Errorf("detecting platform: %w", err)
	}
	switch {
	case strings.Contains(string(plat), "arm64"):
		return "aarch64-linux", nil
	case strings.Contains(string(plat), "amd64"):
		return "x86_64-linux", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s", plat)
	}
}

// buildBase builds and activates the home-manager configuration on the
// given container. It detects the container platform and selects the
// matching home-manager config. Callers are expected to configure any
// cache mounts on ctr before calling this method.
func (m *Dev) buildBase(ctx context.Context, ctr *dagger.Container) (*dagger.Container, error) {
	sys, err := nixSystem(ctx, ctr)
	if err != nil {
		return nil, err
	}
	config := fmt.Sprintf(`.#homeConfigurations."%s".activationPackage`, fmt.Sprintf(homeConfigFmt, sys))
	return ctr.
		WithEnvVariable("NIX_CONFIG", "experimental-features = nix-command flakes\nfilter-syscalls = false\n").
		WithDirectory("/dotfiles", m.Source).
		WithWorkdir("/dotfiles").
		WithExec([]string{"nix", "build", config}).
		WithExec([]string{"mkdir", "-p", homeDir}).
		WithExec([]string{"mkdir", "-p", homeDir + "/.local/state/nix/profiles"}).
		WithExec([]string{"mkdir", "-p", "/nix/var/nix/profiles/per-user/" + username}).
		WithEnvVariable("HOME", homeDir).
		WithEnvVariable("USER", username).
		WithExec([]string{"./result/activate"}).
		WithEnvVariable("PATH",
			homeDir+"/.local/state/home-manager/gcroots/current-home/home-path/bin:"+
				homeDir+"/.nix-profile/bin:"+
				"/nix/var/nix/profiles/default/bin:"+
				"/usr/bin:/bin",
		).
		WithEnvVariable("EDITOR", "vim").
		WithEnvVariable("TERM", "xterm-256color").
		WithoutDirectory("/dotfiles").
		WithWorkdir(homeDir), nil
}

// cachedBuild builds the home-manager configuration with nix store and
// eval caches mounted, so repeated builds reuse prior work. The nix
// store and var directories share a single cache volume to keep them
// atomically consistent.
func (m *Dev) cachedBuild(ctx context.Context) (*dagger.Container, error) {
	ctr := dag.Container().From(nixImage)
	ctr = ctr.
		WithMountedCache("/nix", dag.CacheVolume(devCacheNamespace+":nix"),
			dagger.ContainerWithMountedCacheOpts{Source: ctr.Directory("/nix")}).
		WithMountedCache("/root/.cache/nix", dag.CacheVolume(devCacheNamespace+":nix-eval-cache"))

	return m.buildBase(ctx, ctr)
}

// DevBase returns a base development container with nix and home-manager
// tools activated but no project source mounted. Used by integration
// tests to verify tool availability without requiring an interactive
// terminal.
func (m *Dev) DevBase(
	ctx context.Context,
	// Kagi API key for the MCP server configuration. Injected as
	// KAGI_API_KEY during home-manager activation.
	// +optional
	kagiApiKey *dagger.Secret,
) (*dagger.Container, error) {
	built, err := m.cachedBuild(ctx)
	if err != nil {
		return nil, err
	}
	ctr := built.
		WithMountedCache(homeDir+"/.krew", dag.CacheVolume(devCacheNamespace+":krew"))

	if kagiApiKey != nil {
		plaintext, err := kagiApiKey.Plaintext(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading kagi api key: %w", err)
		}

		ctr = ctr.WithEnvVariable("KAGI_API_KEY", plaintext)
	}

	// setup-dev: create fish history symlink, persist claude.json to
	// cache volume, disable atuin systemd socket, create atuin data dir.
	setupScript := `set -e
mkdir -p ` + homeDir + `/.local/share/fish
ln -sf /commandhistory/fish_history ` + homeDir + `/.local/share/fish/fish_history
if [ ! -f /claude-state/claude.json ] && [ -f ` + homeDir + `/.claude.json ]; then
  cp ` + homeDir + `/.claude.json /claude-state/claude.json
fi
rm -f ` + homeDir + `/.claude.json
ln -sf /claude-state/claude.json ` + homeDir + `/.claude.json
sed -i 's/systemd_socket = true/systemd_socket = false/' ` + homeDir + `/.config/atuin/config.toml 2>/dev/null || true
mkdir -p ` + homeDir + `/.local/share/atuin
`

	return ctr.
		WithEnvVariable("IS_SANDBOX", "1").
		WithMountedCache("/claude-state", dag.CacheVolume(devCacheNamespace+":claude-state")).
		WithExec([]string{"sh", "-c", setupScript}), nil
}

// SandboxBase returns a development container with DNS-based domain
// filtering and an Envoy transparent SNI-filtering proxy. Only domains
// in the allowlist resolve (via dnsmasq) and pass through Envoy's
// filter chain matching. nftables redirects user traffic to Envoy,
// which checks TLS SNI (port 443) or HTTP Host (port 80) against the
// allowlist. The proxy, DNS, and firewall are applied at runtime via
// terrarium init, which also drops to a non-root user, preventing the
// sandboxed process from modifying the rules.
func (m *Dev) SandboxBase(
	ctx context.Context,
	// Kagi API key for the MCP server configuration.
	// +optional
	kagiApiKey *dagger.Secret,
	// YAML sandbox config file defining egress rules and firewall
	// options. Overrides the home-manager-deployed config when provided.
	// +optional
	sandboxConfig *dagger.File,
) (*dagger.Container, error) {
	ctr, err := m.DevBase(ctx, kagiApiKey)
	if err != nil {
		return nil, err
	}

	// Override the home-manager-deployed config if one was provided.
	if sandboxConfig != nil {
		ctr = ctr.WithFile(terrariumConfigPath, sandboxConfig)
	}

	// setup-user: create non-root user in /etc/passwd and /etc/group,
	// then chown the home directory.
	setupUserScript := fmt.Sprintf(`set -e
printf '%s:x:%s:%s::%s:/bin/sh\n' >> /etc/passwd
printf '%s:x:%s:\n' >> /etc/group
chown -R %s:%s %s
`, username, uid, gid, homeDir, username, gid, uid, gid, homeDir)

	return ctr.
		WithExec([]string{"sh", "-c", setupUserScript}).
		WithEnvVariable("IS_SANDBOX_NETWORK", "1"), nil
}

// Sandbox opens an interactive development container with DNS-based
// domain filtering and an Envoy transparent SNI-filtering proxy. Only
// allowed domains resolve and are reachable. The shell runs as a
// non-root user after nftables, dnsmasq, and Envoy setup.
//
// +cache="never"
func (m *Dev) Sandbox(
	ctx context.Context,
	// Source directory to mount in the dev container.
	// +optional
	// +ignore=[".git"]
	repoSource *dagger.Directory,
	// Git configuration directory (~/.config/git).
	// +optional
	gitConfig *dagger.Directory,
	// Timezone for the container (e.g. "America/New_York").
	// +optional
	tz string,
	// COLORTERM value (e.g. "truecolor").
	// +optional
	colorterm string,
	// TERM_PROGRAM value (e.g. "Apple_Terminal", "iTerm.app").
	// +optional
	termProgram string,
	// TERM_PROGRAM_VERSION value.
	// +optional
	termProgramVersion string,
	// Command to run in the terminal session. Defaults to ["fish"].
	// +optional
	cmd []string,
	// Kagi API key for the MCP server configuration.
	// +optional
	kagiApiKey *dagger.Secret,
	// YAML sandbox config file defining egress rules and firewall
	// options. Overrides the home-manager-deployed config when provided.
	// +optional
	sandboxConfig *dagger.File,
) (*dagger.Container, error) {
	ctr, err := m.SandboxBase(ctx, kagiApiKey, sandboxConfig)
	if err != nil {
		return nil, err
	}

	ctr = ctr.
		WithMountedCache("/commandhistory", dag.CacheVolume(devCacheNamespace+":shell-history"))

	if repoSource != nil {
		ctr = ctr.
			WithDirectory("/src", repoSource).
			WithWorkdir("/src")
	}

	ctr = applyDevConfig(ctr, gitConfig,
		tz, colorterm, termProgram, termProgramVersion)

	if len(cmd) == 0 {
		cmd = []string{"fish"}
	}

	initCmd := append([]string{"terrarium", "init", "--"}, wrapWithAtuinDaemon(cmd)...)
	ctr = ctr.Terminal(dagger.ContainerTerminalOpts{
		Cmd:                      initCmd,
		InsecureRootCapabilities: true,
	})

	return ctr, nil
}

// PublishShell builds a self-contained dev container and pushes it to a
// container registry. Unlike [Dev.DevBase], the published image bakes
// all nix store contents into image layers so it works without Dagger
// cache volumes.
func (m *Dev) PublishShell(
	ctx context.Context,
	// Registry password or personal access token.
	password *dagger.Secret,
	// Image tags. Defaults to ["latest"].
	// +optional
	tags []string,
	// Full image reference without tag.
	// +optional
	// +default="ghcr.io/macropower/shell"
	image string,
) (string, error) {
	if len(tags) == 0 {
		tags = []string{"latest"}
	}

	built, err := m.cachedBuild(ctx)
	if err != nil {
		return "", err
	}

	ctr := built.
		WithoutMount("/nix/store").
		WithoutMount("/nix/var/nix").
		WithoutMount("/root/.cache/nix").
		WithDirectory("/nix", built.Directory("/nix")).
		WithDirectory(homeDir, built.Directory(homeDir)).
		WithWorkdir(homeDir).
		WithEntrypoint([]string{"fish"}).
		WithRegistryAuth("ghcr.io", "MacroPower", password).
		WithLabel("org.opencontainers.image.source", "https://github.com/MacroPower/dotfiles").
		WithLabel("org.opencontainers.image.description", "Development container with nix home-manager tools")

	var addr string
	for _, tag := range tags {
		ref := fmt.Sprintf("%s:%s", image, tag)
		addr, err = ctr.Publish(ctx, ref)
		if err != nil {
			return "", fmt.Errorf("publishing %s: %w", ref, err)
		}
	}

	return addr, nil
}

// PublishSandbox builds a self-contained sandbox container and pushes it
// to a container registry. The published image includes the terrarium
// binary and config.yaml deployed by home-manager; firewall configs are
// generated at runtime by terrarium init. Users customize behavior by
// mounting their own config.yaml at the terrarium XDG config path.
func (m *Dev) PublishSandbox(
	ctx context.Context,
	// Registry password or personal access token.
	password *dagger.Secret,
	// Image tags. Defaults to ["latest"].
	// +optional
	tags []string,
	// Full image reference without tag.
	// +optional
	// +default="ghcr.io/macropower/sandbox"
	image string,
) (string, error) {
	if len(tags) == 0 {
		tags = []string{"latest"}
	}

	base, err := m.DevBase(ctx, nil)
	if err != nil {
		return "", err
	}

	// setup-user: create non-root user.
	setupUserScript := fmt.Sprintf(`set -e
printf '%s:x:%s:%s::%s:/bin/sh\n' >> /etc/passwd
printf '%s:x:%s:\n' >> /etc/group
chown -R %s:%s %s
`, username, uid, gid, homeDir, username, gid, uid, gid, homeDir)

	built := base.
		WithExec([]string{"sh", "-c", setupUserScript}).
		WithEnvVariable("IS_SANDBOX_NETWORK", "1")

	ctr := built.
		WithoutMount("/nix/store").
		WithoutMount("/nix/var/nix").
		WithoutMount("/root/.cache/nix").
		WithoutMount("/claude-state").
		WithDirectory("/nix", built.Directory("/nix")).
		WithDirectory(homeDir, built.Directory(homeDir)).
		WithWorkdir(homeDir).
		WithEntrypoint([]string{"terrarium", "init", "--", "fish"}).
		WithRegistryAuth("ghcr.io", "MacroPower", password).
		WithLabel("org.opencontainers.image.source", "https://github.com/MacroPower/dotfiles").
		WithLabel("org.opencontainers.image.description", "Sandboxed development container with nix home-manager tools")

	var addr string
	for _, tag := range tags {
		ref := fmt.Sprintf("%s:%s", image, tag)
		addr, err = ctr.Publish(ctx, ref)
		if err != nil {
			return "", fmt.Errorf("publishing %s: %w", ref, err)
		}
	}

	return addr, nil
}

// Shell opens an interactive development container with an optional source
// directory mounted at /src.
//
// +cache="never"
func (m *Dev) Shell(
	ctx context.Context,
	// Source directory to mount in the dev container.
	// +optional
	// +ignore=[".git"]
	repoSource *dagger.Directory,
	// Git configuration directory (~/.config/git).
	// +optional
	gitConfig *dagger.Directory,
	// Timezone for the container (e.g. "America/New_York").
	// +optional
	tz string,
	// COLORTERM value (e.g. "truecolor").
	// +optional
	colorterm string,
	// TERM_PROGRAM value (e.g. "Apple_Terminal", "iTerm.app").
	// +optional
	termProgram string,
	// TERM_PROGRAM_VERSION value.
	// +optional
	termProgramVersion string,
	// Command to run in the terminal session. Defaults to ["fish"].
	// +optional
	cmd []string,
	// Kagi API key for the MCP server configuration.
	// +optional
	kagiApiKey *dagger.Secret,
) (*dagger.Container, error) {
	devCtr, err := m.DevBase(ctx, kagiApiKey)
	if err != nil {
		return nil, err
	}

	devCtr = devCtr.
		WithMountedCache("/commandhistory", dag.CacheVolume(devCacheNamespace+":shell-history"))

	if repoSource != nil {
		devCtr = devCtr.
			WithDirectory("/src", repoSource).
			WithWorkdir("/src")
	}

	devCtr = applyDevConfig(devCtr, gitConfig,
		tz, colorterm, termProgram, termProgramVersion)

	if len(cmd) == 0 {
		cmd = []string{"fish"}
	}

	devCtr = devCtr.Terminal(dagger.ContainerTerminalOpts{
		Cmd:                           wrapWithAtuinDaemon(cmd),
		ExperimentalPrivilegedNesting: true,
	})

	return devCtr, nil
}

// applyDevConfig applies optional configuration mounts and environment
// variables to a dev container.
func applyDevConfig(
	ctr *dagger.Container,
	gitConfig *dagger.Directory,
	tz, colorterm, termProgram, termProgramVersion string,
) *dagger.Container {
	if gitConfig != nil {
		ctr = ctr.WithMountedDirectory(homeDir+"/.config/git", gitConfig)
	}

	if tz != "" {
		ctr = ctr.WithEnvVariable("TZ", tz)
	}

	if colorterm != "" {
		ctr = ctr.WithEnvVariable("COLORTERM", colorterm)
	}

	if termProgram != "" {
		ctr = ctr.WithEnvVariable("TERM_PROGRAM", termProgram)
	}

	if termProgramVersion != "" {
		ctr = ctr.WithEnvVariable("TERM_PROGRAM_VERSION", termProgramVersion)
	}

	return ctr
}

// wrapWithAtuinDaemon wraps a command so that the atuin daemon is started
// in the background before exec'ing the original command. This is needed
// because the container lacks systemd to start the daemon service.
func wrapWithAtuinDaemon(cmd []string) []string {
	return append([]string{"sh", "-c", `atuin daemon >/dev/null 2>&1 & exec "$@"`, "--"}, cmd...)
}
