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

  # Wrapper script that reads the KAGI_API_KEY from sops at runtime
  kagiWrapper = pkgs.writeShellScript "kagi-mcp-wrapper" ''
    export KAGI_API_KEY="$(cat ${config.sops.secrets.kagi_api_key.path} 2>/dev/null || true)"
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
      description = "Whether to skip Claude Code permission prompts.";
    };

    extraSettings = mkOption {
      type = types.attrsOf types.anything;
      default = { };
      description = "Additional settings merged into Claude Code settings.json.";
    };
  };

  config = lib.mkIf cfg.enable {
    programs = {
      mcp = {
        enable = true;
        servers = {
          fetch = {
            type = "stdio";
            command = "${pkgs.uv}/bin/uvx";
            args = [
              "--isolated"
              "mcp-server-fetch"
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
        };
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
            deny = [
              "WebSearch"
              "WebFetch"
              "Read(./.env)"
              "Read(./.secrets)"
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
            ];
            ask = [
              "Bash(git push *)"
              "Bash(git push)"
            ];
          };
          statusLine = {
            type = "command";
            command = "${pkgs.llm-agents.ccstatusline}/bin/ccstatusline";
            padding = 0;
          };
          enabledPlugins = {
            "commit-commands@claude-plugins-official" = true;
            "claude-md-management@claude-plugins-official" = true;
            "skill-creator@claude-plugins-official" = true;
            "code-review@claude-plugins-official" = true;
          };
          sandbox = {
            enabled = pkgs.stdenv.isDarwin;
            network = {
              allowAllUnixSockets = true;
              allowLocalBinding = true;
              allowedDomains = [
                "jacobcolvin.com"
                "api.githubcopilot.com"
              ];
            };
            filesystem = {
              allowWrite = [
                "//tmp/git"
              ];
            };
            excludedCommands = [
              "docker"
              "dagger"
              "git"
            ];
          };
          hooks = {
            PreToolUse = [
              {
                matcher = "Bash";
                hooks = [
                  {
                    type = "command";
                    command = "${pkgs.llm-agents.rtk}/libexec/rtk/hooks/rtk-rewrite.sh";
                  }
                ];
              }
            ];
          };
          alwaysThinkingEnabled = true;
          skipDangerousModePermissionPrompt = true;
          teammateMode = "auto";
        } cfg.extraSettings;

        agentsDir = ../configs/claude/agents;
        skillsDir = ../configs/claude/skills;
      };

      fish.shellAliases = lib.optionalAttrs skipPerms {
        claude = "command claude --dangerously-skip-permissions";
      };
    };

    xdg.configFile."ccstatusline/settings.json".source = ../configs/ccstatusline/settings.json;

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
        - To read files from a GitHub repo, `git clone` it into `/tmp/git/<owner>/<repo>` and read from there.
        - If a repo already exists in `/tmp/git/<owner>/<repo>`, `git status` and `git pull` if needed.

        Remember: Do research, don't guess.

        ## Writing Style

        - Keep responses to plain ASCII text. Use commas, semicolons, parentheses, or separate sentences for clauses.
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

        ${lib.optionalString skipPerms ''
          # Pre-trust home directory and authenticate with scoped PAT (sandbox only)
          UPDATED=$(echo "$UPDATED" | ${pkgs.jq}/bin/jq \
            '.projects["${config.dotfiles.homeDirectory}"].hasTrustDialogAccepted = true')
          GH_TOKEN=$(cat ${config.sops.secrets.gh_token.path} 2>/dev/null || true)
          if [ -z "$DRY_RUN_CMD" ] && [ -n "''${GH_TOKEN:-}" ]; then
            echo "''${GH_TOKEN}" | ${pkgs.gh}/bin/gh auth login --with-token
            ${pkgs.fish}/bin/fish -c "set -Ux GITHUB_PERSONAL_ACCESS_TOKEN ''${GH_TOKEN}"
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
