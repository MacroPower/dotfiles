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
- **`lib/stylix.nix`**: Shared stylix theme config (base16 scheme, fonts, cursor) that `mkDarwin`, `mkHome`, and `mkNixOS` import. `home/stylix.nix` turns off the stylix targets of tools that set their own theme.
- **`hosts/shared.nix`**: Nix settings shared between nix-darwin and NixOS (experimental features, GC, store optimization, flake registry).
- **`hosts/darwin/`**: Per-host metadata for macOS machines. `hosts/darwin/default.nix` has system-level nix-darwin config (Homebrew, PAM/Touch ID, user accounts).
- **`hosts/nixos/`**: NixOS system configs: `orbstack.nix` (OrbStack container), `truenas.nix` (TrueNAS server), `default.nix` (shared NixOS settings, imports `../shared.nix` and `clones.nix`), `clones.nix` (keeps each clone under `/tmp/git` whole on every NixOS host).
- **`hosts/linux/`**: Non-NixOS Linux hosts using standalone home-manager (`mkHome`). `container.nix` is the Dagger dev container config.
- **`home/`**: Home-manager modules imported from `home/default.nix`. Each `.nix` file is a self-contained module for a tool domain (shell, git, editors, k8s, etc.).
- **`configs/`**: Raw config files, normally symlinked into `~/.config/` via `xdg.configFile` and `home.file`.
- **`pkgs/`**: Custom Nix package derivations. The `pkg:update` task in `taskfiles/nix/Taskfile.yaml` runs each `pkgs/<name>-update.sh` script as a command of its own. Each script updates the derivations of one upstream project. `CheckPkgUpdate` of the `nix` toolchain fails the commit hook when the commands of that task and the scripts disagree. It also fails when `pkgs/<name>-update.sh` exists and `pkgs/<name>.nix`, which the script updates, does not. When you delete a package file, delete its update script and the command of that script as well.
- **`toolchains/`**: Dagger-based dev toolchains.
- **`Taskfile.yaml`**: Task runner for common operations (`task switch` auto-detects platform and uses `nh`).

## Code Style

- Prefer explicit configuration (e.g. named imports, spelled-out lists) over automatic file discovery or convention-based loading.

## Claude Code Configuration

Claude Code config lives in this repo and is symlinked into `~/.claude/` by `home/claude.nix`. When asked to edit Claude skills, agents, settings, hooks, slash commands, status line, or anything else under `~/.claude/`, edit the source in this repo, not the symlink target.

- **Skills**: `configs/claude/skills/<name>/` (each skill is a directory with `SKILL.md` plus assets).
- **Agents**: `configs/claude/agents/<name>.md`.
- **Settings, hooks, slash commands, status line, MCP servers, env**: defined in `home/claude.nix` (the home-manager module that generates `settings.json` and writes files into `~/.claude/`).
- **Global instructions (`~/.claude/CLAUDE.md`)**: also rendered by `home/claude.nix`.
- **Host-specific instructions**: `dotfiles.claude.hostContext` in the host's file under `hosts/`. `home/claude.nix` appends it to that host's `~/.claude/CLAUDE.md` as the `## Host Environment` section. Put guidance that holds on only one host there, so that the other hosts do not render it.
- **Clone directory**: `cloneRoot` in `home/claude.nix`. Agents clone other repositories into `/tmp/git` on a Linux host and into `~/Library/Caches/git-clones` on a Darwin host. In a skill, send agents to the directory that their instructions name, never to a fixed path.

Permission rules, hook-router command rules, and fetch rules steer model behavior, such as pointing Claude at an MCP tool or prompting before a remote write. They are not a security boundary. The sandbox provides security by limiting access to credentials and the filesystem. Write and review these rules for the behavior they produce, and never add a rule or keep a prompt only to close an escape path.

After editing, run `task switch` to re-link. Never edit files under `~/.claude/` directly -- the next `home-manager` activation will overwrite them.
