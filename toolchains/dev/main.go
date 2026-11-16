// Reusable development container functions powered by nix home-manager.
// Provides pre-configured dev environments with tools and shell
// configuration derived from the dotfiles flake's homeConfigurations
// output.

package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"

	"dagger/dev/internal/dagger"
)

const (
	// nixImage is the pinned lix container image.
	nixImage = "ghcr.io/lix-project/lix:2.94.0@sha256:25f5eee428aa1bc217cfcfa7c6d1e072274001ba07d4bb10a92fb7e3e16d1838"

	// devCacheNamespace is the namespace prefix for dev-specific cache volumes.
	devCacheNamespace = "go.jacobcolvin.com/dotfiles/toolchains/dev"

	// homeConfigFmt is the format string for home-manager configuration names.
	// The placeholder is filled with the Nix system string (e.g. "aarch64-linux").
	homeConfigFmt = "dev@%s"

	// golangciLintVersion is the golangci-lint image tag used by [Dev.CheckLint].
	golangciLintVersion = "v2.11" // renovate: datasource=github-releases depName=golangci/golangci-lint

	// toolchainSubpath is the path of this toolchain from the root of
	// the repository.
	toolchainSubpath = "toolchains/dev"

	// lintDir is the directory of the lint container that holds the
	// source of the toolchain.
	lintDir = "/src"

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

// goModules holds the path of each Go module of the toolchain from the
// root of the toolchain. Each Go module is also a Dagger module.
// golangci-lint does not lint a Go module that is nested in the module
// of its working directory. [Dev.CheckLint] and [Dev.Format] therefore
// run it in each module of this list.
var goModules = []string{".", "tests"}

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
	// +ignore=["result", ".git", "toolchains/", "lima/"]
	source *dagger.Directory,
) *Dev {
	return &Dev{Source: source}
}

// Toolchain returns the source directory of this toolchain, which holds
// the Go modules that [Dev.CheckLint] and [Dev.Format] lint.
func (m *Dev) Toolchain() *dagger.Directory {
	return dag.CurrentModule().Source()
}

// lintSource returns src, a source directory of the toolchain, with the
// generated bindings of each Go module. The code of a module imports
// its bindings, so golangci-lint cannot check the types of the module
// without them. Git ignores the bindings, and the engine generates only
// the bindings of the module that it loads. A source directory
// therefore does not hold the bindings of the test module.
func lintSource(src *dagger.Directory) *dagger.Directory {
	withBindings := src

	for _, mod := range goModules {
		bindings := src.
			AsModuleSource(dagger.DirectoryAsModuleSourceOpts{SourceRootPath: mod}).
			GeneratedContextDirectory()
		withBindings = withBindings.WithDirectory("/", bindings)
	}

	return withBindings
}

// lintBase returns a golangci-lint container whose working directory,
// [lintDir], holds src and the linter configuration of the repository.
// golangci-lint reads that configuration in each Go module of src,
// because it searches the parents of its working directory. The
// container mounts the caches of Go and of golangci-lint.
func (m *Dev) lintBase(src *dagger.Directory) *dagger.Container {
	return dag.Container().
		From("golangci/golangci-lint:"+golangciLintVersion).
		WithMountedCache("/go/pkg/mod", dag.CacheVolume(devCacheNamespace+":modules")).
		WithEnvVariable("GOMODCACHE", "/go/pkg/mod").
		WithMountedCache("/go/build-cache", dag.CacheVolume(devCacheNamespace+":build")).
		WithEnvVariable("GOCACHE", "/go/build-cache").
		WithMountedDirectory(lintDir, src).
		WithFile(lintDir+"/.golangci.yaml", m.Source.File(".golangci.yaml")).
		WithWorkdir(lintDir).
		WithMountedCache("/root/.cache/golangci-lint",
			dag.CacheVolume(devCacheNamespace+":golangci-lint-"+golangciLintVersion))
}

// CheckLint verifies that golangci-lint passes on each Go module of the
// toolchain, which includes the test module. It lints the modules at
// the same time and returns after every lint ends, so one run reports
// the findings of each module.
//
// +check
func (m *Dev) CheckLint(
	ctx context.Context,
	// Source directory of the toolchain to lint. The default is the
	// directory that [Dev.Toolchain] returns.
	// +optional
	toolchain *dagger.Directory,
) error {
	if toolchain == nil {
		toolchain = m.Toolchain()
	}

	base := m.lintBase(lintSource(toolchain))

	// No run of golangci-lint depends on another, so the runs start at
	// the same time. errs keeps the order of goModules, whichever run
	// ends first.
	errs := make([]error, len(goModules))

	var wg sync.WaitGroup

	for i, mod := range goModules {
		wg.Go(func() {
			_, err := base.
				WithWorkdir(path.Join(lintDir, mod)).
				WithExec([]string{"golangci-lint", "run"}).
				Sync(ctx)
			if err != nil {
				errs[i] = fmt.Errorf("linting %s: %w", path.Join(toolchainSubpath, mod), err)
			}
		})
	}

	wg.Wait()

	return errors.Join(errs...)
}

// Format runs golangci-lint --fix on each Go module of the toolchain,
// which includes the test module. It returns the changeset of the
// source files that golangci-lint fixed. Each path of the changeset
// starts at the root of the repository, so that dagger generate writes
// each fix to its file (see dagger/dagger#11160).
//
// Format fails when a run of golangci-lint fails, such as on a
// .golangci.yaml file that golangci-lint rejects. An issue that
// golangci-lint cannot fix does not fail the run. The changeset then
// holds the other fixes of the run, and [Dev.CheckLint] reports the
// issue.
//
// +generate
func (m *Dev) Format(
	// Source directory of the toolchain to fix. The default is the
	// directory that [Dev.Toolchain] returns.
	// +optional
	toolchain *dagger.Directory,
) *dagger.Changeset {
	if toolchain == nil {
		toolchain = m.Toolchain()
	}

	src := lintSource(toolchain)
	ctr := m.lintBase(src)

	// By default, golangci-lint exits with status 1 when it finds an
	// issue that it cannot fix. --issues-exit-code=0 makes it exit with
	// status 0 after such a run. So the container keeps the fixes of
	// that run and fixes the next module.
	//
	// With the flag, golangci-lint still exits with status 3 and fixes
	// nothing when it cannot run, such as on a .golangci.yaml file that
	// it rejects. It exits with status 7 after a run in which a linter
	// logged an error, whether or not an issue remains. Each status
	// other than 0 fails the step, so each read of the changeset
	// returns the error of the step, not an empty changeset.
	//
	// The linter configuration turns off the newexpr pass of modernize,
	// because that pass panics under --fix.
	for _, mod := range goModules {
		ctr = ctr.
			WithWorkdir(path.Join(lintDir, mod)).
			WithExec([]string{"golangci-lint", "run", "--fix", "--issues-exit-code=0"})
	}

	// lintBase copies .golangci.yaml into lintDir. Without this step,
	// the changeset adds the file to the toolchain.
	fixed := ctr.Directory(lintDir).WithoutFile(".golangci.yaml")

	// Both directories hold the generated bindings, so the changeset
	// holds only the files that golangci-lint fixed. Both directories
	// sit at toolchainSubpath, so each path of the changeset starts at
	// the root of the repository.
	original := dag.Directory().WithDirectory(toolchainSubpath, src)
	updated := dag.Directory().WithDirectory(toolchainSubpath, fixed)

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
//
// The activation installs the krew plugins that the home configuration
// declares into the krew root, ~/.krew. The krew root is a directory of
// the root filesystem of the container, so each container of this
// module holds the plugins of its build. home-manager puts the bin
// directory of the krew root ahead of the rest of PATH in each shell
// session.
//
// No container of this module mounts a cache volume at the krew root,
// because such a volume has these faults:
//
//   - A volume that a container mounts after the activation hides the
//     plugins of the build.
//   - The activation fills a volume that ctr mounts ahead of it. The
//     engine caches the exec of the activation until the build changes,
//     so a volume that the engine creates later stays empty.
//   - A volume that [Dev.Shell] and [Dev.Sandbox] both mount lets a
//     sandboxed process plant a program that a shell session of
//     [Dev.Shell] runs as root (see [withSandboxUser]).
//
// An engine that ran an earlier build of this module might hold krew
// cache volumes. The sandbox user owns the volumes whose keys end in
// :krew and :krew-sandbox, so they can hold programs that a sandboxed
// process wrote. A container of this module must not mount those keys.
func (m *Dev) buildBase(ctx context.Context, ctr *dagger.Container) (*dagger.Container, error) {
	sys, err := nixSystem(ctx, ctr)
	if err != nil {
		return nil, err
	}

	config := fmt.Sprintf(".#homeConfigurations.%q.activationPackage", fmt.Sprintf(homeConfigFmt, sys))

	return ctr.
		WithEnvVariable("NIX_CONFIG", "experimental-features = nix-command flakes\n"+
			"max-jobs = auto\n").
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
				"/usr/local/bin:/usr/bin:/bin",
		).
		WithEnvVariable("EDITOR", "vim").
		WithEnvVariable("TERM", "xterm-256color").
		// Move out of /dotfiles before deleting it, otherwise the next
		// WithExec recreates an empty /dotfiles to honor the workdir.
		WithWorkdir(homeDir).
		WithoutDirectory("/dotfiles").
		WithExec([]string{"nix", "store", "gc"}), nil
}

// cachedBuild builds the home-manager configuration with nix store and
// eval caches mounted, so repeated builds reuse prior work. Cache
// volumes are scoped per Nix system so amd64 and arm64 builds keep
// separate /nix stores; mixing arches in one volume corrupts the
// store database. An empty platform leaves arch selection to the
// engine.
func (m *Dev) cachedBuild(ctx context.Context, platform dagger.Platform) (*dagger.Container, error) {
	// Squash the base image into a single layer so its many small
	// layers don't bloat the final published image.
	// Squashing discards OCI config; restore env vars so nix, git,
	// and SSL work during the build.
	opts := dagger.ContainerOpts{Platform: platform}
	base := dag.Container(opts).From(nixImage)

	sys, err := nixSystem(ctx, base)
	if err != nil {
		return nil, err
	}

	ctr := dag.Container(opts).WithRootfs(base.Rootfs()).
		WithEnvVariable("PATH", "/root/.nix-profile/bin:/nix/var/nix/profiles/default/bin:/nix/var/nix/profiles/default/sbin").
		WithEnvVariable("SSL_CERT_FILE", "/nix/var/nix/profiles/default/etc/ssl/certs/ca-bundle.crt").
		WithEnvVariable("NIX_SSL_CERT_FILE", "/nix/var/nix/profiles/default/etc/ssl/certs/ca-bundle.crt").
		WithEnvVariable("GIT_SSL_CAINFO", "/nix/var/nix/profiles/default/etc/ssl/certs/ca-bundle.crt")
	ctr = ctr.
		WithMountedCache("/nix", dag.CacheVolume(devCacheNamespace+":nix:"+sys),
			dagger.ContainerWithMountedCacheOpts{Source: ctr.Directory("/nix")}).
		WithMountedCache("/root/.cache/nix", dag.CacheVolume(devCacheNamespace+":nix-eval-cache:"+sys))

	return m.buildBase(ctx, ctr)
}

// DevBase returns a base development container with nix and home-manager
// tools activated but no project source mounted. Used by integration
// tests to verify tool availability without requiring an interactive
// terminal.
//
// The container keeps ~/.claude.json in the claude-state cache volume
// (see [withClaudeState]) and carries container-init. The terminal
// sessions of [Dev.Shell] and [Dev.Sandbox] start through
// container-init, so each start syncs the MCP servers of the persisted
// claude.json with the build.
func (m *Dev) DevBase(
	ctx context.Context,
	// Kagi API key for the Kagi MCP server. The container gets the key
	// as the secret variable KAGI_API_KEY, which the engine sets in the
	// environment of each process that the container runs.
	// +optional
	kagiApiKey *dagger.Secret,
) (*dagger.Container, error) {
	ctr, err := m.devBaseForPlatform(ctx, "", kagiApiKey)
	if err != nil {
		return nil, err
	}

	// The published sandbox image builds from devBaseForPlatform and
	// runs without the claude-state volume, so only this container gets
	// the volume and container-init.
	ctr = ctr.WithNewFile(containerInitPath, containerInitScript,
		dagger.ContainerWithNewFileOpts{Permissions: 0o755})

	return withClaudeState(ctr), nil
}

// withClaudeState mounts the claude-state cache volume on ctr and
// replaces ~/.claude.json with a link to the claude.json of the volume.
// The file therefore outlives the container. When the volume holds no
// claude.json, the file that the activation wrote seeds it.
//
// A published image must not get the link. The image does not carry the
// volume, so the link dangles there and Claude Code starts without the
// MCP servers of the build.
//
// The engine caches the exec until the build changes, so the script
// does not sync the MCP servers of the persisted claude.json.
// container-init syncs them at each terminal start (see
// [wrapWithContainerInit]).
func withClaudeState(ctr *dagger.Container) *dagger.Container {
	script := `set -e
if [ ! -f /claude-state/claude.json ] && [ -f ` + homeDir + `/.claude.json ]; then
  cp ` + homeDir + `/.claude.json /claude-state/claude.json
fi
rm -f ` + homeDir + `/.claude.json
ln -sf /claude-state/claude.json ` + homeDir + `/.claude.json
`

	return ctr.
		WithMountedCache("/claude-state", dag.CacheVolume(devCacheNamespace+":claude-state")).
		WithExec([]string{"sh", "-c", script})
}

// devBaseForPlatform is the platform-aware implementation behind
// [Dev.DevBase]. The Publish helpers call this directly so each
// architecture variant runs through the same activation steps with
// its own /nix cache.
//
// The container does not mount the claude-state cache volume, and
// ~/.claude.json stays the regular file that the activation wrote.
// [Dev.DevBase] adds the volume (see [withClaudeState]).
//
// When kagiApiKey is set, the container gets the key as the secret
// variable KAGI_API_KEY. The activation runs ahead of that step, so it
// does not read the key. The engine sets the variable in the
// environment of each process that the container runs after that step.
// The terminal sessions of [Dev.Shell] and [Dev.Sandbox] are such
// processes, and the Kagi MCP server of a session reads the key when
// it starts.
//
// The container mounts no cache volume at the krew root, so its shell
// sessions run the krew plugins that the activation installed (see
// [Dev.buildBase]).
func (m *Dev) devBaseForPlatform(
	ctx context.Context,
	platform dagger.Platform,
	kagiApiKey *dagger.Secret,
) (*dagger.Container, error) {
	ctr, err := m.cachedBuild(ctx, platform)
	if err != nil {
		return nil, err
	}

	// The key must stay a secret. The value of a plain variable is an
	// argument of the withEnvVariable call. The engine writes that
	// argument to the ID of the container and to the trace of the call.
	// A publish of the container writes the value to the config of the
	// image. The engine passes the value of a secret variable only to
	// the processes of the container, and it replaces the value in
	// their output.
	if kagiApiKey != nil {
		ctr = ctr.WithSecretVariable("KAGI_API_KEY", kagiApiKey)
	}

	// setup-dev: create fish history symlink, disable atuin systemd
	// socket, create atuin data dir.
	setupScript := `set -e
mkdir -p ` + homeDir + `/.local/share/fish
ln -sf /commandhistory/fish_history ` + homeDir + `/.local/share/fish/fish_history
sed -i 's/systemd_socket = true/systemd_socket = false/' ` + homeDir + `/.config/atuin/config.toml 2>/dev/null || true
mkdir -p ` + homeDir + `/.local/share/atuin
`

	return ctr.
		WithEnvVariable("IS_SANDBOX", "1").
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
//
// The non-root user owns the home directory of the container, except
// the krew plugins of the build (see [withSandboxUser]). [Dev.Shell]
// runs the krew plugins of its own container, so a sandboxed process
// cannot change the plugins that a shell session of [Dev.Shell] runs.
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
	// Otherwise dereference the home-manager symlink chain so the file
	// lives in the rootfs; dagger.Container.File cannot follow links
	// into cache-mounted /nix store paths.
	if sandboxConfig != nil {
		ctr = ctr.WithFile(terrariumConfigPath, sandboxConfig)
	} else {
		ctr = ctr.WithExec([]string{
			"sh", "-c",
			fmt.Sprintf("cp -L --remove-destination %s %s.tmp && mv %s.tmp %s",
				terrariumConfigPath, terrariumConfigPath,
				terrariumConfigPath, terrariumConfigPath),
		})
	}

	return withSandboxUser(ctr).
		WithEnvVariable("IS_SANDBOX_NETWORK", "1"), nil
}

// withSandboxUser creates the non-root sandbox user on ctr and makes
// that user the owner of the home directory. terrarium init drops to
// the sandbox user ahead of the sandboxed process.
//
// The home directory holds the krew root (see [Dev.buildBase]). The
// script skips the plugin directories of the krew store, so root keeps
// the plugins of the build. An image layer stores in full each file
// whose owner changes. The skip therefore keeps a second copy of the
// plugins out of the published sandbox image.
//
// The plugin files are executable for every user, so the sandbox user
// can run them. The sandbox user owns the rest of the krew root, so a
// sandboxed process can install plugins in its own container. krew
// cannot upgrade a plugin of the build. An uninstall of a plugin of the
// build removes the link of the plugin from the bin directory. The
// uninstall then fails, so krew still lists the plugin.
//
// find crosses mount points. The sandbox user therefore owns each
// volume that ctr mounts under the home directory when the script
// runs. The shell sessions of [Dev.Shell] run as root with no egress
// filter, so ctr must not mount there a volume that [Dev.Shell] also
// mounts. For example, a sandboxed process that can write a shared
// volume at the krew root can plant a program in the bin directory of
// the krew root. A shell session of [Dev.Shell] finds that program
// ahead of the rest of PATH and runs it.
func withSandboxUser(ctr *dagger.Container) *dagger.Container {
	krewStore := homeDir + "/.krew/store"

	// setup-user: create non-root user in /etc/passwd and /etc/group,
	// then chown the home directory except the plugins of the krew
	// store. chown -h changes the owner of each link, not of its target.
	setupUserScript := fmt.Sprintf(`set -e
printf '%s:x:%s:%s::%s:/bin/sh\n' >> /etc/passwd
printf '%s:x:%s:\n' >> /etc/group
find %s -path '%s/*' -prune -o -exec chown -h %s:%s {} +
`, username, uid, gid, homeDir, username, gid, homeDir, krewStore, uid, gid)

	return ctr.WithExec([]string{"sh", "-c", setupUserScript})
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

	// container-init runs ahead of terrarium init, because root owns the
	// claude-state volume and terrarium init drops to the non-root user.
	initCmd := append([]string{"terrarium", "init", "--"}, wrapWithAtuinDaemon(cmd)...)
	ctr = ctr.Terminal(dagger.ContainerTerminalOpts{
		Cmd:                      wrapWithContainerInit(initCmd),
		InsecureRootCapabilities: true,
	})

	return ctr, nil
}

// BuildShell builds a self-contained dev container with all nix store
// contents baked into image layers so it works without Dagger cache
// volumes. Use [Dev.PublishShell] to build and push in one step. An
// empty platform builds for the engine's native architecture.
//
// The image is SSH-ready (see ssh.go): sshd prerequisites are baked in
// and the container-init / sshd-entrypoint helpers let deployments
// expose the container over SSH with nothing but environment variables.
// fish is the default command rather than the entrypoint so that
// runtime command overrides (e.g. compose `command:`) exec directly
// instead of being parsed by fish as a script path.
func (m *Dev) BuildShell(
	ctx context.Context,
	// Target platform (e.g. "linux/amd64", "linux/arm64"). Empty
	// uses the Dagger engine's native architecture.
	// +optional
	platform dagger.Platform,
) (*dagger.Container, error) {
	built, err := m.cachedBuild(ctx, platform)
	if err != nil {
		return nil, err
	}

	// Bake SSH readiness before snapshotting so the passwd/shadow
	// fixups and helper scripts land in the published layers.
	built = withSSHReadiness(built)

	// Snapshot cache-mounted paths into the rootfs. Dagger cannot
	// convert cache-mounted directories into immutable Directory
	// references, so we cp to a non-mounted path, get a Directory
	// from there, then overlay it after removing the mounts.
	snapshot := built.
		WithExec([]string{"cp", "-a", "/nix", "/nix-snapshot"}).
		WithExec([]string{"sh", "-c", stripScript})
	nixDir := snapshot.Directory("/nix-snapshot")

	ctr := built.
		WithoutMount("/nix").
		WithoutMount("/root/.cache/nix").
		WithoutDirectory("/nix").
		WithDirectory("/nix", nixDir).
		WithWorkdir(homeDir).
		WithDefaultArgs([]string{"fish", "-l"}).
		WithLabel("org.opencontainers.image.source", "https://github.com/MacroPower/dotfiles").
		WithLabel("org.opencontainers.image.description", "Development container with nix home-manager tools")

	return ctr, nil
}

// stripScript shrinks /nix-snapshot before it gets baked into the image.
//
// Two passes. First, delete categories of files nothing in a CI/dev
// container reads: docs, non-English locales, GUI desktop integration,
// shell completions for shells we don't use, build-only artifacts
// (.a/.la/.js.map), Python __pycache__ (regenerated to a writable cache
// on first import), and the lix base image's nix-channel scaffolding
// (the dev container drives rebuilds through the dotfiles flake, not
// channels).
//
// Second, prune /nix-snapshot/store down to the closure of the
// home-manager generation plus the latest system profile. Everything
// else is kept alive only by dagger-engine cache gcroots and stale
// profile generations the cache volume accumulates across runs, so
// `nix store gc` against the live store leaves them in place. The
// published image performs no further nix store operations and store
// paths are self-contained, so dropping them from the snapshot is safe.
const stripScript = `set -eo pipefail
find /nix-snapshot -type d \( \
    -name man -o -name doc -o -name info \
    -o -name gtk-doc -o -name devhelp -o -name help \
    -o -name applications -o -name icons -o -name pixmaps \
    -o -name zsh -o -name bash-completion \
    -o -name __pycache__ \
\) -prune -exec rm -rf {} +
find /nix-snapshot -type d -path '*/share/locale/*' \
    ! -path '*/share/locale/en' \
    ! -path '*/share/locale/en_*' \
    ! -path '*/share/locale/en/*' \
    ! -path '*/share/locale/en_*/*' \
    -prune -exec rm -rf {} +
find /nix-snapshot -type f \
    \( -name '*.a' -o -name '*.la' -o -name '*.js.map' \) \
    -delete
rm -rf /nix-snapshot/var/nix/profiles/per-user/root/channels \
       /nix-snapshot/var/nix/profiles/per-user/root/channels-* \
       /nix-snapshot/var/nix/gcroots/docker

# Closure roots: home-manager's gcroot, the latest per-user/root
# profile, and the system default profile. nix-store -qR reads from
# the live /nix (cache volume) because /nix-snapshot is a plain
# directory tree, not a Nix store. We delete by store-path basename
# under /nix-snapshot/store rather than running ` + "`nix store gc`" + `, since
# the cache volume's docker gcroots and lingering profile generations
# defeat gc on the live store.
keep=$(mktemp)
trap 'rm -f "$keep"' EXIT
roots=
for r in /home/dev/.local/state/home-manager/gcroots/current-home \
         /nix/var/nix/profiles/per-user/root/profile \
         /nix/var/nix/profiles/default; do
    [ -e "$r" ] || continue
    roots="$roots $(readlink -f "$r")"
done
if [ -z "$roots" ]; then
    echo "stripScript: no closure roots resolved under /nix" >&2
    exit 1
fi
nix-store -qR $roots | awk -F/ '{print $NF}' | sort -u > "$keep"
keep_count=$(wc -l < "$keep")
# Healthy closures land around 1500-2500 paths. Anything dramatically
# smaller means ` + "`nix-store -qR`" + ` lost some of the closure and the comm
# diff below would treat live paths as orphans, gutting /nix/store.
if [ "$keep_count" -lt 500 ]; then
    echo "stripScript: keep set has only $keep_count paths, refusing to prune" >&2
    exit 1
fi
ls /nix-snapshot/store | sort -u | comm -23 - "$keep" \
    | while IFS= read -r name; do
        [ -n "$name" ] || continue
        rm -rf "/nix-snapshot/store/$name"
    done
`

// publishMultiArch builds one container per platform via buildVariant
// and delegates the manifest assembly to [Dev.publishVariants]. Empty
// platforms defaults to amd64 + arm64.
func (m *Dev) publishMultiArch(
	ctx context.Context,
	password *dagger.Secret,
	image string,
	tags []string,
	platforms []dagger.Platform,
	buildVariant func(context.Context, dagger.Platform) (*dagger.Container, error),
) (string, error) {
	if len(platforms) == 0 {
		platforms = []dagger.Platform{"linux/amd64", "linux/arm64"}
	}

	variants := make([]*dagger.Container, 0, len(platforms))

	for _, p := range platforms {
		ctr, err := buildVariant(ctx, p)
		if err != nil {
			return "", fmt.Errorf("building %s: %w", p, err)
		}

		variants = append(variants, ctr)
	}

	return m.publishVariants(ctx, password, image, tags, variants)
}

// publishVariants pushes the given platform variants as a single
// multi-platform OCI manifest list at each tag. The publisher
// container only carries registry auth; PlatformVariants fully
// determines the manifest contents. Empty tags defaults to ["latest"].
// Used by [Dev.publishMultiArch] and [Dev.PublishManifest].
func (m *Dev) publishVariants(
	ctx context.Context,
	password *dagger.Secret,
	image string,
	tags []string,
	variants []*dagger.Container,
) (string, error) {
	if len(tags) == 0 {
		tags = []string{"latest"}
	}

	publisher := dag.Container().WithRegistryAuth("ghcr.io", "MacroPower", password)

	var addr string

	for _, tag := range tags {
		ref := fmt.Sprintf("%s:%s", image, tag)

		a, err := publisher.Publish(ctx, ref, dagger.ContainerPublishOpts{
			PlatformVariants: variants,
			// zstd shrinks the published image by ~7% over forced
			// gzip on this content (mostly /nix/store binaries) and
			// decompresses faster on pull.
			ForcedCompression: dagger.ImageLayerCompressionZstd,
		})
		if err != nil {
			return "", fmt.Errorf("publishing %s: %w", ref, err)
		}

		addr = a
	}

	return addr, nil
}

// PublishShell builds self-contained dev container variants via
// [Dev.BuildShell] for each requested platform and pushes a single
// multi-platform OCI manifest list per tag to the registry.
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
	// Target platforms for the manifest list. Defaults to
	// ["linux/amd64", "linux/arm64"].
	// +optional
	platforms []dagger.Platform,
) (string, error) {
	return m.publishMultiArch(ctx, password, image, tags, platforms, m.BuildShell)
}

// buildSandboxForPlatform builds a self-contained sandbox container
// for a single platform. It activates home-manager via
// [Dev.devBaseForPlatform] and creates the non-root sandbox user via
// [withSandboxUser]. It then snapshots the /nix cache mount into the
// rootfs, and sets the terrarium entrypoint and OCI labels.
//
// The build does not mount the claude-state cache volume. The image
// keeps the claude.json that the activation wrote as a regular file,
// so Claude Code starts with the MCP servers of the image.
func (m *Dev) buildSandboxForPlatform(
	ctx context.Context,
	platform dagger.Platform,
) (*dagger.Container, error) {
	base, err := m.devBaseForPlatform(ctx, platform, nil)
	if err != nil {
		return nil, err
	}

	built := withSandboxUser(base).
		WithEnvVariable("IS_SANDBOX_NETWORK", "1")

	// Snapshot cache-mounted paths into the rootfs. Dagger cannot
	// convert cache-mounted directories into immutable Directory
	// references, so we cp to a non-mounted path, get a Directory
	// from there, then overlay it after removing the mounts.
	snapshot := built.
		WithExec([]string{"cp", "-a", "/nix", "/nix-snapshot"}).
		WithExec([]string{"sh", "-c", stripScript})
	nixDir := snapshot.Directory("/nix-snapshot")

	// Splitting the terrarium wrapper (entrypoint) from fish (default
	// args) lets runtime command overrides replace the sandboxed
	// process while keeping the firewall setup in front of it.
	return built.
		WithoutMount("/nix").
		WithoutMount("/root/.cache/nix").
		WithoutDirectory("/nix").
		WithDirectory("/nix", nixDir).
		WithWorkdir(homeDir).
		WithEntrypoint([]string{"terrarium", "init", "--"}).
		WithDefaultArgs([]string{"fish"}).
		WithLabel("org.opencontainers.image.source", "https://github.com/MacroPower/dotfiles").
		WithLabel(
			"org.opencontainers.image.description",
			"Sandboxed development container with nix home-manager tools",
		), nil
}

// PublishSandbox builds self-contained sandbox container variants via
// [Dev.buildSandboxForPlatform] for each requested platform and pushes
// a single multi-platform OCI manifest list per tag to the registry.
// The published image includes the terrarium binary and config.yaml
// deployed by home-manager; firewall configs are generated at runtime
// by terrarium init. Users customize behavior by mounting their own
// config.yaml at the terrarium XDG config path.
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
	// Target platforms for the manifest list. Defaults to
	// ["linux/amd64", "linux/arm64"].
	// +optional
	platforms []dagger.Platform,
) (string, error) {
	return m.publishMultiArch(ctx, password, image, tags, platforms, m.buildSandboxForPlatform)
}

// PublishManifest assembles a multi-arch OCI manifest list from
// previously published single-platform images and pushes it at each
// tag via [Dev.publishVariants]. refs and platforms are parallel
// slices: refs[i] must point at a single-platform image whose
// architecture matches platforms[i]. Pinning Platform on each pull
// ensures the right variant is selected even if a ref ever resolves
// to a manifest list.
func (m *Dev) PublishManifest(
	ctx context.Context,
	// Registry password or personal access token.
	password *dagger.Secret,
	// Full target image reference without tag, e.g.
	// "ghcr.io/macropower/shell".
	image string,
	// Source image references (with tag), one per platform.
	refs []string,
	// Platform per ref. Must be the same length as refs.
	platforms []dagger.Platform,
	// Image tags to publish. Defaults to ["latest"].
	// +optional
	tags []string,
) (string, error) {
	if len(refs) != len(platforms) {
		return "", fmt.Errorf("refs (%d) and platforms (%d) length mismatch",
			len(refs), len(platforms))
	}

	variants := make([]*dagger.Container, 0, len(refs))

	for i, ref := range refs {
		variants = append(variants,
			dag.Container(dagger.ContainerOpts{Platform: platforms[i]}).From(ref))
	}

	return m.publishVariants(ctx, password, image, tags, variants)
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
		Cmd:                           wrapWithContainerInit(wrapWithAtuinDaemon(cmd)),
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

// wrapWithContainerInit wraps a terminal command so that container-init
// runs ahead of it (see scripts/container-init.sh). [Dev.DevBase]
// installs container-init.
//
// The claude.json in the claude-state cache volume outlives the build
// that wrote it. The commands of its MCP servers are Nix store paths of
// that build. A later build deletes the store paths that it does not
// reference, so those servers fail to start. container-init syncs the
// servers with the build that the terminal runs, and removes each
// server that the build stops declaring.
//
// The sync is part of the terminal command because the engine caches
// each exec of a build that did not change. The terminal command runs
// at every start.
func wrapWithContainerInit(cmd []string) []string {
	return append([]string{containerInitPath}, cmd...)
}
