{
  inputs,
  sharedOverlays,
  sharedStylixConfig,
  mkHomeManagerBlock,
}:
{
  system,
  hostModule,
  username,
  homeModule,
}:
inputs.nixpkgs.lib.nixosSystem {
  modules = [
    hostModule
    { dotfiles.system = { inherit username; }; }
    inputs.home-manager.nixosModules.home-manager
    inputs.stylix.nixosModules.stylix
    sharedStylixConfig
    {
      nixpkgs.hostPlatform = system;
      nixpkgs.overlays = sharedOverlays system;
      home-manager = mkHomeManagerBlock { inherit username homeModule; };
    }
    # NixOS-specific home-manager defaults
    (
      { config, ... }:
      {
        home-manager.users.${username} = {
          dotfiles = {
            inherit username;
            hostname = config.networking.hostName;
            homeDirectory = "/home/${username}";
          };
        };
      }
    )
  ];
}
