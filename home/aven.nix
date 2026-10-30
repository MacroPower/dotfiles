{
  pkgs,
  config,
  lib,
  ...
}:

let
  # Tasks sync between each macOS host and its terrarium Lima VM through
  # an aven sync server on the Mac, bound to loopback. Every device uses
  # the server URL from the setup invitation. So the guest proxies its own
  # loopback port to the Mac through the Lima host gateway
  # (host.lima.internal), and both sides reach the server at the same URL.
  # Other hosts keep a local-only database.
  #
  # Enrollment is a one-time manual step, because invitations carry keys:
  #   1. On the Mac: aven server setup --url http://127.0.0.1:3746
  #   2. On the device whose tasks to keep: aven sync setup
  #   3. On that device: aven sync invite
  #   4. On the other device: aven sync join
  syncPort = 3746;
  isTerrarium = config.dotfiles.hostname == "terrarium";
  isDarwin = pkgs.stdenv.hostPlatform.isDarwin;
  syncEnabled = isDarwin || isTerrarium;

  # `aven server` exits when its storage is missing, so launchd starts it
  # only once `aven server setup` has created this file.
  serverDb = "${config.xdg.stateHome}/aven/server/sync-server.sqlite";
  daemonLogDir = "${config.home.homeDirectory}/Library/Logs/aven";

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
    sync.enabled = syncEnabled;
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

    # launchd does not create the parent directories of its log paths, so
    # they must exist before setupLaunchAgents bootstraps the agents.
    activation.ensureAvenLogDirs = lib.mkIf isDarwin (
      lib.hm.dag.entryBetween [ "setupLaunchAgents" ] [ "writeBoundary" ] ''
        run mkdir -p ${lib.escapeShellArg daemonLogDir} "$(dirname ${lib.escapeShellArg serverDb})"
      ''
    );
  };

  # The daemon runs the background sync. Nix declares it instead of
  # `aven daemon install`, which bakes in a store path that the next
  # upgrade garbage-collects. The label and log paths match what
  # `aven daemon install` writes, so `aven daemon status` recognizes it.
  launchd.agents = lib.mkIf isDarwin {
    aven-daemon = {
      enable = true;
      config = {
        Label = "com.raine.aven.daemon";
        ProgramArguments = [
          (lib.getExe pkgs.aven)
          "daemon"
        ];
        KeepAlive = true;
        RunAtLoad = true;
        StandardOutPath = "${daemonLogDir}/daemon.out.log";
        StandardErrorPath = "${daemonLogDir}/daemon.err.log";
      };
    };

    aven-server = {
      enable = true;
      config = {
        ProgramArguments = [
          (lib.getExe pkgs.aven)
          "server"
          "--bind"
          "127.0.0.1:${toString syncPort}"
        ];
        KeepAlive.PathState.${serverDb} = true;
        StandardOutPath = "${dirOf serverDb}/server.log";
        StandardErrorPath = "${dirOf serverDb}/server.err.log";
      };
    };
  };

  systemd.user = lib.mkIf isTerrarium {
    services.aven-daemon = {
      Unit.Description = "aven background sync";
      Service = {
        ExecStart = "${lib.getExe pkgs.aven} daemon";
        Restart = "on-failure";
        RestartSec = 10;
      };
      Install.WantedBy = [ "default.target" ];
    };

    # Forwards the guest's loopback sync port to the server on the Mac.
    # The Lima config in home/virtualization.nix keeps Lima from forwarding
    # this port back to the host.
    sockets.aven-sync-proxy = {
      Unit.Description = "aven sync server proxy to the macOS host";
      Socket.ListenStream = "127.0.0.1:${toString syncPort}";
      Install.WantedBy = [ "sockets.target" ];
    };
    services.aven-sync-proxy = {
      Unit.Description = "aven sync server proxy to the macOS host";
      Service.ExecStart = "${pkgs.systemd}/lib/systemd/systemd-socket-proxyd host.lima.internal:${toString syncPort}";
    };
  };
}
