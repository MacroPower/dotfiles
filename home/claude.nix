{
  pkgs,
  lib,
  config,
  ...
}:

let
  inherit (lib) mkOption mkEnableOption types;
  cfg = config.dotfiles.claude;
  skipPerms = cfg.dangerouslySkipPermissions;

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

  cleanAttrs = lib.filterAttrs (_: v: v != "" && v != [ ]);
  cleanRule =
    rule:
    let
      cleaned = cleanAttrs rule;
    in
    if cleaned ? except then cleaned // { except = map cleanAttrs cleaned.except; } else cleaned;

  rtkConfig = (pkgs.formats.toml { }).generate "config.toml" {
    display = {
      colors = false;
      emoji = false;
      max_width = 120;
    };
  };

  fetchRules = (pkgs.formats.json { }).generate "mcp-fetch-rules.json" (
    {
      reason = "URL not in allowlist. If you need to fetch this content, ask the user to add an entry to the allowlist. Present the user with both the URL and your justification.";
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
    // {
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

  blockExitPlan = pkgs.writeShellApplication {
    name = "block-exit-plan";
    runtimeInputs = [ pkgs.jq ];
    text = builtins.readFile ../configs/claude/hooks/block-exit-plan.sh;
  };

  # Single Bash PreToolUse hook that dispatches command rewrites.
  # All matching hooks run concurrently, so we use one hook to avoid
  # non-deterministic updatedInput races between multiple Bash matchers.
  hookRouter = pkgs.writeShellApplication {
    name = "hook-router-wrapper";
    runtimeInputs = [ pkgs.hook-router ];
    runtimeEnv = {
      RTK_REWRITE = "${pkgs.llm-agents.rtk}/libexec/rtk/hooks/rtk-rewrite.sh";
    };
    text = "exec hook-router";
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
    exec ${pkgs.uv}/bin/uvx --isolated --managed-python --python=3.13 kagimcp "$@"
  '';
in
{
  options.dotfiles.claude = {
    enable = mkEnableOption "Claude Code" // {
      default = true;
    };

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
  };

  config = lib.mkIf cfg.enable {
    programs = {
      mcp = {
        enable = true;
        servers = injectCaEnv (
          lib.recursiveUpdate {
            fetch = {
              type = "stdio";
              command = "${pkgs.mcp-fetch}/bin/mcp-fetch";
              args = [
                "--rules-file"
                "${fetchRules}"
              ];
            };
            git = {
              type = "stdio";
              command = "${gitWrapper}";
              args = [
                "--allow-dir"
                "/tmp/git"
                "--allow-dir"
                "/private/tmp/git"
              ];
            };
            kagi = {
              type = "stdio";
              command = "${kagiWrapper}";
            };
            github = {
              type = "http";
              url = "https://api.githubcopilot.com/mcp/readonly";
              headers = {
                Authorization = "Bearer \${GITHUB_PERSONAL_ACCESS_TOKEN}";
              };
            };
          } cfg.extraMcpServers
        );
      };

      claude-code = {
        enable = true;
        package = pkgs.llm-agents.claude-code;
        enableMcpIntegration = true;

        settings = lib.recursiveUpdate {
          env = {
            CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS = "1";
          };
          attribution = {
            commit = "";
            pr = "";
          };
          permissions = {
            allow = [
              "Edit(//tmp/git/**)"
              "Edit(//private/tmp/git/**)"
              "mcp__fetch__fetch"
              "mcp__git__git_clone"
              "mcp__kagi__kagi_search_fetch"
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
            ]
            ++ cfg.extraPermissions.allow;
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

              # Usage hints
              "WebSearch"
              "WebFetch"
              "mcp__kagi__kagi_summarizer"
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
            ]
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
              "Bash(git rebase)"
              "Bash(git rebase *)"
              "Bash(git merge *)"
              "Bash(git tag *)"
              "Bash(git rm *)"
              "Bash(git remote *)"
            ]
            ++ cfg.extraPermissions.ask;
          };
          statusLine = {
            type = "command";
            command = "${pkgs.llm-agents.ccstatusline}/bin/ccstatusline";
            padding = 0;
          };
          enabledPlugins = {
            "claude-md-management@claude-plugins-official" = true;
            "skill-creator@claude-plugins-official" = true;
            "code-review@claude-plugins-official" = true;
          };
          sandbox = {
            enabled = pkgs.stdenv.isDarwin;
            network = {
              allowLocalBinding = true;
              allowedDomains = [
                "jacobcolvin.com"
                "registry.dagger.io"
                "api.dagger.cloud"
              ];
            };
            filesystem = {
              allowWrite = [
                "/tmp/git"
                "/private/tmp/git"
              ];
            };
            excludedCommands = [
              "docker"
              "dagger"
            ];
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
                    command = lib.getExe hookRouter;
                  }
                ];
              }
              {
                matcher = "ExitPlanMode";
                hooks = [
                  {
                    type = "command";
                    command = lib.getExe blockExitPlan;
                  }
                ];
              }
            ];
          };
          autoMemoryEnabled = false;
          alwaysThinkingEnabled = true;
          skipDangerousModePermissionPrompt = true;
          teammateMode = "auto";
        } cfg.extraSettings;

        agents = {
          code-simplifier = ../configs/claude/agents/code-simplifier.md;
          humanizer = ../configs/claude/agents/humanizer.md;
          plan-reviewer = ../configs/claude/agents/plan-reviewer.md;
        }
        // cfg.extraAgents;

        skills = {
          commit = ../configs/claude/skills/commit;
          commit-push-pr = ../configs/claude/skills/commit-push-pr;
          dagger-modules = ../configs/claude/skills/dagger-modules;
        }
        // cfg.extraSkills;
      };

      fish.shellAliases = lib.optionalAttrs skipPerms {
        claude = "command claude --dangerously-skip-permissions";
      };
    };

    xdg.configFile = {
      "ccstatusline/settings.json".source = ../configs/ccstatusline/settings.json;
      "rtk/config.toml".source = rtkConfig;
    };

    home = {
      packages = [
        pkgs.llm-agents.ccusage
        pkgs.llm-agents.rtk
      ];

      file.".claude/CLAUDE.md".text = ''
        # Global Instructions

        ## Web Search & Fetching

        - Use `mcp__kagi__kagi_search_fetch` for web searches.
        - Use `mcp__fetch__fetch` for fetching known URLs and web page content.
        - Use `mcp__github__*` tools for reading GitHub data (issues, PRs, repos, code search, etc.)
        - Use `mcp__git__git_clone` to clone repositories into `/tmp/git/<owner>/<repo>` and read from there.

        Remember: Do research, don't guess.

        ## Shell Commands

        - Use `fd` instead of `find` in Bash commands.

        ## Writing Style

        - Keep responses to plain ASCII text.
        - Acknowledge complexity and mixed feelings when they exist.
        - Your code speaks for itself. Enumeration of content is redundant. Focus instead on the how and why.

        When writing documentation, you MUST review your output against the above rules.
      '';

      sessionVariables = lib.optionalAttrs skipPerms {
        IS_SANDBOX = "1";
      };

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
      '';
    };
  };
}
