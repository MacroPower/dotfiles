{ pkgs, hostConfig, ... }:

{
  home.username = hostConfig.username;
  home.homeDirectory = hostConfig.homeDirectory;

  nix = {
    package = pkgs.nix;
    settings.experimental-features = [
      "nix-command"
      "flakes"
    ];
  };
}
