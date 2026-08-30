{
  pkgs,
  lib,
  config,
  osConfig ? { },
  ...
}:

let
  inherit (lib) mkOption types;
  inherit (config.lib.stylix) colors;
  inherit (import ../lib/colors.nix { inherit lib; }) lighten;
  cfg = config.dotfiles.claude;

  # Resolved from the host config (nix-darwin / NixOS) when home-manager
  # runs as a host module. Standalone home-manager (linux containers,
  # Dagger) has no `osConfig.users`, so the lookup returns null and the
  # uid-scoped paths drop out of the lists below.
  hostUid = lib.attrByPath [ "users" "users" config.home.username "uid" ] null osConfig;
  claudeTmpPaths = lib.optionals (hostUid != null) [
    "/tmp/claude-${toString hostUid}"
    "/private/tmp/claude-${toString hostUid}"
  ];
  sopsEnabled = config.dotfiles.sops.enable;
  skipPerms = cfg.dangerouslySkipPermissions;

  # Claude Code's built-in sandbox. Darwin-only; the Linux backend is
  # unverified, and `failIfUnavailable = true` would break startup.
  sandboxEnabled = pkgs.stdenv.hostPlatform.isDarwin;

  # True when the process is contained — by Claude Code's sandbox on
  # Darwin, or by the Lima VM on terrarium. Drives `--auto-allow` on
  # hook-router, which lets the bash hook emit PreToolUse "allow" and
  # skip the static analyzer prompt on shell expansions. See
  # tools/hook-router/main.go config.autoAllow.
  autoAllowEnabled = sandboxEnabled || config.dotfiles.hostname == "terrarium";

  # Returns the sops secret path when sops is enabled, or a nonexistent
  # path when disabled.  Nix is lazy so the `then` branch (which accesses
  # config.sops.secrets) is never evaluated when sopsEnabled is false.
  secretPath =
    name: if sopsEnabled then config.sops.secrets.${name}.path else "/run/secrets/disabled";

  # Generate shell snippet that exports an env var from a sops secret file.
  exportSecret = envVar: secretName: ''
    if [ -f "${secretPath secretName}" ]; then
      export ${envVar}="$(cat "${secretPath secretName}" 2>/dev/null || true)"
    fi
  '';

  # Generate shell snippets for multiple env-var-to-secret mappings.
  exportSecrets = mappings: lib.concatStrings (lib.mapAttrsToList exportSecret mappings);

  claudePowerlineConfig = builtins.toJSON {
    inherit (cfg.powerline) theme;
    colors.${cfg.powerline.theme} = cfg.powerline.colors;
    display = {
      inherit (cfg.powerline.display)
        style
        charset
        colorCompatibility
        autoWrap
        padding
        lines
        ;
    };
    inherit (cfg.powerline) budget;
  };

  urlMatchOptions = {
    scheme = mkOption {
      type = types.str;
      default = "";
      description = "Regex pattern matching the URL scheme. Empty string means no constraint.";
    };
    host = mkOption {
      type = types.str;
      default = "";
      description = "Regex pattern matching the URL host. Empty string means no constraint.";
    };
    path = mkOption {
      type = types.str;
      default = "";
      description = "Regex pattern matching the URL path. Empty string means no constraint.";
    };
    query = mkOption {
      type = types.str;
      default = "";
      description = "Regex pattern matching the URL query string. Empty string means no constraint.";
    };
    fragment = mkOption {
      type = types.str;
      default = "";
      description = "Regex pattern matching the URL fragment. Empty string means no constraint.";
    };
  };

  urlMatchType = types.submodule { options = urlMatchOptions; };

  powerlineColorType = types.submodule {
    options = {
      bg = mkOption {
        type = types.str;
        description = "Background color for the segment. Any color string accepted by claude-powerline (e.g. `#rrggbb`).";
      };
      fg = mkOption {
        type = types.str;
        description = "Foreground color for the segment. Any color string accepted by claude-powerline (e.g. `#rrggbb`).";
      };
    };
  };

  powerlineSegmentType = types.submodule {
    freeformType = types.attrsOf types.anything;
    options.enabled = mkOption {
      type = types.bool;
      default = true;
      description = "Whether this segment renders.";
    };
  };

  mkBudgetType =
    defaults:
    types.submodule {
      options = {
        amount = mkOption {
          type = types.int;
          default = defaults.amount;
          description = "Budget limit. Units are interpreted per `type`.";
        };
        type = mkOption {
          type = types.enum [
            "tokens"
            "cost"
          ];
          default = defaults.type;
          description = "Budget unit.";
        };
        warningThreshold = mkOption {
          type = types.ints.between 0 100;
          default = 80;
          description = "Percent of budget at which to switch to the warning color.";
        };
      };
    };

  denyRuleType = types.submodule {
    options = urlMatchOptions // {
      reason = mkOption {
        type = types.nonEmptyStr;
        description = "Human-readable reason shown when a matching URL is denied.";
      };
      except = mkOption {
        type = types.listOf urlMatchType;
        default = [ ];
        description = "URL patterns exempted from this deny rule.";
      };
    };
  };

  # Bash command rule, evaluated by hook-router on PreToolUse:Bash.
  # Used for both deny rules (block the command) and ask rules (force
  # a permission prompt); the action is set by list membership when
  # the rules are serialized for the hook-router wrapper. See
  # tools/hook-router/cmdrules/cmdrules.go Rule for matching
  # semantics; the JSON tags there must stay aligned with these option
  # names.
  commandRuleType = types.submodule {
    options = {
      command = mkOption {
        type = types.nonEmptyStr;
        description = ''
          Executable name matched literally against the first word of
          the call.
        '';
      };
      args = mkOption {
        type = types.listOf types.nonEmptyStr;
        default = [ ];
        description = ''
          Positional arguments that must follow `command`, matched
          literally in order. With `command = "git"`, leading
          top-level git flags are skipped before matching; other
          commands match strictly from position 1. An empty list
          matches every invocation of `command`, so set a non-empty
          list to scope the rule to a specific subcommand.
        '';
      };
      except = mkOption {
        type = types.listOf types.nonEmptyStr;
        default = [ ];
        description = ''
          Literal arguments that exempt the call when they appear as
          the next argument after `args`. Lets `git stash pop` through
          while still denying `git stash push`. No flag-skipping is
          applied to the candidate slot, so intervening flags break
          the exemption. Bare `command + args` calls (no further
          arguments) fire regardless of `except` unless `exceptBare`
          is set.
        '';
      };
      exceptBare = mkOption {
        type = types.bool;
        default = false;
        description = ''
          Whether a bare `command + args` call (no further arguments)
          is exempt. Defaults to false because a bare `git stash` is
          still a save form; set it for groups whose bare form is a
          read, such as `git remote`.
        '';
      };
      reason = mkOption {
        type = types.nonEmptyStr;
        description = "Message shown when the rule denies a command or asks for confirmation.";
      };
    };
  };

  # File-formatter routing rule, evaluated by hook-router on
  # PostToolUse:Write/Edit. The matched file path is appended
  # as the final argv element when the rule's command runs. See
  # tools/hook-router/formatter_rules.go FormatterRule for matching
  # semantics; option names below are camelCase because
  # `builtins.toJSON` emits attribute names verbatim and the Go struct
  # tags expect camelCase.
  formatterRuleType = types.submodule {
    options = {
      pathGlob = mkOption {
        type = types.nonEmptyStr;
        description = ''
          Absolute file-path glob evaluated with doublestar v4
          (`doublestar.PathMatch`), OS-separator aware like the older
          `filepath.Match`. `**` crosses path separators only when it
          occupies a full segment: `/a/**/*.md` is recursive, but
          `/a/**foo` is not (the `**` degrades to a single `*`).
          Tilde expansion does not happen at runtime, so the Nix
          evaluator must produce the resolved absolute path (e.g.
          `''${config.home.homeDirectory}/.claude/plans/*.md`).
        '';
      };
      command = mkOption {
        type = types.listOf types.nonEmptyStr;
        description = ''
          Formatter argv. The matched file path is appended as the
          final argument, so the binary must accept a path positional.
        '';
      };
      timeout = mkOption {
        type = types.str;
        default = "5s";
        description = ''
          Per-invocation wall-clock budget (Go time.ParseDuration
          syntax). Defaults to 5s; malformed values silently fall
          back to the default.
        '';
      };
    };
  };

  toPermGlob =
    path:
    let
      expanded =
        if lib.hasPrefix "~" path then config.home.homeDirectory + lib.removePrefix "~" path else path;
    in
    "/${expanded}/**";

  cleanAttrs = lib.filterAttrs (_: v: v != "" && v != [ ]);
  cleanRule =
    rule:
    let
      cleaned = cleanAttrs rule;
    in
    if cleaned ? except then cleaned // { except = map cleanAttrs cleaned.except; } else cleaned;

  workmux = "${lib.getExe' pkgs.workmux-bin "workmux"} set-window-status";

  # Grants a workmux-created linked worktree's Claude session write access to
  # the shared parent .git so git add/commit/rebase/push work under the macOS
  # Seatbelt sandbox (the sandbox allowlists the worktree cwd but not the
  # parent repo's .git; anthropics/claude-code#64792). hooks/ and config stay
  # write-denied. Merges into (never overwrites) the
  # .claude/settings.local.json that files.copy just seeded from the parent
  # checkout.
  worktreeAllowGitWrites = pkgs.writeShellScript "workmux-worktree-allow-git-writes" ''
    set -euo pipefail
    cd "''${WM_WORKTREE_PATH:-$PWD}"
    common_dir=$(git rev-parse --path-format=absolute --git-common-dir)
    settings=".claude/settings.local.json"
    mkdir -p .claude
    [ -f "$settings" ] || printf '{}' > "$settings"
    tmp=$(mktemp)
    ${pkgs.jq}/bin/jq \
      --arg gitdir "$common_dir" \
      '.sandbox.filesystem.allowWrite =
          ((.sandbox.filesystem.allowWrite // []) + [$gitdir] | unique)
        | .sandbox.filesystem.denyWrite =
          ((.sandbox.filesystem.denyWrite // [])
            + [$gitdir + "/hooks", $gitdir + "/config"] | unique)' \
      "$settings" > "$tmp"
    mv "$tmp" "$settings"
  '';

  workmuxConfig = (pkgs.formats.yaml { }).generate "config.yaml" {
    nerdfont = true;
    merge_strategy = "rebase";
    # Start independent worktrees from the repo's effective main branch
    # (configured main_branch or detected default) instead of whatever
    # branch happens to be checked out, so spawning a task while sitting
    # on a feature branch doesn't silently stack unrelated work on it.
    # Explicit --base and --pr checkouts still take precedence.
    base_branch = "auto";
    inherit (cfg.workmux) agent;
    # Named agent entry consumed by the focused pane's <agent> placeholder.
    # The structured agent path re-derives the command on every render, so
    # workmux's double render_command() stays idempotent and agent mutations
    # (sandbox skip-permissions flag, --fork/--continue resume args) apply.
    # A raw pane command string is rendered twice, which double-wraps it in
    # sh -c and breaks quoting under the devbox toolchain wrapper.
    agents.${cfg.workmux.agent} = cfg.workmux.command;
    window_prefix = "wm-";
    worktree_dir = ".worktrees";
    status_format = false;
    status_icons = {
      working = "󱚣";
      waiting = "󰍻";
      sleeping = "󰤄";
      done = "󰄴";
    };
    theme = {
      custom = {
        current_row_bg = "#${colors.base00}";
        text = "#${colors.base05}";
        accent = "#${colors.base0D}";
        success = "#${colors.base0B}";
        warning = "#${colors.base0A}";
        danger = "#${colors.base08}";
        dimmed = "#${colors.base03}";
        border = "#${colors.base02}";
        header = "#${colors.base0E}";
        keycap = "#${colors.base0C}";
        info = "#${colors.base0C}";
      };
    };
    post_create =
      lib.optionals cfg.lima.enable [
        "direnv allow >/dev/null 2>&1 || true"
        "lefthook install >/dev/null 2>&1 || true"
        # Authenticate this fresh sandbox to the self-hosted atuin server so its
        # shell history syncs with the host. Credentials arrive via
        # env_passthrough; the key is read from the mounted host key (key_path).
        # Best-effort: an unreachable server, missing creds, or a failed sync
        # never aborts sandbox setup.
        "atuin login -u \"$ATUIN_USERNAME\" -p \"$ATUIN_PASSWORD\" >/dev/null 2>&1 && atuin sync >/dev/null 2>&1 || true"
      ]
      # Darwin-only because that's where the Seatbelt sandbox runs. Kept on
      # even with lima: the agent pane runs in the VM (where the settings key
      # is inert), but host-side Claude sessions opened in the worktree still
      # need the write grant.
      ++ lib.optionals sandboxEnabled [
        "${worktreeAllowGitWrites}"
      ];
    panes = [
      {
        command = "<agent>";
        focus = true;
      }
      {
        split = "horizontal";
        command = "nvim";
      }
      (
        {
          split = "vertical";
        }
        // lib.optionalAttrs cfg.lima.enable {
          command = "workmux sandbox shell -- fish";
        }
      )
    ];
    files = {
      copy = [
        ".claude"
        # Seed new worktrees with the main checkout's tofu/terraform cache
        # (.terraform is gitignored, so fresh worktrees would otherwise
        # cold-init and re-download every provider and module). Bounded depths
        # on purpose: worktree_dir is ".worktrees" *inside* the repo, so a
        # recursive "**/.terraform" glob would descend into sibling worktrees
        # and copy their provider caches. ".terraform" covers a root module at
        # the repo root; "*/.terraform" covers a module one level down. Copies
        # stay lightweight because .terraform/providers entries are symlinks
        # into the shared plugin cache (see home/tools.nix) and workmux's
        # copy_dir_recursive preserves symlinks rather than following them.
        ".terraform"
        "*/.terraform"
      ];
    };
    sandbox = lib.optionalAttrs cfg.lima.enable {
      enabled = true;
      backend = "lima";
      image = "file://${config.home.homeDirectory}/.lima/_images/terrarium.qcow2";
      host_commands = [ "mcp-kubectx" ];
      toolchain = "auto";
      env_passthrough = [
        "GITHUB_TOKEN"
        "GITHUB_PERSONAL_ACCESS_TOKEN"
        "GH_TOKEN"
        "ARGOCD_API_TOKEN"
        "ARGOCD_BASE_URL"
        "DAGGER_CLOUD_TOKEN"
        "KAGI_API_KEY"
        "SPACELIFT_API_KEY_ENDPOINT"
        "SPACELIFT_API_KEY_ID"
        "SPACELIFT_API_KEY_SECRET"
        "TF_CLI_CONFIG_FILE"
        "TERM_PROGRAM"
        "TERM_PROGRAM_VERSION"
        "ATUIN_USERNAME"
        "ATUIN_PASSWORD"
      ];
      extra_mounts = [
        {
          host_path = researchDir;
          guest_path = researchDir;
          writable = true;
        }
        {
          # mcp-kubectx serve writes scoped kubeconfig files at
          # this path via `host select` running on the macOS host.
          # The guest's kubectl reads them through this bind mount,
          # and shutdown cleanup uses a local os.Remove from the
          # guest -- so the mount must be writable. Blast radius
          # is bounded to this directory, which only ever holds
          # mcp-kubectx kubeconfigs.
          host_path = "${config.xdg.stateHome}/mcp-kubectx";
          guest_path = "${config.xdg.stateHome}/mcp-kubectx";
          writable = true;
        }
        {
          # Output dir for the web-archive skill (btrix, yt-dlp).
          # Mount it like ~/Documents/repos so captures from inside
          # the sandbox persist to the host filesystem.
          host_path = cfg.archivesDir;
          guest_path = cfg.archivesDir;
          writable = true;
        }
        {
          # Read-only: the guest reads only the static key. Its own history
          # DB lives at the Linux /home/<user>/.local/share/atuin -- a
          # different absolute path -- so there is no write conflict.
          host_path = atuinDataDir;
          guest_path = atuinDataDir;
          writable = false;
        }
      ];
      lima = {
        isolation = "shared";
        projects_dir = "${config.home.homeDirectory}/Documents/repos";
        skip_default_provision = true;
        # Shared isolation trades Git metadata isolation for writable repo
        # mounts; workmux requires acknowledging that for unattended starts.
        accept_reduced_git_metadata_isolation = true;
        inherit (cfg.lima) cpus;
        inherit (cfg.lima) memory;
        inherit (cfg.lima) disk;
      };
    };
  };

  claudeStylixBase = if config.stylix.polarity == "light" then "light" else "dark";

  # Shimmer tokens in the upstream Claude Code dark theme are hardcoded
  # constants ~+12 lightness points above their base. base16 has no
  # lightened slots, so we derive them by bumping HSL lightness. This keeps
  # the warm pair-with-base relationship intact under any stylix scheme.
  shimmerOf = name: lighten colors name 12;

  claudeStylixTheme = (pkgs.formats.json { }).generate "stylix.json" {
    name = "Stylix";
    base = claudeStylixBase;
    overrides = {
      claude = "#${colors.base09}";
      claudeShimmer = shimmerOf "base09";
      text = "#${colors.base05}";
      inverseText = "#${colors.base00}";
      inactive = "#${colors.base04}";
      inactiveShimmer = shimmerOf "base04";
      subtle = "#${colors.base03}";
      permission = "#${colors.base0D}";
      permissionShimmer = shimmerOf "base0D";
      remember = "#${colors.base0D}";

      success = "#${colors.base0B}";
      error = "#${colors.base08}";
      warning = "#${colors.base0A}";
      warningShimmer = shimmerOf "base0A";
      merged = "#${colors.base0E}";

      promptBorder = "#${colors.base04}";
      promptBorderShimmer = shimmerOf "base04";
      planMode = "#${colors.base0D}";
      autoAccept = "#${colors.base0E}";
      bashBorder = "#${colors.base0F}";
      ide = "#${colors.base0D}";
      fastMode = "#${colors.base09}";
      fastModeShimmer = shimmerOf "base09";

      userMessageBackground = "#${colors.base01}";
      userMessageBackgroundHover = "#${colors.base02}";
      selectionBg = "#${colors.base02}";

      red_FOR_SUBAGENTS_ONLY = "#${colors.base08}";
      orange_FOR_SUBAGENTS_ONLY = "#${colors.base09}";
      yellow_FOR_SUBAGENTS_ONLY = "#${colors.base0A}";
      green_FOR_SUBAGENTS_ONLY = "#${colors.base0B}";
      cyan_FOR_SUBAGENTS_ONLY = "#${colors.base0C}";
      blue_FOR_SUBAGENTS_ONLY = "#${colors.base0D}";
      purple_FOR_SUBAGENTS_ONLY = "#${colors.base0E}";
      pink_FOR_SUBAGENTS_ONLY = "#${colors.base0F}";

      # Diff dimmed tokens (diffAddedDimmed, diffRemovedDimmed) are left to
      # the preset. Their upstream relationship to diffAdded/diffRemoved is
      # a desaturate-and-tint, not a simple lighten, so HSL lightening would
      # not reproduce the muted background look.
    };
  };

  fetchRules = (pkgs.formats.json { }).generate "mcp-fetch-rules.json" (
    lib.optionalAttrs cfg.fetchAllowlist {
      reason = "URL not in allowlist. If you need to fetch this content, ask the user to add an entry to the allowlist. Present the user with both the URL and your justification.";
    }
    // {
      deny = map cleanRule (bundledFetchDeny ++ cfg.extraFetchRules.deny);
      robotsExempt = map cleanAttrs (bundledFetchRobotsExempt ++ cfg.extraFetchRules.robotsExempt);
    }
    // lib.optionalAttrs cfg.fetchAllowlist {
      allow = map cleanAttrs (
        [
          { host = "(.*\\.)?adguard\\.com"; }
          { host = "(.*\\.)?anthropic\\.com"; }
          { host = "(.*\\.)?archlinux\\.org"; }
          { host = "argoproj\\.github\\.io"; }
          { host = "(.*\\.)?argoproj\\.io"; }
          { host = "(.*\\.)?artifacthub\\.io"; }
          { host = "(.*\\.)?astral\\.sh"; }
          { host = "(.*\\.)?atuin\\.sh"; }
          { host = "(.*\\.)?docs\\.auth0\\.com"; }
          { host = "bjw-s-labs\\.github\\.io"; }
          { host = "(.*\\.)?brew\\.sh"; }
          { host = "(.*\\.)?cert-manager\\.io"; }
          { host = "(.*\\.)?cilium\\.io"; }
          { host = "(.*\\.)?cncf\\.io"; }
          { host = "(.*\\.)?cloudflare\\.com"; }
          { host = "(.*\\.)?cloudnative-pg\\.io"; }
          { host = "(.*\\.)?containerd\\.io"; }
          { host = "(.*\\.)?coredns\\.io"; }
          { host = "(.*\\.)?crates\\.io"; }
          { host = "(.*\\.)?crossplane\\.io"; }
          { host = "(.*\\.)?crds\\.dev"; }
          { host = "(.*\\.)?dagger\\.io"; }
          { host = "(.*\\.)?daggerverse\\.dev"; }
          { host = "(.*\\.)?direnv\\.net"; }
          { host = "(.*\\.)?docker\\.com"; }
          { host = "(.*\\.)?docs\\.rs"; }
          { host = "(.*\\.)?editorconfig\\.org"; }
          { host = "(.*\\.)?docs\\.doppler\\.com"; }
          { host = "(.*\\.)?dragonflydb\\.io"; }
          { host = "(.*\\.)?envoyproxy\\.io"; }
          { host = "(.*\\.)?etcd\\.io"; }
          { host = "(.*\\.)?external-secrets\\.io"; }
          { host = "(.*\\.)?fishshell\\.com"; }
          { host = "(.*\\.)?fluxcd\\.io"; }
          { host = "(.*\\.)?freedesktop\\.org"; }
          { host = "(.*\\.)?gethomepage\\.dev"; }
          { host = "(.*\\.)?getsops\\.io"; }
          { host = "(.*\\.)?ghostty\\.org"; }
          { host = "(.*\\.)?git-scm\\.com"; }
          { host = "(.*\\.)?github\\.com"; }
          { host = "(.*\\.)?githubusercontent\\.com"; }
          { host = "(.*\\.)?gnu\\.org"; }
          { host = "(.*\\.)?gnupg\\.org"; }
          { host = "(.*\\.)?go\\.dev"; }
          { host = "(.*\\.)?golang\\.org"; }
          { host = "(.*\\.)?grafana\\.com"; }
          { host = "(.*\\.)?grpc\\.io"; }
          { host = "(.*\\.)?hashicorp\\.com"; }
          { host = "(.*\\.)?helm\\.sh"; }
          { host = "(.*\\.)?hetzner\\.com"; }
          { host = "(.*\\.)?ietf\\.org"; }
          { host = "(.*\\.)?inspektor-gadget\\.io"; }
          { host = "(.*\\.)?jacobcolvin\\.com"; }
          { host = "(.*\\.)?jetify\\.com"; }
          { host = "(.*\\.)?jsonnet\\.org"; }
          { host = "(.*\\.)?k9scli\\.io"; }
          { host = "(.*\\.)?kagi\\.com"; }
          { host = "(.*\\.)?kcl-lang\\.io"; }
          { host = "(.*\\.)?kernel\\.org"; }
          { host = "(.*\\.)?kubernetes\\.io"; }
          { host = "(.*\\.)?kyverno\\.io"; }
          { host = "(.*\\.)?letsencrypt\\.org"; }
          { host = "(.*\\.)?lix\\.systems"; }
          { host = "(.*\\.)?man7\\.org"; }
          { host = "(.*\\.)?microsoft\\.com"; }
          { host = "(.*\\.)?mozilla\\.org"; }
          { host = "(.*\\.)?nats\\.io"; }
          { host = "(.*\\.)?nix\\.dev"; }
          { host = "(.*\\.)?nixos\\.org"; }
          { host = "(.*\\.)?nmap\\.org"; }
          { host = "(.*\\.)?nodejs\\.org"; }
          { host = "(.*\\.)?npmjs\\.com"; }
          { host = "(.*\\.)?npmjs\\.org"; }
          { host = "(.*\\.)?obsidian\\.md"; }
          { host = "(.*\\.)?opencontainers\\.org"; }
          { host = "(.*\\.)?openebs\\.io"; }
          { host = "(.*\\.)?openssl\\.org"; }
          { host = "(.*\\.)?opentelemetry\\.io"; }
          { host = "(.*\\.)?opentofu\\.org"; }
          { host = "(.*\\.)?postgresql\\.org"; }
          { host = "(.*\\.)?prettier\\.io"; }
          { host = "(.*\\.)?prometheus\\.io"; }
          { host = "(.*\\.)?pypi\\.org"; }
          { host = "(.*\\.)?python\\.org"; }
          { host = "old\\.reddit\\.com"; }
          { host = "(.*\\.)?redis\\.io"; }
          { host = "(.*\\.)?rfc-editor\\.org"; }
          { host = "(.*\\.)?robusta\\.dev"; }
          { host = "(.*\\.)?rook\\.io"; }
          { host = "(.*\\.)?rust-lang\\.org"; }
          { host = "(.*\\.)?securecodebox\\.io"; }
          { host = "(.*\\.)?sigstore\\.dev"; }
          { host = "(.*\\.)?sigs\\.k8s\\.io"; }
          { host = "(.*\\.)?spacelift\\.io"; }
          { host = "(.*\\.)?sqlite\\.org"; }
          { host = "(.*\\.)?stakater\\.com"; }
          { host = "(.*\\.)?stackoverflow\\.com"; }
          { host = "(.*\\.)?starship\\.rs"; }
          { host = "(.*\\.)?talos\\.dev"; }
          { host = "(.*\\.)?taskfile\\.dev"; }
          { host = "(.*\\.)?terraform\\.io"; }
          { host = "(.*\\.)?tetragon\\.io"; }
          { host = "(.*\\.)?truenas\\.com"; }
          { host = "(.*\\.)?typescriptlang\\.org"; }
          { host = "(.*\\.)?ui\\.com"; }
          { host = "(.*\\.)?w3\\.org"; }
          { host = "(.*\\.)?whatwg\\.org"; }
          { host = "(.*\\.)?wikipedia\\.org"; }
          { host = "(.*\\.)?wireguard\\.com"; }
          { host = "(.*\\.)?wireshark\\.org"; }
          { host = "(.*\\.)?zed\\.dev"; }
        ]
        ++ bundledFetchAllow
        ++ cfg.extraFetchRules.allow
      );
    }
  );

  # JSON payload passed to hook-router via --post-impl-skills.
  # Derived from cfg.postImplSkills, filtered to enabled entries.
  # Field names must match the lowercase JSON tags on PostImplSkill
  # in tools/hook-router/plan.go.
  postImplCatalog = lib.mapAttrsToList (_: s: { inherit (s) label description; }) (
    lib.filterAttrs (_: s: s.enable) cfg.postImplSkills
  );

  # Wrap-up skills whose UserPromptSubmit invocation clears the
  # plan-guard session state, releasing Stop. `merge` is the public
  # name of the wm-merge skill (see configs/claude/skills/wm-merge/
  # SKILL.md `name: merge`). Other wm-* skills (rebase, coordinator,
  # workmux) are not wrap-up actions and are intentionally excluded.
  commitSkills = [
    "commit"
    "commit-push-pr"
    "merge"
  ];

  hookRouter = pkgs.writeShellApplication {
    name = "hook-router-wrapper";
    runtimeInputs = [
      pkgs.hook-router
      pkgs.git
    ];
    # The --command-rules JSON is shell-escaped via lib.escapeShellArg
    # (single-quoted). Backticks inside the JSON are literal data, but
    # shellcheck warns SC2016 about them as if they were unexpanded
    # command substitutions.
    excludeShellChecks = [ "SC2016" ];
    text = ''
      exec hook-router \
        --db "${config.xdg.stateHome}/hook-router/state.db" \
        --log-file "${config.xdg.stateHome}/hook-router/hook-router.log" \
        --post-impl-skills ${lib.escapeShellArg (builtins.toJSON postImplCatalog)} \
        --commit-skills ${lib.escapeShellArg (builtins.toJSON commitSkills)} \
        --command-rules ${
          # Deny rules are serialized before ask rules: hook-router
          # evaluates the list in order with first-match-wins, so this
          # ordering preserves deny precedence.
          lib.escapeShellArg (
            builtins.toJSON (
              map (r: r // { action = "deny"; }) (bundledCommandDeny ++ cfg.extraCommandRules.deny)
              ++ map (r: r // { action = "ask"; }) (bundledCommandAsk ++ cfg.extraCommandRules.ask)
            )
          )
        } \
        --mcp-rules ${
          # The MCP entries of the same aggregated lists that feed
          # settings.permissions, re-enforced at hook level because
          # plan mode ignores settings allow rules for
          # subagent-originated MCP calls
          # (anthropics/claude-code#73633). Filtering by the mcp__
          # prefix keeps hook and settings in lockstep as bundles
          # change.
          lib.escapeShellArg (
            builtins.toJSON {
              allow = lib.filter (lib.hasPrefix "mcp__") (bundledAllow ++ cfg.extraPermissions.allow);
              ask = lib.filter (lib.hasPrefix "mcp__") (bundledAsk ++ cfg.extraPermissions.ask);
              deny = lib.filter (lib.hasPrefix "mcp__") (bundledDeny ++ cfg.extraPermissions.deny);
            }
          )
        } \
        --formatter-rules ${
          lib.escapeShellArg (builtins.toJSON (defaultFormatterRules ++ cfg.formatterRules))
        } \
        --compaction-config ${
          lib.escapeShellArg (builtins.toJSON (removeAttrs cfg.outputCompaction [ "saveFullOutput" ]))
        } \
        ${lib.optionalString cfg.outputCompaction.saveFullOutput ''--compaction-output-dir "${config.xdg.stateHome}/hook-router/outputs"''} \
        ${lib.optionalString cfg.searchRewrite.enable "--search-rewrite-config ${lib.escapeShellArg (builtins.toJSON (removeAttrs cfg.searchRewrite [ "enable" ]))}"} \
        --sleep-guard-config ${
          # Passed unconditionally: enable travels inside the JSON (the
          # --compaction-config pattern) because maxSeconds cannot double
          # as a disabled sentinel.
          lib.escapeShellArg (builtins.toJSON cfg.sleepGuard)
        } \
        ${lib.optionalString autoAllowEnabled "--auto-allow"} \
        ${lib.optionalString cfg.skipPlanReview "--skip-plan-review"} \
        ${lib.optionalString cfg.enforceAsciiTypography "--enforce-ascii-typography"} \
        "$@"
    '';
  };

  # Appends every settings and skills edit Claude Code reports, including
  # the settings.local.json the workmux worktree hook writes. hook-router
  # creates this directory in its own logger, so a host that has never run
  # it needs the mkdir.
  configChangeLog = pkgs.writeShellApplication {
    name = "claude-config-change-log";
    runtimeInputs = [ pkgs.jq ];
    text = ''
      log_dir="${config.xdg.stateHome}/hook-router"
      # Exit code 2 blocks the config change and every other non-zero code
      # only prints stderr. jq exits 2 when the append fails, so remap both
      # failures to 1: a full disk must not stop a skill reload.
      mkdir -p "$log_dir" || exit 1
      jq -c '{ time: (now | todateiso8601), source, file_path }' \
        >>"$log_dir/config-changes.log" || exit 1
    '';
  };

  # CA env vars injected into all stdio MCP servers
  caEnvVars = lib.optionalAttrs (config.dotfiles.caBundlePath != null) {
    NIX_SSL_CERT_FILE = config.dotfiles.caBundlePath;
    SSL_CERT_FILE = config.dotfiles.caBundlePath;
    CURL_CA_BUNDLE = config.dotfiles.caBundlePath;
    GIT_SSL_CAINFO = config.dotfiles.caBundlePath;
    REQUESTS_CA_BUNDLE = config.dotfiles.caBundlePath;
    NODE_EXTRA_CA_CERTS = config.dotfiles.caBundlePath;
  };

  # Post-process all servers to inject CA env into stdio servers
  injectCaEnv =
    servers:
    lib.mapAttrs (
      _: server:
      if (server.type or "") == "stdio" && caEnvVars != { } then
        server // { env = caEnvVars // (server.env or { }); }
      else
        server
    ) servers;

  gitWrapper = pkgs.writeShellScript "git-mcp-wrapper" ''
    ${exportSecret "GH_TOKEN" "gh_token"}
    exec ${pkgs.mcp-git}/bin/mcp-git "$@"
  '';

  kagiWrapper = pkgs.writeShellScript "kagi-mcp-wrapper" ''
    ${exportSecret "KAGI_API_KEY" "kagi_api_key"}
    exec ${pkgs.mcp-kagi}/bin/kagimcp "$@"
  '';

  spaceliftWrapper = pkgs.writeShellScript "spacelift-mcp-wrapper" ''
    ${exportSecrets {
      SPACELIFT_API_KEY_ENDPOINT = "spacelift_api_key_endpoint";
      SPACELIFT_API_KEY_ID = "spacelift_api_key_id";
      SPACELIFT_API_KEY_SECRET = "spacelift_api_key_secret";
    }}
    exec ${pkgs.spacectl}/bin/spacectl "$@"
  '';

  # --deny-tool flags for the github proxy, derived from the github tool
  # bundle's own permissions.deny (the single source of truth). Denying a
  # github MCP tool there also strips its schema from the proxy's tools/list,
  # so it stops costing context tokens even though the upstream
  # endpoint still serves it. The mcp__github__ prefix is stripped back to the
  # upstream tool name the proxy matches; non-MCP deny entries are ignored.
  ghProxyDenyFlags = lib.concatMapStringsSep " " (t: "--deny-tool ${t}") (
    map (lib.removePrefix "mcp__github__") (
      lib.filter (lib.hasPrefix "mcp__github__") cfg.toolBundles.github.permissions.deny
    )
  );

  # The x/all endpoint serves every tool across all upstream toolsets.
  # The narrower /mcp endpoint would serve only the `default` toolset
  # (context, repos, issues, pull_requests, users), which carries no
  # Actions, security-alert, discussion, or project tools. Claude
  # reaches these through ToolSearch, so an unused tool costs one name
  # in the deferred listing rather than a schema, and permissions.deny
  # below stays the place where tool policy is spelled out: it strips
  # every write tool except the ask-gated pull request set. A new
  # upstream tool appears here on its own and prompts on first use
  # until it is allowed or denied.
  githubWrapper = pkgs.writeShellScript "github-mcp-wrapper" ''
    ${exportSecret "GH_TOKEN" "gh_token"}
    export GITHUB_PERSONAL_ACCESS_TOKEN="''${GH_TOKEN:-}"
    exec ${pkgs.mcp-http-proxy}/bin/mcp-http-proxy \
      --url https://api.githubcopilot.com/mcp/x/all \
      --header "Authorization=Bearer $GITHUB_PERSONAL_ACCESS_TOKEN" \
      --log-file "${config.xdg.stateHome}/mcp-http-proxy/github.log" \
      ${ghProxyDenyFlags} \
      "$@"
  '';

  # The launcher wrapper's in-sandbox merged $KUBECONFIG, emitted as
  # exactly ONE `export KUBECONFIG` --run line so a later --run cannot
  # shadow an earlier one. local.yaml (MCP-owned selection overlay,
  # first-file-wins) and the sidecar (external SA creds) always bracket
  # the colon-list. On the Lima guest image (guestKubeconfigLocal) the
  # guest's own ~/.kube/config is spliced between them, with a preceding
  # --run that exports $CLAUDE_KUBECTX_GUEST_CONFIG, so guest-local
  # clusters (kind / k3d / minikube / Talos-in-Docker) resolve like any
  # normal cluster. The interpolation carries no trailing line
  # continuation; the wrapProgram call site supplies the `\`.
  kubectxKubeconfigRunArgs =
    if cfg.guestKubeconfigLocal then
      "--run 'export CLAUDE_KUBECTX_GUEST_CONFIG=\"$HOME/.kube/config\"' \\\n        "
      + "--run 'export KUBECONFIG=\"$CLAUDE_KUBECTX_LOCAL:$CLAUDE_KUBECTX_GUEST_CONFIG:$CLAUDE_KUBECTX_SIDECAR\"'"
    else
      "--run 'export KUBECONFIG=\"$CLAUDE_KUBECTX_LOCAL:$CLAUDE_KUBECTX_SIDECAR\"'";

  # Wrap claude with its invocation-time env so vars survive boundaries that
  # don't propagate the shell env (lima VMs, ssh without SendEnv, etc.).
  claudeWrapped = pkgs.symlinkJoin {
    name = "claude-code-wrapped";
    paths = [ pkgs.llm-agents.claude-code ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    inherit (pkgs.llm-agents.claude-code) meta version;
    postBuild = ''
      wrapProgram $out/bin/claude \
        --run 'if [ -n "''${KUBECONFIG-}" ]; then export KUBECONFIG_HOST="$KUBECONFIG"; fi' \
        --run 'export CLAUDE_KUBECTX_DIR="''${XDG_RUNTIME_DIR:-/tmp}/claude-kubectx.$$"' \
        --run 'mkdir -p "$CLAUDE_KUBECTX_DIR"' \
        --run 'export CLAUDE_KUBECTX_SIDECAR="$CLAUDE_KUBECTX_DIR/kubeconfig"' \
        --run 'export CLAUDE_KUBECTX_LOCAL="$CLAUDE_KUBECTX_DIR/local.yaml"' \
        --run '[ -f "$CLAUDE_KUBECTX_LOCAL" ] || printf "apiVersion: v1\nkind: Config\n" > "$CLAUDE_KUBECTX_LOCAL"' \
        ${kubectxKubeconfigRunArgs} \
        --set CLAUDE_CODE_TMUX_TRUECOLOR 1 \
        --set CLAUDE_CODE_NO_FLICKER 1 \
        --set DISABLE_AUTOUPDATER 1 \
        --set CLAUDE_RESEARCH_DIR ${lib.escapeShellArg researchDir} \
        --set TF_CLI_CONFIG_FILE "${config.xdg.configHome}/opentofu/tofurc" \
        --set PLUGIN_UNIX_SOCKET_DIR "${config.home.homeDirectory}/.terraform.versions" \
        ${lib.optionalString skipPerms "--set IS_SANDBOX 1 --add-flags --allow-dangerously-skip-permissions --add-flags --permission-mode --add-flags plan"}
    '';
  };

  # Wrapper that injects sops secrets as env vars for sandbox env_passthrough.
  # Uses symlinkJoin so share/fish/vendor_completions.d/ from workmux-bin is preserved.
  workmuxWrapped = pkgs.symlinkJoin {
    name = "workmux-wrapped";
    paths = [ pkgs.workmux-bin ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    postBuild = ''
      wrapProgram $out/bin/workmux \
        --run '
          ${exportSecret "GH_TOKEN" "gh_token"}
          if [ -n "''${GH_TOKEN:-}" ]; then
            export GITHUB_TOKEN="$GH_TOKEN"
            export GITHUB_PERSONAL_ACCESS_TOKEN="$GH_TOKEN"
          fi
          ${exportSecrets {
            ARGOCD_API_TOKEN = "argocd_api_token";
            ARGOCD_BASE_URL = "argocd_base_url";
            DAGGER_CLOUD_TOKEN = "dagger_cloud_token";
            KAGI_API_KEY = "kagi_api_key";
            SPACELIFT_API_KEY_ENDPOINT = "spacelift_api_key_endpoint";
            SPACELIFT_API_KEY_ID = "spacelift_api_key_id";
            SPACELIFT_API_KEY_SECRET = "spacelift_api_key_secret";
            ATUIN_USERNAME = "atuin_username";
            ATUIN_PASSWORD = "atuin_password";
          }}
          export TF_CLI_CONFIG_FILE="${config.xdg.configHome}/opentofu/tofurc"
        '
    '';
  };

  # Aggregate enabled tool bundles
  enabledBundles = lib.filterAttrs (_: b: b.enable) cfg.toolBundles;
  bundleValues = lib.attrValues enabledBundles;

  applyAlwaysLoad =
    alwaysLoad: server: if alwaysLoad then server // { alwaysLoad = true; } else server;

  bundledServers = lib.foldl' lib.recursiveUpdate { } (
    map (b: lib.mapAttrs (_: server: applyAlwaysLoad b.alwaysLoad server) b.servers) bundleValues
  );
  bundledAllow = lib.concatMap (b: b.permissions.allow) bundleValues;
  bundledDeny = lib.concatMap (b: b.permissions.deny) bundleValues;
  bundledAsk = lib.concatMap (b: b.permissions.ask) bundleValues;
  bundledDomains = lib.concatMap (b: b.sandbox.allowedDomains) bundleValues;
  bundledSockets = lib.concatMap (b: b.sandbox.allowUnixSockets) bundleValues;
  bundledReadPaths = lib.concatMap (b: b.sandbox.allowRead) bundleValues;
  bundledWritePaths = lib.concatMap (b: b.sandbox.allowWrite) bundleValues;
  bundledFetchDeny = lib.concatMap (b: b.fetchRules.deny) bundleValues;
  bundledFetchAllow = lib.concatMap (b: b.fetchRules.allow) bundleValues;
  bundledFetchRobotsExempt = lib.concatMap (b: b.fetchRules.robotsExempt) bundleValues;
  bundledCommandDeny = lib.concatMap (b: b.commandRules.deny) bundleValues;
  bundledCommandAsk = lib.concatMap (b: b.commandRules.ask) bundleValues;

  researchDir =
    if cfg.research.useVault then
      "${config.dotfiles.obsidian.vaultsDir}/${cfg.research.vault}/research"
    else
      "${config.home.homeDirectory}/.local/share/claude/research";

  # Host atuin data dir, shared read-only into the sandbox so the guest's
  # atuin reads the host encryption key (key_path).
  atuinDataDir = "${config.home.homeDirectory}/.local/share/atuin";

  # mdformat's CommonMark core has no table parser, and stock pkgs.mdformat
  # ships no plugins. Without mdformat-gfm every pipe table parses as a
  # paragraph and `--wrap no` joins it onto one line, destroying it
  # irrecoverably. `--no-extensions` disables the plugin and reintroduces
  # that, so it must not come back; the explicit `--extensions` flags pin
  # the entry points the plugin registers. mdformat-gfm also preserves task
  # lists, strikethrough, and bare autolinks. The wrapped python binary is
  # invoked directly through its store-path `/bin/mdformat` so
  # hook-router-wrapper doesn't need it on PATH and the formatter's own
  # python wrapper script stays intact.
  mdformatBin = pkgs.mdformat.withPlugins (ps: [ ps.mdformat-gfm ]);

  # `--compact-tables` leaves table cells unpadded, so a table doesn't get
  # rewritten end-to-end whenever one cell's width changes.
  mdformatCommand = [
    "${mdformatBin}/bin/mdformat"
    "--wrap"
    "no"
    "--number"
    "--no-validate"
    "--extensions"
    "gfm"
    "--extensions"
    "tables"
    "--compact-tables"
    "--no-codeformatters"
  ];

  # Default formatter routes auto-installed by hook-router on
  # PostToolUse:Write/Edit. Plans and research notes
  # accumulate token-wasteful patterns (multi-blank-line runs, trailing
  # whitespace, inter-word double spaces) across many tool calls;
  # mdformat collapses them in place.
  defaultFormatterRules = [
    {
      pathGlob = "${config.home.homeDirectory}/.claude/plans/**/*.md";
      command = mdformatCommand;
      timeout = "5s";
    }
    {
      pathGlob = "${researchDir}/**/*.md";
      command = mdformatCommand;
      timeout = "5s";
    }
  ];

  extraDenyReadPaths = [ "/" ];

  extraReadPaths = [
    "/nix/store"
    "/nix/var/nix/profiles"

    "/etc"
    "/private/etc"
    "/usr"
    "/bin"
    "/sbin"

    # macOS frameworks and developer tools (CLT, Xcode toolchain).
    "/Library/Frameworks"
    "/Library/Apple"
    "/Library/Developer"
    "/System"

    # Narrow /dev set — blanket /dev would expose /dev/disk*, /dev/kmem,
    # /dev/mem. Mirrors the nodes Claude Code auto-allows for writes plus
    # common stdin/entropy/fd nodes.
    "/dev/null"
    "/dev/zero"
    "/dev/random"
    "/dev/urandom"
    "/dev/stdin"
    "/dev/stdout"
    "/dev/stderr"
    "/dev/tty"
    "/dev/fd"
    "/dev/dtracehelper"
    "/dev/autofs_nowait"

    "/tmp"
    "/private/tmp"

    # macOS dyld shared cache — every binary launch reads this.
    "/var/db/dyld"
    "/private/var/db/dyld"

    # macOS per-user temp. Claude Code overrides $TMPDIR, but Apple
    # frameworks (codesign, security, simctl, parts of Xcode CLT)
    # resolve via confstr(_CS_DARWIN_USER_TEMP_DIR) and still hit
    # /var/folders.
    "/var/folders"
    "/private/var/folders"

    # Runtime dirs. /run is the Linux standard; on macOS the same
    # role lives at /private/var/run (with /var/run symlinked to it).
    "/run"
    "/private/var/run"

    # macOS default shell selector. /var/select/sh is the symlink some
    # Apple tooling consults to resolve the system shell.
    "/var/select/sh"
    "/private/var/select/sh"

    "/opt/homebrew"

    "~/.config"
    "~/.cache"
    "~/.local/state"
    "~/.local/share"
    "~/.local/bin"
    "~/.claude"
    "~/.tflint.d"

    # ~/Library narrowed — blanket access leaks Keychains, Cookies,
    # Messages, Mail, Containers, Application Support subdirs, etc.
    "~/Library/Caches"
    "~/Library/Preferences"
    "~/Library/Fonts"
    "~/Library/Frameworks"

    "~/Documents/repos"
    "~/Documents/screenshots"
    "~/go"

    # home-manager `home.file` places these directly under $HOME, so
    # `~/.config/**` and friends don't cover them.
    "~/Taskfile.yaml"
    "~/task"

    # Global gitconfig and gitignore. Resolved via XDG, but listed
    # explicitly so git can read them even when the sandbox does not
    # follow the home-manager-files symlink chain into /nix/store.
    "~/.config/git/config"
    "~/.config/git/ignore"

    # Listed explicitly so the read list doesn't depend on the vaultsDir
    # default (which resolves under ~/Library/Mobile Documents/...).
    researchDir
  ]
  ++ bundledReadPaths;

  extraWritePaths = [
    "~/go/pkg"
    "~/Library/Caches"
    "~/.cache/nix"
    "~/.cache/helm"
    "~/.cache/gh"
    "~/.cache/uv"
    "~/.local/share/uv"
    # Recent gh releases also touch the config dir at runtime and fail
    # when it is read-only. Safe to expose: auth is env-var only (sops
    # GH_TOKEN), so gh never stores a token in hosts.yml, and the
    # hosts.yml Read deny below applies regardless.
    "~/.config/gh"
    "~/.local/state/workmux"
    "~/.local/state/hook-router"
    "~/.local/state/gh"
    "~/.local/share/claude"
    "~/.local/share/gh"
    "~/.tflint.d"

    # macOS per-user temp. Apple tooling (codesign, security, simctl,
    # Xcode CLT) resolves $TMPDIR via confstr(_CS_DARWIN_USER_TEMP_DIR)
    # and writes scratch files under /var/folders regardless of the
    # $TMPDIR Claude Code exports.
    "/var/folders"
    "/private/var/folders"

    researchDir
    cfg.archivesDir
  ]
  ++ claudeTmpPaths
  ++ bundledWritePaths;

  readPermEntries = map (p: "Read(${toPermGlob p})") extraReadPaths;

  # Edit(path) rules cover all file-editing tools (Write, Edit,
  # NotebookEdit); a separate Write(path) rule is not matched by file
  # permission checks and triggers a startup warning since Claude Code
  # 2.1.210.
  writePermEntries = lib.concatMap (p: [
    "Read(${toPermGlob p})"
    "Edit(${toPermGlob p})"
  ]) extraWritePaths;

  bundledInstructions =
    let
      items = lib.concatMap (b: b.instructions.items) bundleValues;
    in
    lib.optionalString (items != [ ]) (
      "## Tools\n\n" + lib.concatMapStringsSep "\n" (i: "- ${i}") items
    );
in
{
  imports = [ ./playwright.nix ];

  options.dotfiles.claude = {
    kubeApiDomains = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = ''
        Kubernetes API server hostnames that `mcp__kubectx__select`
        is allowed to switch to. The same set is added to the sandbox
        network allowlist (effective on darwin, where Seatbelt
        sandboxing is active) so kubectl can reach them. Empty (the
        default) lets the tool select any apiserver in the kubeconfig;
        sandbox network access still has to be granted elsewhere when
        needed.
      '';
    };

    kubeClusterRole = mkOption {
      type = types.str;
      default = "view";
      description = "ClusterRole to bind ServiceAccounts to when selecting a Kubernetes context.";
    };

    kubectxSocketSlots = mkOption {
      type = types.ints.positive;
      default = 16;
      description = ''
        Number of UDS slots `mcp-kubectx serve` may bind. Each slot is
        enumerated as a literal entry in the sandbox's allowUnixSockets
        list, since Claude Code matches that allowlist as exact paths
        rather than as globs. Bumping this allows more concurrent Claude
        sessions on this host before slot exhaustion fails new serve
        starts. Drives both the `--socket-slots` flag passed to the
        binary and the size of the rendered allowlist; the two cannot
        drift because they share this option.
      '';
    };

    guestKubeconfigLocal = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Treat the guest's own `~/.kube/config` as an in-sandbox
        "local" kubectx source. When enabled, the Claude launcher
        wrapper exports `$CLAUDE_KUBECTX_GUEST_CONFIG` and splices the
        guest config into the middle of the merged `$KUBECONFIG`
        (`$CLAUDE_KUBECTX_LOCAL : ~/.kube/config :
        $CLAUDE_KUBECTX_SIDECAR`), mcp-kubectx enumerates and routes
        its contexts as `(local)`, and the `~/.kube/config*` Read deny
        is dropped so the Read tool can see guest cluster definitions.
        A guest-local cluster (kind / k3d / minikube / Talos-in-Docker)
        then behaves like any normal cluster from any guest shell, with
        no per-consumer kubeconfig merge glue.

        Enable this ONLY on the Lima guest image that runs Claude
        inside the sandbox (`hosts/nixos/terrarium.nix`). On the macOS
        host and every other profile it must stay false: there
        `~/.kube/config` holds real admin credentials that must never
        enter the sandbox, the Read deny stays in force, and the merge
        keeps its byte-identical two-entry form.
      '';
    };

    dangerouslySkipPermissions = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Allow toggling Claude Code into bypass-permissions mode at
        runtime (Shift+Tab). Sets IS_SANDBOX=1, passes
        --allow-dangerously-skip-permissions, pre-trusts the home
        directory, and runs gh auth login.
      '';
    };

    skipPlanReview = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Skip the plan-reviewer deny gate on the first ExitPlanMode call.
        Normally hook-router denies the first ExitPlanMode of a session
        and instructs Claude to run the plan-reviewer agent first. With
        this enabled, that deny is skipped and ExitPlanMode proceeds
        without the review round-trip. All plan-guard bookkeeping (plan
        path, baseline SHA, clearing in_plan_mode, the pending-plan
        handoff) still happens, so the Stop hook's post-implementation
        review gate is unaffected.
      '';
    };

    enforceAsciiTypography = mkOption {
      type = types.bool;
      default = true;
      description = ''
        Deny Write/Edit calls that introduce non-ASCII
        typographic characters into a file: dash punctuation other than
        ASCII '-', the minus sign, curly quotation marks, and the
        horizontal ellipsis. Only newly introduced characters are
        blocked; characters already present in the file survive
        editing. The deny message nudges toward ASCII equivalents
        (restructure or "--" for dashes, straight quotes, "..." for
        ellipsis). When false, no PreToolUse matcher is registered for
        these tools and the wrapper omits the flag.
      '';
    };

    extraSettings = mkOption {
      type = types.attrsOf types.anything;
      default = { };
      description = "Additional settings merged into Claude Code settings.json.";
    };

    stylixTheme = mkOption {
      type = types.submodule {
        options = {
          enable = mkOption {
            type = types.bool;
            default = config.stylix.enable or false;
            description = "Generate ~/.claude/themes/stylix.json from the active stylix base16 scheme and select it as the Claude Code theme.";
          };
        };
      };
      default = { };
      description = "Wire Claude Code's custom-theme JSON to the active stylix base16 palette.";
    };

    hostContext = mkOption {
      type = types.lines;
      default = "";
      description = ''
        Host-specific prose appended to the generated ~/.claude/CLAUDE.md
        under a "## Host Environment" section. Use this to tell Claude
        Code about the environment it's running in (e.g. "You're in docker").
        Empty (the default) emits no section at all.
      '';
    };

    powerline = mkOption {
      type = types.submodule {
        options = {
          theme = mkOption {
            type = types.str;
            default = "custom";
            description = ''
              Top-level `theme` key in the emitted claude-powerline config,
              also used as the dynamic attr key under which `colors` is
              emitted (`colors.<theme>`). If you set `theme` to a value
              other than `custom`, also override `colors` — otherwise the
              stylix-derived default palette will be emitted under that
              theme name, shadowing any built-in palette claude-powerline
              ships.
            '';
          };

          colors = mkOption {
            type = types.attrsOf powerlineColorType;
            default = {
              directory = {
                bg = "#${colors.base09}";
                fg = "#${colors.base00}";
              };
              git = {
                bg = "#${colors.base02}";
                fg = "#${colors.base0E}";
              };
              model = {
                bg = "#${colors.base0B}";
                fg = "#${colors.base00}";
              };
              session = {
                bg = "#${colors.base01}";
                fg = "#${colors.base0C}";
              };
              block = {
                bg = "#${colors.base02}";
                fg = "#${colors.base0D}";
              };
              today = {
                bg = "#${colors.base00}";
                fg = "#${colors.base0B}";
              };
              tmux = {
                bg = "#${colors.base02}";
                fg = "#${colors.base0B}";
              };
              context = {
                bg = "#${colors.base0B}";
                fg = "#${colors.base00}";
              };
              contextWarning = {
                bg = "#${colors.base09}";
                fg = "#${colors.base00}";
              };
              contextCritical = {
                bg = "#${colors.base08}";
                fg = "#${colors.base00}";
              };
              metrics = {
                bg = "#${colors.base02}";
                fg = "#${colors.base05}";
              };
              version = {
                bg = "#${colors.base02}";
                fg = "#${colors.base04}";
              };
              env = {
                bg = "#${colors.base01}";
                fg = "#${colors.base0E}";
              };
              weekly = {
                bg = "#${colors.base01}";
                fg = "#${colors.base0D}";
              };
            };
            description = ''
              Per-segment colors keyed by claude-powerline segment name
              (directory, git, model, session, block, today, tmux,
              context, contextWarning, contextCritical, metrics, version,
              env, weekly), not theme name — the outer theme wrapping is
              applied at emit time. Overriding a segment requires setting
              both `bg` and `fg`; partial overrides fail module
              evaluation. Unknown segment names pass type-checking and
              are silently ignored by claude-powerline. The default
              palette is derived from `config.lib.stylix.colors`;
              replacing this option with a literal attrset decouples the
              status line from the host's base16 scheme.
            '';
          };

          display = mkOption {
            type = types.submodule {
              options = {
                style = mkOption {
                  type = types.str;
                  default = "powerline";
                  description = "Display style passed through to claude-powerline.";
                };
                charset = mkOption {
                  type = types.str;
                  default = "unicode";
                  description = "Character set passed through to claude-powerline.";
                };
                colorCompatibility = mkOption {
                  type = types.str;
                  default = "auto";
                  description = "Color compatibility mode passed through to claude-powerline.";
                };
                autoWrap = mkOption {
                  type = types.bool;
                  default = true;
                  description = "Whether claude-powerline auto-wraps lines that exceed the terminal width.";
                };
                padding = mkOption {
                  type = types.int;
                  default = 1;
                  description = "Padding applied between segments.";
                };
                lines = mkOption {
                  type = types.listOf (
                    types.submodule {
                      options.segments = mkOption {
                        type = types.attrsOf powerlineSegmentType;
                        default = { };
                        description = "Segments rendered on this line, keyed by segment name.";
                      };
                    }
                  );
                  default = [
                    {
                      segments = {
                        git = {
                          enabled = true;
                          showRepoName = true;
                        };
                        context = {
                          enabled = true;
                          showPercentageOnly = false;
                          displayStyle = "text";
                          autocompactBuffer = 100000;
                        };
                      };
                    }
                    {
                      segments = {
                        block = {
                          enabled = true;
                        };
                        weekly = {
                          enabled = true;
                        };
                      };
                    }
                  ];
                  description = ''
                    Lines rendered by claude-powerline, top-to-bottom.
                    Lists replace, not merge: a host that overrides this
                    option must supply the full layout. The same
                    replacement applies one level deeper — redefining a
                    single line's `segments` drops the other segments on
                    that line rather than merging. Segment names inside
                    `segments` must match the set claude-powerline
                    recognizes; unknown names pass type-checking and are
                    silently ignored.
                  '';
                };
              };
            };
            default = { };
            description = "claude-powerline `display` config block.";
          };

          budget = mkOption {
            type = types.submodule {
              options = {
                session = mkOption {
                  type = mkBudgetType {
                    amount = 220000;
                    type = "tokens";
                  };
                  default = { };
                  description = "Per-session budget.";
                };
                weekly = mkOption {
                  type = mkBudgetType {
                    amount = 1100;
                    type = "cost";
                  };
                  default = { };
                  description = "Weekly budget.";
                };
              };
            };
            default = { };
            description = "claude-powerline `budget` config block.";
          };
        };
      };
      default = { };
      description = "claude-powerline status line configuration emitted to ~/.config/claude-powerline/config.json.";
    };

    extraFetchRules = mkOption {
      type = types.submodule {
        options = {
          deny = mkOption {
            type = types.listOf denyRuleType;
            default = [ ];
            description = "Additional deny rules appended to the base mcp-fetch rules.";
          };
          allow = mkOption {
            type = types.listOf urlMatchType;
            default = [ ];
            description = "Additional allow rules appended to the base mcp-fetch allowlist.";
          };
          robotsExempt = mkOption {
            type = types.listOf urlMatchType;
            default = [ ];
            description = ''
              Additional URL patterns exempted from mcp-fetch's robots.txt
              enforcement. Enforcement stays on for every host not named
              here, so exempting one site never disables it globally.
            '';
          };
        };
      };
      default = { };
      description = "Extra mcp-fetch URL filtering rules merged with the base deny, allow, and robots-exemption lists.";
    };

    extraCommandRules = mkOption {
      type = types.submodule {
        options = {
          deny = mkOption {
            type = types.listOf commandRuleType;
            default = [ ];
            description = ''
              Extra bash command deny rules, appended after the
              bundle-contributed deny rules in hook-router's
              evaluation order. No `allow` counterpart exists:
              hook-router has only deny and ask rules with optional
              per-rule `except` exemptions; non-matching commands
              fall through to the normal permission flow.
            '';
          };
          ask = mkOption {
            type = types.listOf commandRuleType;
            default = [ ];
            description = ''
              Extra bash command ask rules, appended after the
              bundle-contributed ask rules in hook-router's
              evaluation order. All deny rules are evaluated before
              any ask rule.
            '';
          };
        };
      };
      default = { };
      description = "Extra hook-router command deny/ask rules appended after bundle-contributed rules.";
    };

    formatterRules = mkOption {
      type = types.listOf formatterRuleType;
      default = [ ];
      description = ''
        Extra hook-router formatter routing rules, appended after the
        built-in plans and research mdformat rules. Each rule maps an
        absolute file-path glob to a formatter argv; the matched path
        is appended as the final argument. Rules are evaluated in
        order on PostToolUse:Write/Edit and the first
        matching glob wins.
      '';
    };

    outputCompaction = mkOption {
      type = types.submodule {
        options = {
          enable = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether hook-router compacts redundant successful Bash
              output on PostToolUse:Bash. Failing commands route to
              PostToolUseFailure, which carries no output, so this is
              a success-path-only optimization.
            '';
          };
          stripAnsi = mkOption {
            type = types.bool;
            default = true;
            description = "Whether to strip ANSI/VT escape sequences (color, cursor, OSC) from the surfaced output.";
          };
          minRunLength = mkOption {
            type = types.ints.positive;
            default = 3;
            description = ''
              Minimum number of consecutive byte-identical lines before a
              run is collapsed to one line plus a marker. Shorter runs
              pass through verbatim.
            '';
          };
          minBytes = mkOption {
            type = types.ints.unsigned;
            default = 2048;
            description = ''
              Skip compaction for outputs smaller than this many bytes,
              checked against the raw pre-strip length. Keeps the
              transforms off small outputs that carry no redundancy
              worth collapsing.
            '';
          };
          streams = mkOption {
            type = types.listOf (
              types.enum [
                "stdout"
                "stderr"
              ]
            );
            default = [
              "stdout"
              "stderr"
            ];
            description = ''
              Which tool_response output streams to compact. An empty
              list compacts nothing (equivalent to disabling the
              feature).
            '';
          };
          saveFullOutput = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether hook-router archives the uncompacted stream to a
              file under ''${XDG_STATE_HOME}/hook-router/outputs before
              compacting it, appending a one-line pointer to the
              surfaced output that names the file so the model can read
              back the exact ANSI and repeated lines compaction drops.
              When false, compaction stays lossy: no file is written and
              no pointer is appended. This toggle gates the
              --compaction-output-dir wrapper flag and is not serialized
              into --compaction-config.
            '';
          };
        };
      };
      default = { };
      description = ''
        hook-router PostToolUse:Bash output compaction. camelCase keys so
        `builtins.toJSON` matches the Go struct tags in
        tools/hook-router/compactor.go CompactConfig.
      '';
    };

    searchRewrite = mkOption {
      type = types.submodule {
        options = {
          enable = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether hook-router transparently rewrites `grep` and
              `find` Bash commands into `rg` and `bfs` on
              PreToolUse:Bash. When false, no --search-rewrite-config
              flag is passed and the wrapper leaves search commands
              untouched.
            '';
          };
          grep = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether to rewrite `grep` into `rg`, mapping the known
              flag set and injecting exclude globs. Conservative:
              commands using an unmapped flag or a BRE pattern that
              would mis-translate are left untouched.
            '';
          };
          find = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether to rewrite `find` into `bfs` with a global
              `-exclude` prune of findExcludes injected after the
              command word.
            '';
          };
          findExcludes = mkOption {
            type = types.listOf types.str;
            default = [
              ".git"
              ".worktrees"
              ".claude/worktrees"
            ];
            description = ''
              Directories pruned from both rewrites. The single source
              of truth shared by the hook (via --search-rewrite-config)
              and the interactive fish `find` wrapper. A basename entry
              (no slash) prunes by name; a slash-bearing entry prunes by
              path. Each entry becomes a bfs `-exclude` clause and an rg
              `-g` exclude glob.
            '';
          };
        };
      };
      default = { };
      description = ''
        hook-router PreToolUse:Bash search rewriting. camelCase keys so
        `builtins.toJSON` matches the Go struct tags in
        tools/hook-router/searchrewrite/searchrewrite.go Config.
      '';
    };

    sleepGuard = mkOption {
      type = types.submodule {
        options = {
          enable = mkOption {
            type = types.bool;
            default = true;
            description = ''
              Whether hook-router denies a foreground `sleep` on
              PreToolUse:Bash. A `sleep` whose duration is not a
              literal (`sleep $VAR`, `sleep $((5*60))`) is always
              denied, since its length cannot be read. A command whose
              only content is `sleep` plus no-op filler (`echo`,
              `true`, `jobs`) is denied at any duration as a filler
              wait, so the ceiling only governs sleeps embedded in real
              work. A Bash call with `run_in_background` set is never
              checked: a poll loop inside a background call is the
              documented way to wait on a condition, so `sleep` there
              is correct rather than a smell.
            '';
          };
          maxSeconds = mkOption {
            type = types.ints.positive;
            default = 10;
            description = ''
              Longest foreground `sleep` embedded in real work that
              stays allowed, in seconds. Durations sum across operands
              (`sleep 5 10` is 15s) and GNU suffixes count (`5m` is
              300s).
            '';
          };
        };
      };
      default = { };
      description = ''
        hook-router PreToolUse:Bash foreground-`sleep` guard. camelCase
        keys so `builtins.toJSON` matches the Go struct tags in
        tools/hook-router/sleepguard/sleepguard.go Config.
      '';
    };

    fetchAllowlist = mkOption {
      type = types.bool;
      default = true;
      description = "Whether to enforce the mcp-fetch URL allowlist. When false, all URLs are allowed unless explicitly denied.";
    };

    skills = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            enable = mkOption {
              type = types.bool;
              default = true;
              description = "Whether this skill is installed into ~/.claude/skills/<name>.";
            };
            source = mkOption {
              type = types.either types.path types.lines;
              description = "Skill directory path, or raw SKILL.md contents.";
            };
          };
        }
      );
      default = { };
      description = "Claude Code skills keyed by skill name. Defaults populated by this module; override `<name>.enable = false` to drop a bundled skill, or set `<name>.source` to add a custom one.";
    };

    agents = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            enable = mkOption {
              type = types.bool;
              default = true;
              description = "Whether this agent is installed into ~/.claude/agents/<name>.md.";
            };
            source = mkOption {
              type = types.either types.path types.lines;
              description = "Agent markdown file path, or raw agent body contents.";
            };
          };
        }
      );
      default = { };
      description = "Claude Code agents keyed by agent name (no .md suffix). Defaults populated by this module; override `<name>.enable = false` to drop a bundled agent, or set `<name>.source` to add a custom one.";
    };

    postImplSkills = mkOption {
      type = types.attrsOf (
        types.submodule (
          { name, ... }:
          {
            options = {
              enable = mkOption {
                type = types.bool;
                default = true;
                description = "Whether this option is offered in the Stop-gate post-implementation AskUserQuestion catalog.";
              };
              label = mkOption {
                type = types.str;
                default = "/${name}";
                description = "Slash-command label Claude dispatches when the user picks this option. Includes the leading slash.";
              };
              description = mkOption {
                type = types.str;
                description = "One-line description shown alongside the label in the post-implementation picker.";
              };
            };
          }
        )
      );
      default = { };
      description = "Post-implementation slash commands offered when a plan's Stop gate fires. Defaults populated by this module; override `<name>.enable = false` to drop a bundled option, or add an entry with `<name>.description` to extend the catalog. The label defaults to `/<name>`; set `<name>.label` to override.";
    };

    extraMcpServers = mkOption {
      type = types.attrsOf types.anything;
      default = { };
      description = "Additional MCP servers deep-merged into programs.mcp.servers.";
    };

    remoteControl = lib.mkEnableOption "Claude Code remote control for all sessions";

    research = {
      vault = mkOption {
        type = types.nonEmptyStr;
        default = "docs";
        description = "Obsidian vault name (under dotfiles.obsidian.vaultsDir) where the research skill writes reports.";
      };
      useVault = mkOption {
        type = types.bool;
        default = pkgs.stdenv.hostPlatform.isDarwin;
        description = "Whether CLAUDE_RESEARCH_DIR resolves to the Obsidian vault. Defaults to true on Darwin. Set true on Linux hosts that have the vault mounted at the same absolute path as the Darwin host (e.g. terrarium inside a workmux sandbox).";
      };
    };

    archivesDir = mkOption {
      type = types.nonEmptyStr;
      default = "${config.home.homeDirectory}/Documents/archives";
      description = ''
        Output directory for the web-archive skill (btrix, yt-dlp).
        Added to the sandbox write allowlist and bind-mounted into
        the Lima sandbox at the same host and guest path.
      '';
    };

    attribution = mkOption {
      type = types.submodule {
        options = {
          commit = mkOption {
            type = types.str;
            default = "";
            description = "Attribution footer appended to commit messages. Empty string disables attribution.";
          };
          pr = mkOption {
            type = types.str;
            default = "";
            description = "Attribution footer appended to pull request descriptions. Empty string disables attribution.";
          };
        };
      };
      default = { };
      description = "Per-host attribution strings for commits and PRs authored via Claude Code.";
    };

    lima = {
      enable = lib.mkEnableOption "Lima sandbox backend";
      cpus = mkOption {
        type = types.int;
        default = 8;
        description = "Number of CPUs allocated to the Lima VM.";
      };
      memory = mkOption {
        type = types.str;
        default = "8GiB";
        description = "Memory allocated to the Lima VM.";
      };
      disk = mkOption {
        type = types.str;
        default = "80GiB";
        description = "Disk size allocated to the Lima VM.";
      };
    };

    workmux = {
      agent = mkOption {
        type = types.str;
        default = "claude";
        description = "Value emitted for workmux's top-level agent key in ~/.config/workmux/config.yaml. Workmux uses this label to pick the output-pattern profile for status routing (working / waiting / done) and to select which agents.<name> entry defines the launch command for the focused pane's <agent> placeholder. Override on hosts that drive workmux with a different coding agent.";
      };
      command = mkOption {
        type = types.str;
        default = "claude --permission-mode plan";
        description = "Launch command registered as the agents.<agent> entry in workmux's config and consumed via the <agent> placeholder in the focused pane. Decoupled from agent because flag conventions vary between coding agents.";
      };
    };

    extraPermissions = mkOption {
      type = types.submodule {
        options = {
          allow = mkOption {
            type = types.listOf types.str;
            default = [ ];
            description = "Additional tool patterns appended to the permissions allow list.";
          };
          deny = mkOption {
            type = types.listOf types.str;
            default = [ ];
            description = "Additional tool patterns appended to the permissions deny list.";
          };
          ask = mkOption {
            type = types.listOf types.str;
            default = [ ];
            description = "Additional tool patterns appended to the permissions ask list.";
          };
        };
      };
      default = { };
      description = "Additional permission entries appended to the base allow/deny/ask lists.";
    };

    toolBundles = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            enable = mkOption {
              type = types.bool;
              default = true;
              description = "Whether this tool bundle is enabled.";
            };
            servers = mkOption {
              type = types.attrsOf types.anything;
              default = { };
              description = "MCP server definitions merged into programs.mcp.servers.";
            };
            permissions = {
              allow = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Tool patterns appended to the permissions allow list.";
              };
              deny = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Tool patterns appended to the permissions deny list.";
              };
              ask = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Tool patterns appended to the permissions ask list.";
              };
            };
            sandbox = {
              allowedDomains = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Network domains to add to the sandbox allowlist.";
              };
              allowUnixSockets = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Unix socket paths to add to the sandbox allowlist.";
              };
              allowRead = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Filesystem paths to add to the sandbox read allowlist.";
              };
              allowWrite = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Filesystem paths to add to the sandbox write allowlist.";
              };
            };
            instructions = {
              items = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Instruction lines rendered as bullets under the `## Tools` heading in ~/.claude/CLAUDE.md.";
              };
            };
            alwaysLoad = mkOption {
              type = types.bool;
              default = false;
              description = ''
                Set `alwaysLoad = true` on every server in this bundle so its
                tool schemas load at session start rather than being deferred
                behind Claude Code's ToolSearch. Use sparingly — each upfront
                tool consumes context that would otherwise be available for
                the conversation. See
                https://code.claude.com/docs/en/mcp#configure-tool-search.
              '';
            };
            fetchRules = {
              deny = mkOption {
                type = types.listOf denyRuleType;
                default = [ ];
                description = ''
                  mcp-fetch deny rules contributed by this bundle. Aggregated
                  at `enable` granularity, so redirect messages that point at
                  this bundle's MCP tools automatically turn off when the
                  bundle is disabled.
                '';
              };
              allow = mkOption {
                type = types.listOf urlMatchType;
                default = [ ];
                description = "mcp-fetch allow rules contributed by this bundle.";
              };
              robotsExempt = mkOption {
                type = types.listOf urlMatchType;
                default = [ ];
                description = ''
                  URL patterns this bundle exempts from mcp-fetch's
                  robots.txt enforcement. Enforcement stays on for every
                  host not named here.
                '';
              };
            };
            commandRules = {
              deny = mkOption {
                type = types.listOf commandRuleType;
                default = [ ];
                description = ''
                  hook-router PreToolUse:Bash deny rules contributed by
                  this bundle. Aggregated at `enable` granularity, so
                  redirect messages that point at this bundle's MCP
                  tools turn off automatically when the bundle is
                  disabled.
                '';
              };
              ask = mkOption {
                type = types.listOf commandRuleType;
                default = [ ];
                description = ''
                  hook-router PreToolUse:Bash ask rules contributed by
                  this bundle. A matching command gets a forced
                  permission prompt, even when settings allow rules or
                  sandbox auto-allow would otherwise let it run. List
                  order matters (first match wins): scope rules to
                  subcommands before a catch-all fallback for the same
                  command. All deny rules are evaluated before any ask
                  rule.
                '';
              };
            };
          };
        }
      );
      default = { };
      description = "Tool bundles grouping MCP servers, permissions, sandbox rules, mcp-fetch rules, and CLAUDE.md instructions.";
    };
  };

  config = {
    dotfiles.claude.skills = {
      commit.source = ../configs/claude/skills/commit;
      commit-push-pr.source = ../configs/claude/skills/commit-push-pr;
      create-skill.source = ../configs/claude/skills/create-skill;
      dagger-modules.source = ../configs/claude/skills/dagger-modules;
      file-manager.source = ../configs/claude/skills/file-manager;
      frontend-design.source = ../configs/claude/skills/frontend-design;
      worktree.source = ../configs/claude/skills/worktree;
      wm-merge.source = ../configs/claude/skills/wm-merge;
      wm-rebase.source = ../configs/claude/skills/wm-rebase;
      wm-coordinator.source = ../configs/claude/skills/wm-coordinator;
      wm-spawn.source = ../configs/claude/skills/wm-spawn;
      wm-workmux.source = ../configs/claude/skills/wm-workmux;
      git-surgeon.source = ../configs/claude/skills/git-surgeon;
      presenterm.source = ../configs/claude/skills/presenterm;
      prose.source = ../configs/claude/skills/prose;
      research.source = ../configs/claude/skills/research;
      review-implementation.source = ../configs/claude/skills/review-implementation;
      self-improve.source = ../configs/claude/skills/self-improve;
      taskfile.source = ../configs/claude/skills/taskfile;
      technical-writing.source = ../configs/claude/skills/technical-writing;
      web-archive.source = ../configs/claude/skills/web-archive;
    };

    dotfiles.claude.agents = {
      implementation-reviewer-code.source = ../configs/claude/agents/implementation-reviewer-code.md;
      implementation-reviewer-docs.source = ../configs/claude/agents/implementation-reviewer-docs.md;
      plan-reviewer.source = ../configs/claude/agents/plan-reviewer.md;
    };

    dotfiles.claude.postImplSkills = {
      review-implementation.description = "Review code changes against the plan.";
      simplify.description = "Review and simplify the implemented code.";
      commit.description = "Wrap up the cycle by creating a git commit.";
    };

    dotfiles.claude.toolBundles = {
      fetch = {
        alwaysLoad = true;
        servers.fetch = {
          type = "stdio";
          command = "${pkgs.mcp-fetch}/bin/mcp-fetch";
          args = [
            "--db"
            "${config.xdg.stateHome}/mcp-fetch/fetches.db"
            "--rules-file"
            "${fetchRules}"
            "--log-file"
            "${config.xdg.stateHome}/mcp-fetch/fetch.log"
          ];
        };
        permissions.allow = [ "mcp__fetch__fetch" ];
        permissions.deny = [
          "WebSearch"
          "WebFetch"
        ];
        # Reddit's robots.txt is a blanket "Disallow: /" on every host it
        # serves, aimed at bulk scrapers. These fetches are human-directed
        # and one page at a time, so the exemption is scoped to reddit.com
        # rather than turning enforcement off globally.
        fetchRules.robotsExempt = [ { host = "(.*\\.)?reddit\\.com"; } ];
        # www.reddit.com answers non-browser clients with a bot-verification
        # interstitial; old.reddit.com serves the real thread markup.
        fetchRules.deny = [
          {
            host = "(www\\.)?reddit\\.com";
            reason = "www.reddit.com serves a bot-verification page instead of content. Fetch the same path on old.reddit.com instead.";
          }
        ];
        instructions = {
          items = [
            "Use `mcp__fetch__fetch` for fetching known URLs and web page content."
            "Fetch Reddit threads from `old.reddit.com`; `www.reddit.com` returns a verification page, and the `.json` and `.rss` endpoints are rate-limited."
          ];
        };
      };

      git =
        let
          # Git subcommands with no mutating mode: every form is a
          # query, so the exact + glob pair is safe. Subcommands with a
          # write mode (apply, archive, bundle, format-patch,
          # hash-object, symbolic-ref, bisect, gc, update-ref, fsck)
          # are absent on purpose and fall through to the normal
          # prompt. fetch is included deliberately as an accepted risk:
          # the common forms only update remote-tracking refs, though
          # refspec forms (git fetch origin main:main, +refs/heads/*)
          # can write local branches. Known write/exec escapes inside
          # these subcommands include --output= on diff/log/show,
          # grep -O, and --upload-pack= on fetch/ls-remote; all obscure
          # enough to accept. The cherry pair cannot match
          # git cherry-pick: the glob requires the trailing space.
          gitReadCommands = [
            "blame"
            "cat-file"
            "check-ignore"
            "cherry"
            "count-objects"
            "describe"
            "diff"
            "diff-tree"
            "fetch"
            "for-each-ref"
            "grep"
            "help"
            "log"
            "ls-files"
            "ls-remote"
            "ls-tree"
            "merge-base"
            "name-rev"
            "range-diff"
            "rev-list"
            "rev-parse"
            "shortlog"
            "show"
            "show-ref"
            "status"
            "version"
          ];

          # Mixed subcommands: only the listed forms read. Spelled out
          # one by one because a `git <group> *` glob would also cover
          # the mutating verbs (git remote set-url, git branch -D,
          # git config <key> <value>, git reflog expire). The =-joined
          # flag variants sit alongside the space-separated ones
          # because permission globs match literally and the = forms
          # are what actually gets typed.
          gitReadForms = [
            "--version"
            "remote"
            "remote -v"
            "remote --verbose"
            "remote show"
            "remote show *"
            "remote get-url *"
            "branch"
            "branch -a"
            "branch --all"
            "branch -r"
            "branch --remotes"
            "branch -v"
            "branch -vv"
            "branch --list"
            "branch --list *"
            "branch --show-current"
            "branch --contains *"
            "branch --merged *"
            "branch --no-merged *"
            "branch --format *"
            "branch --format=*"
            "branch --sort *"
            "branch --sort=*"
            "tag"
            "tag -l"
            "tag -l *"
            "tag -n *"
            "tag -n*"
            "tag --list"
            "tag --list *"
            "tag --contains *"
            "tag --contains=*"
            "tag --points-at *"
            "tag --points-at=*"
            "tag --sort *"
            "tag --sort=*"
            "stash list"
            "stash list *"
            "stash show"
            "stash show *"
            "config --get *"
            "config --get-all *"
            "config --get-regexp *"
            "config --list"
            "config --list *"
            "config -l"
            "config -l *"
            "config get *"
            "config list"
            "config list *"
            "worktree list"
            "worktree list *"
            "reflog"
            "reflog show"
            "reflog show *"
            "submodule status"
            "submodule status *"
            "notes list"
            "notes list *"
            "notes show *"
          ];

          gitAllowPair = cmd: [
            "Bash(git ${cmd})"
            "Bash(git ${cmd} *)"
          ];
        in
        {
          alwaysLoad = true;
          servers.git = {
            type = "stdio";
            command = "${gitWrapper}";
            args = [
              "--allow-dir"
              "/tmp/git"
              "--allow-dir"
              "/private/tmp/git"
              # git_fetch / git_pull / git_push operate on working
              # checkouts, which live here. The allowlist is shared
              # with git_clone's dest check, so clones into this tree
              # are also allowed; the instruction below still points
              # clone at /tmp/git.
              "--allow-dir"
              "${config.home.homeDirectory}/Documents/repos"
              # Linux VMs (lima/terrarium) mount the macOS repos tree
              # at its original /Users path; allow that spelling too.
              # On darwin this duplicates the entry above, which is
              # harmless.
              "--allow-dir"
              "/Users/${config.home.username}/Documents/repos"
            ];
          };
          permissions.allow = [
            "mcp__git__git_clone"
            "mcp__git__git_fetch"
            "mcp__git__git_pull"
          ]
          # Read-only git derived from the gitReadCommands /
          # gitReadForms tables above. These matter on hosts where the
          # sandbox auto-allow is off; on auto-allow hosts hook-router
          # already allows them. An ask entry like Bash(git remote *)
          # would shadow every one of these (permission rules evaluate
          # deny -> ask -> allow, regardless of specificity), so the
          # ask list below stays narrow and the mutating git remote
          # verbs are gated by commandRules.ask instead.
          ++ lib.concatMap gitAllowPair gitReadCommands
          ++ map (form: "Bash(git ${form})") gitReadForms;
          # Next to the allow table so shadowing is visible: the Bash
          # forms were moved here from the top-level ask list, and the
          # exact MCP name cannot shadow the MCP allow entries above.
          # No entry overlaps the allow table.
          permissions.ask = [
            "mcp__git__git_push"
            "Bash(git push)"
            "Bash(git push *)"
            "Bash(git switch *)"
          ];
          sandbox.allowWrite = [
            "/tmp/git"
            "/private/tmp/git"
          ];
          # Mirrored by canonicalRules in
          # tools/hook-router/{helpers,cmdrules/cmdrules}_test.go;
          # update together.
          commandRules.deny = [
            {
              command = "git";
              args = [ "clone" ];
              reason = "Direct git clone usage is blocked. Use mcp__git__git_clone instead.";
            }
            {
              command = "git";
              args = [ "stash" ];
              except = [
                "pop"
                "apply"
                "list"
                "show"
                "branch"
                "drop"
                "clear"
              ];
              reason = "Do not use git stash to shelve changes. All issues in the working tree are your responsibility to fix, regardless of origin.";
            }
          ];
          # Replaces the old top-level Bash(git remote *) ask entry,
          # which shadowed every remote read form above. exceptBare
          # lets a bare `git remote` (a listing) through. Deliberately
          # no top-level git catch-all: unlike gh, local git mutations
          # (add, commit, restore) are routine and would prompt
          # constantly.
          commandRules.ask = [
            {
              command = "git";
              args = [ "remote" ];
              except = [
                "-v"
                "--verbose"
                "show"
                "get-url"
                "-h"
                "--help"
              ];
              exceptBare = true;
              reason = "This git remote subcommand rewrites where the repository pushes and fetches. Confirm before running.";
            }
          ];
          fetchRules.deny = [
            {
              host = "raw\\.githubusercontent\\.com";
              except = [ { path = ".*\\.md"; } ];
              reason = "Use mcp__git__git_clone to clone the repo to /tmp/git/<owner>/<repo> and read files locally instead of fetching raw GitHub files.";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/(blob|tree)(/.*)?";
              reason = "Use mcp__git__git_clone to clone the repo to /tmp/git/<owner>/<repo> and read files locally instead of fetching GitHub file pages.";
            }
            {
              host = "gitlab\\.com";
              path = "/.+/-/(blob|tree)(/.*)?";
              reason = "Use mcp__git__git_clone to clone the repo to /tmp/git/<owner>/<repo> and read files locally instead of fetching GitLab file pages.";
            }
            {
              host = "codeberg\\.org";
              path = "/[^/]+/[^/]+/src/(branch|commit|tag)/.*";
              reason = "Use mcp__git__git_clone to clone the repo to /tmp/git/<owner>/<repo> and read files locally instead of fetching Codeberg file pages.";
            }
          ];
          instructions = {
            items = [
              "Use `mcp__git__git_clone` to clone repositories into `/tmp/git/<owner>/<repo>` and read from there."
              "Use `mcp__git__git_fetch`, `mcp__git__git_pull`, and `mcp__git__git_push` for git operations that contact a remote; plain `git` in Bash handles local work (add, commit, branch, rebase)."
            ];
          };
        };

      kagi = {
        alwaysLoad = true;
        servers.kagi = {
          type = "stdio";
          command = "${kagiWrapper}";
        };
        permissions.allow = [ "mcp__kagi__kagi_search_fetch" ];
        # kagi_extract overlaps with mcp__fetch__fetch, which stays the
        # preferred page fetcher.
        permissions.deny = [ "mcp__kagi__kagi_extract" ];
        fetchRules.deny = [
          {
            host = "(google|bing|duckduckgo|brave)\\.com";
            reason = "Fetching from general-purpose search engines is blocked. Use mcp__kagi__kagi_search_fetch instead.";
          }
        ];
        instructions = {
          items = [
            "Use `mcp__kagi__kagi_search_fetch` for web searches."
          ];
        };
      };

      nixos = {
        servers.nixos = {
          type = "stdio";
          command = "${pkgs.mcp-nixos}/bin/mcp-nixos";
        };
        permissions.allow = [
          "mcp__nixos__nix"
          "mcp__nixos__nix_versions"
        ];
        fetchRules.deny = [
          {
            host = "search\\.nixos\\.org";
            path = "/(packages|options)(\\?.*)?";
            reason = "Use mcp__nixos__nix for Nix package searches and NixOS/home-manager/nix-darwin option lookups instead of scraping search.nixos.org.";
          }
          {
            host = "home-manager-options\\.extranix\\.com";
            reason = "Use mcp__nixos__nix to look up home-manager options instead of scraping home-manager-options.extranix.com.";
          }
        ];
      };

      github =
        let
          # gh read subcommands that have a mcp__github__* equivalent.
          # These are denied at the hook level and redirected to the MCP
          # tool named in the value, so reads flow through the github MCP
          # rather than the gh CLI. Write commands live in
          # ghWriteRedirectGroups below. Mirrored by ghRedirectRules in
          # tools/hook-router/{helpers,cmdrules/cmdrules}_test.go; update
          # all together.
          ghRedirectGroups = {
            issue = {
              view = "mcp__github__issue_read";
              list = "mcp__github__list_issues";
            };
            pr = {
              view = "mcp__github__pull_request_read";
              list = "mcp__github__list_pull_requests";
              diff = "mcp__github__pull_request_read (diff method)";
            };
            release = {
              view = "mcp__github__get_release_by_tag / mcp__github__get_latest_release";
              list = "mcp__github__list_releases";
            };
            label = {
              list = "mcp__github__list_label";
            };
            run = {
              view = "mcp__github__actions_get (get_workflow_run) / mcp__github__get_job_logs for logs";
              list = "mcp__github__actions_list (list_workflow_runs)";
            };
            workflow = {
              view = "mcp__github__actions_get (get_workflow)";
              list = "mcp__github__actions_list (list_workflows)";
            };
          };
          ghRedirectCommands = {
            search = "mcp__github__search_code / search_issues / search_pull_requests / search_repositories";
          };

          # gh write subcommands whose mutation a github MCP tool in
          # permissions.ask covers. These are denied at the hook level and
          # redirected to the MCP tool named in the value, which prompts on
          # each call, so the confirmation moves from the gh CLI ask rule
          # to the MCP permission prompt. Writes with no equivalent (pr
          # comment, run delete, workflow enable/disable, issue writes,
          # ...) stay with the ask rules below. These leaves stay out of
          # the ask-rule except lists on purpose: the deny rules win
          # first, and dropping one falls back to a prompt rather than an
          # allow. Mirrored by ghWriteRedirectRules in
          # tools/hook-router/{helpers,cmdrules/cmdrules}_test.go; update
          # all together.
          ghWriteRedirectGroups = {
            pr = {
              create = "mcp__github__create_pull_request";
              edit = "mcp__github__update_pull_request";
              close = "mcp__github__update_pull_request (state: closed)";
              reopen = "mcp__github__update_pull_request (state: open)";
              ready = "mcp__github__update_pull_request (draft: false)";
              merge = "mcp__github__merge_pull_request";
              review = "mcp__github__pull_request_review_write (+ add_comment_to_pending_review for inline comments)";
              "update-branch" = "mcp__github__update_pull_request_branch";
            };
            run = {
              rerun = "mcp__github__actions_run_trigger (rerun_workflow_run / rerun_failed_jobs)";
              cancel = "mcp__github__actions_run_trigger (cancel_workflow_run)";
            };
            workflow = {
              run = "mcp__github__actions_run_trigger (run_workflow)";
            };
          };

          # Read-only gh subcommands with no mcp__github__* equivalent
          # (repo metadata, PR checks/status, run watching, notification
          # status). The MCP does not serve these, so they stay
          # allowed on the gh CLI. `gh run watch` stays here because it
          # streams a run to completion, which no MCP tool does. Single
          # source of truth for the permission allow entries below.
          # Mirrored by ghAskRules in
          # tools/hook-router/{helpers,cmdrules/cmdrules}_test.go.
          ghAllowGroups = {
            cache = [ "list" ];
            pr = [
              "checks"
              "status"
            ];
            repo = [
              "view"
              "list"
            ];
            run = [ "watch" ];
          };
          ghAllowCommands = [
            "status"
          ];

          # Every group carrying a read-only leaf (allowed or redirected),
          # and its full read-only leaf set. The ask rules exempt these
          # leaves so only mutating subcommands prompt; redirected leaves
          # (read and write) are already caught by the deny rules, which
          # hook-router evaluates before any ask rule.
          ghGroups = lib.attrNames (ghAllowGroups // ghRedirectGroups);
          ghReadLeaves =
            group: (ghAllowGroups.${group} or [ ]) ++ lib.attrNames (ghRedirectGroups.${group} or { });

          ghAllowPair = prefix: [
            "Bash(gh ${prefix})"
            "Bash(gh ${prefix} *)"
          ];
        in
        {
          servers.github = {
            type = "stdio";
            command = "${githubWrapper}";
          };
          # Every read-only tool the x/all endpoint serves that
          # permissions.deny below does not strip. Spelled out so a tool
          # added upstream lands outside this list and prompts on first
          # use rather than running unannounced.
          permissions.allow = [
            "mcp__github__actions_get"
            "mcp__github__actions_list"
            "mcp__github__check_dependency_vulnerabilities"
            "mcp__github__get_code_quality_finding"
            "mcp__github__get_code_scanning_alert"
            "mcp__github__get_copilot_space"
            "mcp__github__get_dependabot_alert"
            "mcp__github__get_discussion"
            "mcp__github__get_discussion_comments"
            "mcp__github__get_gist"
            "mcp__github__get_global_security_advisory"
            "mcp__github__get_job_logs"
            "mcp__github__get_label"
            "mcp__github__get_latest_release"
            "mcp__github__get_notification_details"
            "mcp__github__get_release_by_tag"
            "mcp__github__get_secret_scanning_alert"
            "mcp__github__get_tag"
            "mcp__github__github_support_docs_search"
            "mcp__github__issue_read"
            "mcp__github__list_code_scanning_alerts"
            "mcp__github__list_copilot_spaces"
            "mcp__github__list_dependabot_alerts"
            "mcp__github__list_discussion_categories"
            "mcp__github__list_discussions"
            "mcp__github__list_gists"
            "mcp__github__list_global_security_advisories"
            "mcp__github__list_issue_fields"
            "mcp__github__list_issue_types"
            "mcp__github__list_issues"
            "mcp__github__list_label"
            "mcp__github__list_notifications"
            "mcp__github__list_org_repository_security_advisories"
            "mcp__github__list_pull_requests"
            "mcp__github__list_releases"
            "mcp__github__list_repository_security_advisories"
            "mcp__github__list_secret_scanning_alerts"
            "mcp__github__list_starred_repositories"
            "mcp__github__list_tags"
            "mcp__github__projects_get"
            "mcp__github__projects_list"
            "mcp__github__pull_request_read"
            "mcp__github__search_code"
            "mcp__github__search_commits"
            "mcp__github__search_issues"
            "mcp__github__search_orgs"
            "mcp__github__search_pull_requests"
            "mcp__github__search_repositories"
            "mcp__github__semantic_issue_similarity_search"
          ]
          # Read-only gh CLI commands with no MCP equivalent, derived
          # from the ghAllowGroups / ghAllowCommands tables above. These
          # cover hosts where the sandbox auto-allow is off; mutating gh
          # commands are caught by the commandRules.ask rules below, and
          # reads with an MCP equivalent by the commandRules.deny rules,
          # which prompt / redirect on every host. An ask entry like
          # Bash(gh *) would shadow all of these (permission rules
          # evaluate deny -> ask -> allow, regardless of specificity), so
          # gh must not appear in the ask list.
          ++ lib.concatLists (
            lib.mapAttrsToList (
              group: leaves: lib.concatMap (leaf: ghAllowPair "${group} ${leaf}") leaves
            ) ghAllowGroups
          )
          ++ lib.concatMap ghAllowPair ghAllowCommands;
          # This list is the single source of truth for the github MCP tool
          # filter: the proxy wrapper derives its --deny-tool flags from it
          # (see ghProxyDenyFlags), so denying a tool here both blocks the
          # call and strips the tool's schema from tools/list, reclaiming the
          # context tokens it would otherwise cost.
          permissions.deny = [
            # GitHub MCP: deny redundant / low-value tools.
            "mcp__github__get_commit"
            "mcp__github__get_copilot_job_status"
            "mcp__github__get_file_contents"
            "mcp__github__get_me"
            # Browsing a repository's paths over the API is the same
            # detour the fetch rules and the gh api deny close off; clone
            # with mcp__git__git_clone and read the tree locally.
            "mcp__github__get_repository_tree"
            "mcp__github__get_team_members"
            "mcp__github__get_teams"
            "mcp__github__list_branches"
            "mcp__github__list_commits"
            "mcp__github__list_repository_collaborators"
            "mcp__github__search_users"
            # Overlaps mcp__kagi__kagi_search_fetch, which stays the one
            # web search tool.
            "mcp__github__web_search"
            # GitHub MCP: deny every write/mutating tool except the pull
            # request set in permissions.ask below. The proxy filter strips
            # the denied tools' schemas from tools/list.
            "mcp__github__add_issue_comment"
            "mcp__github__assign_copilot_to_issue"
            "mcp__github__create_branch"
            "mcp__github__create_gist"
            "mcp__github__create_or_update_file"
            "mcp__github__create_pull_request_with_copilot"
            "mcp__github__create_repository"
            "mcp__github__delete_file"
            "mcp__github__dismiss_notification"
            "mcp__github__fork_repository"
            "mcp__github__issue_write"
            "mcp__github__label_write"
            "mcp__github__manage_notification_subscription"
            "mcp__github__manage_repository_notification_subscription"
            "mcp__github__mark_all_notifications_read"
            "mcp__github__projects_write"
            "mcp__github__push_files"
            "mcp__github__request_copilot_review"
            "mcp__github__star_repository"
            "mcp__github__sub_issue_write"
            "mcp__github__unstar_repository"
            "mcp__github__update_gist"
            "mcp__github__run_secret_scanning"
          ];
          # Pull request and Actions write tools, the only write surface
          # the MCP serves here. Each call prompts, matching the ask gating
          # on mutating gh CLI commands. add_comment_to_pending_review
          # rides along with pull_request_review_write: a pending review
          # needs both. actions_run_trigger triggers, re-runs, and cancels
          # workflow runs.
          permissions.ask = [
            "mcp__github__actions_run_trigger"
            "mcp__github__add_comment_to_pending_review"
            "mcp__github__add_reply_to_pull_request_comment"
            "mcp__github__create_pull_request"
            "mcp__github__merge_pull_request"
            "mcp__github__pull_request_review_write"
            "mcp__github__update_pull_request"
            "mcp__github__update_pull_request_branch"
          ];
          # Redirect read-only gh subcommands that have a github MCP
          # equivalent to the MCP, derived from the ghRedirectGroups /
          # ghRedirectCommands / ghWriteRedirectGroups tables above.
          # hook-router evaluates deny rules before ask rules, so these
          # win over the mutating-command ask fallback below and over the
          # sandbox auto-allow. Subcommands with no MCP equivalent stay on
          # gh: reads run, writes prompt via the ask rules.
          commandRules.deny =
            # `gh api` is the CLI form of fetching api.github.com, which
            # the fetchRules below deny outright, and its
            # repos/*/contents/* endpoints are the same repository-file
            # browsing the git bundle denies on raw.githubusercontent.com
            # and github.com blob/tree pages. Denied as a whole because
            # the rule matcher compares positional args literally and
            # cannot scope on an endpoint path. Mirrored by
            # ghRedirectRules in
            # tools/hook-router/{helpers,cmdrules/cmdrules}_test.go.
            [
              {
                command = "gh";
                args = [ "api" ];
                reason = "`gh api` reaches api.github.com, which is denied. Clone with mcp__git__git_clone to read repository files locally, or use the mcp__github__* tools for issues, PRs, releases, and search.";
              }
            ]
            ++ lib.concatLists (
              lib.mapAttrsToList (
                group: leaves:
                lib.mapAttrsToList (leaf: tool: {
                  command = "gh";
                  args = [
                    group
                    leaf
                  ];
                  reason = "Read via ${tool} instead of the gh CLI.";
                }) leaves
              ) ghRedirectGroups
            )
            ++ lib.mapAttrsToList (cmd: tool: {
              command = "gh";
              args = [ cmd ];
              reason = "Read via ${tool} instead of the gh CLI.";
            }) ghRedirectCommands
            ++ lib.concatLists (
              lib.mapAttrsToList (
                group: leaves:
                lib.mapAttrsToList (leaf: tool: {
                  command = "gh";
                  args = [
                    group
                    leaf
                  ];
                  reason = "Write via ${tool} instead of the gh CLI.";
                }) leaves
              ) ghWriteRedirectGroups
            );
          # Fail-closed gating for the gh CLI, derived from the
          # ghGroups / ghAllowCommands tables above: read-only subcommands
          # fall through (redirected by the deny rules above, allowed by
          # the permission entries above, or by the sandbox auto-allow),
          # everything else gets a forced prompt -- including gh
          # subcommands that do not exist yet. Order matters:
          # subcommand-scoped rules run before the top-level fallback.
          commandRules.ask =
            map (group: {
              command = "gh";
              args = [ group ];
              except = ghReadLeaves group;
              reason = "This gh subcommand can mutate GitHub state. Confirm before running.";
            }) ghGroups
            ++ [
              {
                command = "gh";
                except =
                  ghGroups
                  ++ ghAllowCommands
                  ++ [
                    "help"
                    "version"
                    "--version"
                  ];
                reason = "This gh subcommand is not on the read-only allowlist. Confirm before running; prefer mcp__github__* tools for reads.";
              }
            ];
          fetchRules.deny = [
            {
              host = "api\\.github\\.com";
              reason = "-> mcp__github__* / mcp__git__*";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/issues(/.*)?";
              reason = "-> mcp__github__list_issues / mcp__github__issue_read";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/pulls?(/.*)?";
              reason = "-> mcp__github__list_pull_requests / mcp__github__pull_request_read";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/(commit|compare)(/.*)?";
              reason = "-> mcp__git__git_clone -> git show / git log";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/releases(/.*)?";
              reason = "-> mcp__github__list_releases / mcp__github__get_latest_release";
            }
            {
              host = "github\\.com";
              path = "/[^/]+/[^/]+/tags(/.*)?";
              reason = "-> mcp__github__list_tags / mcp__github__get_tag";
            }
            {
              host = "github\\.com";
              path = "/search(/.*)?";
              reason = "-> mcp__github__search_*";
            }
          ];
          instructions = {
            items = [
              "Use `mcp__github__*` and `mcp__git__*` tools for reading GitHub data (issues, PRs, releases, Actions runs and job logs, security alerts, discussions, projects, code search, etc.)"
            ];
          };
        };

      opentofu = {
        servers.opentofu = {
          type = "stdio";
          command = "${pkgs.mcp-opentofu}/bin/mcp-opentofu";
        };
        permissions.allow = [
          "mcp__opentofu__search_registry"
          "mcp__opentofu__get_provider_details"
          "mcp__opentofu__get_module_details"
          "mcp__opentofu__get_resource_docs"
          "mcp__opentofu__get_datasource_docs"
        ];
        sandbox.allowedDomains = [
          "api.opentofu.org"
          "get.opentofu.org"
          "registry.opentofu.org"
        ];
        sandbox.allowWrite = [
          "~/.terraform.versions"
          # tofu init populates ~/.terraform.d/plugins (filesystem
          # mirror) and ~/.terraform.d/plugin-cache (shared download
          # cache when TF_PLUGIN_CACHE_DIR is set). Both need
          # read+write so providers can be downloaded and loaded.
          # Sibling ~/.terraform.d/credentials.tfrc.json stays
          # unreachable -- it is not under either subdirectory and
          # is additionally denied at the permission layer.
          "~/.terraform.d/plugins"
          "~/.terraform.d/plugin-cache"
        ];
        sandbox.allowRead = [ "~/.tfswitch.toml" ];
        # hashicorp/go-plugin (used by tofu providers and tflint plugins)
        # binds a Unix domain socket for IPC with the parent process. The
        # claude wrapper sets PLUGIN_UNIX_SOCKET_DIR=~/.terraform.versions
        # so tofu's socket lands in a directory the sandbox can both write
        # to and bind within. tflint ignores PLUGIN_UNIX_SOCKET_DIR and
        # always uses os.TempDir(), so a separate tflint wrapper points
        # TMPDIR at ~/.tflint.d/tmp; ~/.tflint.d is added here so sockets
        # inside it can be bound.
        sandbox.allowUnixSockets = [
          "~/.terraform.versions"
          "~/.tflint.d"
        ];
        fetchRules.deny = [
          {
            host = "registry\\.opentofu\\.org";
            path = "/(providers|modules)(/.*)?";
            reason = "Use mcp__opentofu__* tools instead of fetching OpenTofu Registry pages.";
          }
          {
            host = "registry\\.terraform\\.io";
            path = "/(providers|modules)(/.*)?";
            reason = "Use mcp__opentofu__* tools instead of fetching Terraform Registry pages.";
          }
        ];
      };

      spacelift = {
        servers.spacelift = {
          type = "stdio";
          command = "${spaceliftWrapper}";
          args = [
            "mcp"
            "server"
          ];
        };
        permissions.allow = [
          "mcp__spacelift__get_authentication_guide"
          "mcp__spacelift__get_blueprint"
          "mcp__spacelift__get_context"
          "mcp__spacelift__get_graphql_type_details"
          "mcp__spacelift__get_module"
          "mcp__spacelift__get_module_guide"
          "mcp__spacelift__get_module_version"
          "mcp__spacelift__get_policy"
          "mcp__spacelift__get_policy_sample"
          "mcp__spacelift__get_space"
          "mcp__spacelift__get_stack_run"
          "mcp__spacelift__get_stack_run_changes"
          "mcp__spacelift__get_stack_run_logs"
          "mcp__spacelift__get_worker_pool"
          "mcp__spacelift__introspect_graphql_schema"
          "mcp__spacelift__list_blueprints"
          "mcp__spacelift__list_contexts"
          "mcp__spacelift__list_module_versions"
          "mcp__spacelift__list_modules"
          "mcp__spacelift__list_policies"
          "mcp__spacelift__list_policy_samples"
          "mcp__spacelift__list_policy_samples_indexed"
          "mcp__spacelift__list_resources"
          "mcp__spacelift__list_spaces"
          "mcp__spacelift__list_stack_proposed_runs"
          "mcp__spacelift__list_stack_runs"
          "mcp__spacelift__list_stacks"
          "mcp__spacelift__list_worker_pools"
          "mcp__spacelift__local_preview"
          "mcp__spacelift__search_contexts"
          "mcp__spacelift__search_graphql_schema_fields"
          "mcp__spacelift__search_modules"
        ];
        permissions.ask = [
          "mcp__spacelift__trigger_stack_run"
          "mcp__spacelift__discard_stack_run"
          "mcp__spacelift__confirm_stack_run"
        ];
        permissions.deny = [
          "mcp__spacelift__list_api_keys"
          "mcp__spacelift__get_api_key"
        ];
        commandRules.deny = [
          {
            command = "spacectl";
            args = [
              "stack"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_stacks` instead of `spacectl stack list`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "show"
            ];
            reason = "Use `mcp__spacelift__list_stacks` instead of `spacectl stack show`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "run"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_stack_runs` instead of `spacectl stack run list`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "logs"
            ];
            reason = "Use `mcp__spacelift__get_stack_run_logs` instead of `spacectl stack logs`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "changes"
            ];
            reason = "Use `mcp__spacelift__get_stack_run_changes` instead of `spacectl stack changes`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "resources"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_resources` instead of `spacectl stack resources list`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "confirm"
            ];
            reason = "Use `mcp__spacelift__confirm_stack_run` instead of `spacectl stack confirm`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "discard"
            ];
            reason = "Use `mcp__spacelift__discard_stack_run` instead of `spacectl stack discard`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "local-preview"
            ];
            reason = "Use `mcp__spacelift__local_preview` instead of `spacectl stack local-preview`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "preview"
            ];
            reason = "Use `mcp__spacelift__trigger_stack_run` instead of `spacectl stack preview`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "deploy"
            ];
            reason = "Use `mcp__spacelift__trigger_stack_run` instead of `spacectl stack deploy`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "retry"
            ];
            reason = "Use `mcp__spacelift__trigger_stack_run` instead of `spacectl stack retry`.";
          }
          {
            command = "spacectl";
            args = [
              "stack"
              "replan"
            ];
            reason = "Use `mcp__spacelift__trigger_stack_run` instead of `spacectl stack replan`.";
          }
          {
            command = "spacectl";
            args = [
              "module"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_modules` instead of `spacectl module list`.";
          }
          {
            command = "spacectl";
            args = [
              "module"
              "list-versions"
            ];
            reason = "Use `mcp__spacelift__list_module_versions` instead of `spacectl module list-versions`.";
          }
          {
            command = "spacectl";
            args = [
              "blueprint"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_blueprints` instead of `spacectl blueprint list`.";
          }
          {
            command = "spacectl";
            args = [
              "blueprint"
              "show"
            ];
            reason = "Use `mcp__spacelift__get_blueprint` instead of `spacectl blueprint show`.";
          }
          {
            command = "spacectl";
            args = [
              "policy"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_policies` instead of `spacectl policy list`.";
          }
          {
            command = "spacectl";
            args = [
              "policy"
              "show"
            ];
            reason = "Use `mcp__spacelift__get_policy` instead of `spacectl policy show`.";
          }
          {
            command = "spacectl";
            args = [
              "policy"
              "samples"
            ];
            reason = "Use `mcp__spacelift__list_policy_samples` instead of `spacectl policy samples`.";
          }
          {
            command = "spacectl";
            args = [
              "policy"
              "sample"
            ];
            reason = "Use `mcp__spacelift__get_policy_sample` instead of `spacectl policy sample`.";
          }
          {
            command = "spacectl";
            args = [
              "policy"
              "samples-indexed"
            ];
            reason = "Use `mcp__spacelift__list_policy_samples_indexed` instead of `spacectl policy samples-indexed`.";
          }
          {
            command = "spacectl";
            args = [
              "workerpool"
              "list"
            ];
            reason = "Use `mcp__spacelift__list_worker_pools` instead of `spacectl workerpool list`.";
          }
          {
            # Catch-all deny
            command = "spacectl";
            reason = "Do not invoke `spacectl` directly. Use the `mcp__spacelift__*` tools instead.";
          }
        ];
        instructions = {
          items = [
            "Use `mcp__spacelift__*` tools for Spacelift operations. Do not invoke `spacectl` directly."
          ];
        };
      };

      kubectx = {
        servers.kubectx = {
          type = "stdio";
          command = "${pkgs.mcp-kubectx}/bin/mcp-kubectx";
          args = [
            "serve"
            "--sa-role-name"
            cfg.kubeClusterRole
            "--sa-cluster-scoped"
            "--log-file"
            "${config.xdg.stateHome}/mcp-kubectx/kubectx.log"
            "--socket-slots"
            (toString cfg.kubectxSocketSlots)
          ]
          ++ lib.concatMap (host: [
            "--allow-apiserver-host"
            host
          ]) cfg.kubeApiDomains;
        };
        permissions.allow = [
          "mcp__kubectx__list"
          "mcp__kubectx__select"
        ];
        commandRules.deny = [
          {
            command = "kubectx";
            reason = "Do not use kubectx or kubens directly. Use mcp__kubectx__list to list contexts and mcp__kubectx__select to switch contexts.";
          }
          {
            command = "kubens";
            reason = "Do not use kubectx or kubens directly. Use mcp__kubectx__list to list contexts and mcp__kubectx__select to switch contexts.";
          }
        ];
        sandbox.allowedDomains = cfg.kubeApiDomains;
        # Per-`serve` UDS: kubectl's exec credential plugin
        # (`mcp-kubectx exec-plugin --socket <path>`) connects here
        # instead of forking out to `host token` itself, so the
        # plugin can run inside Claude's bash sandbox without
        # tripping the `~/.kube/config` read deny.
        #
        # Claude Code's allowUnixSockets matcher is literal-only:
        # entries must be exact paths, not globs. We enumerate one
        # entry per slot (host + guest variant of each) so the
        # rendered allowlist is 1:1 with the slot range
        # `mcp-kubectx serve` walks at startup. Both env tags are
        # listed because the same bundle entry flows into both the
        # host (Darwin) and guest (Lima) profiles.
        sandbox.allowUnixSockets = lib.concatMap (slot: [
          "${config.xdg.stateHome}/mcp-kubectx-run/serve.${toString slot}.host.sock"
          "${config.xdg.stateHome}/mcp-kubectx-run/serve.${toString slot}.guest.sock"
        ]) (lib.genList lib.id cfg.kubectxSocketSlots);
        instructions = {
          items = [
            "Use `mcp__kubectx__list` to see available Kubernetes contexts."
            "Use `mcp__kubectx__select` to activate a context before running kubectl commands."
          ];
        };
      };

      claude-ai-integrations = {
        # Claude.ai web-app integration auth tools with no configured server here.
        # Denied defensively in case such a server gets wired up by accident.
        permissions.deny = [
          "mcp__claude_ai_Asana__authenticate"
          "mcp__claude_ai_Asana__complete_authentication"
          "mcp__claude_ai_Gmail__authenticate"
          "mcp__claude_ai_Gmail__complete_authentication"
          "mcp__claude_ai_Google_Calendar__authenticate"
          "mcp__claude_ai_Google_Calendar__complete_authentication"
          "mcp__claude_ai_Google_Drive__authenticate"
          "mcp__claude_ai_Google_Drive__complete_authentication"
        ];
      };
    };

    programs = {
      mcp = {
        enable = true;
        servers = injectCaEnv (lib.recursiveUpdate bundledServers cfg.extraMcpServers);
      };

      claude-code = {
        enable = true;
        package = claudeWrapped;

        settings = lib.recursiveUpdate (
          {
            env = {
              CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS = "1";
              # Restore one level of nested subagent delegation. Claude
              # Code 2.1.217 caps subagents at depth 1 by default, which
              # stops a general-purpose/claude subagent (and in-process
              # team teammates) from fanning out their own subagents.
              CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH = "2";
              # Keep /code-review in the session it was typed in. As of
              # Claude Code 2.1.229 it is the only command running in a
              # "fork" context, which spawns its orchestrator into a
              # backgrounded sub-conversation whose results often never
              # land. This flag routes it inline and has it report
              # through the ReportFindings tool instead. The read site
              # is a bare Boolean(), so reverting means deleting this
              # line -- "0" still reads as truthy.
              CLAUDE_CODE_REPORT_FINDINGS = "1";
            };
            # Prompt cache TTL. The main conversation holds a 1-hour
            # cache so long thinking pauses and slow tool runs still hit
            # it; everything spawned around it (subagents, workflows,
            # background helpers) keeps the cheaper 5-minute writes,
            # since those turns are short-lived anyway. Unset would mean
            # "automatic", which already picks 1h for a subscription --
            # setting both pins the split explicitly.
            promptCacheTtl = "1h";
            subagentPromptCacheTtl = "5m";
            disableAutoMode = "disable";
            includeGitInstructions = false;
            respondToBashCommands = true;
            inherit (cfg) attribution;
            permissions = {
              defaultMode = "plan";
              allow = readPermEntries ++ writePermEntries ++ bundledAllow ++ cfg.extraPermissions.allow;
              deny = [
                # Key material & certificates
                "Read(//**/*.key)"
                "Read(//**/*.p12)"
                "Read(//**/*.jks)"
                "Read(//**/*.asc)"
                "Read(//**/*.keystore)"
                "Read(//**/*.kdbx)"
                "Read(//**/wallet.dat)"
                "Read(//**/keystore/**)"
                "Read(//**/.ssh/**)"
                "Read(//**/.gnupg/**)"

                # Generic secrets
                "Read(//**/.env)"
                "Read(//**/.env.*)"
                "Read(//**/.secrets/**)"
                "Read(//**/.git-credentials)"
                "Read(//**/git/credentials)"
                "Read(//**/.netrc)"
                "Read(//**/.curlrc)"
                "Read(//**/.wgetrc)"
                "Read(//**/.password-store/**)"

                # Cloud credentials
                "Read(//**/.aws/credentials)"
                "Read(//**/.aws/config)"
                "Read(//**/.aws/sso/**)"
                "Read(//**/.azure/**)"
                "Read(//**/.config/gcloud/**)"
                "Read(//**/.config/hcloud/config.json)"
                "Read(//**/.config/argocd/**)"
                "Read(//**/.snyk)"
                "Read(//**/.wrangler/**)"

                # Container & Kubernetes
                "Read(//**/.docker/config.json)"
                "Read(//**/.docker/certs.d/**)"
                "Read(//**/.config/containers/auth.json)"
                "Read(//**/.talos/**)"
                "Read(//**/.cosign/**)"
                "Read(//**/.helm/repository/repositories.yaml)"

                # Secret managers & encryption
                "Read(//**/.doppler/**)"
                "Read(//**/age/keys.txt)"
                "Read(//**/rclone.conf)"
                "Read(//**/.op/**)"
                "Read(//**/.config/op/**)"

                # IaC state & credentials
                "Read(//**/credentials.tfrc.json)"
                "Read(//**/.terraformrc)"
                "Read(//**/.terraform.d/credentials.tfrc.json)"
                "Read(//**/*.tfstate)"
                "Read(//**/*.tfstate.*)"
                "Read(//**/.pulumi/credentials.json)"

                # CI/CD & deployment tokens
                "Read(//**/.config/gh/hosts.yml)"
                "Read(//**/.jira.d/config.yml)"
                "Read(//**/.config/dagger/**)"
                "Read(//**/.config/spacelift/**)"
                "Read(~/.spacelift/**)"

                # Package manager credentials
                "Read(//**/.npmrc)"
                "Read(//**/.pypirc)"
                "Read(//**/.cargo/credentials.toml)"
                "Read(//**/.gem/credentials)"
                "Read(//**/.m2/settings.xml)"
                "Read(//**/.m2/settings-security.xml)"
                "Read(//**/.gradle/gradle.properties)"
                "Read(//**/.composer/auth.json)"
                "Read(//**/.config/poetry/auth.toml)"
                "Read(//**/.bunfig.toml)"

                # Claude Code credentials
                "Read(//**/.claude/.credentials.json)"

                # Developer tool sessions & generated keys
                "Read(//**/atuin/key)"
                "Read(//**/.lima/_config/user)"
              ]
              # On the Lima guest image ~/.kube/config is a readable
              # local kubectx source (guestKubeconfigLocal); everywhere
              # else it holds host admin creds and stays denied. The
              # sidecar and scoped kubeconfigs live under
              # $CLAUDE_KUBECTX_DIR / $XDG_STATE_HOME, not any .kube/
              # path, so they are unaffected either way.
              ++ lib.optionals (!cfg.guestKubeconfigLocal) [
                "Read(//**/.kube/config)"
                "Read(//**/.kube/config*)"
              ]
              ++ bundledDeny
              ++ cfg.extraPermissions.deny;
              # git entries live in the git tool bundle, next to the
              # allow table they would otherwise shadow (permission
              # rules evaluate deny -> ask -> allow, regardless of
              # specificity).
              ask = bundledAsk ++ cfg.extraPermissions.ask;
            };
            worktree.bgIsolation = "none";
            statusLine = {
              type = "command";
              command = "${pkgs.claude-powerline}/bin/claude-powerline";
              padding = 0;
            };
            enabledPlugins = {
              "skill-creator@claude-plugins-official" = true;
            };
            sandbox = {
              enabled = sandboxEnabled;
              failIfUnavailable = true;
              allowUnsandboxedCommands = false;
              # Allow access to the system TLS trust service.
              enableWeakerNetworkIsolation = true;
              network = {
                allowLocalBinding = true;
                allowUnixSockets = [
                  "/nix/var/nix/daemon-socket/socket"
                  (
                    if config.dotfiles.tmux.socketPath != null then
                      config.dotfiles.tmux.socketPath
                    else
                      "/private/tmp/tmux-501/default"
                  )
                ]
                ++ bundledSockets;
                allowedDomains = [
                  "jacobcolvin.com"
                  "registry.dagger.io"
                  "api.dagger.cloud"
                  "auth.dagger.cloud"
                  "proxy.golang.org"
                  "sum.golang.org"
                ]
                ++ bundledDomains;
              };
              filesystem = {
                denyRead = extraDenyReadPaths;
                allowRead = lib.unique (extraReadPaths ++ extraWritePaths);
                allowWrite = extraWritePaths;
              };
            };
            hooks =
              let
                # hook-router enforces its own 45s context timeout
                # (tools/hook-router/main.go); 60s is a backstop for a
                # hung process, not a working budget.
                routerTimeout = 60;
                # Exec form: with `args` present Claude Code resolves
                # `command` as an executable and spawns it directly, so no
                # shell parses the store path or the flags.
                router = event: tool: {
                  type = "command";
                  command = lib.getExe hookRouter;
                  args = [
                    "--event"
                    event
                  ]
                  ++ lib.optionals (tool != null) [
                    "--tool"
                    tool
                  ];
                  timeout = routerTimeout;
                };
                # workmux hooks emit nothing Claude Code reads, so they
                # run in the background and never block a turn.
                status = s: {
                  type = "command";
                  command = "${workmux} ${s}";
                  async = true;
                };
              in
              {
                # NOTE: All matching hooks run concurrently with the original input.
                # Only one hook per tool should return updatedInput to avoid
                # non-deterministic last-writer-wins races.
                PreToolUse = [
                  {
                    matcher = "Bash";
                    hooks = [ (router "PreToolUse" "Bash") ];
                  }
                  {
                    matcher = "ExitPlanMode";
                    hooks = [ ((router "PreToolUse" "ExitPlanMode") // { statusMessage = "plan gate"; }) ];
                  }
                  {
                    matcher = "EnterPlanMode";
                    hooks = [ (router "PreToolUse" "EnterPlanMode") ];
                  }
                  {
                    # All MCP tools route to one hook entry; "MCP" is a
                    # routing sentinel and hook-router reads the real
                    # tool name from the payload. Only this matcher
                    # matches mcp__* names, so there is exactly one
                    # decision emitter per MCP call.
                    matcher = "mcp__.*";
                    hooks = [ (router "PreToolUse" "MCP") ];
                  }
                ]
                ++ lib.optionals cfg.enforceAsciiTypography [
                  {
                    # Write and Edit route to one entry; "FileWrite" is a
                    # routing sentinel and hook-router reads the real
                    # tool name from the payload. Emits only deny or
                    # nothing (never updatedInput), so the last-writer-wins
                    # race the note above warns about cannot occur here.
                    # Anchored: matchers are unanchored regexes, and a
                    # bare `Edit` would also match NotebookEdit.
                    matcher = "^(Write|Edit)$";
                    hooks = [ (router "PreToolUse" "FileWrite") ];
                  }
                ];
                UserPromptSubmit = [
                  { hooks = [ (status "working") ]; }
                  {
                    # Claude Code caps UserPromptSubmit command hooks at
                    # 30s by default; keep that rather than the 60s
                    # backstop.
                    hooks = [ ((router "UserPromptSubmit" null) // { timeout = 30; }) ];
                  }
                ];
                Notification = [
                  {
                    matcher = "permission_prompt|elicitation_dialog";
                    hooks = [ (status "waiting") ];
                  }
                ];
                PostToolUse = [
                  # Matcher-less on purpose: any tool result must flip the
                  # window back from `waiting` to `working`.
                  { hooks = [ (status "working") ]; }
                  {
                    # Mirrors hook-router's PostToolUse switch
                    # (tools/hook-router/main.go). Every other tool is a
                    # no-op there, so the matcher saves a process spawn
                    # per call. Bash output compaction returns
                    # updatedToolOutput, which Claude reads, so this entry
                    # stays synchronous. Extend this list when a handler
                    # is added, unless the handler returns nothing Claude
                    # reads, which belongs on the entry below.
                    matcher = "^(AskUserQuestion|Bash)$";
                    hooks = [ (router "PostToolUse" null) ];
                  }
                  {
                    # The formatter is the whole PostToolUse handler for
                    # Write and Edit and returns nothing Claude reads, so
                    # it runs off the critical path. This buys latency and
                    # not failure reporting: asyncRewake wakes Claude only
                    # on exit code 2, hook-router exits 0 or 1
                    # (tools/hook-router/main.go), and handlePostFileWrite
                    # swallows formatter failures after a warn log
                    # (tools/hook-router/filewrite.go), so failures stay in
                    # the hook-router log. Reporting them needs an exit-2
                    # path in Go.
                    #
                    # Passes no --tool: FileWrite is a PreToolUse routing
                    # sentinel, and the PostToolUse switch reads the real
                    # tool name from the payload.
                    matcher = "^(Write|Edit)$";
                    hooks = [ ((router "PostToolUse" null) // { asyncRewake = true; }) ];
                  }
                ];
                # Fires when a tool call throws, not when one exits
                # non-zero: the Bash tool returns normally on a non-zero
                # exit and folds the code into stderr. The realistic
                # trigger here is a sandbox violation, which is exactly
                # when the window should ask for attention. Permission
                # denials and pre-execution rejections do not fire it, so
                # hook-router's own denies never reach it.
                PostToolUseFailure = [
                  { hooks = [ (status "waiting") ]; }
                ];
                Stop = [
                  { hooks = [ (status "done") ]; }
                  { hooks = [ ((router "Stop" null) // { statusMessage = "post-impl gate"; }) ]; }
                ];
                # Turn ended on an API error (rate limit, auth, overloaded,
                # ...). workmux has no error status; `waiting` is the one
                # that asks for attention and auto-clears on focus.
                StopFailure = [
                  { hooks = [ (status "waiting") ]; }
                ];
                SessionStart = [
                  { hooks = [ (router "SessionStart" null) ]; }
                ];
                SessionEnd = [
                  {
                    # SessionEnd hooks share a 1.5s budget by default; a
                    # per-hook timeout raises it (up to 60s). 10s gives the
                    # kubectx session-dir cleanup room without delaying
                    # exit noticeably.
                    hooks = [ ((router "SessionEnd" null) // { timeout = 10; }) ];
                  }
                ];
                # Unanchored on purpose: a matcher of bare names and pipes
                # takes Claude Code's literal path, splitting on `|` and
                # testing exact membership, and `^(...)$` would force it
                # onto the regex path instead.
                #
                # Synchronous rather than async: the script remaps its
                # failures off exit code 2, the only code that blocks a
                # config change, so it cannot block. A synchronous failure
                # still prints stderr where an async one would vanish into
                # a background log.
                ConfigChange = [
                  {
                    matcher = "user_settings|local_settings|skills";
                    hooks = [
                      {
                        type = "command";
                        command = lib.getExe configChangeLog;
                      }
                    ];
                  }
                ];
              };
            autoMemoryEnabled = false;
            alwaysThinkingEnabled = true;
            skipDangerousModePermissionPrompt = true;
            teammateMode = "in-process";
            showThinkingSummaries = true;
            showClearContextOnPlanAccept = true;
            fileCheckpointingEnabled = true;
            todoFeatureEnabled = true;
            askUserQuestionTimeout = "never";
            showTurnDuration = true;
            terminalProgressBarEnabled = true;
            autoCompactEnabled = true;
            autoCompactWindow = 666666;
            # Straight-ASCII prompt input: no :shortcode: emoji expansion,
            # matching enforceAsciiTypography and the plain-ASCII policy.
            emojiCompletionEnabled = false;
          }
          // lib.optionalAttrs cfg.stylixTheme.enable {
            theme = "custom:stylix";
          }
        ) cfg.extraSettings;

        agents = lib.mapAttrs (_: a: a.source) (lib.filterAttrs (_: a: a.enable) cfg.agents);

        skills = lib.mapAttrs (_: s: s.source) (lib.filterAttrs (_: s: s.enable) cfg.skills);
      };

    };

    xdg.configFile = {
      "claude-powerline/config.json".text = claudePowerlineConfig;
      "workmux/config.yaml".source = workmuxConfig;
    };

    dotfiles.extraInventoryPackages = [
      pkgs.hook-router
      pkgs.mcp-git
      pkgs.mcp-http-proxy
    ];

    home = {
      packages = [
        pkgs.chief
        pkgs.llm-agents.ccusage
        pkgs.mcp-fetch
        pkgs.mcp-kubectx
        workmuxWrapped
        pkgs.claude-history
        pkgs.git-surgeon
        pkgs.slugify
      ];

      file.".claude/themes/stylix.json" = lib.mkIf cfg.stylixTheme.enable {
        source = claudeStylixTheme;
      };

      file.".claude/CLAUDE.md".text = ''
        ## Agents
        - Use `Agent({..., model: "<model>"})` to downgrade models for tasks where reasoning is not required.
        - Explore agents (`subagent_type: "Explore"`) inherit the session model by default; downgrade to `model: "sonnet"` or `model: "haiku"`.
        - Plan agents (`subagent_type: "Plan"`) MUST never be downgraded; omit `model` so they inherit the session model.
        - For generic agents, use your best judgement based on the task being assigned.

        ## Shell
        - `cd` persists between Bash calls only while it stays inside the project directory; a `cd` outside it resets the cwd to the project root on the next call.
        - Never wait by sleeping; a foreground poll loop is the wrong shape even when each sleep is short.
      ''
      + lib.optionalString cfg.sleepGuard.enable ''
        - A hook denies foreground waiting: a filler command that only passes time (`sleep 6`, `sleep 1; echo waiting`) at any duration, a `sleep` over ${toString cfg.sleepGuard.maxSeconds}s even inside real work, and a `sleep` whose duration it cannot read (`sleep $VAR`). A short `sleep` is only for a settle step inside a command doing real work (`kill "$pid"; sleep 1; pgrep -f server`).
        - Do not bide time with no-op filler (`true`, `jobs`, bare `echo`) while a background task runs; end the turn instead -- the completion notification arrives on its own, and idle turns only delay it.
      ''
      + ''
        - Long-running work belongs in a Bash call with `run_in_background: true`. The session stays free, you get a notification when the command exits, and `Read` fetches its captured output. `sleep` inside a background call is fine, so an `until <check>; do sleep 1; done` poll loop there is the right way to wait on a condition.
        - For one notification per event rather than one on completion, use the `Monitor` tool. It is deferred, so load it with `ToolSearch("select:Monitor")` before calling it.
        - Fire off independent work in parallel, then act on completion notifications as they arrive. A spawn-one, wait, spawn-the-next loop serializes work that could have run at once.

        ## Python
        - Python is always available: `uv` manages a default interpreter on every host, so `python3` and `uv run` work with no setup.
        - Reach for a Python script instead of bash once logic outgrows a simple pipeline: parsing structured data beyond a `jq`/`yq` one-liner, arithmetic or date math, multi-step text transforms, anything wanting data structures or real error handling. A short script in the scratchpad beats a fragile chain of shell escapes.
        - Scripts that need third-party packages declare them inline with PEP 723 metadata (`# /// script` block with `dependencies = [...]`) and run via `uv run script.py`; uv resolves and caches the packages automatically. Never `pip install` into a shared environment.
        - One-liners that need a package: `uv run --with <pkg> python -c '...'`.

        ## Installed CLI Tools
        - `coreutils` are ALWAYS the GNU versions: sed, awk, grep, find, diff, patch, tar, and make on PATH all come from nix's GNU releases, never BSD system tools. Use GNU syntax freely (`sed -i`, `date -d`, `readlink -f`, `sort -V`, `grep -P`), you do not need to write BSD fallbacks.
        - `rust-parallel` runs commands in parallel: `rust-parallel -j4 cmd ::: a b c`, args from stdin, `-s` for shell pipelines. Per-job output is buffered, so results never interleave. Prefer it over `xargs -P` and over bare `parallel` (from moreutils).
        - `yq` (Go yq) queries and edits YAML/TOML/XML with `jq`-style syntax (`jq` is also available).
        - `agrind` (angle-grinder) slices and aggregates large log files with a query language; prefer it over long grep/awk pipelines.
        - `sponge` (moreutils) soaks up a pipeline's output before writing, allowing safe in-place file rewrites; `ts` prepends timestamps to lines.
        - Any nixpkgs package can be used without installing it: `nix run nixpkgs#<pkg> -- <args>` for a one-shot program, `nix shell nixpkgs#<pkg> --command <cmd>` when the binary name differs from the attribute or you need several packages at once. The bare `nixpkgs#` ref resolves to this flake's pinned nixpkgs, so cache hits are near-certain and nothing persists in a profile.

        ## Plan Mode
        - Writing untracked files is allowed in plan mode: scratch notes, files under /tmp, and repo clones via `mcp__git__git_clone` are all fine. Only files tracked by git are off-limits until the plan is approved.
        - Every plan ends with a `## Skills` section listing the skills the implementer invokes before starting work, one bullet per skill with the reason it applies (for example `prose` for any documentation or commit messages, `taskfile` for Taskfile edits). Write `None` when no skill applies. The implementer invokes each listed skill via the `Skill` tool before the first edit.

        ## Writing Style
        - Always load the `prose` skill BEFORE writing ANY prose content.
        - Keep responses to plain ASCII text.
      ''
      + lib.optionalString (bundledInstructions != "") "\n${bundledInstructions}\n"
      + lib.optionalString (cfg.hostContext != "") "\n## Host Environment\n\n${cfg.hostContext}\n";

      activation.ensureClaudeResearchDir =
        lib.mkIf (pkgs.stdenv.hostPlatform.isDarwin || !cfg.research.useVault)
          (
            lib.hm.dag.entryAfter [ "writeBoundary" ] ''
              run mkdir -p "${researchDir}"
            ''
          );

      # Lima refuses to start when an extra_mounts host_path is
      # missing, and `host select` only creates this dir lazily on
      # the first kubectx select. Pre-create it at activation so
      # `task lima:rebuild` succeeds on a fresh host.
      activation.ensureMcpKubectxStateDir = lib.mkIf cfg.lima.enable (
        lib.hm.dag.entryAfter [ "writeBoundary" ] ''
          run mkdir -p "${config.xdg.stateHome}/mcp-kubectx"
        ''
      );

      # Same Lima-needs-host-path-to-exist constraint as above.
      activation.ensureArchivesDir = lib.mkIf cfg.lima.enable (
        lib.hm.dag.entryAfter [ "writeBoundary" ] ''
          run mkdir -p ${lib.escapeShellArg cfg.archivesDir}
        ''
      );

      # Lima refuses to start when this extra_mounts host_path is missing,
      # and atuin creates the dir lazily on first run -- pre-create it.
      activation.ensureAtuinDataDir = lib.mkIf cfg.lima.enable (
        lib.hm.dag.entryAfter [ "writeBoundary" ] ''
          run mkdir -p "${atuinDataDir}"
        ''
      );

      # Per-`serve` UDS lives outside the Lima bind-mounted state
      # dir (UDS-over-bind-mount semantics on macOS-host are
      # unverified; the safe design avoids the question). Both host
      # and guest profiles need this dir at mode 0700 to match
      # listenSocket's parent-dir invariant; create it
      # unconditionally so non-Lima Darwin hosts also get it.
      activation.ensureMcpKubectxRunDir = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        run install -d -m 700 "${config.xdg.stateHome}/mcp-kubectx-run"
      '';

      # Merge MCP servers into mutable ~/.claude.json. Fenced ahead of
      # sibling entries that can abort the activation script so the merge
      # cannot be silently skipped by an unrelated upstream failure.
      activation.syncClaudeJson =
        lib.hm.dag.entryBetween [ "sops-nix" "caffeineTccReset" ] [ "writeBoundary" ]
          ''
            CLAUDE_JSON="$HOME/.claude.json"

            # Read existing file or start fresh
            if [ -f "$CLAUDE_JSON" ]; then
              if ${pkgs.jq}/bin/jq empty "$CLAUDE_JSON" 2>/dev/null; then
                EXISTING=$(cat "$CLAUDE_JSON")
              else
                echo "Warning: ~/.claude.json is malformed, backing up" >&2
                $DRY_RUN_CMD cp "$CLAUDE_JSON" "$CLAUDE_JSON.bak.$(date +%s)"
                EXISTING='{}'
              fi
            else
              EXISTING='{}'
            fi

            # Merge MCP servers from home-manager config
            MCP_CONFIG="${config.xdg.configHome}/mcp/mcp.json"
            MCP_SERVERS='{}'
            if [ -f "$MCP_CONFIG" ]; then
              MCP_SERVERS=$(${pkgs.jq}/bin/jq '.mcpServers // {}' "$MCP_CONFIG")
            fi
            UPDATED=$(echo "$EXISTING" | ${pkgs.jq}/bin/jq \
              --argjson mcp "$MCP_SERVERS" \
              '.mcpServers = (.mcpServers // {} | to_entries | map(select(.key as $k | $mcp | has($k) | not)) | from_entries) * $mcp')

            ${lib.optionalString cfg.remoteControl ''
              # Enable remote control for all interactive sessions
              UPDATED=$(echo "$UPDATED" | ${pkgs.jq}/bin/jq '.remoteControlAtStartup = true')
            ''}

            ${lib.optionalString skipPerms ''
              # Pre-trust home directory (sandbox only); workspace trust does
              # not require a token, so this runs independently of secrets.
              UPDATED=$(echo "$UPDATED" | ${pkgs.jq}/bin/jq \
                '.projects["${config.dotfiles.homeDirectory}"].hasTrustDialogAccepted = true')
            ''}

            # Atomic write
            if [ -z "$DRY_RUN_CMD" ]; then
              TMPFILE=$(mktemp "$CLAUDE_JSON.tmp.XXXXXX")
              echo "$UPDATED" > "$TMPFILE"
              chmod 600 "$TMPFILE"
              mv "$TMPFILE" "$CLAUDE_JSON"
            else
              echo "Would write merged MCP config to $CLAUDE_JSON"
            fi

            # Prune stale worktree entries from ~/.claude.json
            if [ -z "$DRY_RUN_CMD" ] && command -v workmux >/dev/null 2>&1 && [ -f "$CLAUDE_JSON" ]; then
              ${lib.getExe' pkgs.workmux-bin "workmux"} claude prune 2>/dev/null || true
            fi

            # Load-bearing diagnostic: surfaces silent-skip regressions when
            # a future sibling entry sorts ahead of this one and aborts.
            MCP_COUNT=$(${pkgs.jq}/bin/jq 'length' <<<"$MCP_SERVERS")
            echo "syncClaudeJson: synced $MCP_COUNT MCP servers to ~/.claude.json" >&2
          '';

      # Best-effort propagation of the GitHub PAT to fish and gh. Kept
      # separate from syncClaudeJson so a sops-nix failure cannot prevent
      # the MCP merge from running.
      activation.syncClaudeSecrets = lib.mkIf sopsEnabled (
        lib.hm.dag.entryAfter [ "sops-nix" "syncClaudeJson" ] ''
          # Set GitHub PAT as a universal fish variable for MCP auth
          if [ -f "${secretPath "gh_token"}" ]; then
            GH_TOKEN=$(cat "${secretPath "gh_token"}" 2>/dev/null || true)
            if [ -z "$DRY_RUN_CMD" ] && [ -n "''${GH_TOKEN:-}" ]; then
              ${pkgs.fish}/bin/fish -c "set -Ux GITHUB_PERSONAL_ACCESS_TOKEN ''${GH_TOKEN}"
            fi
          fi

          ${lib.optionalString skipPerms ''
            # Authenticate gh with the scoped PAT (sandbox only)
            if [ -z "$DRY_RUN_CMD" ] && [ -n "''${GH_TOKEN:-}" ]; then
              echo "''${GH_TOKEN}" | ${pkgs.gh}/bin/gh auth login --with-token
            fi
          ''}
        ''
      );
    };
  };
}
