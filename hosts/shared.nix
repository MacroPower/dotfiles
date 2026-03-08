{
  imports = [ ./settings.nix ];

  # Use scheduled optimisation instead of auto-optimise-store to avoid
  # the .tmp-link race condition (NixOS/nix#7273), which also affects Lix.
  nix.optimise.automatic = true;

  nixpkgs = {
    flake.setFlakeRegistry = true;
    flake.setNixPath = true;
    config.allowUnfree = true;
  };

  nix.gc = {
    automatic = true;
    options = "--delete-older-than 30d";
  };

  programs.fish.enable = true;
}
