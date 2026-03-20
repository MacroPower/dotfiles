{
  inputs,
  self,
  paths,
}:
let
  inherit (inputs)
    nur
    nur-jacobcolvin
    nix-vscode-extensions
    llm-agents
    dagger
    sops-nix
    nix-index-database
    krewfile
    ;

  localOverlay = final: _prev: {
    chief = final.callPackage paths.chief { };
    otel-tui = final.callPackage paths.otel-tui { };
    displayplacer = final.callPackage paths.displayplacer { };
    zed-bin = final.callPackage paths.zed-bin { };
    photo-cli = final.callPackage paths.photo-cli { };
    git-idempotent = final.callPackage paths.git-idempotent { };
    hook-router = final.callPackage paths.hook-router { };
  };

  nurJacobColvinOverlay =
    system: _final: _prev:
    nur-jacobcolvin.packages.${system};

  sharedOverlays = system: [
    localOverlay
    (nurJacobColvinOverlay system)
    nur.overlays.default
    nix-vscode-extensions.overlays.default
    llm-agents.overlays.default
    dagger.overlays.default
  ];

  sharedStylixConfig = import paths.stylix;

  hmSharedModules = [
    sops-nix.homeManagerModules.sops
    nix-index-database.homeModules.nix-index
    (import paths.krewfileModule krewfile)
  ];

  mkHomeManagerBlock =
    { username, homeModule }:
    {
      useGlobalPkgs = true;
      useUserPackages = true;
      backupFileExtension = "bak";
      sharedModules = hmSharedModules;
      users.${username} = {
        imports = [
          paths.home
          homeModule
        ];
      };
    };
in
{
  mkDarwin = import ./mkDarwin.nix {
    inherit
      inputs
      self
      paths
      sharedOverlays
      sharedStylixConfig
      mkHomeManagerBlock
      ;
  };
  mkHome = import ./mkHome.nix {
    inherit
      inputs
      paths
      sharedOverlays
      sharedStylixConfig
      hmSharedModules
      ;
  };
  mkNixOS = import ./mkNixOS.nix {
    inherit
      inputs
      sharedOverlays
      sharedStylixConfig
      mkHomeManagerBlock
      ;
  };
}
