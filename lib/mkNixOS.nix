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
  caCertificateFiles ? [ ],
  homeModule,
}:
inputs.nixpkgs.lib.nixosSystem {
  modules = [
    hostModule
    { dotfiles.system = { inherit username caCertificateFiles; }; }
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
            inherit username caCertificateFiles;
            hostname = config.networking.hostName;
            homeDirectory = "/home/${username}";
          };
        };
      }
    )
  ];
}
