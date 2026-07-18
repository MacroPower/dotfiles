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

  # Pin Lix to 2.94 rather than tracking `stable`: Lix 2.95 removed
  # builtins.fetchClosure (it is no longer even a known experimental feature).
  # devbox's generated flakes fetch every package via fetchClosure from
  # cache.nixos.org, so on 2.95 `devbox`/`print-dev-env` fails with
  # "attribute 'fetchClosure' missing". Revisit when devbox stops relying on
  # fetchClosure or Lix restores it.
  nix.package = pkgs.lixPackageSets.lix_2_94.lix;

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
        "claude-code-wrapped"
        "diagram.nvim"
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
