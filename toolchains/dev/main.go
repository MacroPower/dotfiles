// Reusable development container functions powered by nix home-manager.
// Provides pre-configured dev environments with tools and shell
// configuration derived from the dotfiles flake's homeConfigurations
// output.

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dagger/dev/internal/dagger"
)

const (
	// nixImage is the pinned nixos/nix container image.
	nixImage = "nixos/nix:2.32.6@sha256:8b7cc7ccc4c6a3b7852d81db9c4d0875b5a98867729351ed6fbfbf2839f1fa25"

	// devCacheNamespace is the namespace prefix for dev-specific cache volumes.
	devCacheNamespace = "github.com/MacroPower/dotfiles/toolchains/dev"

	// homeConfig is the home-manager configuration name from the flake.
	homeConfig = "jacobcolvin@linux"

	// username is the non-root user created inside the dev container.
	username = "jacobcolvin"

	// homeDir is the home directory for the dev container user.
	homeDir = "/home/jacobcolvin"
)

// devInitScript is the shell script that initializes the git repository
// and overlays local source files in the dev container. It expects BRANCH,
// BASE, and CLONE_URL environment variables to be set.
const devInitScript = `set -e

# Clone if needed (blobless: full history, blobs fetched on demand).
if [ ! -d /src/.git ]; then
  git clone --filter=blob:none --no-checkout \
    "${CLONE_URL}" /src
fi

cd /src

# Fetch latest refs from origin. Non-fatal when the branch already
# exists locally (cached in the Dagger volume from a prior session).
if ! git fetch origin; then
  if git rev-parse --verify "${BRANCH}" >/dev/null 2>&1; then
    echo "WARNING: git fetch origin failed, using cached branch '${BRANCH}'" >&2
  else
    echo "ERROR: git fetch origin failed and branch '${BRANCH}' has no local cache" >&2
    exit 1
  fi
fi

# Checkout or create the branch. Force checkout (-f) avoids "untracked
# working tree files would be overwritten" errors when the cache volume
# retains files from a previous session that are now tracked on the branch.
if git rev-parse --verify "${BRANCH}" >/dev/null 2>&1; then
  git checkout -f "${BRANCH}"
  # Advance local branch to match remote. The cache volume may hold a
  # stale branch tip from a previous session; git fetch updated
  # origin/${BRANCH} but the local ref wasn't moved. Any prior-session
  # commits were already exported to the host by _dev-sync, and the
  # working tree is about to be replaced by rsync, so reset is safe.
  if git rev-parse --verify "origin/${BRANCH}" >/dev/null 2>&1; then
    git reset --hard "origin/${BRANCH}"
  fi
elif git rev-parse --verify "origin/${BRANCH}" >/dev/null 2>&1; then
  git checkout -f -b "${BRANCH}" "origin/${BRANCH}"
elif git rev-parse --verify "origin/${BASE}" >/dev/null 2>&1; then
  git checkout -f -b "${BRANCH}" "origin/${BASE}"
else
  echo "ERROR: cannot create branch '${BRANCH}': ref 'origin/${BASE}' does not exist" >&2
  echo "Ensure the base branch '${BASE}' exists on the remote." >&2
  exit 1
fi

# Validate seed before overlay to prevent wiping /src with empty source.
SEED_FILES=$(ls -A /tmp/src-seed/ 2>/dev/null | wc -l)
if [ "$SEED_FILES" -eq 0 ]; then
  echo "ERROR: seed validation failed: /tmp/src-seed/ is empty" >&2
  exit 1
fi

# Overlay local source (repoSource excludes .git via +ignore).
# rsync --delete removes files present in git but deleted locally.
rsync -a --delete --exclude=.git /tmp/src-seed/ /src/
`

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
	ctr := dag.Container().From(nixImage)
	ctr = ctr.
		WithMountedCache("/nix/store", dag.CacheVolume(devCacheNamespace+":nix-store"),
			dagger.ContainerWithMountedCacheOpts{Source: ctr.Directory("/nix/store")}).
		WithMountedCache("/nix/var/nix", dag.CacheVolume(devCacheNamespace+":nix-var"),
			dagger.ContainerWithMountedCacheOpts{Source: ctr.Directory("/nix/var/nix")}).
		WithMountedCache("/root/.cache/nix", dag.CacheVolume(devCacheNamespace+":nix-eval-cache")).
		WithEnvVariable("NIX_CONFIG", "experimental-features = nix-command flakes\nfilter-syscalls = false\n").
		// rsync is needed for source overlay in DevEnv.
		// Installed before source mount so this layer is source-independent.
		WithExec([]string{"nix", "profile", "install", "nixpkgs#rsync"}).
		WithDirectory("/dotfiles", m.Source).
		WithWorkdir("/dotfiles").
		// Build and activate home-manager configuration.
		WithExec([]string{"nix", "build",
			`.#homeConfigurations."` + homeConfig + `".activationPackage`}).
		WithExec([]string{"mkdir", "-p", homeDir}).
		WithExec([]string{"mkdir", "-p", homeDir + "/.local/state/nix/profiles"}).
		WithExec([]string{"mkdir", "-p", "/nix/var/nix/profiles/per-user/" + username}).
		WithEnvVariable("HOME", homeDir).
		WithEnvVariable("USER", username).
		WithMountedCache(homeDir+"/.krew", dag.CacheVolume(devCacheNamespace+":krew"))

	if kagiApiKey != nil {
		plaintext, err := kagiApiKey.Plaintext(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading kagi api key: %w", err)
		}
		ctr = ctr.WithEnvVariable("KAGI_API_KEY", plaintext)
	}

	return ctr.WithExec([]string{"./result/activate"}).
		// PATH includes home-manager bin dirs + nix paths.
		WithEnvVariable("PATH",
			homeDir+"/.local/state/home-manager/gcroots/current-home/home-path/bin:"+
				homeDir+"/.nix-profile/bin:"+
				"/nix/var/nix/profiles/default/bin:"+
				"/usr/bin:/bin",
		).
		WithEnvVariable("EDITOR", "vim").
		WithEnvVariable("TERM", "xterm-256color").
		WithEnvVariable("IS_SANDBOX", "1").
		// Symlink fish history to cache volume path for persistence.
		WithExec([]string{"sh", "-c",
			"mkdir -p " + homeDir + "/.local/share/fish && " +
				"ln -sf /commandhistory/fish_history " + homeDir + "/.local/share/fish/fish_history",
		}), nil
}

// DevEnv returns a development container with the git repository cloned,
// the requested branch checked out, and local source files overlaid.
// Cache volumes provide per-branch workspace isolation. Unlike [Dev.Dev],
// this does not open an interactive terminal or export results.
func (m *Dev) DevEnv(
	ctx context.Context,
	// Branch to check out in the dev container. Each branch gets its
	// own Dagger cache volume for workspace isolation.
	branch string,
	// Git clone URL for the repository.
	cloneURL string,
	// Base branch name used when creating a new branch that does not
	// exist locally or on the remote. Looked up as origin/<base> in
	// the container clone. Defaults to "main" when empty.
	// +optional
	base string,
	// Override the base container. Uses [Dev.DevBase] when nil.
	// +optional
	ctr *dagger.Container,
	// Working repository source directory to overlay on the checked-out
	// branch. This is the project you want to work on, distinct from the
	// dotfiles source used to build the nix environment.
	// +ignore=[".git"]
	repoSource *dagger.Directory,
	// Kagi API key, forwarded to DevBase when ctr is nil.
	// +optional
	kagiApiKey *dagger.Secret,
) (*dagger.Container, error) {
	if base == "" {
		base = "main"
	}
	if ctr == nil {
		var err error
		ctr, err = m.DevBase(ctx, kagiApiKey)
		if err != nil {
			return nil, err
		}
	}

	return ctr.
		// Stage source on regular filesystem for the seed step.
		WithDirectory("/tmp/src-seed", repoSource).
		// Cache volume at /src so changes survive Terminal().
		// Each branch gets its own volume for workspace isolation.
		WithMountedCache("/src",
			dag.CacheVolume(devCacheNamespace+":src-"+sanitizeCacheKey(branch)),
			dagger.ContainerWithMountedCacheOpts{Sharing: dagger.CacheSharingModePrivate}).
		WithMountedCache("/commandhistory", dag.CacheVolume(devCacheNamespace+":shell-history")).
		WithWorkdir("/src").
		WithEnvVariable("BRANCH", branch).
		WithEnvVariable("BASE", base).
		WithEnvVariable("CLONE_URL", cloneURL).
		// _DEV_TS busts the Dagger function cache on every call. Without
		// it, if repoSource hasn't changed, Dagger returns a cached
		// DevEnv() result and skips git fetch origin, so remote branch
		// updates would not be picked up.
		WithEnvVariable("_DEV_TS", time.Now().String()).
		WithExec([]string{"sh", "-c", devInitScript}), nil
}

// Dev opens an interactive development container with a real git
// repository and returns the modified source directory when the session
// ends. The container is created via [Dev.DevEnv], which clones the
// upstream repo (blobless) and checks out the specified branch, enabling
// pushes, rebases, and other git operations.
//
// Source files from the repo source directory are overlaid on top of the
// checked-out branch, bringing in local uncommitted changes. Each branch
// gets its own Dagger cache volume for workspace isolation.
//
// The returned directory includes .git with full commit history.
//
// +cache="never"
func (m *Dev) Dev(
	ctx context.Context,
	// Branch to check out in the dev container. Each branch gets its
	// own Dagger cache volume for workspace isolation.
	branch string,
	// Git clone URL for the repository.
	cloneURL string,
	// Base branch name used when creating a new branch that does not
	// exist locally or on the remote. Looked up as origin/<base> in
	// the container clone. Defaults to "main" when empty.
	// +optional
	base string,
	// Override the base container. Uses [Dev.DevBase] when nil.
	// +optional
	ctr *dagger.Container,
	// Working repository source directory to overlay on the checked-out
	// branch. This is the project you want to work on, distinct from the
	// dotfiles source used to build the nix environment.
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
) (*dagger.Directory, error) {
	devCtr, err := m.DevEnv(ctx, branch, cloneURL, base, ctr, repoSource, kagiApiKey)
	if err != nil {
		return nil, err
	}

	devCtr = applyDevConfig(devCtr, gitConfig,
		tz, colorterm, termProgram, termProgramVersion)

	// Open interactive terminal. Changes to /src persist in the cache
	// volume through the Terminal() call.
	if len(cmd) == 0 {
		cmd = []string{"fish"}
	}
	devCtr = devCtr.Terminal(dagger.ContainerTerminalOpts{
		Cmd:                           cmd,
		ExperimentalPrivilegedNesting: true,
	})

	// Copy from cache volume to regular filesystem so Directory() can
	// read it (Container.Directory rejects cache mount paths).
	devCtr = devCtr.WithExec([]string{"sh", "-c", "mkdir -p /output && cp -a /src/. /output/"})

	return devCtr.Directory("/output"), nil
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

// sanitizeCacheKey replaces characters that are invalid in Dagger cache
// volume names with hyphens.
func sanitizeCacheKey(name string) string {
	return strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(name)
}
