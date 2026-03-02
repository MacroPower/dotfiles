{ pkgs, ... }:

{
  programs = {
    bat.enable = true;

    eza = {
      enable = true;
      enableFishIntegration = false;
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

    tmux.enable = true;

    ripgrep = {
      enable = true;
      arguments = [
        "--smart-case"
        "--hidden"
        "--glob=!.git"
      ];
    };
  };

  home.packages = with pkgs; [
    jq
    yq-go
    trippy
    viddy
    devbox
    doppler
  ];
}
