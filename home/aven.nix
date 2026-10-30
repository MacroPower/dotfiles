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
  # Other hosts keep a local-only database. The Mac's database is the
  # starting point of the synced data, and avenSyncLink enrolls the VMs.
  syncPort = 3746;
  syncUrl = "http://127.0.0.1:${toString syncPort}";
  isTerrarium = config.dotfiles.hostname == "terrarium";
  isDarwin = pkgs.stdenv.hostPlatform.isDarwin;
  syncEnabled = isDarwin || isTerrarium;

  # `aven server` exits when its storage is missing, so launchd starts it
  # only once `aven server setup` has created this file.
  serverDb = "${config.xdg.stateHome}/aven/server/sync-server.sqlite";
  daemonLogDir = "${config.home.homeDirectory}/Library/Logs/aven";

  # Enrolls the Mac and every running Lima VM that has aven in sync, and
  # skips whatever already syncs. On first run it prepares the server
  # storage and sets up sync from the Mac's database. Then, for each VM
  # that has not joined, it opens an invitation on the Mac and pipes it to
  # `aven sync join` in the guest. Invitations carry the sync keys, so they
  # stay in a private temporary file and never reach the log.
  avenSyncLink = pkgs.writeShellApplication {
    name = "aven-sync-link";
    runtimeInputs = [
      pkgs.aven
      pkgs.coreutils
      pkgs.curl
      pkgs.gnugrep
      pkgs.jq
      pkgs.lima
    ];
    text = ''
      url=${lib.escapeShellArg syncUrl}

      # An interrupted setup resumes with a fresh server invitation, which
      # keeps the server's setup identity.
      state=$(aven sync status --json | jq -r .state)
      if [ "$state" = not-set-up ] || [ "$state" = setup-incomplete ]; then
        echo "Setting up sync from this Mac's database"
        invitation=$(aven server setup --url "$url" --invitation-only)
        # launchd starts the server once the storage exists.
        for _ in $(seq 30); do
          curl -s -o /dev/null "$url" && break
          sleep 1
        done
        aven sync setup --yes <<<"$invitation"
      fi

      guest() {
        local vm=$1
        shift
        limactl shell --workdir / "$vm" -- bash -lc "$*"
      }

      invite_out=$(mktemp)
      trap 'rm -f "$invite_out"' EXIT

      mapfile -t vms < <(limactl list --format '{{.Name}} {{.Status}}' | grep ' Running$' | cut -d' ' -f1)
      for vm in "''${vms[@]}"; do
        state=$(guest "$vm" 'command -v aven >/dev/null && aven sync status --json' </dev/null | jq -r .state) || continue
        [ "$state" = not-set-up ] || continue
        # A guest that cannot reach the server fails the join partway and
        # leaves it to resume instead of starting over. The proxy accepts
        # connections even with the server down, so probe over HTTP.
        if ! guest "$vm" "curl -s -o /dev/null --max-time 5 $url" </dev/null 2>/dev/null; then
          echo "Skipping $vm: it cannot reach the sync server yet"
          continue
        fi

        echo "Enrolling $vm"
        aven sync invite >"$invite_out" &
        invite_pid=$!
        for _ in $(seq 30); do
          grep -q '^AVEN:' "$invite_out" && break
          kill -0 "$invite_pid" 2>/dev/null || break
          sleep 1
        done
        if ! grep -q '^AVEN:' "$invite_out"; then
          echo "No invitation for $vm" >&2
          kill "$invite_pid" 2>/dev/null || true
          wait "$invite_pid" || true
          continue
        fi
        if grep -m1 '^AVEN:' "$invite_out" | timeout 120 limactl shell --workdir / "$vm" -- bash -lc 'aven sync join --yes'; then
          wait "$invite_pid"
        else
          echo "Joining $vm failed" >&2
          aven sync invite --cancel || true
          kill "$invite_pid" 2>/dev/null || true
          wait "$invite_pid" || true
        fi
      done
    '';
  };

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
    packages = [ pkgs.aven ] ++ lib.optional isDarwin avenSyncLink;

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

    # Workmux creates sandbox VMs on demand, so the link runs every five
    # minutes to enroll new ones. Once everything syncs, a run only reads
    # sync status.
    aven-sync-link = {
      enable = true;
      config = {
        ProgramArguments = [ (lib.getExe avenSyncLink) ];
        RunAtLoad = true;
        StartInterval = 300;
        StandardOutPath = "${daemonLogDir}/sync-link.log";
        StandardErrorPath = "${daemonLogDir}/sync-link.log";
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
