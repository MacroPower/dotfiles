{ pkgs, ... }:

{
  programs = {
    bat.enable = true;

    eza = {
      enable = true;
      enableFishIntegration = true;
    };

    fzf = {
      enable = true;
      enableFishIntegration = false;
    };

    zoxide = {
      enable = true;
      enableFishIntegration = true;
    };

    direnv = {
      enable = true;
      nix-direnv.enable = true;
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

    jq.enable = true;
    trippy.enable = true;
  };

  home.packages = with pkgs; [
    yq-go
    viddy
    devbox
    doppler
    dagger
    gitui
  ];
}
