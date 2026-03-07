{
  pkgs,
  lib,
  config,
  ...
}:

let
  skipPerms = config.dotfiles.claude.dangerouslySkipPermissions;
in
{
  programs.claude-code = {
    enable = true;

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

    mcpServers = {
      context7 = {
        type = "stdio";
        command = "npx";
        args = [
          "-y"
          "@upstash/context7-mcp"
        ];
      };
    };

    agentsDir = ../configs/claude/agents;
    skillsDir = ../configs/claude/skills;
  };

  programs.fish.shellAliases = lib.optionalAttrs skipPerms {
    claude = "command claude --dangerously-skip-permissions";
  };

  home.sessionVariables = lib.optionalAttrs skipPerms {
    IS_SANDBOX = "1";
  };

  # Activation: inject sops secrets into mutable ~/.claude.json
  home.activation.syncClaudeMcpServers = lib.hm.dag.entryAfter [ "writeBoundary" "sops-nix" ] ''
    CLAUDE_JSON="$HOME/.claude.json"

    # Read secrets from sops-decrypted files
    KAGI_API_KEY=$(cat ${config.sops.secrets.kagi_api_key.path} 2>/dev/null || true)
    GH_TOKEN=$(cat ${config.sops.secrets.gh_token.path} 2>/dev/null || true)

    # Build kagi MCP server config with injected secret
    KAGI_SERVER='{}'
    if [ -n "''${KAGI_API_KEY:-}" ]; then
      KAGI_SERVER=$(${pkgs.jq}/bin/jq -n \
        --arg key "$KAGI_API_KEY" \
        '{type: "stdio", command: "uvx", args: ["--managed-python", "--python=3.13", "kagimcp"], env: {KAGI_SUMMARIZER_ENGINE: "agnes", KAGI_API_KEY: $key}}')
    fi

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

    # Merge kagi server into .mcpServers (if secret available)
    if [ -n "''${KAGI_API_KEY:-}" ]; then
      UPDATED=$(echo "$EXISTING" | ${pkgs.jq}/bin/jq --argjson kagi "$KAGI_SERVER" \
        '.mcpServers.kagi = $kagi')
    else
      UPDATED="$EXISTING"
    fi

    ${lib.optionalString skipPerms ''
      # Pre-trust home directory to skip workspace trust prompt
      UPDATED=$(echo "$UPDATED" | ${pkgs.jq}/bin/jq \
        '.projects["${config.dotfiles.homeDirectory}"].hasTrustDialogAccepted = true')

      # Authenticate gh CLI with scoped fine-grained PAT
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
}
