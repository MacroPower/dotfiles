{
  pkgs,
  config,
  ...
}:

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
      config.global = {
        hide_env_diff = true;
        warn_timeout = "30s";
      };
    };

    gh = {
      enable = true;
      extensions = [ ];
      settings = {
        git_protocol = "ssh";
        editor = "vim";
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
    lazydocker.enable = true;
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

  home.packages = with pkgs; [
    go-task
    yq-go
    viddy
    doppler
    dagger
    nvd
    nurl
    sops
    age
    dust
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
    tfswitch
    devbox
    angle-grinder
    zstd
  ];
}
