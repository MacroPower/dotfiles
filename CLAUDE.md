# CLAUDE.md

## Overview

Declarative system configuration using **nix-darwin** + **home-manager**, managed as a Nix flake. macOS hosts use [Lix](https://lix.systems/) (a Nix implementation) as the package manager. Configures system packages, Homebrew casks and brews, shell (fish), dev tools, and app settings across macOS and NixOS hosts. We also support a generic "Linux" host that is used for e2e testing via Dagger.

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
- **`hosts/nixos/`**: NixOS system configs. Each host has its own file, which imports `default.nix`: `orbstack.nix` (OrbStack container), `truenas.nix` (TrueNAS server), and `terrarium.nix` (Lima sandbox VM). `default.nix` holds the shared NixOS settings and imports `../shared.nix`, `../options.nix` (the `dotfiles.system` options), `claude-tmp.nix`, and `clones.nix`, so every NixOS host gets those modules. `claude-tmp.nix` keeps each session directory of Claude Code under `/tmp/claude-<uid>` whole, and `clones.nix` keeps each clone under `/tmp/git` whole. `tmpfiles-check.nix` holds the function that builds a system check for `claude-tmp.nix` and for `clones.nix`. The check fails the system build when the daily cleanup of `/tmp` deletes a file under `/tmp/claude-<uid>` or under `/tmp/git`. `orbstack.nix` also imports `orbstack-hardware.nix` (OrbStack guest settings), and `terrarium.nix` also imports `lima-hardware.nix` (Lima guest settings).
- **`hosts/linux/`**: Non-NixOS Linux hosts using standalone home-manager (`mkHome`). `container.nix` is the Dagger dev container config.
- **`home/`**: Home-manager modules. Each `.nix` file is a self-contained module for a tool domain, such as shell, git, editors, or Kubernetes. The builders in `lib/` give each host `home/default.nix` and the `homeModule` of the host's file under `hosts/`. `home/default.nix` imports the modules that every host gets, such as `fish.nix` and `git.nix`. The `homeModule` imports the other modules that the host needs, such as `development.nix` and `kubernetes.nix`. Every `homeModule` must import `claude.nix`, because `fish.nix` reads the `dotfiles.claude.searchRewrite.findExcludes` option that `claude.nix` declares. The home configuration of a host without `claude.nix` fails to evaluate. `lib/mkDarwin.nix` imports more modules for every macOS host, such as `virtualization.nix` and `wireshark.nix`. The `homeModule` of every macOS host must also import `kubernetes.nix`, because `lib/mkDarwin.nix` sets the `dotfiles.kubernetes.extraKrewPlugins` option that `kubernetes.nix` declares. Some modules reach a host through another module of `home/`. For example, `claude.nix` imports `playwright.nix`. Import a new module in `home/default.nix` only when every host needs it.
- **`configs/`**: Raw config files, normally symlinked into `~/.config/` via `xdg.configFile` and `home.file`.
- **`pkgs/`**: Custom Nix package derivations. The `pkg:update` task in `taskfiles/nix/Taskfile.yaml` runs each `pkgs/<name>-update.sh` script as a command of its own. Each script updates the derivations of one upstream project. `CheckPkgUpdate` of the `nix` toolchain fails the commit hook when the commands of that task and the scripts disagree, or when the tree does not hold a package file that the check requires. For `pkgs/<name>-update.sh`, the check requires `pkgs/<name>.nix` and each file that the script names as `$DIRNAME/<file>.nix`, so name each package file in a script in that form. When you delete a package file, delete the lines of each update script that update it, and delete the script and its command when the file carries the name of the script.
- **`toolchains/`**: Dagger-based dev toolchains.
- **`Taskfile.yaml`**: Task runner for common operations (`task switch` auto-detects platform and uses `nh`).

## Code Style

- Prefer explicit configuration, such as named imports and spelled-out lists, over automatic file discovery or convention-based loading.

## Claude Code Configuration

Claude Code config lives in this repo and is symlinked into `~/.claude/` by `home/claude.nix`. When asked to edit Claude skills, agents, settings, hooks, slash commands, status line, or anything else under `~/.claude/`, edit the source in this repo, not the symlink target.

- **Skills**: `configs/claude/skills/<name>/` (each skill is a directory with `SKILL.md` plus assets).
- **Agents**: `configs/claude/agents/<name>.md`.
- **Settings, hooks, slash commands, status line, MCP servers, env**: defined in `home/claude.nix` (the home-manager module that generates `settings.json` and writes files into `~/.claude/`).
- **Global instructions (`~/.claude/CLAUDE.md`)**: also rendered by `home/claude.nix`.
- **Host-specific instructions**: `dotfiles.claude.hostContext` in the host's file under `hosts/`. `home/claude.nix` appends it to that host's `~/.claude/CLAUDE.md` as the `## Host Environment` section. Put guidance that holds on only one host there, so that the other hosts do not render it.
- **Clone directory**: `cloneRoot` in `home/claude.nix`. Agents clone other repositories into `/tmp/git` on a Linux host and into `~/Library/Caches/git-clones` on a Darwin host. In a skill, send agents to the directory that their instructions name, never to a fixed path.
- **Temporary directory**: `claudeTmpDir` in `home/claude.nix`. Claude Code keeps the scratchpad and the other temporary files of each session in `/tmp/claude-<uid>` on a Linux host and in `~/Library/Caches/claude-tmp/claude-<uid>` on a Darwin host. In a skill, name the scratchpad of the session, never a fixed path.

Permission rules, hook-router command rules, and fetch rules steer model behavior, such as pointing Claude at an MCP tool or prompting before a remote write. They are not a security boundary. The sandbox provides security by limiting access to credentials and the filesystem. Write and review these rules for the behavior they produce, and never add a rule or keep a prompt only to close an escape path.

After editing, run `task switch` to re-link. Never edit files under `~/.claude/` directly. The next `home-manager` activation overwrites them.
