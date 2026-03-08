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
