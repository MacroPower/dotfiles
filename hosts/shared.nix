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

  # Lix 2.95 removed builtins.fetchClosure, which stock devbox needs.
  # devboxOverlay in lib/default.nix patches devbox to run without it.
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

  programs.nh = {
    enable = true;
    clean = {
      enable = true;
      # --keep 5 leaves rollback targets on hosts that go more than 30 days
      # between switches. --no-gcroots keeps result links and nix-direnv
      # shells rooted at any age, so a project untouched for 30 days still
      # has its dev shell.
      extraArgs = "--keep 5 --keep-since 30d --no-gcroots";
    };
  };

  programs.fish.enable = true;
}
