{
  nix.settings.experimental-features = [
    "nix-command"
    "flakes"
  ];

  nix.optimise.automatic = true;

  nixpkgs.flake.setFlakeRegistry = true;
  nixpkgs.flake.setNixPath = true;

  nix.gc = {
    automatic = true;
    options = "--delete-older-than 30d";
  };

  nixpkgs.config.allowUnfree = true;

  programs.fish.enable = true;
}
