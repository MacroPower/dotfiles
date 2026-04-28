{
  inputs,
  self,
  paths,
}:
let
  inherit (inputs)
    nur-jacobcolvin
    llm-agents
    workmux
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
    mcp-git = final.callPackage paths.mcp-git { };
    rtk-bin = final.callPackage paths.rtk-bin { };
    hook-router = final.callPackage paths.hook-router { };
    mcp-fetch = final.callPackage paths.mcp-fetch { };
    mcp-http-proxy = final.callPackage paths.mcp-http-proxy { };
    cookie = final.callPackage paths.cookie { };
    mcp-kubernetes = final.callPackage paths.mcp-kubernetes { };
    radar = final.callPackage paths.radar { };
    radar-desktop = final.callPackage paths.radar-desktop { };
    helm-schema = final.callPackage paths.helm-schema { };
    mcp-kagi = final.callPackage paths.mcp-kagi { };
    mcp-argocd = final.callPackage paths.mcp-argocd { };
    mcp-opentofu = final.callPackage paths.mcp-opentofu { };
    leanspec-mcp = final.callPackage paths.leanspec-mcp { };
    leanspec-cli = final.callPackage paths.leanspec-cli { };
    claude-powerline = final.callPackage paths.claude-powerline { };
    claude-history = final.callPackage paths.claude-history { };
    git-surgeon = final.callPackage paths.git-surgeon { };
    comfyui = final.callPackage paths.comfyui { };
  };

  nurJacobColvinOverlay =
    system: _final: _prev:
    nur-jacobcolvin.packages.${system};

  lixOverlay = _final: prev: {
    inherit (prev.lixPackageSets.stable)
      nixpkgs-review
      nix-eval-jobs
      nix-fast-build
      ;
  };

  ryceeOverlay = final: prev: {
    firefox-addons = final.callPackage (inputs.rycee-nur + "/pkgs/firefox-addons") {
      buildMozillaXpiAddon =
        let
          libMozilla = import (inputs.rycee-nur + "/lib/mozilla.nix") { inherit (prev) lib; };
        in
        libMozilla.mkBuildMozillaXpiAddon { inherit (final) fetchurl stdenv; };
    };
  };

  workmuxOverlay = system: _final: _prev: {
    workmux-bin = workmux.packages.${system}.default.overrideAttrs {
      # Sandbox network_proxy and rpc tests need to bind TCP listeners,
      # which the Nix build sandbox does not permit.
      doCheck = false;
    };
  };

  # lupa 2.7's bundled LuaJIT 2.1 Makefile mis-detects the target on
  # aarch64-linux, producing x86_64 objects that fail to link. Strip the
  # luajit21 source so setup.py skips that extension; lua51/52/54 still
  # build, which is all lupa's Python consumers (pydocket -> fastmcp ->
  # mcp-nixos) need.
  lupaOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        lupa = pyprev.lupa.overrideAttrs (old: {
          postPatch = (old.postPatch or "") + ''
            rm -rf third-party/luajit21
          '';
        });
      })
    ];
  };

  # fastmcp's test_server_performance_no_latency asserts a wall-clock
  # request takes <100ms, which is flaky on loaded CI runners.
  fastmcpOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        fastmcp = pyprev.fastmcp.overrideAttrs (old: {
          disabledTests = (old.disabledTests or [ ]) ++ [
            "test_server_performance_no_latency"
          ];
        });
      })
    ];
  };

  # aioboto3's DynamoDB tests hit a moto/werkzeug mock server that
  # emits a duplicate Server header; aiohttp's stricter parser rejects
  # it with HTTPClientError. The remaining suite is also slow (~10 min)
  # and not worth running in our closure rebuild. Note: nixpkgs runs
  # the test suite as the install-check phase, so doInstallCheck is
  # what gates it (doCheck is already false here).
  aioboto3Overlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        aioboto3 = pyprev.aioboto3.overrideAttrs {
          doInstallCheck = false;
        };
      })
    ];
  };

  # direnv's checkPhase runs bash/fish/zsh integration test scripts
  # that take 10+ minutes per closure rebuild and occasionally hang on
  # tty operations under the macOS Nix sandbox. We trust upstream's
  # CI to validate releases; skip the check phase locally.
  direnvOverlay = _final: prev: {
    direnv = prev.direnv.overrideAttrs {
      doCheck = false;
    };
  };

  sharedOverlays = system: [
    lixOverlay
    localOverlay
    lupaOverlay
    fastmcpOverlay
    aioboto3Overlay
    direnvOverlay
    (nurJacobColvinOverlay system)
    ryceeOverlay
    (workmuxOverlay system)
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
