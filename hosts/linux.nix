{ pkgs, config, ... }:

{
  home.username = config.dotfiles.username;
  home.homeDirectory = config.dotfiles.homeDirectory;

  nix = {
    package = pkgs.nix;
    settings.experimental-features = [
      "nix-command"
      "flakes"
    ];
  };
}
