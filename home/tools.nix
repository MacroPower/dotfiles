{
  pkgs,
  config,
  lib,
  ...
}:

let
  baseDirenvConfig = {
    global = {
      hide_env_diff = true;
      warn_timeout = "30s";
    };
  };
  sandboxDirenvConfig = lib.optionalAttrs (config.dotfiles.hostname == "terrarium") {
    whitelist.prefix = [ "/Users/${config.dotfiles.username}/Documents/repos" ];
  };

  # A self-hosted atuin sync server runs on each macOS host, bound to
  # loopback. The terrarium Lima VM reaches it through the Lima host
  # gateway (host.lima.internal), which under vmType=vz proxies the
  # guest's connection to the host's loopback. Hosts with no local server
  # (TrueNAS, OrbStack, the Nix container) are left unsynced.
  atuinPort = 8888;
  atuinSyncAddress =
    if config.dotfiles.hostname == "terrarium" then
      "http://host.lima.internal:${toString atuinPort}"
    else if pkgs.stdenv.isDarwin then
      "http://127.0.0.1:${toString atuinPort}"
    else
      null;

  atuinServerDataDir = "${config.xdg.dataHome}/atuin-server";

  # tfswitch refuses to create the immediate parent of its `-b` symlink
  # target and falls back to ~/bin when missing, which the Claude sandbox
  # denies. Activation materializes ~/.terraform.versions/bin; this mkdir
  # prelude re-creates it at runtime should the directory be removed
  # mid-session.
  tfswitchWrapped = pkgs.symlinkJoin {
    name = "tfswitch-wrapped";
    paths = [ pkgs.tfswitch ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    postBuild = ''
      wrapProgram $out/bin/tfswitch \
        --run "mkdir -p ${config.home.homeDirectory}/.terraform.versions/bin"
    '';
  };

  # tflint vendors a go-plugin version that ignores PLUGIN_UNIX_SOCKET_DIR
  # and always binds its IPC sockets in `os.TempDir()`. The Claude sandbox
  # overrides $TMPDIR to a path that blocks unix-socket binds, so redirect
  # to ~/.tflint.d/tmp, which is in the sandbox's write + unix-socket
  # allowlists via the opentofu bundle. Outside the sandbox the redirect
  # is harmless -- sockets just land under ~/.tflint.d/tmp instead of the
  # system temp dir.
  tflintWrapped = pkgs.symlinkJoin {
    name = "tflint-wrapped";
    paths = [ pkgs.tflint ];
    nativeBuildInputs = [ pkgs.makeWrapper ];
    postBuild = ''
      wrapProgram $out/bin/tflint \
        --run "mkdir -p ${config.home.homeDirectory}/.tflint.d/tmp" \
        --set TMPDIR "${config.home.homeDirectory}/.tflint.d/tmp"
    '';
  };
in
{
  programs = {
    bat = {
      enable = true;
      config = {
        style = "numbers,changes,header";
        pager = "less -FR";
      };
    };

    eza = {
      enable = true;
      icons = "auto";
      colors = "auto";
      git = true;
      extraOptions = [
        "--group-directories-first"
        "--header"
        "--all"
      ];
    };

    fzf.enable = true;

    zoxide.enable = true;

    direnv = {
      enable = true;
      nix-direnv.enable = true;
      config = baseDirenvConfig // sandboxDirenvConfig;
    };

    gh = {
      enable = true;
      extensions = [ ];
      settings = {
        git_protocol = "ssh";
        editor = "nvim";
      };
    };

    bottom.enable = true;

    ripgrep = {
      enable = true;
      arguments = [
        "--smart-case"
        "--hidden"
        "--glob=!.git"
      ];
    };

    nix-index.enable = true;
    nix-index-database.comma.enable = true;

    nix-your-shell = {
      enable = true;
      nix-output-monitor.enable = true;
    };

    carapace = {
      enable = true;
      ignoreCase = true;
    };

    fd.enable = true;

    yazi = {
      enable = true;
      shellWrapperName = "y";
    };

    atuin = {
      enable = true;
      daemon.enable = true;
      flags = [ "--disable-up-arrow" ];
      settings = {
        update_check = false;
        style = "auto";
        keymap_mode = "auto";

        # Show synced history from all machines by default
        filter_mode = "global";
        # Auto-filter by git repo when inside one
        workspaces = true;
        # Search bar at the top (fzf-like)
        invert = true;

        # Keep history clean, exclude trivial commands
        history_filter = [
          "^cd$"
          "^ls$"
          "^clear$"
          "^exit$"
          "^pwd$"
        ];

        # Track subcommands for better `atuin stats`
        stats.common_subcommands = [
          "cargo"
          "docker"
          "git"
          "go"
          "kubectl"
          "nix"
          "npm"
          "systemctl"
          "tmux"
          "task"
          "dagger"
          "gh"
          "brew"
        ];
      }
      // lib.optionalAttrs (atuinSyncAddress != null) {
        sync_address = atuinSyncAddress;
        auto_sync = true;
        sync_frequency = "5m";
      }
      // lib.optionalAttrs (config.dotfiles.hostname == "terrarium") {
        # Read the encryption key straight from the macOS host's atuin data
        # dir, which Lima mounts into the guest at the same absolute path.
        # The guest decrypts the same synced history as the host without a
        # local copy, so `atuin login` needs no `-k`. The mount is
        # read-only, which is fine -- atuin only reads the key.
        key_path = "/Users/${config.dotfiles.username}/.local/share/atuin/key";
      };
    };

    jq.enable = true;
    trippy.enable = true;
    lazydocker = {
      enable = true;
      settings = {
        # `settings` replaces the home-manager module's default wholesale,
        # so re-state the compose template it ships with.
        commandTemplates.dockerCompose = "docker compose";

        gui.theme = {
          activeBorderColor = [
            "#${config.lib.stylix.colors.base0D}"
            "bold"
          ];
          inactiveBorderColor = [ "#${config.lib.stylix.colors.base04}" ];
          selectedLineBgColor = [ "#${config.lib.stylix.colors.base02}" ];
          optionsTextColor = [ "#${config.lib.stylix.colors.base0C}" ];
        };
      };
    };
    difftastic.enable = true;
    docker-cli.enable = true;

    nh = {
      enable = true;
      flake = "${config.home.homeDirectory}/repos/dotfiles";
    };

    tealdeer = {
      enable = true;
      settings.updates.auto_update = true;
    };

    gitui = {
      enable = true;
      theme = builtins.readFile ../configs/gitui/theme.ron;
    };
  };

  home.packages =
    with pkgs;
    [
      leanspec-cli
      copilot-api-proxy
      go-task
      yq-go
      viddy
      doppler
      dagger
      nvd
      nurl
      sops
      age
      hyperfine
      sd
      procs
      xh
      knot-dns
      dive
      cosign
      tokei
      gping
      lefthook
      tfswitchWrapped
      tflintWrapped
      devbox
      angle-grinder
      zstd
      pv
      progress
    ]
    ++ lib.optionals pkgs.stdenv.isDarwin [
      mdcopy
    ];

  # Relocates the tofu CLI config out of ~/.terraform.d/ so the Claude
  # sandbox (which blocks $HOME) can reach it via TF_CLI_CONFIG_FILE.
  # Credentials-free on purpose: backend tokens flow through TF_TOKEN_*
  # env vars sourced from sops.
  xdg.configFile."opentofu/tofurc".text = ''
    disable_checkpoint           = true
    disable_checkpoint_signature = true
  '';

  # tfswitch defaults to symlinking /usr/local/bin/tofu (denied by the
  # Claude sandbox) and falls back to ~/bin (creation also denied).
  # Point it at ~/.terraform.versions/bin, which lives under a path
  # already in the sandbox's allowWrite list for the opentofu bundle.
  home.file.".tfswitch.toml".text = ''
    bin = "${config.home.homeDirectory}/.terraform.versions/bin/tofu"
    product = "opentofu"
  '';

  # tfswitch installs the `tofu` symlink into ~/.terraform.versions/bin but
  # never creates that directory itself -- when the parent is missing it
  # silently falls back to symlinking ~/bin, which the Claude sandbox denies.
  # Materialize the directory at activation (independent of the tfswitchWrapped
  # runtime mkdir) so the configured bin path always wins.
  home.file.".terraform.versions/bin/.keep".text = "";

  home.sessionPath = [ "$HOME/.terraform.versions/bin" ];

  # The home-manager atuin module starts the daemon with `atuin daemon start`,
  # the one start path that skips stale-socket cleanup. An unclean termination
  # (panic, hard reboot, SIGKILL) leaves ~/.local/share/atuin/daemon.sock
  # behind; every subsequent start then fails at UnixListener::bind with
  # EADDRINUSE and exits 1, so launchd crash-loops the agent and atuin (which
  # has no sqlite fallback in daemon mode) silently drops all history. The
  # `--force` flag runs force_cleanup() to remove the stale socket + pidfile
  # before binding. Safe here because launchd only relaunches the agent after
  # the prior instance has exited, so force_cleanup's kill targets a dead pid.
  launchd.agents.atuin-daemon.config.ProgramArguments =
    lib.mkIf (pkgs.stdenv.isDarwin && config.programs.atuin.daemon.enable)
      (
        lib.mkForce [
          (lib.getExe config.programs.atuin.package)
          "daemon"
          "start"
          "--force"
        ]
      );

  # Self-hosted atuin sync server, bound to loopback: the local client
  # reaches it directly and the Lima guest via host.lima.internal, but it
  # is never exposed to the LAN, so leaving registration open is low-risk.
  # ATUIN_CONFIG_DIR isolates server state from the home-manager-managed
  # client config at ~/.config/atuin.
  launchd.agents.atuin-server = lib.mkIf pkgs.stdenv.isDarwin {
    enable = true;
    config = {
      ProgramArguments = [
        (lib.getExe' config.programs.atuin.package "atuin-server")
        "start"
      ];
      KeepAlive = true;
      RunAtLoad = true;
      EnvironmentVariables = {
        ATUIN_HOST = "127.0.0.1";
        ATUIN_PORT = toString atuinPort;
        ATUIN_OPEN_REGISTRATION = "true";
        ATUIN_DB_URI = "sqlite://${atuinServerDataDir}/atuin.db";
        ATUIN_CONFIG_DIR = atuinServerDataDir;
      };
      StandardOutPath = "${atuinServerDataDir}/server.log";
      StandardErrorPath = "${atuinServerDataDir}/server.err.log";
    };
  };

  # launchd opens StandardOutPath/StandardErrorPath at load time and does
  # not create their parent directory, so the data dir must exist before
  # home-manager's setupLaunchAgents bootstraps the agent -- otherwise the
  # bootstrap fails on the missing log paths. Ordered after writeBoundary
  # (the dir's home lives under $HOME) and before setupLaunchAgents.
  home.activation.ensureAtuinServerDir = lib.mkIf pkgs.stdenv.isDarwin (
    lib.hm.dag.entryBetween [ "setupLaunchAgents" ] [ "writeBoundary" ] ''
      run mkdir -p ${lib.escapeShellArg atuinServerDataDir}
    ''
  );
}
