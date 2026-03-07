{
  pkgs,
  lib,
  config,
  ...
}:

let
  skipPerms = config.dotfiles.claude.dangerouslySkipPermissions;

  # Wrapper script that reads the KAGI_API_KEY from sops at runtime
  kagiWrapper = pkgs.writeShellScript "kagi-mcp-wrapper" ''
    export KAGI_API_KEY="$(cat ${config.sops.secrets.kagi_api_key.path} 2>/dev/null || true)"
    export KAGI_SUMMARIZER_ENGINE="agnes"
    exec ${pkgs.uv}/bin/uvx --managed-python --python=3.13 kagimcp "$@"
  '';
in
{
  programs = {
    mcp = {
      enable = true;
      servers = {
        context7 = {
          type = "stdio";
          command = "npx";
          args = [
            "-y"
            "@upstash/context7-mcp"
          ];
        };
        kagi = {
          type = "stdio";
          command = "${kagiWrapper}";
        };
      };
    };

    claude-code = {
      enable = true;
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
            "Read(./.env)"
            "Read(./.secrets)"
          ];
          ask = [
            "Bash(git push *)"
            "Bash(git push)"
          ];
        };
        statusLine = {
          type = "command";
          command = "npx -y ccstatusline@latest";
          padding = 0;
        };
        enabledPlugins = {
          "commit-commands@claude-plugins-official" = true;
          "context7@claude-plugins-official" = true;
          "claude-md-management@claude-plugins-official" = true;
          "skill-creator@claude-plugins-official" = true;
        };
        sandbox = {
          enabled = pkgs.stdenv.isDarwin;
          network = {
            allowAllUnixSockets = true;
            allowLocalBinding = true;
            allowedDomains = [ "jacobcolvin.com" ];
          };
          excludedCommands = [
            "docker"
            "dagger"
            "git"
          ];
        };
        alwaysThinkingEnabled = true;
        skipDangerousModePermissionPrompt = true;
        teammateMode = "auto";
      } config.dotfiles.claude.extraSettings;

      agentsDir = ../configs/claude/agents;
      skillsDir = ../configs/claude/skills;
    };

    fish.shellAliases = lib.optionalAttrs skipPerms {
      claude = "command claude --dangerously-skip-permissions";
    };
  };

  home.sessionVariables = lib.optionalAttrs skipPerms {
    IS_SANDBOX = "1";
  };

  # Activation: inject sops secrets and sandbox config into mutable ~/.claude.json
  home.activation.syncClaudeJson = lib.hm.dag.entryAfter [ "writeBoundary" "sops-nix" ] (
    lib.optionalString skipPerms ''
      CLAUDE_JSON="$HOME/.claude.json"
      GH_TOKEN=$(cat ${config.sops.secrets.gh_token.path} 2>/dev/null || true)

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

      # Pre-trust home directory to skip workspace trust prompt
      UPDATED=$(echo "$EXISTING" | ${pkgs.jq}/bin/jq \
        '.projects["${config.dotfiles.homeDirectory}"].hasTrustDialogAccepted = true')

      # Authenticate gh CLI with scoped fine-grained PAT
      if [ -z "$DRY_RUN_CMD" ] && [ -n "''${GH_TOKEN:-}" ]; then
        echo "''${GH_TOKEN}" | ${pkgs.gh}/bin/gh auth login --with-token
      fi

      # Atomic write
      if [ -z "$DRY_RUN_CMD" ]; then
        TMPFILE=$(mktemp "$CLAUDE_JSON.tmp.XXXXXX")
        echo "$UPDATED" > "$TMPFILE"
        chmod 600 "$TMPFILE"
        mv "$TMPFILE" "$CLAUDE_JSON"
      else
        echo "Would write merged MCP config to $CLAUDE_JSON"
      fi
    ''
  );
}
