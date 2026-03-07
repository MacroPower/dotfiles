{ pkgs, config, ... }:

{
  imports = [ ./settings.nix ];

  home.username = config.dotfiles.username;
  home.homeDirectory = config.dotfiles.homeDirectory;

  nix.package = pkgs.nix;
}
