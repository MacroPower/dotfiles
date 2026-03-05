{
  pkgs,
  lib,
  config,
  ...
}:

let
  # ── MCP Servers ──────────────────────────────────────────────
  # Base servers shared across all hosts.
  # Secret env vars are listed in `envVars` and injected from the
  # shell environment at activation time (never stored in nix store).
  baseMcpServers = {
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
      command = "uvx";
      args = [
        "--managed-python"
        "--python=3.13"
        "kagimcp"
      ];
      envVars = [ "KAGI_API_KEY" ];
      env = {
        KAGI_SUMMARIZER_ENGINE = "agnes";
      };
    };
  };

  # Per-host servers override/extend the base set.
  # Set a server to null to remove it on a specific host.
  allMcpServers = lib.filterAttrs (_: v: v != null) (
    baseMcpServers // config.dotfiles.claude.extraMcpServers
  );

  # Strip envVars metadata (not part of Claude's schema)
  cleanServer = _: server: builtins.removeAttrs server [ "envVars" ];
  mcpServersJson = builtins.toJSON (builtins.mapAttrs cleanServer allMcpServers);

  # Collect all envVars entries for shell injection
  envVarsList = lib.concatLists (
    lib.mapAttrsToList (
      serverName: server: map (varName: { inherit serverName varName; }) (server.envVars or [ ])
    ) allMcpServers
  );

  # Shell code to inject env var values into MCP server JSON
  envVarInjections = lib.concatMapStringsSep "\n" (
    entry:
    let
      varRef = "$" + entry.varName;
    in
    ''
      if [ -n "''${${entry.varName}:-}" ]; then
        SERVERS=$(echo "$SERVERS" | ${pkgs.jq}/bin/jq \
          --arg val "${varRef}" \
          '.${entry.serverName}.env.${entry.varName} = $val')
      fi
    ''
  ) envVarsList;

  # ── Dangerous-mode opt-in (per-host) ─────────────────────────
  skipPerms = config.dotfiles.claude.dangerouslySkipPermissions;

  # ── Settings ─────────────────────────────────────────────────
  baseSettings = {
    "$schema" = "https://json.schemastore.org/claude-code-settings.json";
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
  };

  finalSettings = lib.recursiveUpdate baseSettings config.dotfiles.claude.extraSettings;

  # Pretty-print settings.json at nix build time
  settingsFile =
    pkgs.runCommand "claude-settings.json"
      {
        nativeBuildInputs = [ pkgs.jq ];
        json = builtins.toJSON finalSettings;
        passAsFile = [ "json" ];
      }
      ''
        jq '.' "$jsonPath" > "$out"
      '';

in
{
  programs.fish.shellAliases = lib.optionalAttrs skipPerms {
    claude = "command claude --dangerously-skip-permissions";
  };

  home.sessionVariables = lib.optionalAttrs skipPerms {
    IS_SANDBOX = "1";
  };

  home = {
    file = {
      # Deploy agents and skills from source tree
      ".claude/agents" = {
        source = ../configs/claude/agents;
        recursive = true;
      };

      ".claude/skills" = {
        source = ../configs/claude/skills;
        recursive = true;
      };

      # Generated settings.json (pretty-printed)
      ".claude/settings.json".source = settingsFile;
    };

    # Activation: merge mcpServers into mutable ~/.claude.json
    activation.syncClaudeMcpServers = lib.hm.dag.entryAfter [ "writeBoundary" "sops-nix" ] ''
      CLAUDE_JSON="$HOME/.claude.json"
      SERVERS='${mcpServersJson}'

      # Read secrets from sops-decrypted files
      KAGI_API_KEY=$(cat ${config.sops.secrets.kagi_api_key.path} 2>/dev/null || true)
      GH_TOKEN=$(cat ${config.sops.secrets.gh_token.path} 2>/dev/null || true)

      # Inject env var values into MCP server config
      ${envVarInjections}

      # Read existing file or start fresh
      if [ -f "$CLAUDE_JSON" ]; then
        if ${pkgs.jq}/bin/jq empty "$CLAUDE_JSON" 2>/dev/null; then
          EXISTING=$(cat "$CLAUDE_JSON")
        else
          echo "Warning: ~/.claude.json is malformed, backing up" >&2
          cp "$CLAUDE_JSON" "$CLAUDE_JSON.bak.$(date +%s)"
          EXISTING='{}'
        fi
      else
        EXISTING='{}'
      fi

      # Replace .mcpServers entirely (nix is source of truth)
      UPDATED=$(echo "$EXISTING" | ${pkgs.jq}/bin/jq --argjson servers "$SERVERS" \
        '.mcpServers = $servers')

      ${lib.optionalString skipPerms ''
        # Pre-trust home directory to skip workspace trust prompt
        UPDATED=$(echo "$UPDATED" | ${pkgs.jq}/bin/jq \
          '.projects["${config.dotfiles.homeDirectory}"].hasTrustDialogAccepted = true')

        # Authenticate gh CLI with scoped fine-grained PAT
        if [ -n "''${GH_TOKEN:-}" ]; then
          echo "''${GH_TOKEN}" | ${pkgs.gh}/bin/gh auth login --with-token
        fi
      ''}

      # Atomic write
      TMPFILE=$(mktemp "$CLAUDE_JSON.tmp.XXXXXX")
      echo "$UPDATED" > "$TMPFILE"
      chmod 600 "$TMPFILE"
      mv "$TMPFILE" "$CLAUDE_JSON"
    '';
  };
}
