{ pkgs, config, ... }:

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

    gh.enable = true;

    bottom.enable = true;

    tmux = {
      enable = true;
      mouse = true;
      baseIndex = 1;
      historyLimit = 10000;
      escapeTime = 0;
      terminal = "tmux-256color";
      keyMode = "vi";
      plugins = with pkgs.tmuxPlugins; [
        sensible
        yank
      ];
    };

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

    carapace.enable = true;

    fd.enable = true;

    yazi = {
      enable = true;
      shellWrapperName = "y";
    };

    atuin = {
      enable = true;
      daemon.enable = true;
      flags = [ "--disable-up-arrow" ];
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
    yq-go
    viddy
    devbox
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
    tokei
    gping
  ];
}
