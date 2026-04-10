{
  pkgs,
  lib,
  config,
  ...
}:
{
  nix.settings = import ./settings.nix {
    inherit pkgs;
    inherit (config.dotfiles.system) username;
  };

  nix.package = pkgs.lixPackageSets.stable.lix;

  # Use scheduled optimisation instead of auto-optimise-store to avoid
  # the .tmp-link race condition (NixOS/nix#7273), which also affects Lix.
  nix.optimise.automatic = true;

  nixpkgs = {
    flake.setFlakeRegistry = true;
    flake.setNixPath = true;
    config.allowUnfreePredicate =
      pkg:
      builtins.elem (lib.getName pkg) [
        "appcleaner"
        "claude-code"
        "discord"
        "drawio"
        "frankerfacez"
        "firefox-bin"
        "firefox-bin-unwrapped"
        "keka"
        "monodraw"
        "obsidian"
        "slack"
        "vlc-bin"
      ];
  };

  nix.gc = {
    automatic = true;
    options = "--delete-older-than 30d";
  };

  programs.fish.enable = true;
}
