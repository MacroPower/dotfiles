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
    config.allowUnfreePredicate =
      pkg:
      builtins.elem (inputs.nixpkgs.lib.getName pkg) [
        "claude-code"
        "claude-code-wrapped"
        "discord"
        "obsidian"
        "slack"
      ];
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
