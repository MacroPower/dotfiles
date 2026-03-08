{
  imports = [ ./settings.nix ];

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
