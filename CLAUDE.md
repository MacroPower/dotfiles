# CLAUDE.md

## What This Is

Declarative system configuration using **nix-darwin** + **home-manager**, managed as a Nix flake. macOS hosts use [Lix](https://lix.systems/) (a Nix implementation) as the package manager. Configures system packages, Homebrew casks/brews, shell (fish), dev tools, and app settings across macOS and NixOS hosts. We also support a generic "Linux" host that is used for e2e testing via Dagger.

## Key Commands

```bash
# Update all flake inputs
task update

# Validate the flake
task check

# Format all files
task format

# Apply config via task (auto-detects platform)
task switch
```

## Architecture

- **`flake.nix`**: Entry point. Declares inputs, flake-parts scaffolding, and host configurations. Each host passes a `hostConfig` attrset to a builder from `lib/`.
- **`lib/`**: Builder functions (`mkDarwin`, `mkHome`, `mkNixOS`) and shared config (overlays, home-manager modules). `lib/default.nix` is the entry point, each builder lives in its own file.
- **`hosts/shared.nix`**: Nix settings shared between nix-darwin and NixOS (experimental features, GC, store optimization, flake registry).
- **`hosts/stylix.nix`**: Shared stylix theme config (base16 scheme, fonts, cursor) imported by all three helpers.
- **`hosts/darwin/`**: Per-host metadata for macOS machines. `hosts/darwin/default.nix` has system-level nix-darwin config (Homebrew, PAM/Touch ID, user accounts).
- **`hosts/nixos/`**: NixOS system configs: `orbstack.nix` (OrbStack container), `truenas.nix` (TrueNAS server), `default.nix` (shared NixOS settings, imports `../shared.nix`).
- **`hosts/linux/`**: Non-NixOS Linux hosts using standalone home-manager (`mkHome`). `container.nix` is the Dagger dev container config.
- **`home/`**: Home-manager modules imported from `home/default.nix`. Each `.nix` file is a self-contained module for a tool domain (shell, git, editors, k8s, etc.).
- **`configs/`**: Raw config files, normally symlinked into `~/.config/` via `xdg.configFile` and `home.file`.
- **`pkgs/`**: Custom Nix package derivations.
- **`toolchains/`**: Dagger-based dev toolchains.
- **`Taskfile.yaml`**: Task runner for common operations (`task switch` auto-detects platform and uses `nh`).

## Code Style

- Prefer explicit configuration (e.g. named imports, spelled-out lists) over automatic file discovery or convention-based loading.
