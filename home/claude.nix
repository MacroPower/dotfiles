{
  pkgs,
  lib,
  config,
  ...
}:

let
  inherit (lib) mkOption types;
  inherit (config.lib.stylix) colors;
  cfg = config.dotfiles.claude;
  skipPerms = cfg.dangerouslySkipPermissions;

  claudePowerlineConfig = builtins.toJSON {
    theme = "custom";
    colors.custom = {
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
        fg = "#${colors.base0A}";
      };
      contextCritical = {
        bg = "#${colors.base08}";
        fg = "#${colors.base06}";
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
    display = {
      style = "powerline";
      charset = "unicode";
      colorCompatibility = "auto";
      autoWrap = true;
      padding = 1;
      lines = [
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
    };
    budget = {
      session = {
        amount = 220000;
        type = "tokens";
        warningThreshold = 80;
      };
      weekly = {
        amount = 1100;
        type = "cost";
        warningThreshold = 80;
      };
    };
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

  workmuxConfig = (pkgs.formats.yaml { }).generate "config.yaml" {
    nerdfont = true;
    merge_strategy = "rebase";
    agent = "claude";
    window_prefix = "wm-";
    status_format = false;
    status_icons = {
      working = "󱚣";
      waiting = "󰍻";
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
    post_create = lib.optionals cfg.lima.enable [
      "direnv allow"
      "lefthook install"
    ];
    panes = [
      {
        command = "<agent>";
        focus = true;
      }
      (
        {
          split = "horizontal";
        }
        // lib.optionalAttrs cfg.lima.enable {
          command = "workmux sandbox shell -- fish";
        }
      )
    ];
    files = {
      copy = [ ".ck" ];
    };
    sandbox = lib.optionalAttrs cfg.lima.enable {
      enabled = true;
      backend = "lima";
      image = "file://${config.home.homeDirectory}/.lima/_images/terrarium.qcow2";
      toolchain = "auto";
      env_passthrough = [
        "GITHUB_TOKEN"
        "GITHUB_PERSONAL_ACCESS_TOKEN"
        "GH_TOKEN"
        "ARGOCD_API_TOKEN"
        "ARGOCD_BASE_URL"
        "DAGGER_CLOUD_TOKEN"
        "KAGI_API_KEY"
      ];
      lima = {
        isolation = "shared";
        projects_dir = "${config.home.homeDirectory}/Documents/repos";
        skip_default_provision = true;
        inherit (cfg.lima) cpus;
        inherit (cfg.lima) memory;
        inherit (cfg.lima) disk;
      };
    };
  };

  rtkConfig = (pkgs.formats.toml { }).generate "config.toml" {
    display = {
      colors = false;
      emoji = false;
      max_width = 120;
    };
  };

  fetchRules = (pkgs.formats.json { }).generate "mcp-fetch-rules.json" (
    lib.optionalAttrs cfg.fetchAllowlist {
      reason = "URL not in allowlist. If you need to fetch this content, ask the user to add an entry to the allowlist. Present the user with both the URL and your justification.";
    }
    // {
      deny = map cleanRule (
        [
          {
            host = "raw\\.githubusercontent\\.com";
            except = [ { path = ".*\\.md"; } ];
            reason = "Fetching code from raw.githubusercontent.com is blocked. Clone the repo to /tmp/git/<owner>/<repo> and read files locally instead.";
          }
          {
            host = "google\\.com";
            reason = "Fetching from google.com is blocked. Use mcp__kagi__kagi_search_fetch instead.";
          }
          {
            host = "api\\.github\\.com";
            reason = "Use mcp__github__* tools instead of fetching the GitHub API directly.";
          }
          {
            host = "github\\.com";
            path = "/[^/]+/[^/]+/issues(/.*)?";
            reason = "Use mcp__github__list_issues or mcp__github__issue_read instead of fetching GitHub issue pages.";
          }
          {
            host = "github\\.com";
            path = "/[^/]+/[^/]+/pulls?(/.*)?";
            reason = "Use mcp__github__list_pull_requests or mcp__github__pull_request_read instead of fetching GitHub PR pages.";
          }
          {
            host = "github\\.com";
            path = "/[^/]+/[^/]+/(commit|compare)(/.*)?";
            reason = "Use mcp__github__get_commit or mcp__github__list_commits instead of fetching GitHub commit pages.";
          }
          {
            host = "github\\.com";
            path = "/[^/]+/[^/]+/releases(/.*)?";
            reason = "Use mcp__github__list_releases or mcp__github__get_latest_release instead of fetching GitHub release pages.";
          }
          {
            host = "github\\.com";
            path = "/[^/]+/[^/]+/tags(/.*)?";
            reason = "Use mcp__github__list_tags or mcp__github__get_tag instead of fetching GitHub tag pages.";
          }
          {
            host = "github\\.com";
            path = "/[^/]+/[^/]+/(blob|tree)(/.*)?";
            reason = "Use mcp__git__git_clone to clone the repo to /tmp/git/<owner>/<repo> and read files locally instead of fetching GitHub file pages.";
          }
          {
            host = "github\\.com";
            path = "/search(/.*)?";
            reason = "Use mcp__github__search_code, mcp__github__search_issues, mcp__github__search_pull_requests, or mcp__github__search_repositories instead of fetching GitHub search pages.";
          }
        ]
        ++ cfg.extraFetchRules.deny
      );
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
          { host = "(.*\\.)?redis\\.io"; }
          { host = "(.*\\.)?rfc-editor\\.org"; }
          { host = "(.*\\.)?robusta\\.dev"; }
          { host = "(.*\\.)?rook\\.io"; }
          { host = "(.*\\.)?rust-lang\\.org"; }
          { host = "(.*\\.)?securecodebox\\.io"; }
          { host = "(.*\\.)?sigstore\\.dev"; }
          { host = "(.*\\.)?sigs\\.k8s\\.io"; }
          { host = "(.*\\.)?docs\\.spacelift\\.io"; }
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
        ++ cfg.extraFetchRules.allow
      );
    }
  );

  hookRouter = pkgs.writeShellApplication {
    name = "hook-router-wrapper";
    runtimeInputs = [
      pkgs.hook-router
      pkgs.git
    ];
    runtimeEnv = {
      RTK_REWRITE = "${pkgs.rtk-bin}/libexec/rtk/hooks/rtk-rewrite.sh";
    };
    text = ''
      exec hook-router \
        --db "${config.xdg.stateHome}/hook-router/state.db" \
        --log-file "${config.xdg.stateHome}/hook-router/hook-router.log" \
        "$@"
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

  # Wrapper script that reads the GH_TOKEN from sops at runtime
  gitWrapper = pkgs.writeShellScript "git-mcp-wrapper" ''
    if [ -f "${config.sops.secrets.gh_token.path}" ]; then
      export GH_TOKEN="$(cat "${config.sops.secrets.gh_token.path}" 2>/dev/null || true)"
    fi
    exec ${pkgs.mcp-git}/bin/mcp-git "$@"
  '';

  # Wrapper script that reads the KAGI_API_KEY from sops at runtime
  kagiWrapper = pkgs.writeShellScript "kagi-mcp-wrapper" ''
    if [ -f "${config.sops.secrets.kagi_api_key.path}" ]; then
      export KAGI_API_KEY="$(cat "${config.sops.secrets.kagi_api_key.path}" 2>/dev/null || true)"
    fi
    export KAGI_SUMMARIZER_ENGINE="agnes"
    exec ${pkgs.mcp-kagi}/bin/kagimcp "$@"
  '';

  # Wrapper script that reads ArgoCD credentials from sops at runtime
  argocdWrapper = pkgs.writeShellScript "argocd-mcp-wrapper" ''
    if [ -f "${config.sops.secrets.argocd_api_token.path}" ]; then
      export ARGOCD_API_TOKEN="$(cat "${config.sops.secrets.argocd_api_token.path}" 2>/dev/null || true)"
    fi
    if [ -f "${config.sops.secrets.argocd_base_url.path}" ]; then
      export ARGOCD_BASE_URL="$(cat "${config.sops.secrets.argocd_base_url.path}" 2>/dev/null || true)"
    fi
    exec ${pkgs.mcp-argocd}/bin/argocd-mcp "$@"
  '';

  slugify = pkgs.writeShellScriptBin "slugify" ''
    echo "$*" | tr '[:upper:]' '[:lower:]' | tr -cs '[:alnum:]' '-' | sed 's/^-//;s/-$//' | cut -c1-60
  '';

  # Wrapper that injects sops secrets as env vars for sandbox env_passthrough.
  # Uses symlinkJoin so share/fish/vendor_completions.d/ from workmux-bin is preserved.
  workmuxWrapped = pkgs.symlinkJoin {
    name = "workmux-wrapped";
    paths = [ pkgs.workmux-bin ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    postBuild = ''
      wrapProgram $out/bin/workmux \
        --run '
          if [ -f "${config.sops.secrets.gh_token.path}" ]; then
            GH_TOKEN="$(cat "${config.sops.secrets.gh_token.path}" 2>/dev/null || true)"
            export GH_TOKEN
            export GITHUB_TOKEN="$GH_TOKEN"
            export GITHUB_PERSONAL_ACCESS_TOKEN="$GH_TOKEN"
          fi
          if [ -f "${config.sops.secrets.argocd_api_token.path}" ]; then
            export ARGOCD_API_TOKEN="$(cat "${config.sops.secrets.argocd_api_token.path}" 2>/dev/null || true)"
          fi
          if [ -f "${config.sops.secrets.argocd_base_url.path}" ]; then
            export ARGOCD_BASE_URL="$(cat "${config.sops.secrets.argocd_base_url.path}" 2>/dev/null || true)"
          fi
          if [ -f "${config.sops.secrets.dagger_cloud_token.path}" ]; then
            export DAGGER_CLOUD_TOKEN="$(cat "${config.sops.secrets.dagger_cloud_token.path}" 2>/dev/null || true)"
          fi
          if [ -f "${config.sops.secrets.kagi_api_key.path}" ]; then
            export KAGI_API_KEY="$(cat "${config.sops.secrets.kagi_api_key.path}" 2>/dev/null || true)"
          fi
        '
    '';
  };

  # Aggregate enabled MCP server bundles
  enabledBundles = lib.filterAttrs (_: b: b.enable) cfg.mcpServerBundles;
  bundleValues = lib.attrValues enabledBundles;
  bundledServers = lib.foldl' lib.recursiveUpdate { } (map (b: b.servers) bundleValues);
  bundledAllow = lib.concatMap (b: b.permissions.allow) bundleValues;
  bundledDeny = lib.concatMap (b: b.permissions.deny) bundleValues;
  bundledAsk = lib.concatMap (b: b.permissions.ask) bundleValues;
  bundledDomains = lib.concatMap (b: b.sandbox.allowedDomains) bundleValues;
  bundledSockets = lib.concatMap (b: b.sandbox.allowUnixSockets) bundleValues;
  bundledReadPaths = lib.concatMap (b: b.sandbox.allowRead) bundleValues;
  bundledWritePaths = lib.concatMap (b: b.sandbox.allowWrite) bundleValues;

  extraReadPaths = [
    "/nix/store"
  ]
  ++ bundledReadPaths;

  extraWritePaths = [
    "~/go/pkg"
    "~/Library/Application Support/rtk"
    "~/Library/Caches"
    "~/.cache/nix"
    "~/.cache/helm"
    "~/.local/state/workmux"
    "~/.local/state/hook-router"
    "~/.local/share/claude"
  ]
  ++ bundledWritePaths;

  readPermEntries = map (p: "Read(${toPermGlob p})") extraReadPaths;

  writePermEntries = lib.concatMap (p: [
    "Read(${toPermGlob p})"
    "Write(${toPermGlob p})"
    "Edit(${toPermGlob p})"
  ]) extraWritePaths;

  bundledInstructions =
    let
      pairs = lib.filter (p: p.category != "" && p.items != [ ]) (
        map (b: { inherit (b.instructions) category items; }) bundleValues
      );
      grouped = lib.foldl' (
        acc: p: acc // { ${p.category} = (acc.${p.category} or [ ]) ++ p.items; }
      ) { } pairs;
      renderCategory = cat: items: "## ${cat}\n\n" + lib.concatMapStringsSep "\n" (i: "- ${i}") items;
    in
    lib.concatStringsSep "\n\n" (lib.mapAttrsToList renderCategory grouped);
in
{
  options.dotfiles.claude = {
    dangerouslySkipPermissions = mkOption {
      type = types.bool;
      default = false;
      description = "Run Claude Code with --dangerously-skip-permissions, enabling sandbox mode with automatic directory trust and GitHub auth.";
    };

    extraSettings = mkOption {
      type = types.attrsOf types.anything;
      default = { };
      description = "Additional settings merged into Claude Code settings.json.";
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
        };
      };
      default = { };
      description = "Extra mcp-fetch URL filtering rules merged with the base deny and allow lists.";
    };

    fetchAllowlist = mkOption {
      type = types.bool;
      default = true;
      description = "Whether to enforce the mcp-fetch URL allowlist. When false, all URLs are allowed unless explicitly denied.";
    };

    extraAgents = mkOption {
      type = types.attrsOf (types.either types.lines types.path);
      default = { };
      description = "Additional agents merged into programs.claude-code.agents. Keys omit the .md suffix.";
    };

    extraSkills = mkOption {
      type = types.attrsOf (types.either types.lines types.path);
      default = { };
      description = "Additional skills merged into programs.claude-code.skills. Values may be paths to directories or files.";
    };

    extraMcpServers = mkOption {
      type = types.attrsOf types.anything;
      default = { };
      description = "Additional MCP servers deep-merged into programs.mcp.servers.";
    };

    remoteControl = lib.mkEnableOption "Claude Code remote control for all sessions";

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

    mcpServerBundles = mkOption {
      type = types.attrsOf (
        types.submodule {
          options = {
            enable = mkOption {
              type = types.bool;
              default = true;
              description = "Whether this MCP server bundle is enabled.";
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
              category = mkOption {
                type = types.str;
                default = "";
                description = "Section heading (## <category>) in ~/.claude/CLAUDE.md. Bundles sharing a category are grouped.";
              };
              items = mkOption {
                type = types.listOf types.str;
                default = [ ];
                description = "Instruction lines rendered as a bulleted list under the category heading.";
              };
            };
          };
        }
      );
      default = { };
      description = "MCP server bundles grouping server config, permissions, sandbox rules, and CLAUDE.md instructions.";
    };
  };

  config = {
    dotfiles.claude.mcpServerBundles = {
      fetch = {
        servers.fetch = {
          type = "stdio";
          command = "${pkgs.mcp-fetch}/bin/mcp-fetch";
          args = [
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
        instructions = {
          category = "Web Search";
          items = [
            "Use `mcp__fetch__fetch` for fetching known URLs and web page content."
          ];
        };
      };

      git = {
        servers.git = {
          type = "stdio";
          command = "${gitWrapper}";
          args = [
            "--allow-dir"
            "/tmp/git"
            "--allow-dir"
            "/private/tmp/git"
          ];
        };
        permissions.allow = [ "mcp__git__git_clone" ];
        sandbox.allowWrite = [
          "/tmp/git"
          "/private/tmp/git"
        ];
        instructions = {
          category = "Code Search";
          items = [
            "Use `mcp__git__git_clone` to clone repositories into `/tmp/git/<owner>/<repo>` and read from there."
          ];
        };
      };

      kagi = {
        servers.kagi = {
          type = "stdio";
          command = "${kagiWrapper}";
        };
        permissions.allow = [ "mcp__kagi__kagi_search_fetch" ];
        permissions.deny = [ "mcp__kagi__kagi_summarizer" ];
        instructions = {
          category = "Web Search";
          items = [
            "Use `mcp__kagi__kagi_search_fetch` for web searches."
          ];
        };
      };

      kubernetes = {
        servers.kubernetes = {
          type = "stdio";
          command = "${pkgs.mcp-kubernetes}/bin/mcp-kubernetes";
          args = [
            "--access-level"
            "readonly"
          ];
          env = {
            KUBERNETES_MCP_COLLECT_TELEMETRY = "false";
          };
        };
        permissions.allow = [ "mcp__kubernetes__call_kubectl" ];
        instructions = {
          category = "Kubernetes";
          items = [
            "Use `mcp__kubernetes__call_kubectl` for kubectl operations. Do not run kubectl directly."
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
        instructions = {
          category = "Nix";
          items = [
            "Use `mcp__nixos__nix` for Nix package searches, NixOS/home-manager/nix-darwin option lookups, and FlakeHub queries."
            "Use `mcp__nixos__nix_versions` for package version history and channel availability."
          ];
        };
      };

      ck = {
        servers.ck = {
          type = "stdio";
          command = "${pkgs.llm-agents.ck}/bin/ck";
          args = [ "--serve" ];
        };
        permissions.allow = [
          "mcp__ck__semantic_search"
          "mcp__ck__regex_search"
          "mcp__ck__lexical_search"
          "mcp__ck__hybrid_search"
          "mcp__ck__index_status"
          "mcp__ck__health_check"
          "mcp__ck__default_ckignore"
          "mcp__ck__reindex"
        ];
        sandbox = {
          allowWrite = [ "~/.cache/ck" ];
          allowedDomains = [
            "huggingface.co"
            "cdn-lfs.huggingface.co"
          ];
        };
        instructions = {
          category = "Code Search";
          items = [
            "Use `mcp__ck__semantic_search` to find code by meaning when keywords are unknown or fuzzy."
            "Use `mcp__ck__hybrid_search` for combined semantic + regex ranking."
            "Use `mcp__ck__lexical_search` for BM25 full-text search across a repo."
            "Prefer `Grep` for known exact strings/regexes, use `mcp__ck__regex_search` when you need paged results across a large repo."
          ];
        };
      };

      github = {
        servers.github = {
          type = "http";
          url = "https://api.githubcopilot.com/mcp/readonly";
          headers = {
            Authorization = "Bearer \${GITHUB_PERSONAL_ACCESS_TOKEN}";
          };
        };
        permissions.allow = [
          "mcp__github__get_commit"
          "mcp__github__get_copilot_job_status"
          "mcp__github__get_label"
          "mcp__github__get_latest_release"
          "mcp__github__get_me"
          "mcp__github__get_release_by_tag"
          "mcp__github__get_tag"
          "mcp__github__get_team_members"
          "mcp__github__get_teams"
          "mcp__github__issue_read"
          "mcp__github__list_branches"
          "mcp__github__list_commits"
          "mcp__github__list_issue_types"
          "mcp__github__list_issues"
          "mcp__github__list_pull_requests"
          "mcp__github__list_releases"
          "mcp__github__list_tags"
          "mcp__github__pull_request_read"
          "mcp__github__search_code"
          "mcp__github__search_issues"
          "mcp__github__search_pull_requests"
          "mcp__github__search_repositories"
          "mcp__github__search_users"
        ];
        permissions.deny = [
          "mcp__github__get_file_contents"
          # GitHub MCP: deny all write/mutating tools.
          # These are blocked by the MCP config and primarily denied here as a usage hint.
          "mcp__github__actions_run_trigger"
          "mcp__github__add_comment_to_pending_review"
          "mcp__github__add_issue_comment"
          "mcp__github__add_reply_to_pull_request_comment"
          "mcp__github__assign_copilot_to_issue"
          "mcp__github__create_branch"
          "mcp__github__create_gist"
          "mcp__github__create_or_update_file"
          "mcp__github__create_pull_request"
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
          "mcp__github__merge_pull_request"
          "mcp__github__projects_write"
          "mcp__github__pull_request_review_write"
          "mcp__github__push_files"
          "mcp__github__request_copilot_review"
          "mcp__github__star_repository"
          "mcp__github__sub_issue_write"
          "mcp__github__unstar_repository"
          "mcp__github__update_gist"
          "mcp__github__update_pull_request"
          "mcp__github__update_pull_request_branch"
          "mcp__github__run_secret_scanning"
        ];
        instructions = {
          category = "Web Search & Fetching";
          items = [
            "Use `mcp__github__*` tools for reading GitHub data (issues, PRs, repos, code search, etc.)"
          ];
        };
      };

      argocd = {
        servers.argocd = {
          type = "stdio";
          command = "${argocdWrapper}";
          args = [ "stdio" ];
        };
        permissions.allow = [
          "mcp__argocd__list_clusters"
          "mcp__argocd__list_applications"
          "mcp__argocd__get_application"
          "mcp__argocd__get_application_resource_tree"
          "mcp__argocd__get_application_managed_resources"
          "mcp__argocd__get_application_workload_logs"
          "mcp__argocd__get_resource_events"
          "mcp__argocd__get_resource_actions"
          "mcp__argocd__get_application_events"
          "mcp__argocd__get_resources"
        ];
        permissions.ask = [
          "mcp__argocd__create_application"
          "mcp__argocd__update_application"
          "mcp__argocd__delete_application"
          "mcp__argocd__sync_application"
          "mcp__argocd__run_resource_action"
        ];
        instructions = {
          category = "Kubernetes";
          items = [
            "Use the `mcp__argocd__*` tools to interact with Argo CD. Do not use the `argocd` CLI directly."
          ];
        };
      };
      opentofu = {
        servers.opentofu = {
          type = "stdio";
          command = "${pkgs.mcp-opentofu}/bin/mcp-opentofu";
        };
        permissions.allow = [
          "mcp__opentofu__search-opentofu-registry"
          "mcp__opentofu__get-provider-details"
          "mcp__opentofu__get-module-details"
          "mcp__opentofu__get-resource-docs"
          "mcp__opentofu__get-datasource-docs"
        ];
        sandbox.allowedDomains = [ "api.opentofu.org" ];
        instructions = {
          category = "OpenTofu";
          items = [
            "Use `mcp__opentofu__*` tools to query the OpenTofu Registry for providers, modules, resources, and data sources instead of guessing from memory."
          ];
        };
      };
    };

    programs = {
      mcp = {
        enable = true;
        servers = injectCaEnv (lib.recursiveUpdate bundledServers cfg.extraMcpServers);
      };

      claude-code = {
        enable = true;
        package = pkgs.llm-agents.claude-code;
        enableMcpIntegration = true;

        settings = lib.recursiveUpdate {
          env = {
            CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS = "1";
          };
          disableAutoMode = "disable";
          includeGitInstructions = false;
          inherit (cfg) attribution;
          permissions = {
            defaultMode = "plan";
            allow = readPermEntries ++ writePermEntries ++ bundledAllow ++ cfg.extraPermissions.allow;
            deny = [
              # Key material & certificates
              "Read(//**/*.pem)"
              "Read(//**/*.key)"
              "Read(//**/*.p12)"
              "Read(//**/*.pfx)"
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
              "Read(//**/.snyk)"
              "Read(//**/.wrangler/**)"

              # Container & Kubernetes
              "Read(//**/.docker/config.json)"
              "Read(//**/.docker/certs.d/**)"
              "Read(//**/.config/containers/auth.json)"
              "Read(//**/.kube/config)"
              "Read(//**/.kube/config*)"
              "Read(//**/.talos/**)"
              "Read(//**/.cosign/**)"
              "Read(//**/.helm/repository/repositories.yaml)"

              # Secret managers & encryption
              "Read(//**/.doppler/**)"
              "Read(//**/age/keys.txt)"
              "Read(//**/rclone.conf)"

              # IaC state & credentials
              "Read(//**/credentials.tfrc.json)"
              "Read(//**/.terraformrc)"
              "Read(//**/.terraform.d/credentials.tfrc.json)"
              "Read(//**/*.tfstate)"
              "Read(//**/*.tfstate.*)"
              "Read(//**/.pulumi/credentials.json)"

              # CI/CD & deployment tokens
              "Read(//**/.config/gh/hosts.yml)"
              "Read(//**/.spacelift/**)"
              "Read(//**/.jira.d/config.yml)"

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
            ]
            ++ bundledDeny
            ++ cfg.extraPermissions.deny;
            ask = [
              "Bash(git push)"
              "Bash(git push *)"
              "Bash(git reset)"
              "Bash(git reset *)"
              "Bash(git clean *)"
              "Bash(git restore *)"
              "Bash(git checkout *)"
              "Bash(git switch *)"
              "Bash(git merge *)"
              "Bash(git tag *)"
              "Bash(git rm *)"
              "Bash(git remote *)"
            ]
            ++ bundledAsk
            ++ cfg.extraPermissions.ask;
          };
          statusLine = {
            type = "command";
            command = "${pkgs.claude-powerline}/bin/claude-powerline";
            padding = 0;
          };
          enabledPlugins = {
            "claude-md-management@claude-plugins-official" = true;
            "skill-creator@claude-plugins-official" = true;
            "code-review@claude-plugins-official" = true;
          };
          sandbox = {
            enabled = pkgs.stdenv.isDarwin;
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
              allowRead = extraReadPaths;
              allowWrite = extraWritePaths;
            };
          };
          hooks = {
            # NOTE: All matching hooks run concurrently with the original input.
            # Only one hook per tool should return updatedInput to avoid
            # non-deterministic last-writer-wins races.
            PreToolUse = [
              {
                matcher = "Bash";
                hooks = [
                  {
                    type = "command";
                    command = "${lib.getExe hookRouter} --event PreToolUse --tool Bash";
                  }
                ];
              }
              {
                matcher = "ExitPlanMode";
                hooks = [
                  {
                    type = "command";
                    command = "${lib.getExe hookRouter} --event PreToolUse --tool ExitPlanMode";
                  }
                ];
              }
              {
                matcher = "EnterPlanMode";
                hooks = [
                  {
                    type = "command";
                    command = "${lib.getExe hookRouter} --event PreToolUse --tool EnterPlanMode";
                  }
                ];
              }
            ];
            UserPromptSubmit = [
              {
                hooks = [
                  {
                    type = "command";
                    command = "${workmux} working";
                  }
                ];
              }
            ];
            Notification = [
              {
                matcher = "permission_prompt|elicitation_dialog";
                hooks = [
                  {
                    type = "command";
                    command = "${workmux} waiting";
                  }
                ];
              }
            ];
            PostToolUse = [
              {
                hooks = [
                  {
                    type = "command";
                    command = "${workmux} working";
                  }
                ];
              }
            ];
            Stop = [
              {
                hooks = [
                  {
                    type = "command";
                    command = "${workmux} done";
                  }
                ];
              }
              {
                hooks = [
                  {
                    type = "command";
                    command = "${lib.getExe hookRouter} --event Stop";
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
        } cfg.extraSettings;

        agents = {
          code-simplifier = ../configs/claude/agents/code-simplifier.md;
          humanizer = ../configs/claude/agents/humanizer.md;
          implementation-reviewer = ../configs/claude/agents/implementation-reviewer.md;
          plan-reviewer = ../configs/claude/agents/plan-reviewer.md;
        }
        // cfg.extraAgents;

        skills = {
          commit = ../configs/claude/skills/commit;
          commit-push-pr = ../configs/claude/skills/commit-push-pr;
          dagger-modules = ../configs/claude/skills/dagger-modules;
          worktree = ../configs/claude/skills/worktree;
          wm-merge = ../configs/claude/skills/wm-merge;
          wm-rebase = ../configs/claude/skills/wm-rebase;
          wm-coordinator = ../configs/claude/skills/wm-coordinator;
          wm-workmux = ../configs/claude/skills/wm-workmux;
          git-surgeon = ../configs/claude/skills/git-surgeon;
          research = ../configs/claude/skills/research;
        }
        // cfg.extraSkills;
      };

      fish.shellAliases = lib.optionalAttrs skipPerms {
        claude = "command claude --dangerously-skip-permissions";
      };
    };

    xdg.configFile = {
      "claude-powerline/config.json".text = claudePowerlineConfig;
      "rtk/config.toml".source = rtkConfig;
      "workmux/config.yaml".source = workmuxConfig;
    };

    home = {
      packages = [
        pkgs.chief
        pkgs.llm-agents.ccusage
        pkgs.llm-agents.ck
        workmuxWrapped
        pkgs.rtk-bin
        pkgs.claude-history
        pkgs.git-surgeon
        slugify
      ];

      file.".claude/CLAUDE.md".text = ''
        # Global Instructions

        ## Writing Style

        - Keep responses to plain ASCII text.
        - Acknowledge complexity and mixed feelings when they exist.
        - Your code speaks for itself. Enumeration of content is redundant. Focus instead on the how and why.

        ## Agents & Concurrency

        - Prefer parallel tool calls. When a task touches 2+ independent areas, make multiple tool calls (Agent, Read, Grep, etc.) in a single message rather than one at a time.
        - Launch multiple Agent tool calls concurrently when investigating or working on independent areas. Don't serialize what can run in parallel.
        - For large tasks spanning many files or domains, you may orchestrate multiple worktree agents with `/wm-coordinator`.

        ## Quality & Review

        - Your token budget is unlimited. Always prioritize correctness and code quality over speed or token cost.
        - Run reviewer agents (plan-reviewer, implementation-reviewer) iteratively. If a reviewer finds issues, fix them and re-run the reviewer until you get LGTM.
        - When uncertain about correctness, spawn a verification subagent to cross-check your work rather than guessing.
      ''
      + lib.optionalString (bundledInstructions != "") "\n${bundledInstructions}\n";

      sessionVariables = {
        DISABLE_AUTOUPDATER = "1";
        CLAUDE_CODE_TMUX_TRUECOLOR = "1";
      }
      // lib.optionalAttrs skipPerms {
        IS_SANDBOX = "1";
      };

      activation.ensureClaudeResearchDir = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        run mkdir -p "$HOME/.local/share/claude/research"
      '';

      # Activation: merge MCP servers and secrets into mutable ~/.claude.json
      activation.syncClaudeJson = lib.hm.dag.entryAfter [ "writeBoundary" "sops-nix" ] ''
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

        # Set GitHub PAT as a universal fish variable for MCP auth
        if [ -f "${config.sops.secrets.gh_token.path}" ]; then
          GH_TOKEN=$(cat "${config.sops.secrets.gh_token.path}" 2>/dev/null || true)
          if [ -z "$DRY_RUN_CMD" ] && [ -n "''${GH_TOKEN:-}" ]; then
            ${pkgs.fish}/bin/fish -c "set -Ux GITHUB_PERSONAL_ACCESS_TOKEN ''${GH_TOKEN}"
          fi
        fi

        ${lib.optionalString skipPerms ''
          # Pre-trust home directory and authenticate with scoped PAT (sandbox only)
          UPDATED=$(echo "$UPDATED" | ${pkgs.jq}/bin/jq \
            '.projects["${config.dotfiles.homeDirectory}"].hasTrustDialogAccepted = true')
          if [ -z "$DRY_RUN_CMD" ] && [ -n "''${GH_TOKEN:-}" ]; then
            echo "''${GH_TOKEN}" | ${pkgs.gh}/bin/gh auth login --with-token
          fi
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
      '';
    };
  };
}
