# Nix By Example -- Community Dotfiles Survey

## ryan4yin/nix-config

**Source:** [github.com/ryan4yin/nix-config](https://github.com/ryan4yin/nix-config)

One of the most comprehensive community Nix configs, managing 10+ hosts across macOS (nix-darwin), NixOS desktops, NixOS servers, and a KubeVirt homelab cluster. The author also wrote [NixOS & Flakes Book](https://github.com/ryan4yin/nixos-and-flakes-book), so the repo is heavily commented and structured for teaching.

### Comparison Table

| Aspect                       | ryan4yin/nix-config                                                                                                                                                                                                                                                    | Our dotfiles                                                                                                                                                                                 |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Flake structure**          | Minimal `flake.nix` (just `outputs = inputs: import ./outputs inputs`). All output logic lives in `outputs/default.nix`, split by architecture (`outputs/aarch64-darwin/`, `outputs/x86_64-linux/`, etc.). Uses haumea for auto-loading.                               | Everything in `flake.nix` via flake-parts. Three helper functions (`mkDarwin`, `mkHome`, `mkNixOS`) defined inline.                                                                          |
| **Module organization**      | Three-tier: `modules/base/` (cross-platform system modules), `modules/darwin/` (macOS system), `modules/nixos/` (NixOS system with `base/`, `desktop/`, `server/` subdivisions).                                                                                       | Flat: `hosts/shared.nix` + `hosts/mac.nix` + `hosts/nixos/common.nix`. No dedicated `modules/` directory; system config lives directly in host files.                                        |
| **Home-manager layout**      | `home/base/` (cross-platform, subdivided into `core/`, `tui/`, `gui/`), `home/linux/` (Linux-specific), `home/darwin/` (macOS-specific). Each subdirectory further organizes by concern (editors, shells, cloud, encryption, etc.).                                    | Flat `home/` with 14 .nix files (fish.nix, git.nix, vim.nix, kubernetes.nix, etc.). No platform-specific split; platform conditionals handled inline or via `mkEnableOption` guards.         |
| **Custom lib**               | Dedicated `lib/` directory with `macosSystem.nix`, `nixosSystem.nix`, `colmenaSystem.nix`, `attrs.nix`, and infrastructure generators (`genK3sServerModule.nix`, `genKubeVirtHostModule.nix`). Also includes `scanPaths` for auto-loading and `relativeToRoot` helper. | Helper functions defined inline in `flake.nix`. No separate lib directory.                                                                                                                   |
| **Variables/constants**      | Dedicated `vars/` directory (`vars/default.nix`, `vars/networking.nix`) holding username, email, SSH keys, networking constants. Imported once as `myvars` and passed via `specialArgs` everywhere.                                                                    | `hosts/options.nix` and `home/options.nix` define NixOS/HM options (`dotfiles.system.*`, `dotfiles.git.*`, `dotfiles.shell.*`). Values set per-host via `hostConfig` attrset in `flake.nix`. |
| **Overlays**                 | Dedicated `overlays/` directory with auto-loading `default.nix` (reads directory, imports all files except default.nix/README.md). Currently has fcitx5 overlay.                                                                                                       | Overlays defined inline in `flake.nix` as a list (`sharedOverlays`). One local overlay (chief package) + three external overlays.                                                            |
| **Secrets**                  | agenix with a separate private git repo (`nix-secrets`). Split config: `secrets/darwin.nix` and `secrets/nixos.nix` with detailed permission tiers (noaccess, high_security, user_readable). Host SSH keys as age identities.                                          | sops-nix with age encryption. Single `.sops.yaml` config, secrets in `secrets/` directory. Simpler setup with fewer secrets (primarily API keys).                                            |
| **Operations (task runner)** | Justfile with nushell as shell. Grouped commands: nix (up, test, clean, gc, repl, fmt, verify-store, repair-store), desktop (local deploy per platform), homelab (colmena remote deploy, VM image upload), k8s, git, nixpkgs review. ~200 lines.                       | Taskfile.yaml with namespaced includes (nix, vm, secrets). Main tasks: switch (auto-detect platform), update, check (dagger), dev. ~50 lines.                                                |
| **CI/CD**                    | GitHub Actions: `flake_evaltests.yml` (runs `nix eval .#evalTests`), plus `mirror_to_gitee.yml`. Also uses colmena for remote NixOS deployment.                                                                                                                        | Dagger-based: `task check` runs `dagger check`. Flake checks validate all host configs. No GitHub Actions.                                                                                   |
| **Remote deployment**        | Colmena for multi-host NixOS deployment with tags (`@virt-*`, `@kubevirt-*`, `@k3s-prod-*`). Per-node nixpkgs and specialArgs.                                                                                                                                         | No remote deployment tool. NixOS hosts (OrbStack, TrueNAS) are managed locally.                                                                                                              |
| **Eval tests**               | Haumea-based eval tests per architecture (`outputs/aarch64-darwin/tests/`). Validated in CI via `nix eval .#evalTests`.                                                                                                                                                | Flake checks that build/activate all configurations. No separate eval test framework.                                                                                                        |
| **Theming**                  | Catppuccin via `catppuccin/nix` flake input. Applied in `home/base/core/theme.nix`.                                                                                                                                                                                    | Stylix with OneDark base16 scheme. Configured in `hosts/stylix.nix` + `home/stylix.nix`.                                                                                                     |
| **Multiple nixpkgs**         | Five nixpkgs instances: `nixpkgs` (unstable), `nixpkgs-stable`, `nixpkgs-2505`, `nixpkgs-patched` (custom fork), `nixpkgs-master`. All passed as `pkgs-*` via `specialArgs`.                                                                                           | Single `nixpkgs` (unstable).                                                                                                                                                                 |
| **Hosts count**              | 10+ hosts: 2 Darwin (fern, frieren), physical NixOS desktops (shoukei), NixOS VMs (idols-ai, idols-akane, idols-aquamarine, idols-ruby, idols-kana), k3s cluster nodes.                                                                                                | 5 hosts: 2 Darwin, 2 NixOS (orbstack, truenas), 1 generic Linux.                                                                                                                             |
| **Host config pattern**      | Each host is a directory (`hosts/darwin-fern/`) with `default.nix` (system overrides) and `home.nix` (user overrides). Modules are composed in `outputs/` per-architecture.                                                                                            | Hosts declared in `flake.nix` by calling `mkDarwin`/`mkNixOS`/`mkHome` with a `hostConfig` attrset and explicit module lists.                                                                |
| **Dev shell**                | `devShells.default` via `mkShell` with nixfmt, deadnix, statix, typos, prettier. Pre-commit hooks via cachix/git-hooks.nix.                                                                                                                                            | No devShell. Formatting via treefmt-nix flake module (`nix fmt`).                                                                                                                            |
| **NixOS extras**             | lanzaboote (secure boot), preservation (impermanence successor), disko (declarative partitioning), nixos-generators (ISO/qcow2/docker images), nixpak (sandboxing), Apple Silicon support (nixos-apple-silicon).                                                       | Minimal NixOS: nix-ld, nh, SSH config. No disk management, secure boot, or image generation.                                                                                                 |
| **Spell checking**           | typos (`.typos.toml`) integrated as pre-commit hook and in devShell.                                                                                                                                                                                                   | Not configured.                                                                                                                                                                              |
| **Per-directory READMEs**    | Every major directory has a README.md explaining its purpose and structure (`hosts/README.md`, `home/README.md`, `modules/README.md`, `overlays/README.md`, `outputs/README.md`, `secrets/README.md`, `vars/README.md`, `lib/README.md`).                              | Top-level README.md only. CLAUDE.md serves as the architecture doc.                                                                                                                          |

### Home-Manager Modules Comparison

Modules in ryan4yin's `home/` that we lack or configure differently:

| Their module                                   | Our equivalent                                        | Notes                                                                                        |
| ---------------------------------------------- | ----------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `home/base/core/shells/` (nushell, bash, fish) | `home/fish.nix`                                       | They support multiple shells; we only configure fish.                                        |
| `home/base/core/starship.nix`                  | (none)                                                | Cross-shell prompt. We use Tide (fish-specific).                                             |
| `home/base/core/zellij/`                       | (none)                                                | Terminal multiplexer. We don't configure one.                                                |
| `home/base/core/yazi.nix`                      | (none)                                                | TUI file manager.                                                                            |
| `home/base/tui/encryption/`                    | (none)                                                | GPG + age encryption tooling as HM modules.                                                  |
| `home/base/tui/password-store/`                | (none)                                                | pass (password-store) integration.                                                           |
| `home/base/tui/cloud/`                         | `home/kubernetes.nix`                                 | They have broader cloud tooling; ours focuses on k8s.                                        |
| `home/base/tui/ssh.nix`                        | `home/default.nix` (SSH section)                      | Their SSH config is more detailed (per-host match blocks).                                   |
| `home/base/gui/`                               | `home/vscode.nix`, `home/zed.nix`, `home/ghostty.nix` | They separate GUI apps into a dedicated tier.                                                |
| `home/darwin/aerospace/`                       | (none)                                                | macOS tiling window manager config via HM. We use native macOS tiling via `system.defaults`. |
| `home/linux/gui/`                              | (none)                                                | Linux-specific GUI (Hyprland, Wayland, etc.). Not applicable to our use case.                |

### Candidate Changes

1. **Extract helper functions into a `lib/` directory**
   - **Rationale:** Our `mkDarwin`, `mkHome`, `mkNixOS` functions are defined inline in `flake.nix`, making it ~12.5K. ryan4yin splits these into `lib/macosSystem.nix`, `lib/nixosSystem.nix`, etc. A dedicated `lib/` would reduce flake.nix complexity and make helpers reusable/testable.
   - **Source:** `lib/macosSystem.nix`, `lib/nixosSystem.nix`, `lib/default.nix`
   - **Impact:** Medium. Refactoring only; no behavior change.

2. **Centralize user/host variables into a `vars/` or similar constants file**
   - **Rationale:** ryan4yin uses `vars/default.nix` to hold username, email, SSH keys, and networking constants, imported once and passed everywhere via `specialArgs`. Our approach uses NixOS/HM options (`dotfiles.system.*`, `dotfiles.git.*`) which is more idiomatic but means values are scattered across `flake.nix` hostConfig blocks. A constants file could reduce duplication for values shared across all hosts (SSH keys, email, etc.) while keeping per-host options for things that actually vary.
   - **Source:** `vars/default.nix`, `vars/networking.nix`
   - **Impact:** Low. Our options approach already works well; this is an alternative pattern worth noting but not necessarily better.

3. **Split home-manager modules by platform (`home/base/`, `home/darwin/`, `home/linux/`)**
   - **Rationale:** ryan4yin's three-tier split (base/darwin/linux) makes platform-specific code explicit rather than using inline conditionals. Our flat `home/` with `mkEnableOption` guards works, but as the config grows, a platform split could reduce the need for conditional logic. For example, `home/darwin/aerospace/` vs. checking `isDarwin` inline.
   - **Source:** `home/base/`, `home/darwin/`, `home/linux/`
   - **Impact:** Medium. Would require restructuring home/ directory but could improve clarity.

4. **Add pre-commit hooks (via cachix/git-hooks.nix or similar)**
   - **Rationale:** ryan4yin integrates pre-commit hooks for nixfmt, typos, and prettier. Our treefmt handles formatting via `nix fmt`, but there is no enforcement at commit time. Pre-commit hooks would catch formatting issues before they reach the repo.
   - **Source:** `outputs/default.nix` (checks section), devShell with shellHook
   - **Impact:** Low. Nice-to-have enforcement layer on top of existing treefmt.

5. **Add typos spell checker to treefmt or pre-commit**
   - **Rationale:** ryan4yin uses `typos` (a fast spell checker) with auto-fix enabled. It catches typos in code, comments, and config files. Could be added to our treefmt pipeline alongside nixfmt, deadnix, statix.
   - **Source:** `.typos.toml`, pre-commit hook config
   - **Impact:** Low. Minor quality-of-life improvement.

6. **Add eval tests for configuration validation**
   - **Rationale:** ryan4yin has a structured eval test framework via haumea that validates configuration properties without doing full builds. Our flake checks do full build/activation tests, but lightweight eval tests could catch issues faster (they run in CI via `nix eval .#evalTests`).
   - **Source:** `outputs/aarch64-darwin/tests/`, `outputs/default.nix` (evalTests section)
   - **Impact:** Medium. Would complement existing flake checks with faster feedback.

7. **Adopt colmena or deploy-rs for remote NixOS deployment**
   - **Rationale:** ryan4yin uses colmena with tags for deploying to multiple NixOS hosts via SSH. Our TrueNAS host could benefit from remote deployment rather than requiring local access. Not relevant for OrbStack (which is local).
   - **Source:** `Justfile` (colmena commands), `lib/colmenaSystem.nix`, `outputs/default.nix` (colmena section)
   - **Impact:** Low. Only relevant if we add more remote NixOS hosts.

8. **Organize Justfile/Taskfile commands with groups**
   - **Rationale:** ryan4yin's Justfile uses `[group('nix')]`, `[group('homelab')]`, `[group('desktop')]` annotations to categorize commands. Our Taskfile uses namespace includes (taskfiles/nix, taskfiles/vm, taskfiles/secrets) which achieves similar grouping. The pattern is comparable, but ryan4yin also includes many utility commands (repl, history, clean, gc, verify-store, repair-store, gcroot) that we lack.
   - **Source:** `Justfile`
   - **Impact:** Low. We could add some of these utility tasks (repl, gc, history, verify-store) to our Taskfile.

9. **Consider per-directory README.md files**
   - **Rationale:** Every major directory in ryan4yin's config has a README explaining structure and decisions. This is especially helpful for a public config. Our CLAUDE.md serves this purpose for AI-assisted development, but human-facing READMEs per directory could help onboarding.
   - **Source:** `hosts/README.md`, `home/README.md`, `modules/README.md`, `lib/README.md`, etc.
   - **Impact:** Low. Documentation improvement only.

10. **Add a devShell for the flake**
    - **Rationale:** ryan4yin provides a `devShells.default` with all formatting/linting tools, so contributors can `nix develop` to get the right environment. We rely on system-installed tools and treefmt. A devShell would ensure consistent tooling for anyone working on the config.
    - **Source:** `outputs/default.nix` (devShells section)
    - **Impact:** Low. Convenience improvement.

## wimpysworld/nix-config

**Source:** [github.com/wimpysworld/nix-config](https://github.com/wimpysworld/nix-config)

Martin Wimpress's (Wimpy) NixOS, nix-darwin, and Home Manager config. Manages 15+ hosts across NixOS workstations, NixOS servers, a Darwin laptop, Lima VMs, WSL, and ISO images. Uses Determinate Nix, FlakeHub, sops-nix, and a TOML-based host registry. The repo tracks the NixOS stable channel (25.11) with an unstable overlay, in contrast to many community configs that run unstable everywhere.

### Comparison Table

| Aspect                          | wimpysworld/nix-config                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | Our dotfiles                                                                                                                                                                                                   |
| ------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Flake structure**             | Clean `flake.nix` with outputs delegated to `lib/flake-builders.nix`. Host definitions live in TOML registries (`lib/registry-systems.toml`, `lib/registry-users.toml`) parsed by `builtins.fromTOML`. Builder functions iterate registries to generate all configs.                                                                                                                                                                                                                                              | Everything in `flake.nix` via flake-parts. Three helper functions (`mkDarwin`, `mkHome`, `mkNixOS`) defined inline with explicit host declarations.                                                            |
| **common/ directory**           | `common/default.nix` holds config shared between NixOS and Darwin (documentation, system packages, nixpkgs overlays, nix settings, channel disable). Both `nixos/default.nix` and `darwin/default.nix` import `../common`.                                                                                                                                                                                                                                                                                        | `hosts/shared.nix` serves the same purpose (shared nix settings, GC, store optimization, flake registry). Imported by both `hosts/mac.nix` and `hosts/nixos/common.nix`. Equivalent pattern, different naming. |
| **Module organization**         | Three top-level directories: `common/`, `darwin/`, `nixos/`. Each platform dir has a `_mixins/` subdirectory with domain-organized modules (desktop, hardware, network, users, virtualisation, etc.). Modules are granular -- one .nix file per tool/concern.                                                                                                                                                                                                                                                     | Flat: `hosts/shared.nix` + `hosts/mac.nix` + `hosts/nixos/common.nix`. No `_mixins/` pattern; system config lives directly in host files.                                                                      |
| **Home-manager layout**         | `home-manager/default.nix` imports `_mixins/` subdirectories: `desktop/`, `development/`, `filesync/`, `scripts/`, `services/`, `terminal/`, `users/`. Development is deeply nested (one directory per language/tool: `go/`, `rust/`, `python/`, `claude-code/`, `vscode/`, `zed-editor/`, etc.).                                                                                                                                                                                                                 | Flat `home/` with 14 .nix files. No subdirectory nesting. Platform conditionals handled inline or via `mkEnableOption` guards.                                                                                 |
| **Host definition pattern**     | TOML registry (`lib/registry-systems.toml`) defines all hosts declaratively with structured fields: kind, platform, formFactor, gpu vendors/compute/vram, display outputs/resolution/scale/workspaces, keyboard layout, tags. Builder functions iterate the registry to generate NixOS/Darwin/HM configs. Per-host dirs (e.g. `nixos/vader/`, `darwin/momin/`) hold hardware-specific overrides only.                                                                                                             | Hosts declared inline in `flake.nix` by calling `mkDarwin`/`mkNixOS`/`mkHome` with a `hostConfig` attrset. Per-host config is a flat set of option values in the flake.                                        |
| **Custom options system**       | `lib/noughty/default.nix` -- a rich NixOS options module (~500 lines) with typed options for host identity (name, kind, platform, formFactor), derived booleans (`host.is.workstation`, `host.is.laptop`, `host.is.darwin`), GPU classification (vendors, compute acceleration, VRAM), display configuration (multi-monitor, primary detection, orientation, ultrawide, high-DPI), keyboard/locale derivation. Injected as `noughtyLib` module argument with helper functions (`hostHasTag`, `isUser`, `isHost`). | `hosts/options.nix` and `home/options.nix` define simpler options under `dotfiles.*` namespace. Fewer derived booleans; platform checks done via `pkgs.stdenv` inline.                                         |
| **Tag-based feature selection** | Hosts carry freeform tags (studio, thinkpad, gamedev, inference, etc.) and users carry tags (developer, admin, family). Modules use `noughtyLib.hostHasTag "studio"` to conditionally enable features. Tags are the primary composition mechanism.                                                                                                                                                                                                                                                                | `mkEnableOption` guards on individual HM modules (e.g. `dotfiles.kubernetes.enable`). Feature toggling is per-option rather than tag-based.                                                                    |
| **Overlays**                    | Dedicated `overlays/default.nix` with three named overlays: `localPackages` (imports `pkgs/`), `modifiedPackages` (version pins, Darwin build fixes, package overrides), `unstablePackages` (exposes `pkgs.unstable`). Applied in both `common/default.nix` and `home-manager/default.nix`.                                                                                                                                                                                                                       | Overlays defined inline in `flake.nix` as a list (`sharedOverlays`). One local overlay (chief package) + three external overlays.                                                                              |
| **Secrets**                     | sops-nix with per-host secret files (`secrets/host-vader.yaml`, `secrets/host-momin.yaml`, etc.) plus domain-specific files (ssh.yaml, ai.yaml, tailscale.yaml, gnupg.yaml, etc.). Extensive: 20+ secret files. SSH host keys managed via sops (not generated on first boot).                                                                                                                                                                                                                                     | sops-nix with age encryption. Single `.sops.yaml` config, secrets in `secrets/` directory. Simpler setup with fewer secrets. Same tool, smaller scope.                                                         |
| **Nix implementation**          | Determinate Nix via `determinate.darwinModules.default` / `determinate.nixosModules.default`. Includes native Linux builder on Darwin, parallel eval, lazy-trees, increased download parallelism (64 max-substitution-jobs, 128 http-connections).                                                                                                                                                                                                                                                                | Lix (on macOS hosts). Experimental features enabled (nix-command, flakes). Standard substitution settings.                                                                                                     |
| **nixpkgs channel**             | Stable (nixos-25.11) as primary, with `nixpkgs-unstable` available via `pkgs.unstable` overlay.                                                                                                                                                                                                                                                                                                                                                                                                                   | Unstable (nixpkgs-unstable) as primary. No stable channel.                                                                                                                                                     |
| **Operations (task runner)**    | Justfile (~30KB). Extensive commands organized by domain: nix management, host switching (per-platform), secrets management (sops rotation, key enrollment), Lima VM management, WSL bootstrapping, ISO building, hardware scanning.                                                                                                                                                                                                                                                                              | Taskfile.yaml with namespaced includes. ~50 lines. Core tasks: switch, update, check, format.                                                                                                                  |
| **Theming**                     | Catppuccin via `catppuccin/nix` flake input. Custom palette helper in `lib/flake-builders.nix` (`mkCatppuccinPalette`) that pre-computes color access functions (hex, RGB, HSL, Hyprland-specific, CSS rgba), VT color mapping, dark/light detection.                                                                                                                                                                                                                                                             | Stylix with OneDark base16 scheme. Configured in `hosts/stylix.nix` + `home/stylix.nix`. Automatic propagation to supported programs.                                                                          |
| **Custom packages**             | `pkgs/` directory with 15+ packages (DaVinci Resolve, fonts, OBS plugins, game engines, etc.). Exposed via `localPackages` overlay with platform filtering (`meta.platforms`).                                                                                                                                                                                                                                                                                                                                    | `pkgs/` directory with custom derivations. Exposed via inline overlay.                                                                                                                                         |
| **Nix build priority**          | `nix.daemonProcessType = "Background"` and `nix.daemonIOLowPriority = true` on Darwin; `nix.daemonCPUSchedPolicy = "idle"` and `nix.daemonIOSchedClass = "idle"` on NixOS workstations. Prevents audio stutter and UI jank during builds.                                                                                                                                                                                                                                                                         | Not configured. Nix builds run at default priority.                                                                                                                                                            |
| **Homebrew**                    | Managed via `nix-homebrew` flake module with Rosetta support, auto-migration, `cleanup = "zap"` (removes unmanaged casks).                                                                                                                                                                                                                                                                                                                                                                                        | Homebrew managed via `nix-darwin` homebrew module. Similar approach.                                                                                                                                           |
| **Custom NixOS modules**        | `modules/nixos/` exports reusable NixOS modules (falcon-sensor, wavebox) consumed via `outputs.nixosModules.*`.                                                                                                                                                                                                                                                                                                                                                                                                   | No exported NixOS modules.                                                                                                                                                                                     |
| **DevShell**                    | `devShells.default` with deadnix, git, home-manager, jq, just, micro, nh, nixfmt, nix-output-monitor, openssh, sops, statix, taplo. Also includes packages from Determinate, disko, and fh flake inputs.                                                                                                                                                                                                                                                                                                          | No devShell. Formatting via treefmt-nix flake module.                                                                                                                                                          |
| **CI/CD**                       | GitHub Actions via `.github/workflows/`. Dependabot for flake lock updates.                                                                                                                                                                                                                                                                                                                                                                                                                                       | Dagger-based: `task check`. No GitHub Actions.                                                                                                                                                                 |
| **Per-host README**             | Not present (no per-host READMEs), but the top-level README.md (~26KB) is detailed with screenshots and per-host tables.                                                                                                                                                                                                                                                                                                                                                                                          | Top-level README.md only.                                                                                                                                                                                      |

### Home-Manager Modules Comparison

Modules in wimpysworld's `home-manager/_mixins/` that we lack or configure differently:

| Their module                                                         | Our equivalent       | Notes                                                                                     |
| -------------------------------------------------------------------- | -------------------- | ----------------------------------------------------------------------------------------- |
| `terminal/starship.nix`                                              | (none)               | Cross-shell prompt (~11.5KB of config). We use Tide (fish-specific).                      |
| `terminal/yazi.nix`                                                  | (none)               | TUI file manager with extensive keybindings (~20KB).                                      |
| `terminal/bat.nix`, `bottom.nix`, `btop.nix`                         | (none)               | System monitoring and cat-replacement tools.                                              |
| `terminal/eza.nix`, `fd.nix`, `fzf.nix`, `ripgrep.nix`, `zoxide.nix` | (none) as HM modules | We install some of these as packages but don't configure them via HM programs.\* options. |
| `development/claude-code/`                                           | (none)               | Claude Code configuration managed via HM.                                                 |
| `development/direnv/`                                                | (none)               | direnv-instant integration. We install direnv but don't configure it via HM.              |
| `development/go/`, `rust/`, `python/`, `c/`                          | (none)               | Per-language development environments as HM modules.                                      |
| `development/neovim/`                                                | `home/vim.nix`       | Both configure Neovim; theirs is a directory with multiple files.                         |
| `development/vscode/`                                                | `home/vscode.nix`    | Similar approach; theirs uses nix-vscode-extensions for marketplace access.               |
| `development/zed-editor/`                                            | `home/zed.nix`       | Both configure Zed via HM.                                                                |
| `desktop/`                                                           | `home/ghostty.nix`   | Their desktop mixins handle kitty, Hyprland, Waybar, etc. Our GUI config is simpler.      |
| `filesync/`                                                          | (none)               | Dropbox and Syncthing integration via HM.                                                 |
| `services/`                                                          | (none)               | Background services (borgbackup, ollama, etc.) managed via HM.                            |
| `scripts/`                                                           | (none)               | Custom scripts packaged and installed via HM.                                             |

### Candidate Changes

1. **TOML-based host registry for declarative host definitions**
   - **Rationale:** Wimpy defines all hosts in `lib/registry-systems.toml` with structured fields (kind, platform, GPU specs, display config, tags) and builder functions iterate the registry to generate configs. This separates host metadata from Nix code. Our inline `hostConfig` attrsets in `flake.nix` mix data with logic. A TOML registry would make host inventory scannable without understanding Nix, and new hosts could be added by editing a data file.
   - **Source:** `lib/registry-systems.toml`, `lib/registry-users.toml`, `lib/flake-builders.nix` (resolveEntry, mkSystemConfig, generateConfigs)
   - **Impact:** Medium. Significant refactor of flake.nix but cleaner separation of concerns.

2. **Extract flake builder functions into a `lib/` directory**
   - **Rationale:** Same pattern as ryan4yin (US-001 candidate #1). Wimpy's `lib/flake-builders.nix` is a ~13KB file containing `mkDarwin`, `mkNixos`, `mkHome`, `forAllSystems`, and registry iteration helpers. Our inline helpers in `flake.nix` could benefit from the same extraction.
   - **Source:** `lib/default.nix`, `lib/flake-builders.nix`
   - **Impact:** Medium. Refactoring only; reinforces US-001 candidate #1.

3. **Rich typed options module for host classification**
   - **Rationale:** The `noughty` module provides typed options for host identity, GPU classification, display configuration, keyboard/locale derivation, and derived booleans (`host.is.workstation`, `host.is.laptop`, `host.is.darwin`, etc.). This is more structured than our `dotfiles.*` options and enables cleaner conditional logic in modules (e.g. `lib.mkIf host.is.workstation` instead of `lib.mkIf (pkgs.stdenv.isDarwin)`). The tag-based feature selection (`hostHasTag "studio"`) is a flexible composition mechanism.
   - **Source:** `lib/noughty/default.nix` (~500 lines), `lib/noughty-helpers.nix`
   - **Impact:** Medium. Would require rethinking our options structure but adds expressiveness.

4. **Lower Nix build priority on workstations**
   - **Rationale:** Wimpy sets `nix.daemonProcessType = "Background"` on Darwin and `nix.daemonCPUSchedPolicy = "idle"` on NixOS to prevent audio stutter and UI jank during builds. This is a small, practical improvement for workstation usability. No downside beyond slightly slower builds.
   - **Source:** `darwin/default.nix` (daemonProcessType, daemonIOLowPriority), `nixos/default.nix` (daemonCPUSchedPolicy, daemonIOSchedClass, daemonIOSchedPriority)
   - **Impact:** Low. Two-line config change with tangible UX benefit.

5. **Use stable nixpkgs with an unstable overlay**
   - **Rationale:** Wimpy tracks nixos-25.11 (stable) as the primary channel and makes unstable packages available via `pkgs.unstable` overlay. This provides a more stable base while still allowing access to bleeding-edge packages when needed. Our repo uses nixpkgs-unstable everywhere, which means every `flake update` can introduce breakage across the whole system.
   - **Source:** `overlays/default.nix` (unstablePackages overlay), `flake.nix` (nixpkgs-unstable input)
   - **Impact:** Medium. Would require updating package references that need unstable to use `pkgs.unstable.*`, but reduces update risk.

6. **Structured overlay organization**
   - **Rationale:** Wimpy's `overlays/default.nix` separates concerns into three named overlays: `localPackages` (custom pkgs), `modifiedPackages` (version pins and build fixes), `unstablePackages` (access to unstable channel). This is cleaner than our inline overlay list and provides a clear place to add package overrides or version pins.
   - **Source:** `overlays/default.nix`
   - **Impact:** Low. Organizational improvement.

7. **Per-host sops secret files**
   - **Rationale:** Wimpy uses per-host sops files (`secrets/host-vader.yaml`, `secrets/host-momin.yaml`) for SSH host keys and host-specific secrets, while shared secrets go in domain files (secrets.yaml, ssh.yaml, tailscale.yaml). This allows finer-grained access control in `.sops.yaml` -- each host only needs decryption access to its own host file plus shared files. More relevant as the number of hosts and secrets grows.
   - **Source:** `secrets/host-*.yaml`, `nixos/default.nix` (sops config with `sopsFile = ../secrets/host-${host.name}.yaml`)
   - **Impact:** Low. Only relevant if we need to differentiate secret access per host.

8. **Add a devShell to the flake**
   - **Rationale:** Same pattern as ryan4yin (US-001 candidate #10). Wimpy includes a comprehensive devShell with all development tools plus packages from external flake inputs. Reinforces the value of this approach.
   - **Source:** `flake.nix` (devShells output), `lib/flake-builders.nix` (mkDevShells)
   - **Impact:** Low. Convenience improvement; reinforces US-001 candidate #10.

9. **Configure CLI tools via HM programs.\* instead of just installing packages**
   - **Rationale:** Wimpy configures bat, eza, fd, fzf, ripgrep, zoxide, and others via their HM `programs.*` options (which handle shell integration, aliases, config files). We install some of these tools as packages but don't leverage HM's built-in configuration support. Using `programs.bat.enable = true` instead of just adding bat to packages gets you automatic theme integration, shell aliases, and config file management for free.
   - **Source:** `home-manager/_mixins/terminal/bat.nix`, `eza.nix`, `fzf.nix`, `ripgrep.nix`, `zoxide.nix`
   - **Impact:** Low. Incremental improvement to existing tool setup.

## khaneliman/khanelinix

**Source:** [github.com/khaneliman/khanelinix](https://github.com/khaneliman/khanelinix)

One of the largest community Nix configs, managing 8+ hosts across macOS (nix-darwin), NixOS desktops/servers, NixOS WSL, and ISO images. Originally built on Snowfall Lib but has since migrated to flake-parts with Snowfall-style directory conventions retained. Neovim configuration is maintained as a separate flake (khanelivim). The repo features an extensive custom `lib/` with auto-discovery helpers, a deeply nested module hierarchy under `modules/`, and flake-parts partitions to isolate dev tooling into a sub-flake.

### Comparison Table

| Aspect                      | khaneliman/khanelinix                                                                                                                                                                                                                                                                                                             | Our dotfiles                                                                                                                                                              |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Flake structure**         | `flake-parts.lib.mkFlake` with all output logic delegated to `flake/` directory (overlays.nix, packages.nix, configs.nix, home.nix, apps.nix, docs.nix). Dev tooling isolated in `flake/dev/` sub-flake via flake-parts partitions (separate flake.lock).                                                                         | `flake-parts` with `mkDarwin`/`mkHome`/`mkNixOS` helpers defined inline in `flake.nix`. No sub-flake partitioning.                                                        |
| **Module framework**        | Historically Snowfall Lib, now fully migrated to flake-parts + custom `lib/` helpers. Snowfall-style directory conventions (`systems/<arch>/<host>`, `homes/<arch>/<user@host>`, `modules/<target>/`) remain but are wired manually.                                                                                              | flake-parts with inline helpers. No framework history. Flat directory conventions.                                                                                        |
| **Module organization**     | Four-category `modules/` directory: `common/` (cross-platform), `darwin/` (macOS), `nixos/` (NixOS), `home/` (home-manager). Each category deeply nested by domain (archetypes, desktop, environments, programs, services, suites, system, tools, user). All modules auto-imported via `importModulesRecursive`.                  | No `modules/` directory. System config lives in `hosts/` files directly. Home-manager modules are flat `.nix` files in `home/`. Explicit imports from `home/default.nix`. |
| **Custom option namespace** | Everything under `khanelinix.*` options (e.g. `khanelinix.archetypes.gaming = enabled`, `khanelinix.programs.terminal.tools.git.enable = true`). Options created via `lib.khanelinix.mkOpt`/`mkBoolOpt` helpers.                                                                                                                  | Options under `dotfiles.*` namespace (`dotfiles.system.*`, `dotfiles.git.*`, `dotfiles.shell.*`). Standard `lib.mkOption`/`lib.mkEnableOption`.                           |
| **Home config wiring**      | Auto-discovery: `lib/file/parseHomeConfigurations` scans `homes/<arch>/<user@host>/` directories, splits on `@` to extract username/hostname. `mkHomeConfigs` auto-matches homes to systems by arch + hostname and injects as `home-manager.users.<name>`.                                                                        | Explicit: each host in `flake.nix` passes a `hostConfig` attrset. Home modules imported explicitly from `home/default.nix`.                                               |
| **Archetypes/roles**        | `modules/*/archetypes/` directories define high-level machine roles (gaming, personal, workstation, server, wsl). Each archetype enables a bundle of suites. `modules/home/roles/` adds user roles (creator, desktop, developer, gamer, work).                                                                                    | No archetype system. Feature toggling via individual `mkEnableOption` guards per module.                                                                                  |
| **Suites**                  | `modules/home/suites/` bundles related tools: art, business, common, desktop, development, emulation, games, music, networking, photo, social, video, wlroots. Archetypes enable suites; suites enable individual programs. Three-tier hierarchy: archetype -> suite -> program.                                                  | No suite system. Modules are individually toggled.                                                                                                                        |
| **Neovim**                  | Separate flake (`khaneliman/khanelivim`) consumed as a flake input. Fully decoupled from the dotfiles repo.                                                                                                                                                                                                                       | Inline `home/vim.nix` configuring Neovim via home-manager.                                                                                                                |
| **Custom lib**              | Dedicated `lib/` with six modules: `system/` (mkSystem, mkDarwin, mkHome builders), `file/` (importModulesRecursive, parseSystemConfigurations, parseHomeConfigurations), `module/` (mkOpt, mkBoolOpt, enabled/disabled, mkModule), `theme/`, `base64/`, `overlay.nix`. All injected into `lib.khanelinix.*` via nixpkgs overlay. | Helper functions defined inline in `flake.nix`. No separate lib directory.                                                                                                |
| **Overlays**                | Dedicated `overlays/` with 8 named overlays (aerospace, element-desktop, hyprland, input-packages, jankyborders, karabiner-elements, kitty, yabai) plus a default overlay namespacing all custom packages under `pkgs.khanelinix.*`.                                                                                              | Overlays defined inline in `flake.nix` as `sharedOverlays` list. No dedicated directory.                                                                                  |
| **Custom packages**         | `packages/` directory with 24 derivations, auto-discovered via `packagesFromDirectoryRecursive`. Exposed as `perSystem.packages` and through the `khanelinix` overlay namespace.                                                                                                                                                  | `pkgs/` directory with custom derivations. Manual overlay exposure.                                                                                                       |
| **Secrets**                 | sops-nix with per-host secret directories under `secrets/`.                                                                                                                                                                                                                                                                       | sops-nix with age encryption. Same tool, simpler structure.                                                                                                               |
| **Cross-platform**          | Three platforms: macOS (aarch64-darwin, 2 hosts), NixOS (x86_64-linux + aarch64-linux, 6+ hosts), WSL (via nixos-wsl). Extensive desktop support: Hyprland, Niri, Sway, aerospace.                                                                                                                                                | Two platforms: macOS (nix-darwin), NixOS (OrbStack, TrueNAS), plus generic Linux (standalone HM). No WSL. Minimal desktop config.                                         |
| **Secure boot**             | Lanzaboote for NixOS secure boot.                                                                                                                                                                                                                                                                                                 | Not configured.                                                                                                                                                           |
| **Disk management**         | Disko for declarative disk partitioning per host.                                                                                                                                                                                                                                                                                 | Not configured.                                                                                                                                                           |
| **Operations**              | No Justfile or Makefile. Operations via `nix run .#<app>` (update-core, update-system, update-apps, update-all, closure-analyzer) and direnv.                                                                                                                                                                                     | Taskfile.yaml with namespaced includes. `task switch`, `task update`, `task check`, `task format`.                                                                        |
| **CI/CD**                   | GitHub Actions: build dev shells, flake checks, deadnix, fmt check, lint (statix), automated flake updates, PR auto-labeling. Dependabot for GH Actions.                                                                                                                                                                          | Dagger-based: `task check`. No GitHub Actions.                                                                                                                            |
| **Dev tooling isolation**   | flake-parts partitions: `flake/dev/` is a sub-flake with its own `flake.nix` and `flake.lock` for treefmt, devShells, checks, templates. Keeps main flake.lock lighter.                                                                                                                                                           | treefmt-nix as a flake module in the main flake. No sub-flake isolation.                                                                                                  |
| **Templates**               | 12 flake templates (angular, c, cpp, container, dotnetf, go, node, python, rust, etc.) exposed via `nix flake init`.                                                                                                                                                                                                              | No templates.                                                                                                                                                             |
| **Theming**                 | Stylix + Catppuccin + Tokyonight. Extensive wlroots desktop theming (Hyprland, Waybar, etc.).                                                                                                                                                                                                                                     | Stylix with OneDark base16 scheme. Simpler scope (terminal + editors).                                                                                                    |
| **Shell support**           | Fish, Nushell, Zsh, Bash all configured via home-manager modules.                                                                                                                                                                                                                                                                 | Fish only.                                                                                                                                                                |

### Home-Manager Modules Comparison

Modules in khanelinix's `modules/home/` that we lack or configure differently:

| Their module                           | Our equivalent         | Notes                                                                                                       |
| -------------------------------------- | ---------------------- | ----------------------------------------------------------------------------------------------------------- |
| `programs/terminal/tools/atuin/`       | (none)                 | Shell history sync/search.                                                                                  |
| `programs/terminal/tools/bat/`         | (none)                 | Cat replacement with syntax highlighting.                                                                   |
| `programs/terminal/tools/btop/`        | (none)                 | System monitor.                                                                                             |
| `programs/terminal/tools/carapace/`    | (none)                 | Cross-shell completion engine.                                                                              |
| `programs/terminal/tools/direnv/`      | (none)                 | Per-directory environments. We install but don't configure via HM.                                          |
| `programs/terminal/tools/eza/`         | (none)                 | Modern ls replacement.                                                                                      |
| `programs/terminal/tools/fzf/`         | (none)                 | Fuzzy finder. Installed as package but not configured via HM.                                               |
| `programs/terminal/tools/lazygit/`     | (none)                 | TUI git client.                                                                                             |
| `programs/terminal/tools/ripgrep/`     | (none)                 | Fast grep. Installed as package but not configured via HM.                                                  |
| `programs/terminal/tools/tmux/`        | (none)                 | Terminal multiplexer.                                                                                       |
| `programs/terminal/tools/yazi/`        | (none)                 | TUI file manager.                                                                                           |
| `programs/terminal/tools/zellij/`      | (none)                 | Terminal multiplexer (alternative to tmux).                                                                 |
| `programs/terminal/tools/zoxide/`      | (none)                 | Smart cd replacement.                                                                                       |
| `programs/terminal/shells/nushell/`    | (none)                 | Structured data shell. We only configure fish.                                                              |
| `programs/terminal/editors/helix/`     | (none)                 | Modal editor.                                                                                               |
| `programs/graphical/browsers/firefox/` | (none)                 | Firefox with extensions via HM.                                                                             |
| `programs/graphical/wms/aerospace/`    | (none)                 | macOS tiling WM via HM.                                                                                     |
| `services/ollama/`                     | (none)                 | Local LLM service.                                                                                          |
| `services/syncthing/`                  | (none)                 | File sync.                                                                                                  |
| `services/tailscale/`                  | (none)                 | VPN mesh.                                                                                                   |
| `suites/development/`                  | `home/development.nix` | Theirs bundles language tools, editors, and dev services into a suite. Ours is a flat list of dev packages. |
| `roles/developer/`                     | (none)                 | Meta-role enabling development suites, Claude Code, etc.                                                    |

### Candidate Changes

1. **Extract flake output logic into a `flake/` directory**
   - **Rationale:** khanelinix delegates all flake output construction to `flake/` modules (overlays.nix, packages.nix, configs.nix, home.nix, apps.nix), keeping `flake.nix` as a thin entry point that just lists inputs and imports `./flake`. This is cleaner than our approach of defining everything inline. Combined with similar findings from US-001 and US-002, this is a strong pattern across community configs.
   - **Source:** `flake/default.nix`, `flake/configs.nix`, `flake/home.nix`, `flake/packages.nix`, `flake/overlays.nix`
   - **Impact:** Medium. Refactoring only; reinforces US-001 #1 and US-002 #2 with a more granular split.

2. **Isolate dev tooling via flake-parts partitions**
   - **Rationale:** khanelinix uses flake-parts partitions to put treefmt, devShells, checks, and templates into a `flake/dev/` sub-flake with its own `flake.lock`. This means dev-only dependencies (like treefmt-nix, statix, deadnix) don't bloat the main flake lock, and updating dev tools doesn't risk breaking system builds. Our treefmt-nix is currently part of the main flake.
   - **Source:** `flake/dev/flake.nix`, `flake/dev/flake.lock`, `flake/dev/treefmt.nix`, `flake/dev/devshells.nix`, `flake/dev/checks.nix`
   - **Impact:** Medium. Would reduce main flake.lock size and isolate dev tooling risk.

3. **Archetype/suite system for bundling features**
   - **Rationale:** khanelinix uses a three-tier hierarchy (archetype -> suite -> program) where enabling `khanelinix.archetypes.workstation = enabled` cascades through suites (desktop, development, networking) down to individual programs. This reduces per-host boilerplate since you declare intent ("this is a workstation") rather than listing individual features. Our per-module `mkEnableOption` approach is more explicit but requires more per-host configuration.
   - **Source:** `modules/nixos/archetypes/`, `modules/home/suites/`, `modules/home/roles/`
   - **Impact:** Medium. Significant architectural change. Only valuable if we add more hosts with distinct roles. Conflicts with our preference for explicit configuration (CLAUDE.md: "Prefer explicit configuration over convention-based loading").

4. **Custom lib module with convenience helpers (enabled/disabled, mkOpt, mkBoolOpt)**
   - **Rationale:** khanelinix's `lib/module/` provides shorthand: `enabled` = `{ enable = true; }`, `disabled` = `{ enable = false; }`, `mkBoolOpt` wraps `lib.mkOption` with bool type and default. These reduce boilerplate throughout the config. Example: `khanelinix.programs.git = enabled;` vs `khanelinix.programs.git.enable = true;`. The `mkModule` factory creates standardized module skeletons with consistent option naming.
   - **Source:** `lib/module/default.nix`
   - **Impact:** Low. Small convenience; could add to our codebase without significant structural change.

5. **Separate Neovim config into its own flake**
   - **Rationale:** khanelinix maintains Neovim configuration as a standalone flake (`khanelivim`), consumed as a flake input. This decouples editor config from system config, allows independent versioning and testing, and makes the Neovim setup reusable outside the dotfiles repo. Our `home/vim.nix` is relatively small, so the benefit is proportional to how much the Neovim config might grow.
   - **Source:** `inputs.khanelivim` (github:khaneliman/khanelivim), `modules/home/programs/terminal/editors/neovim/`
   - **Impact:** Low. Only valuable if our Neovim config becomes substantially more complex.

6. **Flake apps for grouped input updates**
   - **Rationale:** khanelinix exposes `nix run .#update-core`, `.#update-system`, `.#update-apps` as flake apps that update subsets of flake inputs rather than all-or-nothing. This allows updating core Nix infrastructure independently from application inputs, reducing the risk of a single `nix flake update` breaking everything. Our `task update` runs `nix flake update` for all inputs at once.
   - **Source:** `flake/apps.nix` (update-core, update-system, update-apps, update-all)
   - **Impact:** Low. Useful as the number of flake inputs grows. Could be implemented as Taskfile commands instead of flake apps.

7. **Auto-discovered packages via `packagesFromDirectoryRecursive`**
   - **Rationale:** khanelinix uses `pkgs.lib.packagesFromDirectoryRecursive` to automatically expose all packages in the `packages/` directory without maintaining an explicit list. Each package just needs a `default.nix` in its own subdirectory. Reduces the chance of adding a package but forgetting to wire it up.
   - **Source:** `flake/packages.nix`, `packages/` directory
   - **Impact:** Low. Minor convenience for our `pkgs/` directory.

8. **Namespace custom packages under a single overlay attribute**
   - **Rationale:** khanelinix exposes all custom packages under `pkgs.khanelinix.*` via a single overlay, preventing name collisions with nixpkgs. Our custom packages are added directly to the top-level `pkgs` namespace via inline overlays, which could theoretically shadow nixpkgs packages.
   - **Source:** `overlays/default/` (default overlay), `lib/overlay.nix`
   - **Impact:** Low. Defensive practice; prevents potential name collisions.

## eh8/chenglab

**Source:** [github.com/eh8/chenglab](https://github.com/eh8/chenglab)

A homelab-focused Nix config managing 7 machines: 3 NixOS servers (ThinkCenter M710q Tiny), 1 AMD Ryzen desktop, 1 M1 MacBook Air (nix-darwin), 1 WSL instance, and 1 custom ISO builder. Notably minimal and approachable, with a strong emphasis on impermanence (root on tmpfs), full-disk encryption with remote initrd unlock, and self-hosted services. Tracks stable nixpkgs (25.11) with an unstable overlay available.

### Comparison Table

| Aspect                   | eh8/chenglab                                                                                                                                                                                       | Our dotfiles                                                                                                        |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| Flake structure          | Flat `flake.nix` with `mkNixOSConfig`/`mkDarwinConfig` helpers that take a path; no flake-parts                                                                                                    | `flake.nix` with flake-parts and `mkDarwin`/`mkHome`/`mkNixOS` helpers that take a `hostConfig` attrset             |
| Module organization      | `modules/{nixos,macos,wsl,home-manager}/` split by platform; each has a `base.nix` + `_packages.nix`                                                                                               | `hosts/` for system config, `home/` for flat home-manager modules; no platform subdirectories under home            |
| Host definitions         | `machines/<hostname>/configuration.nix` + `hardware-configuration.nix`; each machine explicitly imports its modules                                                                                | `flake.nix` inline `hostConfig` attrsets that feed `mkDarwin`/`mkHome`/`mkNixOS`; hardware config in `hosts/nixos/` |
| Home-manager integration | Embedded in each machine's `configuration.nix` via `home-manager.users.${vars.userName}.imports = [...]`                                                                                           | Centralized in `home/default.nix` imported by flake helpers; each module self-contained                             |
| Shared variables         | `vars.nix` attrset (fullName, userName, userEmail, SSH keys) imported via `import ./vars.nix` and passed through `specialArgs`                                                                     | `hostConfig` attrset in `flake.nix` plus `dotfiles.*` NixOS/HM options for typed config                             |
| Secrets management       | sops-nix with age keys derived from SSH host keys stored at `/nix/secret/initrd/ssh_host_ed25519_key`; `.sops.yaml` lists all machine age public keys                                              | sops-nix with standalone age key                                                                                    |
| Impermanence             | Root on tmpfs (`/ = { device = "none"; fsType = "tmpfs"; }`), persistent state via `environment.persistence."/nix/persist"` with explicit dirs/files list                                          | Not used; standard persistent root filesystem                                                                       |
| Disk encryption          | LUKS on all NixOS servers; remote initrd SSH unlock via `boot.initrd.network.ssh`                                                                                                                  | Not used                                                                                                            |
| Services layer           | Dedicated `services/` directory with per-service `.nix` files (nextcloud, tailscale, nixarr, homebridge, etc.); underscore prefix for shared infra (`_acme.nix`, `_cloudflared.nix`, `_nginx.nix`) | No services directory; NixOS hosts are lightweight (OrbStack container, TrueNAS)                                    |
| Operations               | `.justfile` with deploy, up, lint, fmt, clean, repair, sops-edit, sops-rotate, sops-update, build-iso                                                                                              | `Taskfile.yaml` with switch, update, check, format                                                                  |
| Nixpkgs channel          | Stable (nixos-25.11) with `nixpkgs-unstable` input available                                                                                                                                       | Unstable (nixos-unstable) everywhere                                                                                |
| Formatter                | Alejandra exposed via `nix fmt`                                                                                                                                                                    | treefmt-nix with prettier + nixfmt                                                                                  |
| CI/CD                    | GitHub Actions: daily `flake.lock` update via Dependabot, release workflow for ISO/WSL tarball builds                                                                                              | Dagger-based CI                                                                                                     |
| Auto-updates             | `system.autoUpgrade` pulling from `github:eh8/chenglab` daily at 07:00 with randomized delay                                                                                                       | Not used                                                                                                            |
| Install/bootstrap        | `install.sh` shell script handling macOS (Determinate Nix installer, Xcode, Rosetta) and Linux (disk partitioning, encryption, mount, SSH key gen, age key derivation)                             | No bootstrap script                                                                                                 |
| WSL support              | Dedicated machine config via `nixos-wsl` + `vscode-server` modules                                                                                                                                 | Not supported                                                                                                       |
| Custom ISO               | `iso1chng` NixOS config builds a minimal installer ISO baked with personal SSH key                                                                                                                 | Not used                                                                                                            |
| Dock management          | Custom `local.dock` module (`modules/macos/_dock.nix`) for declarative macOS Dock entries                                                                                                          | Not used (manual Dock management)                                                                                   |
| Platform packages        | Separate `_packages.nix` per platform (nixos, macos, wsl, home-manager)                                                                                                                            | Packages in `home/default.nix` and per-module; system packages in `hosts/mac.nix`                                   |
| Shell                    | zsh with powerlevel10k                                                                                                                                                                             | fish                                                                                                                |

### Home-Manager Module Comparison

| Module area      | eh8/chenglab                                    | Our dotfiles                                |
| ---------------- | ----------------------------------------------- | ------------------------------------------- |
| Shell            | zsh + powerlevel10k (`_zsh.nix`, `_p10k/`)      | fish (via `home/shell.nix`)                 |
| Terminal         | Alacritty (`alacritty.nix`)                     | Ghostty, WezTerm (via `home/terminals.nix`) |
| Editor           | Helix (default), Vim (backup)                   | Neovim (via nixvim flake)                   |
| Git              | Basic config (`git.nix`)                        | Comprehensive config (`home/git.nix`)       |
| Multiplexer      | Zellij                                          | tmux                                        |
| File tools       | bat, lsd, fd, ripgrep, fzf                      | bat, eza, fd, ripgrep, fzf, zoxide          |
| System monitor   | btop, htop                                      | btop                                        |
| Directory env    | direnv + nix-direnv                             | direnv + nix-direnv                         |
| Media            | yt-dlp, gallery-dl                              | Not in HM modules                           |
| Fonts            | fonts.nix (unclear what fonts)                  | Nerd Fonts via stylix                       |
| Password manager | 1Password (`1password.nix`)                     | 1Password (via Homebrew cask)               |
| Desktop (NixOS)  | `desktop.nix` (Firefox, Nautilus, GNOME tweaks) | Not applicable (no NixOS desktop)           |

### Candidate Changes

1. **Impermanence for NixOS hosts**
   - **Rationale:** chenglab runs root on tmpfs with only `/nix/persist` surviving reboots. This guarantees system state is fully declared in Nix; any undeclared files are wiped on reboot. The `environment.persistence."/nix/persist"` block explicitly lists which directories (`/var/log`, `/var/lib/nixos`) and files (`/etc/machine-id`, SSH host keys) survive. This pattern catches configuration drift by design. Our NixOS hosts (OrbStack, TrueNAS) use persistent root, so undeclared state can accumulate.
   - **Source:** `machines/svr1chng/hardware-configuration.nix` (tmpfs root), `modules/nixos/base.nix` (persistence declarations)
   - **Impact:** Medium. Requires repartitioning existing NixOS hosts and auditing all stateful paths. Most valuable for the TrueNAS host where config drift is harder to detect.

2. **Remote initrd SSH unlock for encrypted hosts**
   - **Rationale:** chenglab enables `boot.initrd.network.ssh` with a dedicated initrd SSH host key, allowing remote LUKS passphrase entry via SSH. The authorized keys are reused from the user's config. This is essential for headless server operation with full-disk encryption. Our TrueNAS NixOS host could benefit if we add LUKS.
   - **Source:** `modules/nixos/remote-unlock.nix`, `machines/svr1chng/hardware-configuration.nix` (LUKS config)
   - **Impact:** Low. Only relevant if we add disk encryption to NixOS hosts.

3. **system.autoUpgrade from GitHub flake URI**
   - **Rationale:** chenglab uses `system.autoUpgrade` pointing at `github:eh8/chenglab` to auto-rebuild servers daily. Combined with CI that updates `flake.lock` via Dependabot, this creates a hands-off update pipeline: Dependabot updates lock -> merge -> servers auto-rebuild. Our NixOS hosts require manual `task switch`.
   - **Source:** `modules/nixos/auto-update.nix`
   - **Impact:** Medium. Reduces maintenance burden for always-on NixOS hosts. Requires confidence in CI catching breaking changes before merge.

4. **Underscore prefix convention for shared/infrastructure modules**
   - **Rationale:** chenglab prefixes shared infrastructure modules with underscores (`_acme.nix`, `_cloudflared.nix`, `_nginx.nix` in services; `_packages.nix`, `_zsh.nix` in home-manager). This visually separates foundational modules from feature modules in directory listings. Similar to wimpysworld's `_mixins/` pattern noted in US-002.
   - **Source:** `services/_acme.nix`, `services/_nginx.nix`, `modules/home-manager/_packages.nix`
   - **Impact:** Low. Naming convention only; no functional change.

5. **Per-platform module directories under modules/**
   - **Rationale:** chenglab splits modules into `modules/{nixos,macos,wsl,home-manager}/`, each with its own `base.nix` and `_packages.nix`. This makes platform boundaries explicit at the filesystem level. Our repo uses `hosts/` for system config and `home/` for HM modules, but home-manager modules are shared across platforms via conditional `mkIf` checks inside the modules themselves.
   - **Source:** `modules/` directory structure
   - **Impact:** Low. Our current approach works because we have fewer platform-specific HM differences. Would become more relevant if we added WSL or NixOS desktop support.

6. **Dedicated services/ directory for self-hosted applications**
   - **Rationale:** chenglab keeps all self-hosted service configs (nextcloud, tailscale, jellyfin, homebridge, etc.) in a top-level `services/` directory, separate from system modules. Each machine imports only the services it runs. This cleanly separates "what this machine is" (modules) from "what this machine runs" (services). Our NixOS hosts are lightweight enough that this separation is not yet needed, but it would help if we add more services to TrueNAS.
   - **Source:** `services/` directory, `machines/svr1chng/configuration.nix` (service imports)
   - **Impact:** Low. Organizational pattern; only relevant if our NixOS hosts start running more services.

7. **sops-nix age keys derived from SSH host keys**
   - **Rationale:** chenglab derives age keys from the SSH host key stored at `/nix/secret/initrd/ssh_host_ed25519_key`, meaning the sops identity is the machine's existing SSH key rather than a separate age key. The `.sops.yaml` lists each machine's age public key. The `install.sh` script automates the `ssh-to-age` conversion during bootstrap. This eliminates managing a separate age key file. Our setup uses a standalone age key.
   - **Source:** `modules/nixos/base.nix` (`sops.age.sshKeyPaths`), `.sops.yaml`, `install.sh` (ssh-to-age conversion)
   - **Impact:** Low. Different trust model; neither approach is strictly better. SSH-derived keys tie secrets access to machine identity, which is simpler but less flexible for multi-user scenarios.

8. **Bootstrap install script with disk setup and encryption**
   - **Rationale:** chenglab's `install.sh` handles the entire bootstrap flow for both macOS (Xcode, Rosetta, Determinate Nix installer, then `nix run nix-darwin`) and Linux (GPT partitioning, LUKS encryption, filesystem creation, tmpfs mount hierarchy, initrd SSH key generation, age key derivation). This makes fresh installs reproducible and documented. Our repo has no bootstrap script.
   - **Source:** `install.sh`
   - **Impact:** Low. Useful for reproducible machine setup, but our macOS bootstrap is already handled by Determinate Nix installer + `task switch`.

## AlexNabokikh/nix-config

**Source:** [github.com/AlexNabokikh/nix-config](https://github.com/AlexNabokikh/nix-config)

A clean, two-host setup managing one NixOS desktop (energy) and one nix-darwin MacBook (PL-OLX-KCGXHGK3PY). Notable for its strict separation between system-level modules (`modules/nixos/`, `modules/darwin/`), home-manager modules (`modules/home-manager/`), and per-user/per-host home configs (`home/<user>/<host>/`). Uses catppuccin/nix for theming, Makefile for operations, and keeps home-manager as a standalone configuration (not integrated into system rebuilds).

### Comparison Table

| Aspect                   | AlexNabokikh/nix-config                                                                                                                            | Our dotfiles                                                                                                                                                                     |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Flake framework          | Plain flake outputs, no flake-parts                                                                                                                | flake-parts with `treefmt-nix.flakeModule`                                                                                                                                       |
| Flake inputs             | 6 inputs (nixpkgs, home-manager, hardware, catppuccin, noctalia, darwin)                                                                           | 12 inputs (nixpkgs, flake-parts, nix-darwin, home-manager, nix-vscode-extensions, krewfile, llm-agents, dagger, stylix, nix-index-database, sops-nix, nix-homebrew, treefmt-nix) |
| Helper functions         | `mkNixosConfiguration`, `mkDarwinConfiguration`, `mkHomeConfiguration` -- three separate functions                                                 | `mkDarwin`, `mkHome`, `mkNixOS` -- three analogous functions                                                                                                                     |
| Home-manager integration | Standalone `homeConfigurations` only; not integrated into system rebuilds                                                                          | Integrated into darwin/NixOS via `darwinModules.home-manager` / `nixosModules.home-manager`, plus standalone `homeConfigurations` for Linux                                      |
| Host layout              | `hosts/<hostname>/default.nix` per host                                                                                                            | `hosts/mac.nix` (shared darwin), `hosts/nixos/<host>.nix` (per NixOS host), `hosts/linux.nix` (standalone HM)                                                                    |
| Module organization      | Three-tier: `modules/{darwin,nixos,home-manager}/{common,desktop,programs,...}/` with per-program subdirectories                                   | Flat: `home/*.nix` per tool domain, `hosts/shared.nix` for system-level shared config                                                                                            |
| Per-user config          | `home/<user>/<host>/default.nix` selects which module groups to import                                                                             | Inline `homeModule` function/attrset in `flake.nix` per host                                                                                                                     |
| User metadata            | `users` attrset in `flake.nix` with avatar, email, fullName, gitKey fields; passed via `specialArgs` as `userConfig`                               | `dotfiles.*` NixOS options module with typed options; passed via home-manager module system                                                                                      |
| Module path passing      | String interpolation: `"${nixosModules}/common"` where `nixosModules = "${self}/modules/nixos"`                                                    | Direct import paths: `./hosts/mac.nix`, `./home`                                                                                                                                 |
| Theming                  | catppuccin/nix (`catppuccin.homeModules.catppuccin`) with flavor/accent in HM common module                                                        | stylix (base16 scheme, fonts, cursor) via `stylix.darwinModules.stylix` / `stylix.nixosModules.stylix`                                                                           |
| Secrets management       | None                                                                                                                                               | sops-nix                                                                                                                                                                         |
| Operations               | Makefile with explicit targets: `darwin-rebuild`, `nixos-rebuild`, `home-manager-switch`, `nix-gc`, `flake-update`, `flake-check`, `bootstrap-mac` | Taskfile.yaml with `task switch` (auto-detects platform via `nh`), `task update`, `task check`, `task format`                                                                    |
| Bootstrap                | `bootstrap-mac` target chains `install-nix` then `install-nix-darwin`                                                                              | No bootstrap script; Determinate Nix installer + `task switch`                                                                                                                   |
| Formatter / linter       | None in flake                                                                                                                                      | treefmt-nix (nixfmt, deadnix, statix, shfmt, prettier)                                                                                                                           |
| Flake checks             | `nix flake check` via Makefile                                                                                                                     | Per-system checks that build all configurations via flake-parts `perSystem.checks`                                                                                               |
| Shell                    | zsh (system-level `programs.zsh.enable = true`)                                                                                                    | fish (home-manager `programs.fish`)                                                                                                                                              |
| Custom packages          | None                                                                                                                                               | `pkgs/` directory with custom derivations                                                                                                                                        |
| Overlays                 | None                                                                                                                                               | Local overlay + nix-vscode-extensions, llm-agents, dagger overlays                                                                                                               |
| Hardware modules         | nixos-hardware (`inputs.hardware.nixosModules.asus-rog-strix-x570e`, `common-gpu-amd`)                                                             | None (our NixOS hosts are OrbStack/TrueNAS VMs, no hardware-specific modules needed)                                                                                             |
| Scripts                  | `modules/home-manager/scripts/` with `home.file.".local/bin"` recursive source from `./bin`                                                        | Shell scripts in `configs/` symlinked via `xdg.configFile`                                                                                                                       |
| Desktop environment      | NixOS: niri (Wayland compositor) + Hyprland option; HM: desktop-specific modules for niri/hyprland/wayland-common                                  | No desktop environment (headless NixOS hosts, macOS manages its own DE)                                                                                                          |
| Nixpkgs config           | `allowUnfree = true` set centrally in `nixpkgsConfig` attrset, applied in each helper                                                              | `config.allowUnfree = true` in standalone HM; darwin/NixOS inherit from nixpkgs module                                                                                           |

### Home-Manager Module Comparison

AlexNabokikh organizes HM modules under `modules/home-manager/` with subdirectories for category (`common/`, `programs/`, `desktop/`, `misc/`, `services/`, `scripts/`). Each program gets its own directory with a `default.nix`. The `common/default.nix` explicitly imports all shared program modules.

| Module (theirs)                    | Equivalent (ours)                  | Notes                                                                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------------------------------------------------- |
| programs/aerospace                 | (none)                             | macOS tiling WM; we don't use one                                            |
| programs/alacritty                 | home/ghostty.nix                   | Different terminal emulators                                                 |
| programs/atuin                     | (none)                             | Shell history sync; we use fish built-in history                             |
| programs/bat                       | home/shell.nix (bat config inline) | We configure bat within shell module                                         |
| programs/btop                      | (none)                             | System monitor; not configured in our repo                                   |
| programs/fzf                       | home/shell.nix (fzf inline)        | We configure fzf within shell module                                         |
| programs/git                       | home/git.nix                       | Similar scope                                                                |
| programs/go                        | home/development.nix               | We bundle Go config in development module                                    |
| programs/gpg                       | (none)                             | GPG agent; not configured in our repo                                        |
| programs/k8s                       | home/kubernetes.nix                | Similar scope                                                                |
| programs/lazygit                   | home/git.nix                       | We bundle lazygit in git module                                              |
| programs/neovim                    | (none)                             | We use VS Code and Zed instead                                               |
| programs/starship                  | home/shell.nix                     | We use tide prompt for fish instead of starship                              |
| programs/tmux                      | (none)                             | Terminal multiplexer; not configured in our repo                             |
| programs/zsh                       | home/shell.nix                     | Different shell (we use fish)                                                |
| programs/saml2aws                  | (none)                             | AWS SSO tool; work-specific                                                  |
| misc/gtk, misc/qt                  | (none)                             | Desktop theming; not relevant for our headless NixOS + macOS setup           |
| misc/xdg                           | home/default.nix                   | We set XDG paths in home default module                                      |
| desktop/niri, desktop/hyprland     | (none)                             | Wayland compositors; not relevant for us                                     |
| services/hypridle, services/kanshi | (none)                             | Desktop services; not relevant for us                                        |
| scripts/                           | configs/                           | Different mechanism: they use `home.file` recursive, we use `xdg.configFile` |

### Candidate Changes

1. **Separate home-manager modules into per-program directories**
   - **Rationale:** AlexNabokikh gives each program its own directory under `modules/home-manager/programs/<name>/default.nix`. Our flat `home/*.nix` files work well at our current scale, but some of our modules (shell.nix, development.nix) bundle multiple programs together. Per-program directories would make it clearer what each module configures and allow independent toggling.
   - **Source:** `modules/home-manager/programs/` (22 program directories)
   - **Impact:** Low. Our flat layout is fine for our current module count (~15 files). Only worth considering if modules grow significantly larger or more numerous.

2. **Centralized user metadata attrset in flake.nix**
   - **Rationale:** AlexNabokikh defines a `users` attrset at the top of `flake.nix` containing all user metadata (name, email, fullName, gitKey, avatar, wallpaper) and passes it to all configurations via `specialArgs`. This is simpler than our approach of scattering user-specific settings across inline `homeModule` definitions. However, our `dotfiles.*` options module provides type checking and defaults that a plain attrset does not.
   - **Source:** `flake.nix` (lines defining `users` attrset)
   - **Impact:** Low. Our typed options approach is more robust. The pattern is worth noting but switching to it would lose type safety.

3. **Standalone home-manager configurations (not integrated into system rebuild)**
   - **Rationale:** AlexNabokikh uses `homeConfigurations` exclusively, running `home-manager switch` separately from `darwin-rebuild`/`nixos-rebuild`. This decouples home config updates from system rebuilds, making home changes faster and independent. Our integrated approach means every `task switch` rebuilds both system and home, which is slower but ensures consistency.
   - **Source:** `flake.nix` (`mkHomeConfiguration` function, `homeConfigurations` output)
   - **Impact:** Medium. Faster iteration on home config changes, at the cost of potential drift between system and home state. Worth considering as an optional workflow alongside the integrated rebuild.

4. **Makefile with explicit, descriptive targets and bootstrap chain**
   - **Rationale:** AlexNabokikh's Makefile has separate targets for each operation (`darwin-rebuild`, `nixos-rebuild`, `home-manager-switch`, `nix-gc`, `flake-update`, `flake-check`) with echo messages before and after each step, plus a `bootstrap-mac` target that chains `install-nix` and `install-nix-darwin`. Our Taskfile uses `task switch` with auto-detection, which is more convenient but less transparent. The bootstrap chain pattern is worth noting.
   - **Source:** `Makefile`
   - **Impact:** Low. Our Taskfile with `nh` auto-detection is more ergonomic. The bootstrap chain is the only meaningfully different pattern.

5. **String-interpolated module paths via specialArgs**
   - **Rationale:** AlexNabokikh passes module base paths as strings via `specialArgs` (`nixosModules = "${self}/modules/nixos"`, `darwinModules = "${self}/modules/darwin"`, `nhModules = "${self}/modules/home-manager"`) and imports them with string interpolation (`"${nixosModules}/common"`). This allows hosts and home configs to import modules without knowing the repo's absolute path structure. Our approach uses direct relative imports (`./hosts/mac.nix`, `./home`), which is simpler and more explicit.
   - **Source:** `flake.nix` (specialArgs), `hosts/energy/default.nix`, `home/nabokikh/energy/default.nix`
   - **Impact:** Low. String interpolation adds indirection without clear benefit for our repo size. Direct imports are preferred per our code style (explicit over convention-based).

6. **Per-user/per-host home directory structure**
   - **Rationale:** AlexNabokikh organizes home configs as `home/<username>/<hostname>/default.nix`, creating a matrix of user-by-host combinations. Each file selects which module groups to import (common, desktop, etc.). Our approach uses inline `homeModule` closures in `flake.nix`. The directory-based approach scales better when the same user has different configs across many hosts, or when multiple users share a host.
   - **Source:** `home/nabokikh/energy/default.nix`, `home/alexander.nabokikh/PL-OLX-KCGXHGK3PY/default.nix`
   - **Impact:** Low. We have two users across four hosts, all with similar configs. The inline approach keeps everything visible in `flake.nix`. The directory approach would only help if user/host combinations grew significantly.

7. **Disable boot-time services for faster startup**
   - **Rationale:** AlexNabokikh explicitly disables `NetworkManager-wait-online` and `plymouth-quit-wait` systemd services to reduce boot time. These are common culprits for slow NixOS boots. Our NixOS hosts (OrbStack container, TrueNAS VM) may not have the same boot path, but it is a practical optimization to be aware of.
   - **Source:** `modules/nixos/common/default.nix` (`systemd.services`)
   - **Impact:** Low. Relevant only if our NixOS hosts experience slow boot times from these services.

## mrjones2014/dotfiles

**Source:** [github.com/mrjones2014/dotfiles](https://github.com/mrjones2014/dotfiles)

A cross-platform config managing 3 NixOS hosts (desktop, laptop, server) and 2 nix-darwin hosts (personal Mac, work Mac). Uses agenix for secrets, treefmt-nix for formatting, Lix as the Nix implementation, and a custom `lib/default.nix` with `mkHost`/`mkDarwinHost` helpers. The repo is organized with clear platform-level separation: `nix-darwin/` for macOS system config, `nixos/` for NixOS system config, `home-manager/` for HM modules, and `hosts/` for per-machine overrides. Neovim config lives as raw Lua in `nvim/` (not generated by Nix). Notably includes `nix-fast-build` in CI, Cachix integration, `nix-auto-follow` for input hygiene, and `flake checks` that validate both system builds and formatting.

### Comparison Table

| Aspect                   | mrjones2014/dotfiles                                                                                                                                              | Our dotfiles                                                                                                     |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Flake structure          | Flat `flake.nix` with `lib/default.nix` helpers (`mkHost`, `mkDarwinHost`); `flake-utils.eachDefaultSystem` for per-system outputs (checks, formatter, devShells) | `flake.nix` with inline `mkDarwin`/`mkHome`/`mkNixOS` helpers; flake-parts for per-system outputs                |
| Platform separation      | Three top-level dirs: `nix-darwin/`, `nixos/`, `home-manager/` -- each with `common.nix` shared modules                                                           | `hosts/` (system config for all platforms), `home/` (HM modules) -- platform split within `hosts/`               |
| Host configs             | `hosts/<name>/default.nix` per host, imported by `lib/` helpers via `../hosts/${name}` path interpolation                                                         | `hosts/mac.nix`, `hosts/linux.nix`, `hosts/nixos/*.nix` -- platform-first rather than host-first                 |
| Home-manager integration | Integrated as NixOS/Darwin module; separate `home.nix` (desktop) and `server.nix` (server) entry points; `shared.nix` for common imports                          | Integrated as NixOS/Darwin module; single `home/default.nix` entry point with conditional logic via `hostConfig` |
| HM module organization   | `home-manager/components/` directory with per-tool files (fish.nix, fzf.nix, nvim.nix, starship.nix, ssh.nix, etc.) and subdirs (gnome/, vcs/)                    | `home/` flat directory with per-domain files (shell.nix, git.nix, editors.nix, k8s.nix, etc.)                    |
| Secrets management       | agenix with SSH host keys as age identities; `secrets.nix` maps secret files to public keys; `secrets/` dir holds `.age` files                                    | sops-nix with standalone age key                                                                                 |
| Theming                  | Custom `tokyonight.nix` flake input (author's own tokyonight-nix module) providing global theme toggle; GTK theming on Linux                                      | Stylix with base16 scheme, fonts, cursor -- applied globally                                                     |
| Nix implementation       | Lix (`pkgs.lixPackageSets.latest.lix`) as default nix package                                                                                                     | Lix (configured in `hosts/shared.nix`)                                                                           |
| Formatter                | treefmt-nix with nixfmt, fish_indent, stylua, rustfmt, taplo, shfmt, yamlfmt, statix                                                                              | treefmt via flake-parts with nixfmt, prettier, shfmt, actionlint                                                 |
| Flake checks             | System builds for all configs + formatting check + `nix-auto-follow` input validation, all per-system                                                             | `nix flake check` via Taskfile                                                                                   |
| CI/CD                    | GitHub Actions: `nix flake check` on PRs, `nix-fast-build` + Cachix push on master, Dependabot for flake.lock updates                                             | Dagger-based CI/CD                                                                                               |
| Custom packages          | `pkgs/` with 2 packages (vim-zellij-navigator, zjstatus); applied as overlay in `nixos/nixpkgs-config.nix`                                                        | `pkgs/` with custom derivations; applied via overlays in flake                                                   |
| Unfree handling          | `allowUnfreePredicate` with explicit package name list, conditional on `isServer`                                                                                 | `allowUnfree = true` globally                                                                                    |
| Neovim config            | Raw Lua in `nvim/` directory, symlinked via HM `xdg.configFile`; Nix only installs neovim + dependencies                                                          | Nix-managed via home-manager `programs.neovim` or similar                                                        |
| Shell                    | Fish via HM `programs.fish`; fish_indent in treefmt                                                                                                               | Fish via HM; fish config in `home/shell.nix`                                                                     |
| Task runner              | None (no Makefile, no Taskfile); uses `nh` (configured in HM `shared.nix`) for rebuilds                                                                           | Taskfile.yaml with `task switch`, `task update`, etc.                                                            |
| Boolean flags            | `specialArgs` with boolean flags (`isServer`, `isDarwin`, `isLinux`, `isThinkpad`, `isWorkMac`) for conditional config                                            | `hostConfig` attrset with `dotfiles.*` NixOS options for conditionals                                            |
| Nix settings sharing     | `nixos/nix-conf.nix` parameterized with `{ isHomeManager }` closure -- same file used by both system and HM configs                                               | `hosts/shared.nix` imported by system configs; HM gets nix settings separately                                   |
| macOS settings           | Extensive `nix-darwin/settings.nix` covering screencapture, NSGlobalDomain, finder, dock, ActivityMonitor, CustomUserPreferences (Unicode Hex Input)              | macOS defaults in `hosts/mac.nix`                                                                                |
| Homebrew                 | nix-darwin `homebrew` module with `onActivation.cleanup = "zap"`, auto-update, per-host cask lists                                                                | nix-darwin `homebrew` module with similar approach                                                               |
| Documentation            | `docs/` directory with mdbook; published to GitHub Pages via CI                                                                                                   | No dedicated docs site                                                                                           |
| Dev shells               | Two devShells: `default` (formatter + mdbook + nix-auto-follow) and `ci` (nix-fast-build + nix-auto-follow)                                                       | devShell defined in flake                                                                                        |

### Home-Manager Module Comparison

| Module domain      | mrjones2014/dotfiles                              | Our dotfiles                    |
| ------------------ | ------------------------------------------------- | ------------------------------- |
| Fish shell         | `components/fish.nix` (8KB, extensive)            | `home/shell.nix`                |
| Starship prompt    | `components/starship.nix`                         | `home/shell.nix` (inline)       |
| Git/VCS            | `components/vcs/` (directory)                     | `home/git.nix`                  |
| SSH                | `components/ssh.nix`                              | `home/shell.nix` or similar     |
| FZF                | `components/fzf.nix` (6.5KB, extensive)           | Configured within shell         |
| Neovim             | `components/nvim.nix` (wrapper) + raw `nvim/` Lua | `home/editors.nix`              |
| Terminal (Ghostty) | `components/terminal.nix`                         | `home/terminal.nix` or configs/ |
| 1Password shell    | `components/_1password-shell.nix`                 | Not present                     |
| GNOME/desktop      | `components/gnome/`                               | Not applicable (macOS focused)  |
| Zellij             | `components/zellij.nix` (server only)             | Not present (we use tmux)       |
| Direnv             | Inline in `home.nix`                              | `home/shell.nix` or similar     |
| nix-index          | Inline in `home.nix`                              | Not present                     |
| nh (Nix helper)    | `shared.nix` with flake path + clean config       | Taskfile wraps nh               |
| Tokyonight theme   | `nixos/theme.nix` via tokyonight-nix input        | Stylix                          |
| OpenCode           | `components/opencode.nix`                         | Not present                     |
| Zen browser        | `components/zen.nix`                              | Not present                     |

### Candidate Changes

1. **Parameterized nix-conf.nix shared between system and home-manager**
   - **Rationale:** mrjones2014's `nixos/nix-conf.nix` takes `{ isHomeManager }` as a parameter, allowing the same file to configure Nix settings for both system-level and home-manager contexts. The `lib.optionalAttrs (!isHomeManager)` guard skips system-only settings (experimental-features, trusted-substituters) when imported by HM. This avoids duplicating Nix configuration across two files. Our setup has `hosts/shared.nix` for system-level Nix settings, but HM may not share them cleanly.
   - **Source:** `nixos/nix-conf.nix`, `nix-darwin/common.nix` (imports with `isHomeManager = false`), `home-manager/shared.nix` (imports with `isHomeManager = true`)
   - **Impact:** Low. Reduces duplication if our HM config needs nix settings like `nixPath` or `registry`. The closure-based parameterization is a clean pattern.

2. **nix-auto-follow for flake input hygiene**
   - **Rationale:** mrjones2014 uses `nix-auto-follow` (from `github:fzakaria/nix-auto-follow`) as both a flake input and a CI check. The `checks/flake-inputs.nix` derivation runs `auto-follow --check` to verify that all transitive inputs that could be followed are actually followed. This catches cases where a new input adds a `nixpkgs` dependency that should use `inputs.nixpkgs.follows` but does not. Our flake manually specifies follows, but has no automated verification.
   - **Source:** `flake.nix` (nix-auto-follow input), `checks/flake-inputs.nix`
   - **Impact:** Medium. Prevents subtle cache misses from duplicate nixpkgs evaluations. Particularly valuable as the number of flake inputs grows.

3. **nix-fast-build for CI**
   - **Rationale:** The CI workflow uses `nix-fast-build` (run inside a `ci` devShell) to build all flake outputs with `--eval-workers 1 --skip-cached --no-nom --systems x86_64-linux,aarch64-darwin`. This is faster than `nix flake check` for multi-system configs because it parallelizes evaluation and skips already-cached derivations. Combined with Cachix, this creates a build cache that speeds up local rebuilds after CI runs.
   - **Source:** `.github/workflows/build-and-push-flake.yml`, `flake.nix` (devShells.ci)
   - **Impact:** Medium. Our Dagger-based CI could benefit from nix-fast-build if we want to cache NixOS/Darwin builds. The `--skip-cached` flag is particularly useful for incremental CI.

4. **statix in treefmt for Nix linting**
   - **Rationale:** mrjones2014 includes `statix` in the treefmt config alongside nixfmt. Statix is a Nix linter that catches anti-patterns like unused let bindings, eta-reducible functions, and deprecated builtins. Our treefmt uses nixfmt for formatting but has no Nix-specific linter. Adding statix would catch issues that nixfmt does not address.
   - **Source:** `treefmt.nix` (`statix.enable = true`)
   - **Impact:** Low. Easy to add and catches real issues, but may flag patterns we intentionally use.

5. **Explicit allowUnfreePredicate with conditional lists**
   - **Rationale:** mrjones2014 uses `allowUnfreePredicate` with an explicit package name list rather than `allowUnfree = true`. The list is conditional on `isServer` to only allow unfree packages where they are actually needed. This is stricter than our blanket `allowUnfree = true` and makes unfree dependencies visible and auditable.
   - **Source:** `nixos/nixpkgs-config.nix`
   - **Impact:** Low. More explicit but requires maintenance as packages are added/removed. Only worth it if tracking unfree dependencies matters for the project.

6. **Separate devShells for development vs. CI**
   - **Rationale:** mrjones2014 defines two devShells: `default` (for local development with formatter, mdbook, nix-auto-follow) and `ci` (for CI with nix-fast-build, nix-auto-follow). This keeps CI-only tools out of the local dev shell and vice versa. Our flake has a single devShell.
   - **Source:** `flake.nix` (devShells.default, devShells.ci)
   - **Impact:** Low. Only relevant if CI needs different tools than local development.

7. **Flake checks that build all system configurations**
   - **Rationale:** mrjones2014's `flake.nix` generates flake checks from all `nixosConfigurations` and `darwinConfigurations` by extracting the system build toplevel. This means `nix flake check` validates that every host config actually builds, not just that the flake evaluates. Our `task check` runs `nix flake check` but may not include system build validation for all hosts.
   - **Source:** `flake.nix` (`checks = checksForConfigs self.nixosConfigurations ...`)
   - **Impact:** Medium. Catches configuration errors early. The `checksForConfigs` helper pattern (filtering by current system, extracting `.config.system.build.toplevel`) is reusable.

## ahmedelgabri/dotfiles

**Source:** [github.com/ahmedelgabri/dotfiles](https://github.com/ahmedelgabri/dotfiles)

A macOS-focused dotfiles repo (two Darwin hosts, one NixOS host) that keeps raw config files in a top-level `config/` directory alongside Nix code in `nix/`. Uses zsh (not fish), Hammerspoon for macOS automation, Karabiner for keyboard remapping, Neovim configured entirely in Lua, and agenix for secrets. Notable for its `my.*` NixOS options abstraction that aliases home-manager paths for ergonomic access, and its heavy use of `system.activationScripts` to symlink mutable config directories.

### Comparison Table

| Aspect                           | ahmedelgabri/dotfiles                                                                                                                                                                                                                                                                                | Our dotfiles                                                                                                                                            |
| -------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Flake structure**              | Single `flake.nix` with inline `sharedConfiguration`, `mapHosts` helper, per-system `forAllSystems`. Hosts registered as `darwinHosts`/`linuxHosts` attrsets mapping hostname to system architecture.                                                                                                | `flake.nix` via flake-parts with `mkDarwin`/`mkHome`/`mkNixOS` helpers. Hosts declared as `mkDarwin`/`mkNixOS` calls with `hostConfig` attrsets.        |
| **Module organization**          | `nix/modules/shared/` (25 files, cross-platform HM modules) + `nix/modules/darwin/` (3 files: default, hammerspoon, karabiner). Each module uses `mkEnableOption` under `my.modules.<name>.enable`.                                                                                                  | Flat `home/` (14 .nix files) for HM modules. System modules live in `hosts/` files. Similar `mkEnableOption` pattern under `dotfiles.*`.                |
| **Config file management**       | Top-level `config/` directory (30+ subdirs: nvim, ghostty, tmux, zsh.d, hammerspoon, karabiner, git, etc.) symlinked via `my.hm.file` or `system.activationScripts`. Two strategies: immutable (hm.file source) and mutable (activationScripts `ln -sf`).                                            | `configs/` directory with raw config files, symlinked via `xdg.configFile` and `home.file`. Single strategy: immutable HM symlinks.                     |
| **Mutable config pattern**       | `system.activationScripts.postActivation.text` for configs that apps modify at runtime (nvim, hammerspoon, karabiner). Uses `ln -sf $HOME/.dotfiles/config/<dir> $XDG_CONFIG_HOME/<dir>` to create regular symlinks that bypass HM's read-only store links.                                          | No mutable config workaround. All config files are immutable Nix store symlinks.                                                                        |
| **Settings/options abstraction** | `nix/modules/shared/settings.nix` defines `my.*` NixOS options: `my.name`, `my.username`, `my.email`, `my.env`, `my.hm.file`, `my.hm.configFile`, `my.hm.dataFile`, `my.user`, etc. The `my.hm.*` options are aliases to `home-manager.users.<user>.xdg.*` and `home.file` via `mkAliasDefinitions`. | `hosts/options.nix` and `home/options.nix` define `dotfiles.*` options. No aliasing of HM paths; modules use `xdg.configFile` and `home.file` directly. |
| **Shell**                        | zsh (not fish). Extensive inline shell config in `user-shell.nix` (~500 lines): pure-prompt, fzf integration, zsh-autosuggestions, fast-syntax-highlighting, atuin, zoxide, mise, direnv. Shell init inlined via `programs.zsh.shellInit`/`interactiveShellInit` for startup performance.            | fish shell via `home/fish.nix`. fish plugins managed via HM `programs.fish.plugins`. Separate tools (direnv, atuin) configured in their own HM modules. |
| **Secrets**                      | agenix with SSH keys as age identities. `nix/secrets/secrets.nix` defines per-host + per-user public keys. Currently stores only npmrc (intentionally minimal -- prefers regenerating secrets per machine). Dedicated `~/.ssh/agenix` key.                                                           | sops-nix with standalone age key. Secrets in `secrets/` directory. More secrets stored (API keys, tokens).                                              |
| **Neovim**                       | Full Lua config in `config/nvim/` (init.lua, lua/, plugin/, after/, colors/, spell/, ftdetect/). Symlinked via `system.activationScripts` (mutable). Nix only installs neovim-unwrapped + LSPs/tools.                                                                                                | Neovim configured via HM `programs.neovim` in `home/vim.nix` with Lua config in `configs/nvim/`. Immutable HM symlink.                                  |
| **Ghostty**                      | Raw config in `config/ghostty/` (config file + custom themes). Installed via `homebrew.casks` on Darwin, `pkgs.ghostty` on Linux. Config symlinked via `my.hm.file`.                                                                                                                                 | Managed via `home/ghostty.nix` using HM `programs.ghostty` module with inline settings. No raw config file.                                             |
| **Hammerspoon**                  | Full Lua config in `config/.hammerspoon/` (init.lua, layout.lua, location.lua, mappings.lua, window-management.lua, utils.lua, Spoons/). Installed via homebrew cask. Config symlinked via `system.activationScripts`.                                                                               | Not used. macOS window management handled by native tiling via `system.defaults`.                                                                       |
| **Karabiner**                    | Config in `config/karabiner/`. Installed via homebrew cask. Symlinked via `system.activationScripts` (mutable, since the GUI modifies the config).                                                                                                                                                   | Not used. No keyboard remapping tool.                                                                                                                   |
| **Terminal**                     | Ghostty (primary, tip build on macOS) + Kitty (secondary). Both configured with raw config files in `config/`.                                                                                                                                                                                       | Ghostty configured via HM `programs.ghostty`.                                                                                                           |
| **Homebrew**                     | Uses `nix-homebrew` flake input (zhaofengli-wip/nix-homebrew) for declarative Homebrew management alongside nix-darwin's `homebrew` module. Enables Rosetta on aarch64. `onActivation.cleanup = "zap"`.                                                                                              | nix-darwin `homebrew` module directly. No `nix-homebrew` input. Similar `onActivation` settings.                                                        |
| **Custom packages**              | `nix/pkgs/` with 2 packages (pragmatapro font, hcron). Additional packages defined inline as overlays (next-prayer, overridden notmuch, pure-prompt with patch).                                                                                                                                     | `pkgs/` with custom derivations. Overlays defined inline in `flake.nix`.                                                                                |
| **Patches**                      | `nix/patches/` directory with `pure.patch` (modifying pure-prompt behavior). Applied via `overrideAttrs` in the overlay.                                                                                                                                                                             | No patches directory. No package patches.                                                                                                               |
| **Bootstrap**                    | Flake apps (`nix run`) with per-architecture bootstrap scripts (`scripts/x86_64-darwin_bootstrap`). Uses `writeShellApplication` to wrap scripts with shared utils.                                                                                                                                  | No bootstrap scripts. Manual setup process.                                                                                                             |
| **Formatter**                    | `alejandra` (set as `formatter` per-system). Also in devShell.                                                                                                                                                                                                                                       | `nixfmt` via treefmt-nix. Formatter applied to all file types via treefmt.                                                                              |
| **Dev shell**                    | Two devShells: `default` (typos, typos-lsp, alejandra, agenix) and `go` (Go toolchain).                                                                                                                                                                                                              | No devShell. Formatting via `nix fmt`.                                                                                                                  |
| **Flake inputs**                 | 12 inputs: nixpkgs, home-manager, darwin, nix-homebrew, agenix, nur, yazi (+ plugins), gh-gfm-preview, git-wt, ccpeek. Several personal tools as flake inputs.                                                                                                                                       | Similar count. Uses flake-parts, stylix, sops-nix instead of agenix. No NUR.                                                                            |
| **macOS defaults**               | Extensive `system.defaults` in `nix/modules/darwin/default.nix` (~150 lines): dock, finder, trackpad, screencapture, NSGlobalDomain, CustomUserPreferences (Safari, TimeMachine, ImageCapture, SoftwareUpdate, AdLib, etc.). Disables all hot corners. Hides menu bar.                               | `system.defaults` in `hosts/mac.nix`. Similar coverage but less exhaustive (no Safari, TimeMachine, ImageCapture, SoftwareUpdate custom preferences).   |
| **Theming**                      | Manual: PragmataPro font via custom derivation, vivid for LS_COLORS, custom Ghostty themes. No unified theming framework.                                                                                                                                                                            | Stylix with OneDark base16 scheme. Unified theming across all programs.                                                                                 |
| **CI/CD**                        | `.github/` directory present but contents not inspected. No visible CI workflows in the root listing.                                                                                                                                                                                                | Dagger-based CI via `task check`.                                                                                                                       |
| **Per-host config**              | Hosts in `nix/hosts/<hostname>.nix`. Each sets `networking.hostName`, `my.*` overrides (username, email, company, devFolder), module enables, user packages, and homebrew casks/brews. Clean, compact format.                                                                                        | Hosts declared in `flake.nix` via `hostConfig` attrsets. Host-specific modules in `hosts/`. Similar data but split across two locations.                |
| **AI tooling**                   | Dedicated `ai.nix` module: ollama as launchd daemon on Darwin, Claude Code (homebrew cask on Darwin, nixpkgs on Linux), llama-cpp. Claude config (CLAUDE.md, agents, commands, hooks, scripts, skills) managed via `my.hm.file` with a mutable settings.json workaround.                             | No AI-specific module. Claude Code installed but no managed Claude config files.                                                                        |

### Home-Manager Modules Comparison

Modules in ahmedelgabri's `nix/modules/shared/` that we lack or configure differently:

| Their module                                   | Our equivalent                           | Notes                                                                                                             |
| ---------------------------------------------- | ---------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `user-shell.nix` (zsh, 500+ lines)             | `home/fish.nix`                          | Different shell. Their inline zsh config approach (reading files into Nix strings) optimizes startup time.        |
| `mail.nix` (29KB)                              | (none)                                   | Full email setup: mbsync, msmtp, notmuch, aerc. Largest module by far.                                            |
| `ai.nix`                                       | (none)                                   | Ollama as launchd daemon, Claude Code config management, llama-cpp.                                               |
| `ghostty.nix`                                  | `home/ghostty.nix`                       | They use raw config files; we use HM `programs.ghostty` module.                                                   |
| `kitty.nix`                                    | (none)                                   | Secondary terminal. Raw config file approach.                                                                     |
| `gui.nix` (15KB)                               | (partially)                              | Firefox with NUR extensions, extensive `about:config` settings. We don't manage browser config.                   |
| `vim.nix`                                      | `home/vim.nix`                           | They install neovim-unwrapped + tools via Nix, config via activationScripts symlink. We use HM `programs.neovim`. |
| `yazi.nix`                                     | (none)                                   | TUI file manager with plugins from flake inputs.                                                                  |
| `zk.nix`                                       | (none)                                   | Zettelkasten note-taking tool.                                                                                    |
| `discord.nix`                                  | (none)                                   | Discord with custom CSS (BetterDiscord-style).                                                                    |
| `mpv.nix`                                      | (none)                                   | Media player with custom config.                                                                                  |
| `go.nix`, `rust.nix`, `python.nix`, `node.nix` | `home/dev.nix` (partial)                 | Per-language dev environment modules. More granular than our approach.                                            |
| `gpg.nix`                                      | (none)                                   | GPG agent with SSH support.                                                                                       |
| `tmux.nix`                                     | (none)                                   | tmux with raw config from `config/tmux/`.                                                                         |
| `agenix.nix`                                   | (sops-nix equivalent)                    | Different secrets tool. Their module is minimal (just a shell alias).                                             |
| `settings.nix`                                 | `hosts/options.nix` + `home/options.nix` | Their `my.hm.*` aliasing pattern is more ergonomic.                                                               |

### Candidate Changes

1. **HM path aliasing via `mkAliasDefinitions`**
   - **Rationale:** ahmedelgabri's `settings.nix` creates short aliases like `my.hm.file`, `my.hm.configFile`, `my.hm.dataFile` that map to `home-manager.users.<user>.home.file`, `...xdg.configFile`, `...xdg.dataFile` using `mkAliasDefinitions`. This reduces the verbosity of placing files from `home-manager.users."${config.my.username}".xdg.configFile.X = ...` to `my.hm.configFile.X = ...`. Our modules use the full HM paths or the slightly shorter `xdg.configFile` (which still requires being inside a HM module context).
   - **Source:** `nix/modules/shared/settings.nix` (options.my.hm, config block with mkAliasDefinitions)
   - **Impact:** Low. Ergonomic improvement. Only valuable if many modules need to place files.

2. **Mutable config via `system.activationScripts` for runtime-modified configs**
   - **Rationale:** Some apps modify their own config at runtime (Karabiner GUI saves changes, Neovim plugin managers write lock files, Hammerspoon reloads). ahmedelgabri uses `system.activationScripts.postActivation.text` to create regular filesystem symlinks (`ln -sf`) from `~/.dotfiles/config/<dir>` to `~/.config/<dir>`, bypassing HM's read-only Nix store symlinks. This lets the app write back to the config directory while still tracking it in git. Our Neovim config uses immutable HM symlinks, which may conflict with plugins that want to write state.
   - **Source:** `nix/modules/shared/vim.nix`, `nix/modules/darwin/hammerspoon.nix`, `nix/modules/darwin/karabiner.nix`
   - **Impact:** Medium. Solves a real problem for mutable configs. The trade-off is losing HM's atomicity guarantees for those specific files.

3. **Ollama as a launchd daemon on macOS**
   - **Rationale:** ahmedelgabri's `ai.nix` configures ollama as a `launchd.daemons.ollama` service on Darwin, with proper logging paths, auto-restart (`KeepAlive = true`), and environment variables. This ensures ollama is always available for local LLM inference without manual startup. If we use ollama, this is a clean integration pattern.
   - **Source:** `nix/modules/shared/ai.nix` (launchd.daemons.ollama)
   - **Impact:** Low. Only relevant if we want local LLM inference always available.

4. **Claude Code config management via home-manager with mutable settings workaround**
   - **Rationale:** ahmedelgabri manages Claude Code configuration (CLAUDE.md, agents, commands, hooks, scripts, skills, docs) as files in `config/claude/` deployed via `my.hm.file` to `~/.claude/`. For `settings.json` (which Claude Code modifies at runtime), they use a workaround: deploy as `.settings.json.bk` via HM, then use `onChange` to copy (not symlink) it to the real path, so Claude Code can write to it. This balances version control with runtime mutability.
   - **Source:** `nix/modules/shared/ai.nix` (my.hm.file entries for .claude/\*)
   - **Impact:** Low. Only relevant if we want to version-control Claude Code configuration.

5. **Extensive macOS `CustomUserPreferences` for Safari, privacy, and system behavior**
   - **Rationale:** ahmedelgabri's darwin module sets ~40 additional macOS preferences via `system.defaults.CustomUserPreferences` beyond what nix-darwin's typed options cover. This includes Safari security/privacy settings (disable search suggestions, prevent auto-open of downloads, disable Java), disabling .DS_Store on network/USB volumes, auto-quit printer apps, preventing Photos from auto-opening on device connect, and TimeMachine backup prompts. These are practical hardening/convenience settings.
   - **Source:** `nix/modules/darwin/default.nix` (system.defaults.CustomUserPreferences)
   - **Impact:** Low. Cherry-pick individual settings as desired. The `.DS_Store` prevention (`DSDontWriteNetworkStores`, `DSDontWriteUSBStores`) and screensaver password settings are broadly useful.

6. **Per-language dev environment modules (go.nix, rust.nix, python.nix, node.nix)**
   - **Rationale:** ahmedelgabri splits dev tooling into per-language modules, each with its own `mkEnableOption`. This allows hosts to opt in/out of language toolchains independently. Each module sets `GOPATH`, `CARGO_HOME`, `npm_config_*`, etc. and installs language-specific tools. Our dev tooling is more monolithic.
   - **Source:** `nix/modules/shared/go.nix`, `rust.nix`, `python.nix`, `node.nix`
   - **Impact:** Low. More granular but adds file count. Only beneficial if different hosts need different language stacks.

7. **nix-homebrew flake input for declarative Homebrew management**
   - **Rationale:** ahmedelgabri uses `nix-homebrew` (zhaofengli-wip/nix-homebrew) as a flake input alongside nix-darwin's `homebrew` module. This manages Homebrew installation itself declaratively (taps, Rosetta support on aarch64), whereas we rely on Homebrew being pre-installed. The `nix-homebrew` input handles the bootstrap problem of Homebrew not being available on a fresh system.
   - **Source:** `flake.nix` (inputs.nix-homebrew), `nix/modules/darwin/default.nix` (nix-homebrew config)
   - **Impact:** Medium. Solves the Homebrew bootstrap problem on fresh installs. Adds one flake input.

## megalithic/dotfiles

**Source:** [github.com/megalithic/dotfiles](https://github.com/megalithic/dotfiles)

A macOS-focused nix-darwin + standalone home-manager setup managing two Apple Silicon laptops (personal and work). Notable for its independent darwin/HM rebuild strategy (they run separately rather than as an integrated module), a custom `mkApp` builder for macOS app bundles from DMG/ZIP sources, extensive macOS `system.defaults` configuration, agenix for secrets, nix-homebrew for declarative Homebrew management, and raw config files for Ghostty, Hammerspoon, tmux, kitty, and Neovim (Lua). Uses jujutsu (jj) as its VCS alongside git.

### Comparison Table

| Aspect                       | megalithic/dotfiles                                                                                                                                                                                                                                                                                          | Our dotfiles                                                                                                                                                    |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Flake structure**          | Flat `flake.nix` with three helpers extracted to `lib/`: `mkDarwin.nix`, `mkHome.nix`, `mkInit.nix`. Two Darwin configs and two standalone HM configs defined explicitly. Single hardcoded `arch = "aarch64-darwin"`.                                                                                        | Everything in `flake.nix` via flake-parts. Three inline helpers (`mkDarwin`, `mkHome`, `mkNixOS`). Supports multiple architectures.                             |
| **Darwin/HM integration**    | Deliberately separated: `darwinConfigurations` has no home-manager module; `homeConfigurations` runs independently via `home-manager switch`. Rebuilt separately with `just darwin` and `just home`.                                                                                                         | Integrated: home-manager is a module inside `darwinSystem`/`nixosSystem`. Single `task switch` rebuilds everything together.                                    |
| **Module organization**      | `modules/system.nix` (macOS defaults, ~450 lines), `modules/brew.nix`, `modules/darwin/services.nix`. System config is dense and centralized.                                                                                                                                                                | `hosts/mac.nix` (system config), `hosts/shared.nix` (common nix settings). macOS defaults spread across host files. Less comprehensive macOS defaults coverage. |
| **Home-manager layout**      | `home/common/` directory with `default.nix` (central imports), plus subdirectories: `programs/` (ai, browsers, email, fish, fzf, ghostty, jj, nvim, shade), `git/`, `karabiner/`, `kanata/`, `starship/`, `yazi/`, `zsh/`, `surfingkeys/`. Per-host files (`home/megabookpro.nix`) just import `./common`.   | Flat `home/` with ~14 .nix files. No subdirectory grouping by concern.                                                                                          |
| **Custom lib**               | `lib/default.nix` extends `nixpkgs.lib` with `lib.mega` namespace containing `mkApp` (macOS app builder) and `mkAppActivation` (activation scripts for /Applications symlinks + cleanup). `lib/paths.nix` centralizes all path constants. `lib/builders/` for additional helpers.                            | Helper functions inline in `flake.nix`. No lib extensions or custom namespace.                                                                                  |
| **Custom packages (pkgs/)**  | `pkgs/default.nix` is an overlay exposing custom `mkApp` derivations for macOS apps (Fantastical, Bloom, Brave Nightly, Helium, Tidewave, Tuna) and CLI tools (chrome-devtools-mcp). Each app specifies `appLocation` (copy/symlink/wrapper) and optional `binaries` list.                                   | `pkgs/` has custom derivations. No macOS app bundle builder pattern.                                                                                            |
| **Overlays**                 | `overlays/default.nix` returns a list: input overlays (jujutsu, NUR, mcp-servers-nix), an unstable nixpkgs overlay (`pkgs.unstable`), plus input aliases (llm-agents, nvim-nightly, expert). Custom pkgs imported as final overlay.                                                                          | Overlays defined as `sharedOverlays` list in `flake.nix`. Similar pattern with fewer external overlays.                                                         |
| **Secrets**                  | agenix with SSH ed25519 public keys (from 1Password SSH agent). Per-host work secrets (`work-env-vars-megabookpro.age`, `work-env-vars-rxbookpro.age`). System-level agenix module in darwin config; HM-level agenix module in home config.                                                                  | sops-nix with age encryption. Single `.sops.yaml`, simpler secret structure.                                                                                    |
| **Operations (task runner)** | justfile (~260 lines). Layered rebuild commands: `just rebuild` (sync + darwin + home), `just darwin`, `just home`, `just bootstrap`, `just validate`. Supports `--dry-run` and `--skip-sync` flags. Auto-syncs from remote via jujutsu before rebuilds. Legacy aliases maintained.                          | Taskfile.yaml (~50 lines). `task switch` auto-detects platform. Simpler, fewer commands.                                                                        |
| **VCS**                      | jujutsu (jj) as primary VCS, with git as backend. `_sync-main` recipe fetches from remote via `jj git fetch`, rebases work onto updated main.                                                                                                                                                                | git exclusively.                                                                                                                                                |
| **Formatting**               | treefmt via `format.nix`: nixfmt-rfc-style, deadnix, statix, stylua, taplo, keep-sorted. Statix wrapped in a shell script for fix mode.                                                                                                                                                                      | treefmt-nix: nixfmt, prettier. Fewer formatters.                                                                                                                |
| **Neovim**                   | Neovim nightly via `neovim-nightly-overlay`. Raw Lua config in `config/nvim/` symlinked via `xdg.configFile`. Extensive LSP/treesitter/plugin config in Lua.                                                                                                                                                 | Neovim via home-manager `programs.neovim`. Config in `configs/nvim/`.                                                                                           |
| **Terminal**                 | Ghostty (primary) + kitty (backup). Raw config files in `config/ghostty/`, `config/kitty/`, symlinked via `xdg.configFile`.                                                                                                                                                                                  | Ghostty via `programs.ghostty` HM module.                                                                                                                       |
| **Tmux**                     | Raw tmux config in `config/tmux/`, symlinked. Nix generates `tmux/nix.conf` with shell path.                                                                                                                                                                                                                 | No tmux config.                                                                                                                                                 |
| **Hammerspoon**              | Raw Lua config in `config/hammerspoon/`, symlinked. Nix generates `nix_path.lua` and `nix_env.lua` data files so Hammerspoon scripts can reference Nix-managed paths.                                                                                                                                        | No Hammerspoon.                                                                                                                                                 |
| **Shell**                    | Fish (primary) + zsh (backup). Fish config in `home/common/programs/fish/`. Zsh config as raw files in `home/common/zsh/` symlinked via `xdg.configFile`.                                                                                                                                                    | Fish only, configured in `home/fish.nix`.                                                                                                                       |
| **macOS defaults**           | Extremely comprehensive `modules/system.nix`: dock (persistent-apps, hot corners), finder, trackpad, keyboard, screencapture, control center, power management, firewall, Bluetooth audio optimization, Raycast hotkeys, symbolic hotkey overrides, text replacements, custom user preferences for 10+ apps. | Basic macOS defaults in `hosts/mac.nix`. Less comprehensive coverage.                                                                                           |
| **Theming**                  | Manual Everforest theme applied per-tool (bat theme, eza theme, process-compose theme, FZF colors). No unified theming system.                                                                                                                                                                               | Stylix with OneDark base16 scheme. Unified theming across tools.                                                                                                |
| **App management**           | Custom `mkApp` builder extracts DMG/ZIP/PKG to nix store. `mkAppActivation` handles symlink/copy to /Applications with metadata tracking, orphan cleanup, and binary linking to `~/.local/bin`. Supports `appLocation` modes: "copy" (for code-signed apps), "symlink", "wrapper".                           | Homebrew casks for macOS apps. No custom app builder.                                                                                                           |
| **Nix daemon**               | Determinate Nix (`nix.enable = false`). Custom nix config via `/etc/nix/nix.custom.conf` applied with `just apply-nix-config`.                                                                                                                                                                               | Lix (Nix implementation). Nix settings managed declaratively via nix-darwin.                                                                                    |
| **Path management**          | Centralized `lib/paths.nix` with named paths (home, icloud, proton, notes, nvimDb, dotfiles, config, localBin, etc.) passed via `specialArgs`.                                                                                                                                                               | Paths defined ad-hoc in modules or via `hostConfig` attrset.                                                                                                    |
| **Bootstrap**                | `mkInit` creates a flake app from a platform-specific bootstrap shell script (`scripts/aarch64_bootstrap.sh`). Also has `just init` and `just bootstrap` recipes for fresh installs and recovery.                                                                                                            | No bootstrap flake app. Manual setup.                                                                                                                           |
| **1Password integration**    | SSH agent socket configured in `environment.extraInit`. `op-shell-plugins` HM module imported. 1Password used as SSH key agent and for agenix identities.                                                                                                                                                    | No 1Password integration for SSH.                                                                                                                               |
| **CI/CD**                    | None visible.                                                                                                                                                                                                                                                                                                | Dagger-based: `task check` runs validation.                                                                                                                     |

### Home-Manager Modules Comparison

Modules in megalithic's `home/common/` that we lack or configure differently:

| Their module           | Our equivalent                   | Notes                                                                            |
| ---------------------- | -------------------------------- | -------------------------------------------------------------------------------- |
| `programs/ai/`         | (none)                           | AI/LLM tool configuration as a dedicated HM module group.                        |
| `programs/browsers/`   | (none)                           | Browser configuration (custom `mkChromiumBrowser` module for Helium, Brave).     |
| `programs/email/`      | (none)                           | Email client configuration (MailMate).                                           |
| `programs/jj/`         | (none)                           | Jujutsu VCS configuration via HM.                                                |
| `programs/shade.nix`   | (none)                           | Shade (Ghostty shade/dimming) configuration.                                     |
| `programs/fish/`       | `home/fish.nix`                  | Similar scope; theirs is a directory with multiple files.                        |
| `programs/fzf.nix`     | `home/default.nix` (fzf section) | Similar. Theirs has Everforest-themed colors.                                    |
| `programs/nvim.nix`    | `home/vim.nix`                   | Theirs uses neovim-nightly overlay; ours uses stable nixpkgs neovim.             |
| `programs/ghostty.nix` | `home/ghostty.nix`               | Similar; theirs is minimal (raw config in `config/ghostty/`).                    |
| `programs/agenix.nix`  | (none)                           | HM-level agenix secret decryption (env-vars, work-env-vars).                     |
| `karabiner/`           | (none)                           | Karabiner-Elements JSON config managed via HM.                                   |
| `kanata/`              | (none)                           | Kanata keyboard remapping daemon config.                                         |
| `starship/`            | (none)                           | Starship prompt with TOML config. We use Tide (fish-specific).                   |
| `yazi/`                | (none)                           | Yazi file manager configuration.                                                 |
| `surfingkeys/`         | (none)                           | SurfingKeys browser extension config.                                            |
| `mac-aliases.nix`      | (none)                           | macOS Finder alias creation for Nix-managed apps.                                |
| `services.nix`         | (none)                           | HM-level launchd services (ollama, process-compose, etc.).                       |
| `rust.nix`             | (none)                           | Rust toolchain paths (`CARGO_HOME`, `RUSTUP_HOME`) via HM.                       |
| `lib.nix`              | (none)                           | Custom HM lib helpers (`config.lib.mega.linkConfig`, `config.lib.mega.linkBin`). |
| `packages.nix`         | `home/default.nix` (packages)    | Separate packages file vs. inline in default.nix.                                |
| `programs/discord.nix` | (none)                           | Discord launch wrapper with Wayland/GPU flags.                                   |

### Candidate Changes

1. **Separate darwin and home-manager rebuilds**
   - **Rationale:** megalithic deliberately decouples darwin-rebuild from home-manager switch. This allows faster iteration on user-level config (packages, dotfiles) without sudo or touching system settings. Their `just home` takes seconds while `just darwin` needs sudo and is slower. Our integrated approach means every `task switch` rebuilds both, even when only HM changes were made.
   - **Source:** `lib/mkDarwin.nix` (no HM module included), `lib/mkHome.nix` (standalone), `justfile` (`darwin` and `home` recipes)
   - **Impact:** Medium. Faster iteration cycle for HM-only changes. Requires maintaining two configurations per host but each is simpler.

2. **Centralized path constants module**
   - **Rationale:** megalithic's `lib/paths.nix` is a single file defining all well-known paths (home, icloud, proton, notes, dotfiles, config, localBin, cargoHome, etc.) as a plain attrset, passed via `specialArgs`. Modules reference `paths.icloud` instead of hardcoding `"${config.home.homeDirectory}/Library/Mobile Documents/..."`. This eliminates path duplication and makes cloud storage / notes paths consistent across all modules.
   - **Source:** `lib/paths.nix`, referenced throughout `hosts/common.nix` and `home/common/default.nix`
   - **Impact:** Low. Simple cleanup pattern. We already pass values via `hostConfig` but paths are more scattered.

3. **Custom mkApp builder for macOS app bundles**
   - **Rationale:** megalithic's `lib/mkApp.nix` + `lib/default.nix` (`mkAppActivation`) provides a complete pipeline for managing macOS apps via Nix: download DMG/ZIP, extract to nix store, symlink or copy to /Applications with metadata tracking for orphan cleanup. Supports three modes: "symlink" (fast but breaks some apps), "copy" (preserves code signatures, needed for sandboxed apps), and "wrapper" (for Chromium-based browsers). This replaces Homebrew casks for many apps.
   - **Source:** `lib/mkApp.nix`, `lib/default.nix` (mkAppActivation), `pkgs/default.nix` (app definitions)
   - **Impact:** High. Would allow managing more macOS apps purely through Nix, reducing Homebrew dependency. Significant implementation effort for the builder + activation scripts.

4. **Comprehensive macOS system.defaults configuration**
   - **Rationale:** megalithic's `modules/system.nix` is ~450 lines of macOS defaults covering areas we don't configure: Bluetooth audio optimization (AAC/AptX bitpool settings), power management (sleep timers, restart after freeze), firewall (stealth mode, signed app allowance), symbolic hotkey overrides (disable Spotlight shortcut to use Raycast), control center items, persistent dock apps, text replacements, per-app CustomUserPreferences (Ghostty auto-update, Raycast hotkeys, Activity Monitor defaults, WindowManager settings). The `postActivation` script applies symbolic hotkey changes without requiring logout.
   - **Source:** `modules/system.nix`
   - **Impact:** Medium. Cherry-pick individual settings rather than adopting the whole file. The Bluetooth audio, power, firewall, and symbolic hotkey patterns are most transferable.

5. **Nix-generated data files for non-Nix tools (Hammerspoon, tmux)**
   - **Rationale:** megalithic generates small data files (`xdg.dataFile`) that inject Nix-managed paths and environment variables into tools that run outside the Nix environment. Hammerspoon gets `nix_path.lua` with `NIX_PATH` and `NIX_ENV` tables; tmux gets `nix.conf` with the Nix-managed fish path. This bridges the gap between Nix-managed config and tools that don't natively integrate with Nix.
   - **Source:** `home/common/default.nix` (`xdg.dataFile."hammerspoon/nix_path.lua"`, `xdg.dataFile."tmux/nix.conf"`)
   - **Impact:** Low. Pattern is useful if we adopt Hammerspoon or other tools that need Nix path awareness.

6. **deadnix and statix in treefmt pipeline**
   - **Rationale:** megalithic's `format.nix` includes deadnix (removes unused Nix code) and statix (Nix anti-pattern linter) alongside nixfmt. Both run as treefmt formatters, so `nix fmt` automatically cleans dead code and fixes common anti-patterns. We only have nixfmt in our treefmt config. This was also noted in US-006 (mrjones2014/dotfiles).
   - **Source:** `format.nix`
   - **Impact:** Low. Small addition to treefmt config. Catches unused bindings and anti-patterns automatically.

7. **keep-sorted formatter for maintaining sorted lists**
   - **Rationale:** megalithic uses `keep-sorted` as a treefmt formatter alongside comments like `# keep-sorted start` / `# keep-sorted end` to automatically maintain alphabetical ordering in package lists, formatter blocks, and other enumerations. This prevents merge conflicts from unordered insertions and keeps lists readable without manual sorting.
   - **Source:** `format.nix` (keep-sorted in runtimeInputs and formatter config)
   - **Impact:** Low. Small quality-of-life improvement. Useful for our package lists in `home/default.nix` and homebrew lists.

8. **nix-homebrew for declarative Homebrew bootstrap**
   - **Rationale:** megalithic uses `nix-homebrew` (zhaofengli-wip/nix-homebrew) with pinned homebrew-core, homebrew-cask, homebrew-services, and homebrew-bundle as non-flake inputs. This manages the Homebrew installation itself declaratively, including Rosetta support (`enableRosetta = true`), immutable taps (`mutableTaps = false`), and auto-migration. This is the third repo (after chenglab US-004 and ahmedelgabri US-007) using this pattern, making it a strong recurring signal.
   - **Source:** `flake.nix` (nix-homebrew + homebrew-\* inputs), `brew_config` function
   - **Impact:** Medium. Solves the Homebrew bootstrap problem. We already have `nix-homebrew` as an input but this shows a more complete integration pattern with immutable taps and custom tap support (e.g., FelixKratz/homebrew-formulae).

## ryan4yin/nix-darwin-kickstarter

**Source:** [github.com/ryan4yin/nix-darwin-kickstarter](https://github.com/ryan4yin/nix-darwin-kickstarter)

This is a beginner-friendly template repo, not a personal config. It provides two template variants: a `minimal/` template (nix-darwin only, no home-manager) and a `rich-demo/` template (nix-darwin + home-manager with macOS defaults, Homebrew, fonts, shell, and git). The repo is designed to be forked and customized, with `__USERNAME__`, `__SYSTEM__`, and `__HOSTNAME__` placeholders that users replace with their own values.

### Comparison Table

| Aspect                       | nix-darwin-kickstarter                                                                                         | Our repo                                                                    |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| **Purpose**                  | Beginner template / kickstarter                                                                                | Personal production config                                                  |
| **Flake structure**          | Two independent sub-flakes (`minimal/`, `rich-demo/`), each self-contained with own `flake.nix` + `flake.lock` | Single flake with `mkDarwin`/`mkHome`/`mkNixOS` helpers                     |
| **Flake framework**          | Raw `outputs` function, no flake-parts/flake-utils                                                             | flake-parts                                                                 |
| **Platform support**         | macOS only (nix-darwin)                                                                                        | macOS (nix-darwin) + NixOS + standalone Linux (home-manager)                |
| **Home-manager integration** | `minimal/`: none; `rich-demo/`: HM as darwin module (like ours)                                                | HM as darwin/NixOS module                                                   |
| **Module organization**      | `modules/` (system) + `home/` (HM), 4-5 files each                                                             | `hosts/` (system) + `home/` (HM modules), many more files                   |
| **Module naming**            | Domain-based: `nix-core.nix`, `system.nix`, `apps.nix`, `host-users.nix`                                       | Domain-based: `shell.nix`, `git.nix`, `editors.nix`, etc.                   |
| **Host config passing**      | `specialArgs` with `username`/`hostname`/`useremail` as plain strings, plus full `inputs` spread               | `hostConfig` attrset via `specialArgs`, plus `dotfiles.*` NixOS options     |
| **Commenting style**         | Heavy inline comments explaining every concept; tutorial-grade documentation                                   | Minimal comments; config speaks for itself                                  |
| **Placeholder pattern**      | `__USERNAME__`/`__SYSTEM__`/`__HOSTNAME__` with sed-based test substitution in Justfile                        | No placeholders; host values defined in `flake.nix`                         |
| **Formatter**                | `alejandra` (set as `formatter.${system}`)                                                                     | `nixfmt` via treefmt                                                        |
| **Operations**               | `Justfile` with `just darwin`/`just up`/`just clean`/`just gc`/`just fmt`                                      | `Taskfile.yaml` with `task switch`/`task update`/`task check`/`task format` |
| **Secrets**                  | None                                                                                                           | sops-nix                                                                    |
| **Overlays**                 | None                                                                                                           | None (minimal)                                                              |
| **Custom packages**          | None                                                                                                           | `pkgs/` directory                                                           |
| **CI/CD**                    | None (template repo)                                                                                           | Dagger-based toolchains                                                     |
| **Nix settings**             | `nix-core.nix` with substituters, GC, experimental features                                                    | `hosts/shared.nix` with similar settings                                    |
| **Homebrew**                 | Direct `homebrew` nix-darwin module; `rich-demo/` adds mirror config via `activationScripts`                   | Direct `homebrew` nix-darwin module                                         |
| **macOS defaults**           | `rich-demo/` has extensive `system.defaults` (dock, finder, trackpad, NSGlobalDomain, CustomUserPreferences)   | Our `hosts/mac.nix` configures defaults                                     |
| **Fonts**                    | `rich-demo/` installs nerd-fonts, material-design-icons, font-awesome via `fonts.packages`                     | Fonts managed via stylix + home-manager                                     |
| **Shell**                    | zsh (darwin default); `rich-demo/` configures via HM `programs.zsh`                                            | fish via home-manager                                                       |
| **Determinate Nix**          | Mentioned in comments (`nix.enable = false` if using Determinate); not default                                 | Not used; we use Lix                                                        |
| **Stable vs. unstable**      | Tracks stable `nixpkgs-25.11-darwin` / `nix-darwin-25.11`                                                      | Tracks unstable nixpkgs                                                     |

### Simplifications Revealed

The kickstarter's minimal template is essentially 4 files + a flake.nix. Comparing this to our setup reveals where our complexity is structural (supporting multiple platforms, many tools) versus incidental:

1. **Our `hostConfig` attrset vs. plain `specialArgs` strings**: The kickstarter passes `username`, `hostname`, and `useremail` as simple string values via `specialArgs`. Our `hostConfig` attrset bundles similar data but with more fields (git config, Homebrew lists, feature flags). Both approaches work; the kickstarter's is simpler because it has fewer per-host knobs to turn. Our complexity is justified by multi-host support.

2. **Domain-split modules are the universal pattern**: The kickstarter's `nix-core.nix` / `system.nix` / `apps.nix` / `host-users.nix` split matches our `hosts/shared.nix` / `hosts/mac.nix` / `home/*.nix` pattern closely. Every surveyed repo uses some variant of this. The naming differs but the intent is identical.

3. **The two-step darwin-rebuild command**: The kickstarter documents the raw `nix build .#darwinConfigurations.hostname.system && sudo -E ./result/sw/bin/darwin-rebuild switch --flake .#hostname` workflow. We abstract this behind `task switch` (which uses `nh`). The kickstarter's Justfile wraps the same thing with `just darwin`. No gap here.

4. **`auto-optimise-store = false` with rationale**: The kickstarter's `nix-core.nix` explicitly disables `auto-optimise-store` with a link to NixOS/nix#7273 ("cannot link .tmp-link: File exists"). Our `hosts/shared.nix` enables store optimization. Worth verifying whether we hit this bug.

5. **`system.primaryUser` setting**: The kickstarter sets `system.primaryUser = username` in `host-users.nix`. This is a newer nix-darwin option that designates the primary user for settings that previously inferred it. Worth checking if we set this.

### Candidate Changes

1. **Verify `auto-optimise-store` safety**
   - **Rationale:** The kickstarter explicitly disables `nix.settings.auto-optimise-store` citing NixOS/nix#7273, a race condition that causes "cannot link .tmp-link: File exists" errors during concurrent builds. Our `hosts/shared.nix` enables store optimization. If we use Lix (which may have fixed this), it could be fine, but it is worth verifying we do not hit this in practice.
   - **Source:** `minimal/modules/nix-core.nix` (comment + `auto-optimise-store = false`)
   - **Impact:** Low. A one-line change if needed, but store optimization saves significant disk space over time.

2. **Ensure `system.primaryUser` is set**
   - **Rationale:** The kickstarter sets `system.primaryUser = username` in `host-users.nix`. This is a nix-darwin option introduced to explicitly declare which user owns system-level settings (replacing previous implicit inference). If we are on a recent nix-darwin version and do not set this, we may get deprecation warnings.
   - **Source:** `minimal/modules/host-users.nix`, `rich-demo/modules/host-users.nix`
   - **Impact:** Low. A one-line addition to `hosts/mac.nix` if missing.

3. **Consider `CustomUserPreferences` for macOS defaults not exposed by nix-darwin**
   - **Rationale:** The `rich-demo/` template uses `system.defaults.CustomUserPreferences` to configure settings not directly supported by nix-darwin options: `.DS_Store` prevention on network/USB volumes, Stage Manager behavior, screen capture format/location, ad personalization, and Photos auto-launch. This is a clean escape hatch for `defaults write` commands that do not have first-class nix-darwin support.
   - **Source:** `rich-demo/modules/system.nix` (`CustomUserPreferences` block)
   - **Impact:** Low. Cherry-pick individual preferences as needed. The pattern itself (using `CustomUserPreferences`) is more important than the specific values.

4. **Add `homebrew.onActivation.cleanup = "zap"` for stricter Homebrew management**
   - **Rationale:** The `rich-demo/` template sets `cleanup = "zap"`, which uninstalls all formulae and related files not listed in the Nix-generated Brewfile. This enforces full declarative control over Homebrew packages. The `minimal/` template comments it out as a safer default. Our config should consider enabling this for tighter reproducibility.
   - **Source:** `rich-demo/modules/apps.nix` (`onActivation.cleanup = "zap"`)
   - **Impact:** Low. Behavioral change; could remove manually-installed brews on next rebuild if not listed in config.

5. **Tutorial-grade README as onboarding documentation**
   - **Rationale:** The `minimal/README.md` provides a step-by-step guide: install Nix, read the files, install Homebrew, search for TODOs, run the build command. It also documents the directory structure with a `tree` output. While our repo is not a template, a similar "getting started" section in our README could help contributors or future-self when setting up a new machine.
   - **Source:** `minimal/README.md`
   - **Impact:** Low. Documentation only, no code changes.

## joshsymonds/nix-config

**Source:** [github.com/joshsymonds/nix-config](https://github.com/joshsymonds/nix-config)

826 stars, 124 forks. One of the most forked nix-config repos on GitHub, managing a macOS laptop (cloudbank, aarch64-darwin) and multiple headless NixOS servers (ultraviolet, bluedesert, echelon, vermissian, stygianlibrary, egoengine). MIT licensed.

### Comparison Table

| Aspect                           | joshsymonds/nix-config                                                                                                                                                   | Our repo                                                                |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------- |
| Flake structure                  | Inline `flake.nix` with `mkNixosHost` / `mkHomeManagerModules` helpers, no flake-parts                                                                                   | `flake.nix` with flake-parts, `mkDarwin` / `mkHome` / `mkNixOS` helpers |
| Host definitions                 | Data-driven `nixosHostDefinitions` attrset in `flake.nix`, iterated by `lib.mapAttrs mkNixosHost`                                                                        | Inline per-host calls in flake outputs                                  |
| System-level modules             | `modules/` split into `darwin/`, `nix/`, `services/`, `performance/`                                                                                                     | `hosts/` with `shared.nix`, `mac.nix`, `hosts/nixos/`                   |
| Home-manager integration         | Integrated into system rebuilds via `mkHomeManagerModules`; also generates standalone `homeConfigurations` programmatically from the same host definitions               | Integrated into system rebuilds; no standalone `homeConfigurations`     |
| Home-manager layout              | `home-manager/` with per-tool subdirectories (atuin/, git/, tmux/, etc.) plus `common.nix`, `aarch64-darwin.nix`, `headless-x86_64-linux.nix`, `minimal.nix`             | `home/` with flat per-tool `.nix` files and `default.nix`               |
| Per-host HM overrides            | `home-manager/hosts/<hostname>.nix` files that import platform profiles and add host-specific overrides                                                                  | Host-specific config via `hostConfig` attrset in flake                  |
| Minimal/constrained host profile | `home-manager/minimal.nix` for resource-constrained devices (disables direnv, autosuggestion, syntax highlighting; uses nano instead of nvim)                            | No equivalent minimal profile                                           |
| Secrets management               | agenix with `secrets.nix` key map, `age.identityPaths`, and a `home.activation.deriveAgenixKey` script that auto-derives age keys from SSH ed25519 keys via `ssh-to-age` | sops-nix with standalone age key                                        |
| Overlays                         | Single `overlays/default.nix` exporting `default`, `darwin`, `additions`, `modifications`, `unstable-packages` (most empty); all applied via `modules/nix/defaults.nix`  | Minimal overlay usage                                                   |
| Custom packages                  | `pkgs/` with 12+ packages (caddy, claude-code-cli, coder-cli, gemini-cli, golangci-lint-bin, nuclei, mcp-atlassian, etc.)                                                | `pkgs/` with custom derivations                                         |
| Network topology                 | `lib/network.nix` -- plain attrset of subnets, hosts (IP, interface, subnet), and infra devices; imported by hosts                                                       | No centralized network topology                                         |
| Performance tuning               | `modules/performance/profiles.nix` -- enum-based profiles (dev/server/workstation/constrained/router/none) with sub-modules for memory, network, CPU (Intel/AMD)         | No equivalent performance module                                        |
| Operations                       | `Makefile` with lint, test, format-check, flake-check, update targets; `FILE=` argument for targeted checks                                                              | `Taskfile.yaml` with switch, check, format, update targets              |
| Nix linting                      | `statix` + `deadnix` + `alejandra` in Makefile and devShell                                                                                                              | `nixfmt` via treefmt                                                    |
| Nix daemon                       | Determinate Nix on both Darwin and NixOS; `nix.gc`/`nix.optimise` disabled on Darwin (managed by Determinate)                                                            | Lix with declarative nix-daemon settings                                |
| Installer system                 | Full `modules/installer.nix` -- NixOS module with `autoInstaller` options for auto-partitioning, LUKS, swap, ISO generation, and prebuilt closures                       | No installer infrastructure                                             |
| CI/CD                            | `.github/workflows/build-base.yml`                                                                                                                                       | No CI (Dagger-based e2e testing locally)                                |
| Dev environment                  | `devShells.default` with alejandra, nixpkgs-fmt, statix, deadnix, shellcheck, git; `.envrc` for direnv                                                                   | devShell via flake-parts                                                |
| Cachix                           | Personal `joshsymonds.cachix.org` + `nix-community`, `devenv`, `cuda-maintainers` binary caches                                                                          | No binary cache                                                         |
| Shell                            | zsh (not fish)                                                                                                                                                           | fish                                                                    |
| Editor                           | Helix (primary), Neovim available                                                                                                                                        | Neovim                                                                  |
| Theming                          | No unified theming system; per-app Catppuccin Mocha                                                                                                                      | stylix with base16 scheme                                               |
| Documentation                    | Comprehensive README with structure, quick start, customization, dev contexts docs; CLAUDE.md; modules/README.md                                                         | Minimal README                                                          |

### What Makes It Fork-Friendly

The repo's high fork count (124) correlates with several structural decisions that lower the barrier for newcomers:

1. **Data-driven host definitions.** The `nixosHostDefinitions` attrset in `flake.nix` separates host metadata (system, modules list, optional homeModule path) from the builder logic (`mkNixosHost`). A forker only needs to add a new entry to the attrset and create the corresponding `hosts/<name>/` directory -- no need to understand the builder internals.

2. **Layered HM profiles.** The `common.nix` -> `headless-x86_64-linux.nix` / `aarch64-darwin.nix` -> `hosts/<hostname>.nix` hierarchy means a forker can start with the common set and override or extend at any layer. The `minimal.nix` profile shows that even resource-constrained devices can participate with a stripped-down config.

3. **No framework dependencies.** Unlike repos using Snowfall Lib or flake-parts, this repo uses plain Nix with small helper functions. The entire `flake.nix` is self-contained and readable without knowing any framework APIs.

4. **Inline documentation.** The README explains how to add a new system and a new package step-by-step. The `modules/README.md` documents the module convention. CLAUDE.md provides testing procedures. This guidance is uncommon in personal dotfiles repos.

5. **Comprehensive devShell.** The default devShell includes all necessary linting and formatting tools, so a forker can run `nix develop` and immediately have a working development environment without installing anything manually.

6. **MIT license.** Explicit permissive licensing removes legal ambiguity about forking.

### Home-Manager Module Comparison

Modules in joshsymonds/nix-config vs. our repo:

| Module area      | joshsymonds                                         | Ours                               |
| ---------------- | --------------------------------------------------- | ---------------------------------- |
| Shell (zsh)      | `home-manager/zsh/`                                 | `home/shell.nix` (fish)            |
| Git              | `home-manager/git/`                                 | `home/git.nix`                     |
| Editor (Helix)   | `home-manager/helix/`                               | N/A (we use Neovim)                |
| Terminal (Kitty) | `home-manager/kitty/`                               | `home/kitty.nix`                   |
| Tmux             | `home-manager/tmux/`                                | N/A                                |
| Starship         | `home-manager/starship/`                            | `home/shell.nix` (starship config) |
| GPG              | `home-manager/gpg/`                                 | `home/gpg.nix`                     |
| SSH agent        | `home-manager/ssh-agent/`                           | System-level SSH agent             |
| SSH config       | `home-manager/ssh-config/`                          | `home/ssh.nix`                     |
| SSH hosts        | `home-manager/ssh-hosts/`                           | `home/ssh.nix` (combined)          |
| K9s              | `home-manager/k9s/`                                 | `home/k8s.nix`                     |
| Atuin            | `home-manager/atuin/`                               | `home/shell.nix` (atuin config)    |
| Aerospace        | `home-manager/aerospace/`                           | N/A (macOS window manager)         |
| Claude Code      | `home-manager/claude-code/` (with hooks)            | N/A                                |
| MCP servers      | `home-manager/mcp/`                                 | N/A                                |
| Go               | `home-manager/go/`                                  | `home/go.nix`                      |
| Media            | `home-manager/media/`                               | N/A                                |
| Security tools   | `home-manager/security-tools/`                      | N/A                                |
| Linkpearl        | `home-manager/linkpearl/` (clipboard sync)          | N/A                                |
| Devspaces        | `home-manager/devspaces-client/`, `devspaces-host/` | N/A                                |
| Egoengine        | `home-manager/egoengine/` (Docker dev env)          | N/A                                |
| Gemini CLI       | `home-manager/gemini-cli/` (disabled)               | N/A                                |
| Gmailctl         | `home-manager/gmailctl/`                            | N/A                                |

### Candidate Changes

1. **Data-driven host definitions attrset**
   - **Rationale:** The `nixosHostDefinitions` pattern in `flake.nix` consolidates all host metadata (system arch, module list, optional home-manager module) into a single attrset, then uses `lib.mapAttrs mkNixosHost` to generate all `nixosConfigurations`. This separates data from logic, making it easier to add hosts and reducing boilerplate. Our flake.nix inlines each host call, which is fine at our scale but becomes harder to scan as host count grows.
   - **Source:** `flake.nix` (`nixosHostDefinitions` attrset and `mkNixosHost` function)
   - **Impact:** Medium. Structural refactor of flake.nix; improves readability but requires reworking our existing helper functions.

2. **Centralized network topology file**
   - **Rationale:** `lib/network.nix` defines all subnets, host IPs, interfaces, gateways, and infrastructure devices (NAS shares, etc.) in a single plain attrset. Hosts import this file instead of hardcoding network values. This is a clean single-source-of-truth pattern for multi-host NixOS setups. Our NixOS hosts (OrbStack, TrueNAS) could benefit from this if we add more NixOS machines.
   - **Source:** `lib/network.nix`
   - **Impact:** Low. Only relevant if our NixOS host count grows; currently we have two NixOS hosts with simple networking.

3. **Performance tuning profiles module**
   - **Rationale:** `modules/performance/profiles.nix` exposes a single `performance.profile` enum option (dev/server/workstation/constrained/router/none) that sub-modules read to configure memory, network, and CPU tuning. This is a clean composition pattern: hosts set `performance.profile = "server";` and get appropriate sysctl, scheduler, and governor settings without managing the details. Could be useful for our TrueNAS host.
   - **Source:** `modules/performance/profiles.nix`, `modules/performance/memory.nix`, `modules/performance/network.nix`, `modules/performance/intel-cpu.nix`, `modules/performance/amd-cpu.nix`
   - **Impact:** Medium. Requires creating a new module hierarchy but adds concrete performance benefits for NixOS hosts.

4. **Minimal home-manager profile for constrained devices**
   - **Rationale:** `home-manager/minimal.nix` provides a stripped-down HM configuration for resource-constrained hosts: disables direnv, autosuggestions, syntax highlighting; uses nano instead of neovim; includes only essential packages. Per-host files like `hosts/bluedesert.nix` import this instead of the full common profile. Our repo assumes all hosts get the full config, which may be wasteful for lightweight NixOS hosts.
   - **Source:** `home-manager/minimal.nix`, `home-manager/hosts/bluedesert.nix`
   - **Impact:** Low. Only relevant if we add resource-constrained hosts; our current hosts can handle the full config.

5. **Auto-derive agenix/age keys from SSH keys via activation script**
   - **Rationale:** The `home.activation.deriveAgenixKey` script in `common.nix` automatically derives an age key from the user's ed25519 SSH key using `ssh-to-age`, storing it at `~/.config/agenix/keys.txt`. This means secrets decryption works on any machine with an authorized SSH key, without manually distributing a separate age key. Our sops-nix setup requires a standalone age key, which is an extra bootstrap step on new machines.
   - **Source:** `home-manager/common.nix` (`home.activation.deriveAgenixKey`)
   - **Impact:** Medium. Would simplify our secrets bootstrap story; requires evaluating whether sops-nix can use SSH-derived keys similarly.

6. **Statix and deadnix for Nix linting**
   - **Rationale:** The Makefile runs `statix check` (anti-pattern detection) and `deadnix` (dead code detection) alongside formatting. Our treefmt only runs nixfmt. Adding statix and deadnix to our treefmt or CI would catch issues like unused let bindings, legacy `with` usage, and redundant patterns. This is now a three-repo signal (mrjones2014, joshsymonds, khaneliman).
   - **Source:** `Makefile` (`lint-nix` target), `flake.nix` (devShell packages)
   - **Impact:** Low. Adding two tools to treefmt or a pre-commit check; no structural changes.

7. **Programmatic homeConfigurations generation from host definitions**
   - **Rationale:** The `homeConfigurations` output in `flake.nix` is generated programmatically from `nixosHostDefinitions` using `lib.genAttrs` and `builtins.attrNames`. This ensures every host with a `homeModule` automatically gets a corresponding standalone `homeConfigurations` entry (formatted as `user@hostname`), without manual duplication. Useful for running `home-manager switch` independently of system rebuilds.
   - **Source:** `flake.nix` (`homeConfigurations` section)
   - **Impact:** Low. We currently only use integrated HM; standalone configs would be an addition, not a replacement.

8. **Installer ISO module with auto-partitioning**
   - **Rationale:** `modules/installer.nix` is a full NixOS module (~450 lines) that generates bootable installer ISOs with auto-partitioning, optional LUKS encryption, optional swap, disk detection heuristics, repo cloning, and nixos-install automation. Hosts define an `installer.nix` that sets `autoInstaller` options and get a buildable ISO via `nix build .#<host>InstallerIso`. This is the most comprehensive installer pattern seen across all surveyed repos.
   - **Source:** `modules/installer.nix`, `hosts/stygianlibrary/installer.nix`, `hosts/ultraviolet/installer.nix`, `hosts/vermissian/installer.nix`
   - **Impact:** High. Significant module to build, but eliminates manual NixOS installation entirely. Only relevant if we need reproducible NixOS provisioning.

## dustinlyons/nixos-config

**Source:** [github.com/dustinlyons/nixos-config](https://github.com/dustinlyons/nixos-config)

A macOS + NixOS configuration with a strong emphasis on the `apps/` directory pattern, where operational scripts (apply, build-switch, install, clean, copy-keys, create-keys, rollback) are exposed as flake apps via `nix run`. Also notable for its flake templates, Dependabot-driven flake.lock updates, and CI that builds templates on every push. Uses agenix for secrets, disko for disk partitioning, nix-homebrew for declarative Homebrew, and Chaotic Nyx for bleeding-edge packages.

### Comparison Table

| Aspect                  | dustinlyons/nixos-config                                                                                                                                     | Our repo                                                                        |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------- |
| **Flake structure**     | Single `flake.nix` with inline helpers (`mkApp`, `mkLinuxApps`, `mkDarwinApps`); `flake-utils` for `forAllSystems`                                           | Single `flake.nix` with `mkDarwin`/`mkHome`/`mkNixOS` helpers                   |
| **Module organization** | `modules/{darwin,nixos,shared}/` three-way split; `shared/` contains cross-platform HM config, packages, files, fonts                                        | `home/` (flat HM modules), `hosts/` (system config), `configs/` (raw files)     |
| **Host definitions**    | `hosts/{darwin,nixos}/default.nix` with `genAttrs` over system strings; named hosts as additional attrset entries (`garfield`)                               | Per-host attrsets in `flake.nix` via `mkDarwin`/`mkHome`/`mkNixOS`              |
| **Apps directory**      | `apps/{aarch64-darwin,x86_64-darwin,x86_64-linux,aarch64-linux}/` with bash scripts exposed as flake apps via `mkApp`                                        | No flake apps; operations handled by `Taskfile.yaml`                            |
| **Operations runner**   | `nix run .#apply`, `nix run .#build-switch`, `nix run .#install`, etc.                                                                                       | `task switch`, `task update`, `task check`, `task format`                       |
| **Flake templates**     | `templates/{starter,starter-with-secrets}/` with `%TOKEN%` placeholders and sed substitution                                                                 | No templates                                                                    |
| **Secrets management**  | agenix with private `nix-secrets` git repo (SSH-accessed); age identity from `~/.ssh/id_ed25519`                                                             | sops-nix with standalone age key                                                |
| **Disk partitioning**   | disko (`disko.nixosModules.disko`) for declarative disk layout                                                                                               | Manual filesystem config                                                        |
| **Homebrew management** | nix-homebrew with pinned homebrew-core/cask/bundle as non-flake inputs; `mutableTaps = false`                                                                | nix-darwin `homebrew` module with direct tap management                         |
| **Overlays**            | `overlays/` directory with auto-discovery via `readDir` + filter; includes AppImage wrappers, version pins                                                   | No overlays directory                                                           |
| **Shell**               | zsh with powerlevel10k, inline config in `modules/shared/home-manager.nix`                                                                                   | fish with custom functions via HM                                               |
| **Editor**              | Emacs (custom overlay + daemon), Vim (configured via HM), Zed                                                                                                | Neovim via nixvim                                                               |
| **Terminal**            | Alacritty (configured via HM with platform-conditional font sizes)                                                                                           | Ghostty (via HM)                                                                |
| **Desktop environment** | KDE Plasma 6 with plasma-manager HM module; rofi launcher                                                                                                    | macOS native; no Linux DE config                                                |
| **CI/CD**               | GitHub Actions: `build-template.yml` (reusable workflow building templates), `lint.yml`, `update-flake-lock.yml` (weekly cron via DeterminateSystems action) | Dagger-based CI                                                                 |
| **Flake lock updates**  | Dependabot + `update-flake-lock.yml` GitHub Action (weekly cron, builds template first, then creates PR)                                                     | `task update` (manual)                                                          |
| **Theming**             | Manual color scheme in Alacritty config; Breeze Dark for KDE/rofi                                                                                            | Stylix with base16 scheme                                                       |
| **Nix implementation**  | `nix.enable = false` on Darwin (external Nix management); standard Nix on NixOS                                                                              | Lix on macOS; standard Nix on NixOS                                             |
| **Shared config**       | `modules/shared/` directory with `default.nix`, `home-manager.nix`, `packages.nix`, `files.nix`, `fonts.nix`, `emacs.nix`                                    | `hosts/shared.nix` for system config; `home/` for HM (no shared/platform split) |
| **Systemd services**    | `modules/nixos/systemd.nix` with dev environment auto-start (tmux sessions), automated content generation timers                                             | No custom systemd services                                                      |
| **Custom packages**     | Overlays for AppImage wrapping (Cider, Obsidian, TablePlus, WoWUp), version pins (PHPStorm, Playwright)                                                      | `pkgs/` directory with custom derivations                                       |

### Home-Manager Module Comparison

| Module/Tool  | dustinlyons                                            | Ours                      |
| ------------ | ------------------------------------------------------ | ------------------------- |
| direnv       | Yes (zsh integration)                                  | Yes                       |
| zsh          | Yes (powerlevel10k, extensive inline config)           | No (we use fish)          |
| fish         | No                                                     | Yes                       |
| git          | Yes (GPG signing, LFS)                                 | Yes                       |
| vim          | Yes (airline, tmux-navigator, extensive vimrc)         | No (we use neovim/nixvim) |
| alacritty    | Yes (platform-conditional settings)                    | No (we use ghostty)       |
| ssh          | Yes (external config includes, matchBlocks)            | Yes                       |
| tmux         | Yes (resurrect, continuum, power-theme, vim-navigator) | Yes                       |
| gpg          | Yes (with systemd key import service)                  | Yes                       |
| rofi         | Yes (Breeze Dark themed)                               | No                        |
| plasma (KDE) | Yes (via plasma-manager)                               | No                        |
| emacs        | Yes (custom overlay, daemon service)                   | No                        |
| starship     | No                                                     | Yes                       |
| k8s tools    | No (only kubectl)                                      | Yes                       |
| bat          | Yes (in packages)                                      | Yes                       |

### Candidate Changes

1. **Flake apps for operational scripts**
   - **Rationale:** The `apps/` directory pattern wraps bash scripts as flake apps, making operations self-documenting and runnable via `nix run .#apply`, `nix run .#build-switch`, etc. Each script is a standalone bash file in `apps/<system>/`, and `mkApp` in `flake.nix` wraps them with `writeScriptBin` to inject git into PATH. This is a fundamentally different approach from our Taskfile: flake apps are Nix-native (no external task runner dependency), system-aware (different scripts per platform), and discoverable via `nix flake show`. The trade-off is more boilerplate (one script file per operation per platform, plus `mkLinuxApps`/`mkDarwinApps` registration in flake.nix) compared to our single `Taskfile.yaml`.
   - **Source:** `apps/`, `flake.nix` (`mkApp`, `mkLinuxApps`, `mkDarwinApps`)
   - **Impact:** Medium. Our Taskfile approach works well and `nh` handles the core switch operation. Flake apps would add Nix-native discoverability but increase file count.

2. **Flake templates for onboarding**
   - **Rationale:** The `templates/` directory provides two complete starter configurations (`starter` and `starter-with-secrets`) that users can initialize via `nix flake init -t github:dustinlyons/nixos-config#starter`. Each template contains a full, working flake with `%USER%`, `%EMAIL%`, `%NAME%` placeholders that the `apply` script substitutes with user input. The CI builds these templates on every push to prevent template rot. This is a fork-friendliness pattern (also seen in joshsymonds/nix-config US-010) but implemented via Nix's native template mechanism rather than just making the repo itself forkable.
   - **Source:** `templates/starter/`, `templates/starter-with-secrets/`, `.github/workflows/build-template.yml`
   - **Impact:** Low. Only relevant if we wanted to make our config usable as a template for others. Our repo is personal-use, not a starter kit.

3. **Overlay auto-discovery from directory**
   - **Rationale:** `modules/shared/default.nix` reads the `overlays/` directory with `builtins.readDir`, filters for `.nix` files, and imports them all as overlays. It also supports per-host exclusions via an `excludeForHost` attrset. This is a practical approach to overlay management, though it conflicts with our CLAUDE.md preference for explicit imports. The per-host exclusion pattern is interesting for cases where an overlay breaks on certain platforms.
   - **Source:** `modules/shared/default.nix`, `overlays/`
   - **Impact:** Low. We do not currently use overlays. If we adopted overlays, the auto-discovery pattern would reduce boilerplate but conflict with our explicit-imports code style.

4. **nix-homebrew with pinned non-flake inputs**
   - **Rationale:** Uses nix-homebrew (zhaofengli-wip/nix-homebrew) with `mutableTaps = false` and pinned `homebrew-core`, `homebrew-cask`, `homebrew-bundle` as non-flake inputs. This is now a four-repo signal (chenglab, ahmedelgabri, megalithic, dustinlyons) for declarative Homebrew management. The `autoMigrate = true` option handles transitioning from an existing Homebrew installation. Compared to our nix-darwin `homebrew` module approach, this pins tap versions in the flake lock, giving reproducible Homebrew builds.
   - **Source:** `flake.nix` (inputs + nix-homebrew module config)
   - **Impact:** Medium. Stronger reproducibility guarantee for Homebrew than our current approach. Four repos now demonstrate this pattern.

5. **CI template validation workflow**
   - **Rationale:** The `build-template.yml` reusable workflow initializes templates in CI, applies token substitution with test values, and builds the NixOS configuration. The `update-flake-lock.yml` workflow runs weekly, builds the template first to verify it works, then creates a PR to update `flake.lock` using DeterminateSystems/update-flake-lock. This two-stage approach (validate, then update) prevents broken lock file updates from merging. The disk space cleanup step in CI (removing Azure CLI, Chrome, .NET, etc.) is a practical pattern for GitHub Actions runners that need to build Nix derivations.
   - **Source:** `.github/workflows/build-template.yml`, `.github/workflows/update-flake-lock.yml`
   - **Impact:** Low. Our Dagger-based CI serves a similar purpose. The weekly flake lock update PR pattern is worth noting but we handle updates manually via `task update`.

6. **Declarative dock management module**
   - **Rationale:** `modules/darwin/dock/default.nix` is a custom NixOS-style module that declaratively manages the macOS Dock. It defines `local.dock.entries` as a typed option (list of `{path, section, options}` submodules), then uses `dockutil` in a `system.activationScripts.postActivation` script to diff the current dock state against the desired state, resetting only when they differ. This is a clean pattern for managing macOS UI state declaratively.
   - **Source:** `modules/darwin/dock/default.nix`, `modules/darwin/home-manager.nix` (dock entries)
   - **Impact:** Low. Nice-to-have for macOS dock consistency across rebuilds. Minimal implementation effort (single module file + dockutil dependency).

7. **Chaotic Nyx for bleeding-edge packages**
   - **Rationale:** The `chaotic` flake input (chaotic-cx/nyx) provides pre-built bleeding-edge packages and kernel modules, including `mesa-git` for latest GPU support. It is imported as a NixOS module (`chaotic.nixosModules.default`) and can be selectively enabled per-host. This is the first surveyed repo using Chaotic Nyx, which is an alternative to maintaining custom overlays for packages that need to be newer than nixpkgs-unstable.
   - **Source:** `flake.nix` (chaotic input), `hosts/nixos/default.nix` (`chaotic.mesa-git.enable`)
   - **Impact:** Low. Only relevant for NixOS hosts that need bleeding-edge GPU drivers or other packages. Not applicable to our macOS-primary workflow.

8. **Shared modules with platform-conditional HM config**
   - **Rationale:** The `modules/shared/home-manager.nix` file contains all cross-platform home-manager program configs (zsh, git, vim, alacritty, ssh, tmux) in a single file, using `lib.mkIf pkgs.stdenv.hostPlatform.isLinux` / `isDarwin` for platform-specific values (font sizes, paths, aliases). Platform-specific HM modules (`modules/darwin/home-manager.nix`, `modules/nixos/home-manager.nix`) then merge with `shared-programs` via `programs = shared-programs // { ... }`. This is a clean pattern for avoiding HM config duplication across platforms, though it puts everything in one large file rather than our per-domain split.
   - **Source:** `modules/shared/home-manager.nix`, `modules/{darwin,nixos}/home-manager.nix`
   - **Impact:** Medium. Our flat `home/*.nix` modules already avoid duplication since they are imported by all hosts. The platform-conditional pattern (`lib.mkIf stdenv.isDarwin`) within shared files is something we already use where needed.

## MatthiasBenaets/nix-config

**Source:** [github.com/MatthiasBenaets/nix-config](https://github.com/MatthiasBenaets/nix-config)

A modular, multi-platform Nix flake managing NixOS (3 hosts), nix-darwin (3 hosts), and standalone home-manager (1 host) using flake-parts with the "dendritic pattern" via import-tree. The author has a YouTube walkthrough explaining the setup. The repo is notable for putting everything -- hosts, programs, hardware, services, theming -- under a single `modules/` directory, with `import-tree` auto-loading all files into the flake-parts module system. A `shells/` directory provides dev shell definitions.

### Comparison Table

| Aspect                       | MatthiasBenaets/nix-config                                                                                                                                                                                                                                                                                                                                                 | Our dotfiles                                                                                                                                                                                                                            |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Flake structure**          | Minimal `flake.nix` using flake-parts + `import-tree` to auto-load everything from `modules/`. Only `perSystem` (for pkgs/devShells) is defined inline. Host definitions, module registrations, and all config live inside `modules/`.                                                                                                                                     | `flake.nix` using flake-parts with three inline helper functions (`mkDarwin`, `mkHome`, `mkNixOS`). Host declarations and module imports are explicit in `flake.nix`.                                                                   |
| **Top-level layout**         | Two directories: `modules/` (everything) and `shells/` (dev shells). Extremely compact top-level.                                                                                                                                                                                                                                                                          | Six directories: `hosts/`, `home/`, `configs/`, `pkgs/`, `toolchains/`, plus `Taskfile.yaml`. More conventional separation of concerns.                                                                                                 |
| **Module organization**      | `modules/` contains subdirectories by domain: `general/`, `hosts/`, `hardware/`, `gui/`, `programs/`, `services/`, `editors/`, `theme/`, `virtualisation/`, plus loose files (`console.nix`, `dconf.nix`, `i18n.nix`, `mime.nix`, `polkit.nix`, `time.nix`). Modules are organized by feature, not by target OS.                                                           | `hosts/` for system config (split by platform), `home/` for HM modules (flat, one file per domain). No `modules/` directory; system config is in host files.                                                                            |
| **Module registration**      | Modules register themselves into `flake.modules.{nixos,darwin,homeManager}.<name>` attrsets. Each `.nix` file exports e.g. `flake.modules.nixos.audio = { ... }`. Host definitions then compose modules by referencing `config.flake.modules.nixos.audio`.                                                                                                                 | No module registry. Modules are standard NixOS/HM modules imported directly via file paths in `flake.nix` helper functions.                                                                                                             |
| **Auto-loading**             | `import-tree` (vic/import-tree) auto-discovers all `.nix` files under `modules/` and loads them as flake-parts modules. No explicit import lists anywhere.                                                                                                                                                                                                                 | Explicit import lists in `flake.nix`. CLAUDE.md explicitly prefers "explicit configuration over automatic file discovery."                                                                                                              |
| **Host definition**          | Each host is a directory under `modules/hosts/{nixos,darwin}/<hostname>/default.nix`. Defines a local `host` attrset (name, user, state, system, monitors), then creates `flake.nixosConfigurations.<name>` by composing named modules from `config.flake.modules.nixos.*`. Also creates a host-specific module (`flake.modules.nixos.<hostname>`) for per-host overrides. | Hosts declared inline in `flake.nix` via `mkDarwin`/`mkNixOS`/`mkHome` helpers, each taking a `hostConfig` attrset and explicit module import lists.                                                                                    |
| **Host options**             | Typed `host` NixOS options (`modules/hosts/options.nix`): `host.name`, `host.user.name`, `host.state.version`, `host.system`, `host.monitors` (list of submodules with display geometry), `host.shell`, `host.isNixOS`/`isDarwin`/`isHomeManager` booleans. Platform booleans auto-set via `mkDefault`.                                                                    | `dotfiles.*` NixOS options in `hosts/options.nix` and `home/options.nix`: `dotfiles.system.hostname`, `dotfiles.system.username`, `dotfiles.git.*`, `dotfiles.shell.*`, etc. Similar typed approach but different namespace and fields. |
| **Home-manager integration** | HM is a module inside system rebuilds (NixOS and Darwin). Shared base config in `modules/hosts/home-manager.nix`. Per-host HM imports composed via `config.flake.modules.homeManager.*` references in host `default.nix`. Standalone HM also supported via `modules/hosts/nix/`.                                                                                           | HM is a module inside system rebuilds (same pattern). Per-host HM imports are explicit file paths in `flake.nix`.                                                                                                                       |
| **Cross-platform modules**   | Modules that apply to multiple platforms define multiple `flake.modules.*` keys in the same file. e.g. `direnv.nix` sets both `flake.modules.nixos.base` and `flake.modules.darwin.base`. The `base` module is always included.                                                                                                                                            | Cross-platform config lives in `hosts/shared.nix` (system-level) or in `home/*.nix` files (HM-level) with `mkIf` guards for platform-specific behavior.                                                                                 |
| **Module composition**       | Hosts compose modules by name: `modules = with config.flake.modules.nixos; [ base audio bluetooth hyprland nixvim ... ]`. The `base` module is always first and provides core config (users, nix settings, HM integration, host options, nixpkgs, state version).                                                                                                          | Hosts compose modules by file import: `modules = [ ./hosts/shared.nix ./hosts/mac.nix ... ]`. No named module registry.                                                                                                                 |
| **Homebrew**                 | nix-darwin `homebrew` module (same as ours). Per-host Homebrew lists as separate named modules (`homebrewIntel`, `homebrewM1`, `homebrewWork`), each adding to the `homebrew.casks`/`brews`/`masApps` lists.                                                                                                                                                               | nix-darwin `homebrew` module. Homebrew lists in `hosts/mac.nix` with `hostConfig` overrides per host.                                                                                                                                   |
| **Theming**                  | Stylix (`nix-community/stylix`), configured in `modules/theme/stylix.nix` with seti base16 scheme. Separate `modules/theme/font.nix` for font packages. NixOS only (no Darwin stylix).                                                                                                                                                                                     | Stylix with OneDark base16 scheme. Configured in `hosts/stylix.nix` (shared) + `home/stylix.nix` (HM). Applied to both Darwin and NixOS.                                                                                                |
| **Secrets**                  | None. No sops-nix, agenix, or any secrets management.                                                                                                                                                                                                                                                                                                                      | sops-nix with age encryption for API keys and credentials.                                                                                                                                                                              |
| **Overlays**                 | NUR overlay + stable nixpkgs overlay (`pkgs.stable`). Defined inline in `flake.nix` `perSystem`.                                                                                                                                                                                                                                                                           | Three external overlays + one local overlay, defined as `sharedOverlays` in `flake.nix`.                                                                                                                                                |
| **Stable nixpkgs**           | Dual nixpkgs: unstable (primary) + stable (`nixpkgs-stable` as `pkgs.stable` overlay).                                                                                                                                                                                                                                                                                     | Single nixpkgs (unstable only).                                                                                                                                                                                                         |
| **Operations**               | No task runner. Manual `nixos-rebuild switch --flake .#<host>` / `darwin-rebuild switch --flake .#<host>` commands documented in README.                                                                                                                                                                                                                                   | Taskfile.yaml with `task switch` (auto-detects platform, uses `nh`).                                                                                                                                                                    |
| **CI/CD**                    | None.                                                                                                                                                                                                                                                                                                                                                                      | Dagger-based checks via `task check`.                                                                                                                                                                                                   |
| **Dev shells**               | Separate `shells/` directory with per-language shells (default, neovim, python, nodejs). Consumed via `perSystem.devShells` in `flake.nix`.                                                                                                                                                                                                                                | No dev shells in the dotfiles repo. Dev shells are per-project.                                                                                                                                                                         |
| **Custom packages**          | Neovim via nixvim as a flake package (`nix run .#neovim`). Defined in `modules/editors/nixvim/`.                                                                                                                                                                                                                                                                           | Custom packages in `pkgs/` directory.                                                                                                                                                                                                   |
| **Flake inputs**             | 14 inputs: nixpkgs, nixpkgs-stable, flake-parts, import-tree, home-manager, darwin, NUR, nixGL, nixvim, stylix, nix-flatpak, hyprland, noctalia, mac-app-util.                                                                                                                                                                                                             | ~15 inputs: nixpkgs, home-manager, nix-darwin, flake-parts, treefmt-nix, stylix, sops-nix, nh, lix-module, etc.                                                                                                                         |
| **mac-app-util**             | Uses `hraban/mac-app-util` for both Darwin system module and HM shared module. Ensures Nix-installed apps appear in macOS Spotlight/Launchpad.                                                                                                                                                                                                                             | Not used.                                                                                                                                                                                                                               |
| **Flatpak**                  | `nix-flatpak` integration for declarative Flatpak management on NixOS.                                                                                                                                                                                                                                                                                                     | Not used. No Flatpak support.                                                                                                                                                                                                           |
| **nixGL**                    | Used for standalone HM on non-NixOS Linux (`modules/hosts/nix/default.nix`) to fix OpenGL issues with Nix-installed GUI apps.                                                                                                                                                                                                                                              | Not used. Our Linux host is NixOS-based (OrbStack).                                                                                                                                                                                     |

### Home-Manager Modules Comparison

Modules in MatthiasBenaets' config that we lack or configure differently:

| Their module                         | Our equivalent                      | Notes                                                                                                   |
| ------------------------------------ | ----------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `modules/programs/zsh/` (zsh + p10k) | `home/fish.nix`                     | They use zsh with powerlevel10k; we use fish with Tide.                                                 |
| `modules/programs/kitty.nix`         | `home/ghostty.nix`                  | Different terminal emulator. Both use HM `programs.*` integration.                                      |
| `modules/programs/aerospace.nix`     | (none)                              | macOS tiling WM with detailed keybinding config. We use native macOS tiling via `system.defaults`.      |
| `modules/programs/hyprspace.nix`     | (none)                              | Custom macOS window management tool (Hyprland-like for Darwin).                                         |
| `modules/gui/hyprland.nix`           | (none)                              | Full Hyprland WM config with keybindings, animations, window rules. NixOS-specific.                     |
| `modules/gui/niri.nix`               | (none)                              | Alternative scrollable tiling WM. Large config (34KB). NixOS-specific.                                  |
| `modules/gui/gnome.nix`              | (none)                              | GNOME desktop config with dconf settings. NixOS-specific.                                               |
| `modules/programs/noctalia.nix`      | (none)                              | Custom Wayland shell/panel. NixOS-specific.                                                             |
| `modules/editors/nixvim/`            | `home/vim.nix`                      | They use nixvim (full Nix-native Neovim config); we use programs.neovim with raw Lua config.            |
| `modules/programs/flatpak.nix`       | (none)                              | Declarative Flatpak package management.                                                                 |
| `modules/programs/games.nix`         | (none)                              | Steam, Lutris, game-related packages and config.                                                        |
| `modules/mime.nix`                   | (none)                              | Full MIME type associations + custom desktop entries (gmail, nvim-kitty).                               |
| `modules/programs/obs.nix`           | (none)                              | OBS Studio with plugins via HM.                                                                         |
| `modules/programs/accounts.nix`      | (none)                              | Email account config (calendar, contacts).                                                              |
| `modules/hardware/dslr.nix`          | (none)                              | DSLR camera support (gphoto2, v4l2loopback).                                                            |
| `modules/programs/skhd.nix`          | (none)                              | macOS hotkey daemon config.                                                                             |
| `modules/programs/yabai.nix`         | (none)                              | macOS tiling WM (alternative to aerospace).                                                             |
| `modules/programs/direnv.nix`        | `home/default.nix` (direnv section) | Same tool, different location. They define it cross-platform in one file via dual `flake.modules` keys. |
| `modules/programs/git.nix`           | `home/git.nix`                      | Both use HM git integration.                                                                            |

### Candidate Changes

1. **Named module registry via flake-parts `flake.modules.*`**
   - **Rationale:** The dendritic pattern's `flake.modules.{nixos,darwin,homeManager}.<name>` registry enables host definitions to compose modules by name (`with config.flake.modules.nixos; [ base audio bluetooth ... ]`) rather than by file path. This gives named, self-documenting composition and allows a single `.nix` file to register modules for multiple platforms simultaneously (e.g. `direnv.nix` sets both `flake.modules.nixos.base` and `flake.modules.darwin.base`). However, it requires the `flake-parts.flakeModules.modules` import and custom option declarations for `darwinConfigurations` and `homeConfigurations`. The trade-off is indirection: module names are strings, not file paths, making it harder to "go to definition."
   - **Source:** `modules/general/flake-parts.nix`, `modules/hosts/options.nix`, `modules/hosts/nixos/beelink/default.nix`
   - **Impact:** High. Would fundamentally change how we compose host configurations. Enables per-file cross-platform module definitions but adds a registration layer.

2. **import-tree for auto-loading modules**
   - **Rationale:** `import-tree` (vic/import-tree) auto-discovers all `.nix` files under a directory tree and loads them as flake-parts modules. This eliminates all explicit import lists -- adding a new module is just creating a file. Combined with the `flake.modules.*` registry, it means new features require zero changes to `flake.nix`. However, this directly conflicts with our CLAUDE.md preference for "explicit configuration over automatic file discovery." It makes the dependency graph implicit and harder to trace.
   - **Source:** `flake.nix` (`imports = [ (inputs.import-tree ./modules) ]`)
   - **Impact:** High. Maximum convenience (zero-touch module addition) at the cost of explicitness. Only worth considering if module count grows substantially or we adopt the dendritic pattern.

3. **Stable nixpkgs overlay (`pkgs.stable`)**
   - **Rationale:** Creating a `stable` overlay attribute (`final: prev: { stable = import inputs.nixpkgs-stable { ... }; }`) makes stable packages available as `pkgs.stable.<name>` anywhere. This is a recurring pattern across surveyed repos (wimpysworld, MatthiasBenaets). It allows pinning individual packages to stable when unstable breaks without maintaining a separate pkgs instance or specialArgs.
   - **Source:** `flake.nix` (perSystem overlays)
   - **Impact:** Low. Simple overlay addition. Useful as a safety valve for unstable-tracking breakages.

4. **mac-app-util for Spotlight/Launchpad integration**
   - **Rationale:** `hraban/mac-app-util` ensures Nix-installed applications appear in macOS Spotlight and Launchpad by creating proper `.app` wrappers. Without it, Nix-installed GUI apps are only accessible via terminal. MatthiasBenaets imports it as both a Darwin system module and an HM shared module.
   - **Source:** `modules/hosts/darwin/mac-app-util.nix`
   - **Impact:** Medium. Directly improves macOS usability for any Nix-installed GUI applications. Requires adding one flake input.

5. **Monitor/display geometry in host options**
   - **Rationale:** The `host.monitors` option (list of submodules with name, resolution, refresh rate, position) stores display configuration as typed Nix data. GUI modules (Hyprland, niri) reference `config.host.monitors` to generate their monitor configs. This is cleaner than hardcoding display geometry in each WM config. Only relevant if we add NixOS desktop hosts with graphical environments.
   - **Source:** `modules/hosts/options.nix` (monitors option), `modules/gui/hyprland.nix` (consumption)
   - **Impact:** Low. Not applicable unless we add NixOS desktop hosts. Good pattern to know about.

6. **Per-host Homebrew modules instead of per-host overrides**
   - **Rationale:** Rather than a single Homebrew config with per-host overrides (our approach), MatthiasBenaets defines separate named modules (`homebrewIntel`, `homebrewM1`, `homebrewWork`), each adding their own `homebrew.casks`/`brews` lists. Hosts include exactly the Homebrew module they need. NixOS module system merges the lists automatically. This is cleaner than our `hostConfig` override pattern for Homebrew because each host's app list is self-contained and the base Homebrew module only contains truly shared apps.
   - **Source:** `modules/programs/homebrew.nix`
   - **Impact:** Low. Our current approach works; this is a different organizational preference.

7. **Neovim as standalone flake package via nixvim**
   - **Rationale:** By configuring Neovim entirely through nixvim and exposing it as a flake package, the editor becomes runnable with `nix run .#neovim` from anywhere, usable in dev shells, and independently testable. This decouples editor config from the system rebuild cycle. Our current approach (programs.neovim with raw Lua config files) ties editor changes to `task switch`.
   - **Source:** `modules/editors/nixvim/`, `flake.nix` (packages output)
   - **Impact:** Medium. Would require migrating our Lua-based Neovim config to nixvim, which is a significant effort. The standalone package benefit is real but may not justify the migration cost.

8. **Dev shells in dotfiles repo**
   - **Rationale:** Having per-language dev shells (python, nodejs, etc.) in the dotfiles repo makes them available system-wide via `nix develop ~/.setup#python`. These serve as quick-start environments without per-project `flake.nix` files. Our approach is per-project dev shells, which is more precise but requires setup for every new project.
   - **Source:** `shells/` directory
   - **Impact:** Low. Different workflow preference. Per-project dev shells are generally more appropriate for reproducibility.

## kclejeune/system

**Source:** [github.com/kclejeune/system](https://github.com/kclejeune/system)

### Comparison Table

| Aspect                | kclejeune/system                                                                                                                                       | Our approach                                                              |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------- |
| Flake framework       | flake-parts with `mkDarwinConfig`/`mkNixosConfig`/`mkHomeConfig` helpers defined outside the `flake-parts.lib.mkFlake` body                            | flake-parts with `mkDarwin`/`mkHome`/`mkNixOS` helpers inside `flake.nix` |
| Module layout         | `modules/{darwin,nixos,home-manager}/` with a shared `modules/common.nix`                                                                              | `hosts/` (system-level) + `home/` (flat HM modules)                       |
| Host composition      | `profiles/` directory (personal, work) imported as `extraModules` by each config                                                                       | `hostConfig` attrsets passed to helper functions                          |
| Config naming         | `"username@system"` keys (e.g. `kclejeune@aarch64-darwin`) with `lib.map` over `darwinSystems`                                                         | Named host keys (e.g. `jacobs-macbook`)                                   |
| Multi-arch generation | `lib.map` over a computed `darwinSystems`/`defaultSystems` list to auto-generate configs for all supported architectures                               | One config per named host with explicit system                            |
| User abstraction      | `primaryUser.nix` module with `user` and `hm` option aliases via `mkAliasDefinitions`                                                                  | `dotfiles.*` NixOS options module                                         |
| Nix implementation    | Determinate Nix (`determinateNix.enable = true`, `nix.enable = false`)                                                                                 | Lix (Nix implementation fork)                                             |
| Nix daemon config     | `determinateNix.customSettings` + `determinateNixd` (garbage collector strategy, builder state, netrc)                                                 | Declarative `nix.settings` via Lix                                        |
| Secrets management    | None (no sops-nix, agenix, or similar)                                                                                                                 | sops-nix                                                                  |
| Overlays              | Single `self.overlays.default` via flake-parts `easyOverlay`, includes `pkgs.stable` from pinned stable nixpkgs                                        | Minimal overlays                                                          |
| Nixpkgs channel       | Tracks unstable with `stable` and `legacy` (25.05) as overlay attributes                                                                               | Tracks unstable                                                           |
| Homebrew management   | nix-darwin `homebrew` module (no nix-homebrew)                                                                                                         | nix-darwin `homebrew` module                                              |
| Shell                 | zsh with oh-my-zsh (30+ plugins), bash as secondary                                                                                                    | fish as primary                                                           |
| Custom packages       | `pkgs/` directory: `sysdo` (Python CLI), `cb` (clipboard), `fnox` (Firefox profiles)                                                                   | `pkgs/` directory with custom derivations                                 |
| Operations CLI        | `sysdo` -- custom Python CLI built with typer, packaged as a Nix derivation                                                                            | `task` (Taskfile.yaml) with `nh`                                          |
| Formatter/linter      | treefmt-nix with deadnix, nixfmt, jsonfmt, mdformat, stylua, ruff, shellcheck, shfmt                                                                   | treefmt with nixfmt, prettier                                             |
| Pre-commit hooks      | git-hooks.nix (cachix/git-hooks.nix) with treefmt hook                                                                                                 | None                                                                      |
| CI: macOS builds      | Cirrus CI with macOS runner (Sequoia image), Determinate Nix installer, cachix push                                                                    | None                                                                      |
| CI: Linux builds      | Cirrus CI with NixOS container (x86_64 + aarch64), cachix push                                                                                         | Dagger-based e2e testing                                                  |
| CI: Garnix            | garnix.yaml for x86_64-linux builds, all config types included                                                                                         | None                                                                      |
| CI: flake updates     | GitHub Actions with `DeterminateSystems/update-flake-lock` (daily cron) + Dependabot                                                                   | Manual `task update`                                                      |
| Flake checks          | Generates checks from all configurations by extracting `activationPackage` (HM) and `system.build.toplevel` (darwin/nixos), filtered by current system | `task check` (nix flake check)                                            |
| Dev shell             | Comprehensive: includes fd, ripgrep, uv, nh, pre-commit hooks, treefmt programs, custom packages                                                       | Basic nix develop                                                         |
| Touch ID sudo         | `security.pam.services.sudo_local.touchIdAuth = true`                                                                                                  | PAM Touch ID via nix-darwin                                               |
| Neovim                | In-repo Lua config via `xdg.configFile`, not nixvim                                                                                                    | In-repo config via `configs/`                                             |
| Raw config files      | `modules/home-manager/dotfiles/` directory with subdirs for aerospace, ghostty, kitty, hammerspoon, etc.                                               | `configs/` directory symlinked via `xdg.configFile`                       |
| Syncthing             | Custom Darwin launchd module (`modules/darwin/syncthing.nix`) + NixOS native `services.syncthing`                                                      | Not configured                                                            |
| macOS preferences     | Dedicated `preferences.nix` with dock, finder, trackpad, keyboard settings                                                                             | Inline in `hosts/mac.nix`                                                 |
| Window manager        | AeroSpace (via Homebrew cask)                                                                                                                          | yabai/skhd or similar                                                     |
| Font management       | JetBrains Mono via `fonts.packages` (system) + `fonts.fontconfig.enable` (HM)                                                                          | Font packages in home.packages                                            |
| nix-index             | nix-index-database (Mic92/nix-index-database) with comma integration                                                                                   | Not configured                                                            |

### Profiles-Based Composition

kclejeune/system's defining architectural pattern is the `profiles/` directory for machine composition. Instead of per-host configuration files, profiles represent **roles** that cut across platform boundaries:

- `profiles/personal/` -- sets `user.name = "kclejeune"`, imports personal HM config (git email, etc.)
- `profiles/work/` -- sets `user.name = "klejeune"`, imports work HM config (AWS tools, teleport, etc.), disables CA cert installation

Each profile has two layers:

1. `default.nix` -- system-level settings (user identity, platform-specific options)
2. `home-manager/default.nix` -- user-level settings (packages, git config)

Hosts compose by selecting a profile as an `extraModule`:

```nix
"kclejeune@aarch64-darwin" = mkDarwinConfig {
  extraModules = [ ./profiles/personal ./modules/darwin/apps.nix ];
};
"klejeune@aarch64-darwin" = mkDarwinConfig {
  extraModules = [ ./profiles/work ];
};
```

This is distinct from our `hostConfig` approach. Our pattern embeds all host-specific values in the flake.nix call site; kclejeune's pattern externalizes them into importable directories. The trade-off: profiles are more reusable across platforms (the same `profiles/personal` works for darwin, nixos, and standalone HM), but add a layer of indirection.

The `primaryUser.nix` module is the glue that makes profiles work. It defines `user` and `hm` as top-level options with `mkAliasDefinitions`, so `user.name = "kclejeune"` expands to `users.users.kclejeune` and `hm.imports = [...]` expands to `home-manager.users.kclejeune.imports = [...]`. This is functionally similar to ahmedelgabri's `mkAliasDefinitions` pattern (US-007) but applied to the user account rather than just xdg paths.

### Multi-Architecture Auto-Generation

A notable pattern is the automatic generation of configs for all supported architectures:

```nix
darwinSystems = lib.intersectLists defaultSystems lib.platforms.darwin;
# generates kclejeune@x86_64-darwin AND kclejeune@aarch64-darwin
lib.map (system: { "kclejeune@${system}" = mkDarwinConfig { inherit system; ... }; }) darwinSystems
```

This eliminates per-architecture duplication. Our approach defines each host once with a fixed system, which is simpler but means adding a new architecture requires a new host entry.

### Home-Manager Module Differences

Modules in kclejeune/system not present in ours:

| Module                | Description                                  |
| --------------------- | -------------------------------------------- |
| 1password.nix         | SSH agent integration with 1Password         |
| nushell.nix           | Nushell shell configuration                  |
| gnome.nix             | GNOME desktop dconf settings                 |
| yazi/                 | Yazi file manager config                     |
| tldr.nix              | tealdeer/tldr pages                          |
| dotfiles/aerospace/   | AeroSpace window manager config              |
| dotfiles/hammerspoon/ | Hammerspoon automation                       |
| dotfiles/raycast/     | Raycast launcher scripts                     |
| nixpkgs.nix (HM)      | Per-user nixpkgs config + nix-index registry |

Tools in their HM packages not in ours: attic (binary cache), ast-grep, bento, bfs, cirrus-cli, d2 (diagrams), dix (nix diff), doxx, flamegraph, flamelens, flawz (CVE browser), flyctl, fnox, fx (JSON viewer), git-absorb, git-who, git-my, grype, helm-docs, hyperfine, jnv (JSON navigator), lazyworktree, mise, mmv, nix-inspect, nix-tree, nixd, nixpacks, ouch, oxfmt, oxlint, prek (pre-commit), process-compose, rclone, restic, rustscan, sig, skopeo, ssh-to-age, sysdo, trivy, usage, yq-go.

### CI Patterns

kclejeune uses a dual CI strategy:

1. **Cirrus CI** (`.cirrus.yml`) -- The primary CI. Runs macOS builds on Apple Silicon (Sequoia runner image) and Linux builds on both x86_64 and aarch64 containers. Uses `cachix watch-exec` to push build artifacts to a binary cache. This is notable because GitHub Actions cannot run macOS ARM builds natively; Cirrus CI provides free macOS ARM runners for open-source projects.

2. **Garnix** (`garnix.yaml`) -- A Nix-native CI service that builds flake outputs declaratively. The YAML config specifies which outputs to build (`homeConfigurations.*`, `darwinConfigurations.*`, etc.). Garnix handles caching automatically. This is the simplest CI setup seen: a 7-line YAML file replaces a full CI pipeline definition.

3. **GitHub Actions** (`.github/workflows/update.yml`) -- Only used for automated flake.lock updates via `DeterminateSystems/update-flake-lock`, running on a daily cron schedule. Combined with Dependabot for dependency PRs.

The three-service split is pragmatic: Cirrus CI for its macOS ARM capability, Garnix for zero-config Nix builds, and GitHub Actions for the update-flake-lock action (which needs PR creation permissions).

### Candidate Changes

1. **Profiles-based composition for personal vs. work differentiation**
   - **Rationale:** The `profiles/` pattern cleanly separates identity and role-specific config from platform config. If we ever need personal vs. work machine variants, externalizing these into importable profile directories is cleaner than conditional logic in `hostConfig`. The `primaryUser.nix` alias module (`user`/`hm` shortcuts) reduces boilerplate significantly.
   - **Source:** `profiles/`, `modules/primaryUser.nix`
   - **Impact:** Medium. Would require restructuring how host-specific values flow through our system, but the pattern is compatible with our existing helpers. Only worth doing if we actually need multi-identity support.

2. **Garnix for zero-config CI**
   - **Rationale:** Garnix requires only a 7-line YAML file to build all flake outputs with caching. No pipeline scripts, no Nix installation steps, no cache push commands. For validating that all configurations build, this is dramatically simpler than any other CI approach surveyed. It complements (rather than replaces) more complex CI for macOS builds.
   - **Source:** `garnix.yaml`
   - **Impact:** Low. Easy to adopt alongside our existing Dagger-based testing. Garnix is free for open-source repos.

3. **Cirrus CI for macOS ARM builds**
   - **Rationale:** GitHub Actions lacks native macOS ARM runners for open-source projects. Cirrus CI provides them free. If we want CI validation of our darwin configurations on Apple Silicon, Cirrus CI is currently the best option. The `cachix watch-exec` pattern pushes all build outputs to a binary cache automatically.
   - **Source:** `.cirrus.yml`
   - **Impact:** Medium. Adds a new CI service dependency. Only relevant if we want remote darwin build validation.

4. **Automated flake.lock updates via GitHub Actions**
   - **Rationale:** `DeterminateSystems/update-flake-lock` creates PRs with flake.lock updates on a daily cron. Combined with CI that validates builds, this creates hands-off dependency management. Our manual `task update` requires remembering to run it. This is now a three-repo signal (chenglab, kclejeune, wimpysworld via Dependabot).
   - **Source:** `.github/workflows/update.yml`
   - **Impact:** Low. Easy to add without changing anything else. The action is well-maintained by DeterminateSystems.

5. **Flake checks from all configurations**
   - **Rationale:** kclejeune generates flake `checks` by extracting `activationPackage` from homeConfigurations and `system.build.toplevel` from darwin/nixos configs, filtered to the current system. This means `nix flake check` validates actual system builds, not just syntax. mrjones2014 (US-006) uses the same pattern with `checksForConfigs`. Two-repo signal.
   - **Source:** `perSystem.checks` in `flake.nix`
   - **Impact:** Medium. Would make `task check` validate real builds. Requires adapting the filtering logic for our config naming scheme.

6. **git-hooks.nix for pre-commit treefmt**
   - **Rationale:** The `cachix/git-hooks.nix` integration runs treefmt as a pre-commit hook, catching formatting issues before they reach CI. The hook is installed automatically via `devShells.default.shellHook`. Our repo has treefmt but no pre-commit enforcement.
   - **Source:** `flake.nix` (git-hooks flakeModule), `perSystem.pre-commit`
   - **Impact:** Low. Easy to add as a flake-parts module. Prevents formatting-only CI failures.

7. **deadnix in treefmt pipeline**
   - **Rationale:** deadnix detects unused variables, unused `let` bindings, and unused lambda arguments in Nix code. Adding it to treefmt (alongside nixfmt) catches dead code automatically. kclejeune configures `no-lambda-arg` and `no-lambda-pattern-names` to reduce noise. Combined with mrjones2014's statix (US-006), there are now two repos signaling Nix-specific linting beyond formatting.
   - **Source:** `perSystem.treefmt.programs.deadnix`
   - **Impact:** Low. Drop-in addition to our existing treefmt config.

8. **nix-index-database with comma integration**
   - **Rationale:** `nix-index-database` provides a pre-built index of all nixpkgs packages, enabling instant `nix-locate` queries and `comma` (`, <command>`) to run any nixpkgs program without installing it. This eliminates the need to build a local nix-index (which takes significant time and resources). The comma integration means typing `, ripgrep` runs ripgrep from nixpkgs without any prior installation.
   - **Source:** `modules/home-manager/default.nix` (nix-index-database import), `programs.nix-index-database.comma.enable`
   - **Impact:** Low. Single flake input addition + two HM options. Immediately useful for ad-hoc tool usage.
