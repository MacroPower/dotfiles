# CLAUDE.md

## What This Is

Declarative system configuration using **nix-darwin** + **home-manager**, managed as a Nix flake. macOS hosts use [Lix](https://lix.systems/) (a Nix implementation) as the package manager. Configures system packages, Homebrew casks/brews, shell (fish), dev tools, and app settings across macOS and NixOS hosts. We also support a generic "Linux" host that is used for e2e testing via Dagger.

## Key Commands

```bash
# Update all flake inputs
nix flake update

# Validate the flake
nix flake check

# Apply nix-darwin configuration (macOS)
sudo darwin-rebuild switch --flake '.#jacobcolvin@Jacobs-Mac-mini'

# Apply standalone home-manager configuration (Linux)
home-manager switch --flake '.#jacobcolvin@linux'

# Apply NixOS configuration (e.g., OrbStack container)
sudo nixos-rebuild switch --flake '.#nixos-orbstack'
```

## Architecture

- **`flake.nix`**: Entry point. Two helpers: `mkDarwin` (nix-darwin + home-manager) and `mkHome` (standalone home-manager for Linux). Each host passes a `hostConfig` attrset.
- **`hosts/mac.nix`**: System-level nix-darwin config (Nix settings, Homebrew, user accounts). Homebrew lists compose base packages + `hostConfig.homebrew.extra*`. `hosts/linux.nix` is the standalone home-manager equivalent.
- **`hosts/nixos/`**: NixOS system configs: `orbstack.nix` (OrbStack container), `truenas.nix` (TrueNAS server), `common.nix` (shared settings).
- **`home/`**: Home-manager modules imported from `home/default.nix`. Each `.nix` file is a self-contained module for a tool domain (shell, git, editors, k8s, etc.).
- **`configs/`**: Raw config files, normally symlinked into `~/.config/` via `xdg.configFile` and `home.file`.
- **`pkgs/`**: Custom Nix package derivations.
- **`toolchains/`**: Dagger-based dev toolchains.
- **`Taskfile.yaml`**: Task runner for common operations.
