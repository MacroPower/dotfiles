{ pkgs, ... }:

{
  programs = {
    bat.enable = true;

    eza = {
      enable = true;
      enableFishIntegration = true;
      icons = "auto";
      colors = "auto";
      git = true;
      extraOptions = [
        "--group-directories-first"
        "--header"
        "--all"
      ];
    };

    fzf = {
      enable = true;
      enableFishIntegration = true;
    };

    zoxide = {
      enable = true;
      enableFishIntegration = true;
    };

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
      enableFishIntegration = true;
    };

    jq.enable = true;
    trippy.enable = true;

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
    nix-output-monitor
    nh
    sops
    age
  ];
}
