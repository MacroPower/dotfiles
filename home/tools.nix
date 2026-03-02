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
  };

  home.packages = with pkgs; [
    ripgrep
    jq
    yq-go
    trippy
    viddy
    delta
    devbox
    doppler
    dagger
  ];
}
