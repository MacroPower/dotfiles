{
  inputs,
  paths,
  sharedOverlays,
  sharedStylixConfig,
  hmSharedModules,
}:
{ system, homeModule }:
inputs.home-manager.lib.homeManagerConfiguration {
  pkgs = import inputs.nixpkgs {
    inherit system;
    config.allowUnfree = true;
    overlays = sharedOverlays system;
  };
  modules = hmSharedModules ++ [
    inputs.stylix.homeModules.stylix
    sharedStylixConfig
    paths.hostLinux
    paths.home
    homeModule
  ];
}
