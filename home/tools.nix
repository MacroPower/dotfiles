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

  # tfswitch refuses to create the immediate parent of its `-b` symlink
  # target and falls back to ~/bin when missing, which the Claude sandbox
  # denies. Wrap with a mkdir prelude so every invocation self-heals.
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

        # Scope searches to the current host by default
        filter_mode = "host";
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
      doggo
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

  home.sessionPath = [ "$HOME/.terraform.versions/bin" ];
}
