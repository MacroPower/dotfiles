// Reusable development container functions powered by nix home-manager.
// Provides pre-configured dev environments with tools and shell
// configuration derived from the dotfiles flake's homeConfigurations
// output.

package main

import (
	"context"
	"fmt"

	"go.jacobcolvin.com/dotfiles/toolchains/dev/sandbox"

	"dagger/dev/internal/dagger"
)

const (
	// nixImage is the pinned nixos/nix container image.
	nixImage = "nixos/nix:2.34.1@sha256:1d59121e0c361076b4f23c158d236702f2f045b3b477b51075b81ceb6188d34a"

	// devCacheNamespace is the namespace prefix for dev-specific cache volumes.
	devCacheNamespace = "go.jacobcolvin.com/dotfiles/toolchains/dev"

	// homeConfig is the home-manager configuration name from the flake.
	homeConfig = "dev@linux"

	// golangciLintVersion is the golangci-lint image tag used by [Dev.CheckLint].
	golangciLintVersion = "v2.11" // renovate: datasource=github-releases depName=golangci/golangci-lint
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
	for _, mod := range []string{".", "sandbox"} {
		cmd := []string{"golangci-lint", "run"}
		if mod != "." {
			cmd = append(cmd, "--path-prefix", mod)
		}

		_, err := m.lintBase(mod).WithExec(cmd).Sync(ctx)
		if err != nil {
			return fmt.Errorf("linting %s: %w", mod, err)
		}
	}

	return nil
}

// CheckTest runs Go unit tests on the sandbox module.
//
// +check
func (m *Dev) CheckTest(ctx context.Context) error {
	src := dag.CurrentModule().Source().Directory("sandbox")

	_, err := dag.Container().
		From("golang:1.26-alpine").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume(devCacheNamespace+":modules")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume(devCacheNamespace+":build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithDirectory("/src", src).
		WithWorkdir("/src").
		WithExec([]string{"go", "test", "./..."}).
		Sync(ctx)
	if err != nil {
		return fmt.Errorf("testing sandbox: %w", err)
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
	mods := []string{".", "sandbox"}

	changesets := make([]*dagger.Changeset, len(mods))
	for i, mod := range mods {
		changesets[i] = m.formatModule(mod)
	}

	return mergeChangesets(changesets)
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

// mergeChangesets combines multiple changesets into one using octopus
// merge. Nil entries are skipped.
func mergeChangesets(changesets []*dagger.Changeset) *dagger.Changeset {
	var nonNil []*dagger.Changeset
	for _, cs := range changesets {
		if cs != nil {
			nonNil = append(nonNil, cs)
		}
	}

	if len(nonNil) == 0 {
		return nil
	}

	if len(nonNil) == 1 {
		return nonNil[0]
	}

	return nonNil[0].WithChangesets(nonNil[1:])
}

// buildBase builds and activates the home-manager configuration on the
// given container. It installs rsync, mounts the dotfiles source, runs
// nix build + activate, and sets PATH/EDITOR/TERM. Callers are expected
// to configure any cache mounts on ctr before calling this method.
func (m *Dev) buildBase(ctr *dagger.Container) *dagger.Container {
	return ctr.
		WithEnvVariable("NIX_CONFIG", "experimental-features = nix-command flakes\nfilter-syscalls = false\n").
		WithDirectory("/dotfiles", m.Source).
		WithWorkdir("/dotfiles").
		WithExec([]string{
			"nix", "build",
			`.#homeConfigurations."` + homeConfig + `".activationPackage`,
		}).
		WithExec([]string{"mkdir", "-p", sandbox.HomeDir}).
		WithExec([]string{"mkdir", "-p", sandbox.HomeDir + "/.local/state/nix/profiles"}).
		WithExec([]string{"mkdir", "-p", "/nix/var/nix/profiles/per-user/" + sandbox.Username}).
		WithEnvVariable("HOME", sandbox.HomeDir).
		WithEnvVariable("USER", sandbox.Username).
		WithExec([]string{"./result/activate"}).
		WithEnvVariable("PATH",
			sandbox.HomeDir+"/.local/state/home-manager/gcroots/current-home/home-path/bin:"+
				sandbox.HomeDir+"/.nix-profile/bin:"+
				"/nix/var/nix/profiles/default/bin:"+
				"/usr/bin:/bin",
		).
		WithEnvVariable("EDITOR", "vim").
		WithEnvVariable("TERM", "xterm-256color").
		WithoutDirectory("/dotfiles").
		WithWorkdir(sandbox.HomeDir)
}

// cachedBuild builds the home-manager configuration with nix store and
// eval caches mounted, so repeated builds reuse prior work. The nix
// store and var directories share a single cache volume to keep them
// atomically consistent.
func (m *Dev) cachedBuild() *dagger.Container {
	ctr := dag.Container().From(nixImage)
	ctr = ctr.
		WithMountedCache("/nix", dag.CacheVolume(devCacheNamespace+":nix"),
			dagger.ContainerWithMountedCacheOpts{Source: ctr.Directory("/nix")}).
		WithMountedCache("/root/.cache/nix", dag.CacheVolume(devCacheNamespace+":nix-eval-cache"))

	return m.buildBase(ctr)
}

// buildSandbox compiles the sandbox CLI binary for the container.
func (m *Dev) buildSandbox() *dagger.File {
	src := dag.CurrentModule().Source().Directory("sandbox")

	return dag.Container().
		From("golang:1.26-alpine").
		WithDirectory("/src", src).
		WithWorkdir("/src/cmd/sandbox").
		WithEnvVariable("CGO_ENABLED", "0").
		WithExec([]string{"go", "build", "-ldflags", "-s -w", "-o", "/sandbox", "."}).
		File("/sandbox")
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
	ctr := m.cachedBuild().
		WithMountedCache(sandbox.HomeDir+"/.krew", dag.CacheVolume(devCacheNamespace+":krew"))

	if kagiApiKey != nil {
		plaintext, err := kagiApiKey.Plaintext(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading kagi api key: %w", err)
		}

		ctr = ctr.WithEnvVariable("KAGI_API_KEY", plaintext)
	}

	sandboxBin := m.buildSandbox()

	return ctr.
		WithEnvVariable("IS_SANDBOX", "1").
		WithMountedCache("/claude-state", dag.CacheVolume(devCacheNamespace+":claude-state")).
		WithFile("/usr/local/bin/sandbox", sandboxBin,
			dagger.ContainerWithFileOpts{Permissions: 0o755}).
		WithExec([]string{"/usr/local/bin/sandbox", "setup-dev"}), nil
}

// resolveSandboxConfig returns a SandboxConfig from an explicit config
// file, or the default config when no file is provided.
func resolveSandboxConfig(ctx context.Context, sandboxConfig *dagger.File) (*sandbox.SandboxConfig, error) {
	if sandboxConfig != nil {
		data, err := sandboxConfig.Contents(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading sandbox config file: %w", err)
		}

		cfg, err := sandbox.ParseConfig([]byte(data))
		if err != nil {
			return nil, fmt.Errorf("parsing sandbox config: %w", err)
		}

		return cfg, nil
	}

	return sandbox.DefaultConfig(), nil
}

// SandboxBase returns a development container with DNS-based domain
// filtering and an Envoy transparent SNI-filtering proxy. Only domains
// in the allowlist resolve (via dnsmasq) and pass through Envoy's
// filter chain matching. iptables redirects user traffic to Envoy,
// which checks TLS SNI (port 443) or HTTP Host (port 80) against the
// allowlist. The proxy, DNS, and firewall are applied at runtime via
// an init script that also drops to a non-root user, preventing the
// sandboxed process from modifying the rules.
func (m *Dev) SandboxBase(
	ctx context.Context,
	// Kagi API key for the MCP server configuration.
	// +optional
	kagiApiKey *dagger.Secret,
	// YAML sandbox config file defining egress rules and firewall
	// options. Uses the default config when not provided.
	// +optional
	sandboxConfig *dagger.File,
) (*dagger.Container, error) {
	ctr, err := m.DevBase(ctx, kagiApiKey)
	if err != nil {
		return nil, err
	}

	cfg, err := resolveSandboxConfig(ctx, sandboxConfig)
	if err != nil {
		return nil, err
	}

	envoyConf, err := sandbox.GenerateEnvoyFromConfig(cfg, "", "")
	if err != nil {
		return nil, fmt.Errorf("generating envoy config: %w", err)
	}

	iptablesRules, ip6tablesRules := sandbox.GenerateIptablesRules(cfg)

	cfgYAML, err := sandbox.MarshalConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling sandbox config: %w", err)
	}

	return ctr.
		WithExec([]string{"/usr/local/bin/sandbox", "setup-user"}).
		WithNewFile("/etc/envoy-sandbox.yaml", envoyConf).
		WithNewFile("/etc/iptables-sandbox.rules", iptablesRules).
		WithNewFile("/etc/ip6tables-sandbox.rules", ip6tablesRules).
		WithNewFile(sandbox.ConfigPath, string(cfgYAML)).
		WithEnvVariable("IS_SANDBOX_NETWORK", "1"), nil
}

// Sandbox opens an interactive development container with DNS-based
// domain filtering and an Envoy transparent SNI-filtering proxy. Only
// allowed domains resolve and are reachable. The shell runs as a
// non-root user after iptables, dnsmasq, and Envoy setup.
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
	// options. Uses the default config when not provided.
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

	initCmd := append([]string{"/usr/local/bin/sandbox", "init", "--"}, wrapWithAtuinDaemon(cmd)...)
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

	built := m.cachedBuild()

	ctr := built.
		WithoutMount("/nix/store").
		WithoutMount("/nix/var/nix").
		WithoutMount("/root/.cache/nix").
		WithDirectory("/nix", built.Directory("/nix")).
		WithDirectory(sandbox.HomeDir, built.Directory(sandbox.HomeDir)).
		WithWorkdir(sandbox.HomeDir).
		WithEntrypoint([]string{"fish"}).
		WithRegistryAuth("ghcr.io", "MacroPower", password).
		WithLabel("org.opencontainers.image.source", "https://github.com/MacroPower/dotfiles").
		WithLabel("org.opencontainers.image.description", "Development container with nix home-manager tools")

	var (
		addr string
		err  error
	)

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
// to a container registry. The published image includes the sandbox
// binary and a default config.yaml; firewall configs are generated at
// runtime by the init command. Users customize behavior by mounting
// their own config.yaml at /etc/sandbox/config.yaml.
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

	defaultCfg, err := sandbox.MarshalDefaultConfig()
	if err != nil {
		return "", fmt.Errorf("generating default config: %w", err)
	}

	built := base.
		WithExec([]string{"/usr/local/bin/sandbox", "setup-user"}).
		WithNewFile("/etc/sandbox/config.yaml", string(defaultCfg)).
		WithEnvVariable("IS_SANDBOX_NETWORK", "1")

	ctr := built.
		WithoutMount("/nix/store").
		WithoutMount("/nix/var/nix").
		WithoutMount("/root/.cache/nix").
		WithoutMount("/claude-state").
		WithDirectory("/nix", built.Directory("/nix")).
		WithDirectory(sandbox.HomeDir, built.Directory(sandbox.HomeDir)).
		WithWorkdir(sandbox.HomeDir).
		WithEntrypoint([]string{"/usr/local/bin/sandbox", "init", "--", "fish"}).
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
		ctr = ctr.WithMountedDirectory(sandbox.HomeDir+"/.config/git", gitConfig)
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
