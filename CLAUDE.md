# CLAUDE.md

## What This Is

Declarative system configuration using **nix-darwin** + **home-manager**, managed as a Nix flake. macOS hosts use [Lix](https://lix.systems/) (a Nix implementation) as the package manager. Configures system packages, Homebrew casks/brews, shell (fish), dev tools, and app settings across macOS and NixOS hosts. We also support a generic "Linux" host that is used for e2e testing via Dagger.

## Key Commands

```bash
# Update all flake inputs
nix flake update

# Validate the flake
nix flake check

# Format Nix files
nix fmt

# Apply config via nh (recommended, uses $FLAKE from env)
nh darwin switch        # macOS
nh os switch            # NixOS
nh home switch          # standalone home-manager (Linux)

# Or via task runner (auto-detects platform)
task switch

# Direct rebuild commands (equivalent, but without nh's diff/confirmation UX)
sudo darwin-rebuild switch --flake '.#jacobcolvin@Jacobs-Mac-mini'
home-manager switch --flake '.#jacobcolvin@linux'
sudo nixos-rebuild switch --flake '.#nixos-orbstack'
```

## Architecture

- **`flake.nix`**: Entry point. Three helpers: `mkDarwin` (nix-darwin + home-manager), `mkHome` (standalone home-manager for Linux), and `mkNixOS` (NixOS + home-manager). Each host passes a `hostConfig` attrset.
- **`hosts/shared.nix`**: Nix settings shared between nix-darwin and NixOS (experimental features, GC, store optimization, flake registry).
- **`hosts/stylix.nix`**: Shared stylix theme config (base16 scheme, fonts, cursor) imported by all three helpers.
- **`hosts/mac.nix`**: System-level nix-darwin config (Homebrew, PAM/Touch ID, user accounts). Imports `shared.nix`. `hosts/linux.nix` is the standalone home-manager equivalent.
- **`hosts/nixos/`**: NixOS system configs: `orbstack.nix` (OrbStack container), `truenas.nix` (TrueNAS server), `common.nix` (shared NixOS settings, imports `../shared.nix`).
- **`home/`**: Home-manager modules imported from `home/default.nix`. Each `.nix` file is a self-contained module for a tool domain (shell, git, editors, k8s, etc.).
- **`configs/`**: Raw config files, normally symlinked into `~/.config/` via `xdg.configFile` and `home.file`.
- **`pkgs/`**: Custom Nix package derivations.
- **`toolchains/`**: Dagger-based dev toolchains.
- **`Taskfile.yaml`**: Task runner for common operations (`task switch` auto-detects platform and uses `nh`).
