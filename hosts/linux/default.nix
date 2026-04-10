{ pkgs, config, ... }:

{
  home.username = config.dotfiles.username;
  home.homeDirectory = config.dotfiles.homeDirectory;

  nix.package = pkgs.lixPackageSets.stable.lix;
  nix.settings = import ../settings.nix {
    inherit pkgs;
    inherit (config.dotfiles) username;
  };
}
