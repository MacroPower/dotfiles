{
  pkgs,
  config,
  lib,
  ...
}:

let
  # Starts a workmux agent on the task selected in the aven TUI. Runs from the
  # directory aven opened in, which must belong to the task's project, so the
  # worktree lands in the right repository.
  avenWorkmuxDispatch = pkgs.writeShellApplication {
    name = "aven-workmux-dispatch";
    runtimeInputs = [
      pkgs.jq
      pkgs.slugify
    ];
    text = ''
      input=$(cat)
      field() { jq -r "$1" <<<"$input"; }

      ref=$(field '.selection.primary.ref')
      title=$(field '.selection.primary.task.title')
      project=$(field '.selection.primary.project.key')
      aven=$(field '.invocation.aven_exe // "aven"')
      db=$(field '.invocation.db_path // empty')
      db_args=()
      if [ -n "$db" ]; then
        db_args=(--db "$db")
      fi

      cd "$(field '.invocation.origin_cwd')"
      here=$("$aven" "''${db_args[@]}" prime --json | jq -r '.project // empty')
      if [ "$here" != "$project" ]; then
        echo "$ref belongs to project '$project', but aven opened in project (''${here:-none}). Open aven from that repository." >&2
        exit 1
      fi

      workmux add "$(slugify "$ref $title")" \
        -p "Work on $ref. Use aven for status and handoff notes."
    '';
  };

  # Settings owned by this module. Activation merges them into the mutable
  # config.yaml, because aven writes to that file itself (`aven config set`,
  # `aven project path add`, sync setup). Keys set here win on every switch,
  # and keys absent here stay as aven wrote them.
  managedConfig = {
    # Nix pins the binary, so update badges only point at a release that
    # `aven update` cannot install.
    update.automatic_checks = false;
    local = {
      # Ghostty draws Kitty graphics, and tmux sets allow-passthrough.
      inline_images = "on";
      image_optimization = "paste";
    };
    tui = {
      # Use the terminal palette, which stylix already themes.
      appearance = "terminal";
      commands = [
        {
          name = "workmux";
          aliases = [ "agent" ];
          description = "Start a workmux agent on the task";
          program = lib.getExe avenWorkmuxDispatch;
          keys = [ "z w" ];
          target = "focused";
          execution = "wait";
          timeout_seconds = 60;
          on_success = "quit";
        }
      ];
    };
    # Natural-language capture (`^N` in the composer). `--bare` skips the
    # OAuth login, so turn off tools, MCP, skills, and hooks instead.
    agent.task_intake = {
      command = "claude";
      args = [
        "-p"
        "--no-session-persistence"
        "--model"
        "haiku"
        "--tools"
        ""
        "--strict-mcp-config"
        "--disable-slash-commands"
        "--settings"
        (builtins.toJSON { disableAllHooks = true; })
        "{prompt}"
      ];
      timeout_seconds = 60;
    };
  };

  # YAML rather than JSON: yq keeps the loaded nodes' flow style, and JSON
  # input turns the merged file into quoted flow mappings.
  managedConfigFile = (pkgs.formats.yaml { }).generate "aven-config.yaml" managedConfig;
  configFile = "${config.xdg.configHome}/aven/config.yaml";
in
{
  home = {
    packages = [ pkgs.aven ];

    # Nix pins aven, so the self-update check only produces noise.
    sessionVariables.AVEN_NO_UPDATE_CHECK = "1";

    activation.mergeAvenConfig = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
      run mkdir -p "$(dirname ${lib.escapeShellArg configFile})"
      [ -s ${lib.escapeShellArg configFile} ] || run touch ${lib.escapeShellArg configFile}
      run ${lib.getExe pkgs.yq-go} -i '. *= load("${managedConfigFile}")' ${lib.escapeShellArg configFile}
    '';
  };
}
