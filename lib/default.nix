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
    mcp-kubectx = final.callPackage paths.mcp-kubectx { };
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
    workmux-bin = workmux.packages.${system}.default.overrideAttrs (old: {
      # Sandbox network_proxy and rpc tests need to bind TCP listeners,
      # which the Nix build sandbox does not permit.
      doCheck = false;

      # mcp-kubectx (declared in workmux host_commands) needs to read
      # ~/.kube/config. Upstream's host-exec sandbox unconditionally denies
      # ~/.kube. Drop that single entry from DENY_READ_DIRS. Trade-off:
      # every host_commands entry now gets read access to ~/.kube; today
      # that's only mcp-kubectx, so blast radius matches the binary that
      # needs it.
      patches = (old.patches or [ ]) ++ [
        ../pkgs/workmux-allow-kube-read.patch
      ];
    });
  };

  # nixpkgs at this pin builds lupa-2.8 with `LUPA_NO_BUNDLE=true` and
  # without fetching git submodules. setup.py then falls through to its
  # pkg-config fallback (find_lua_build) and produces a single unversioned
  # `lupa.lua` extension linked against system LuaJIT. fakeredis (pulled
  # in transitively via mcp-nixos -> fastmcp >= 2.14 -> pydocket) does
  # `import lupa.lua51` at module load, which fails -- breaking mcp-nixos
  # at stdio handshake. Flip lupa back to bundled mode and strip the
  # empty submodule placeholder dirs (only third-party/lua51 is actually
  # vendored in the tarball) so setup.py builds `lupa.lua51` from source.
  # That is all fakeredis needs.
  lupaOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        lupa = pyprev.lupa.overrideAttrs (old: {
          env = (old.env or { }) // {
            LUPA_NO_BUNDLE = "false";
          };
          postPatch = (old.postPatch or "") + ''
            find third-party -mindepth 1 -maxdepth 1 -type d ! -name lua51 \
              -exec rm -rf {} +
          '';
        });
      })
    ];
  };

  # fastmcp's test suite is wall-clock heavy and prone to hanging
  # network/keep-alive tests under the Nix sandbox -- nixpkgs already
  # carries a long disabledTests list, but new flakes keep appearing
  # on each bump. Trust upstream CI and skip the check phase entirely.
  # pytestCheckHook runs in installCheckPhase, so doInstallCheck is the
  # right gate (doCheck already defaults false for pure-python builds).
  fastmcpOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        fastmcp = pyprev.fastmcp.overrideAttrs {
          doInstallCheck = false;
        };
      })
    ];
  };

  # mcp's test_non_compliant_notification_response spawns a subprocess
  # server and waits up to 10s for it to bind a TCP port; under heavy
  # local rebuilds the server doesn't come up in time and the test
  # fails the whole build.
  mcpOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        mcp = pyprev.mcp.overrideAttrs (old: {
          disabledTests = (old.disabledTests or [ ]) ++ [
            "test_non_compliant_notification_response"
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

  # nixpkgs runs grug-far.nvim's mini.test suite during build whenever
  # lua is 5.1-compatible and the host isn't darwin (luajit2.1 hits this
  # on linux). The override at pkgs/development/lua-modules/overrides.nix
  # rm -rf's the bundled screenshots first because they're pinned to
  # specific neovim/ripgrep/ast-grep versions; with no references, the
  # screenshot tests then write fresh ones and mini.test reports each
  # as a failure, so `make test` exits 2. Skip the check phase -- the
  # plugin itself is just lua and we rely on upstream CI for it.
  # buildNeovimPlugin sources from neovim-unwrapped.lua.pkgs (a separate
  # scope from pkgs.luajitPackages), so the override has to be threaded
  # through luajit's packageOverrides for both consumers to pick it up.
  grugFarOverlay = _final: prev: {
    luajit = prev.luajit.override {
      packageOverrides = _luaFinal: luaPrev: {
        grug-far-nvim = luaPrev.grug-far-nvim.overrideAttrs {
          doCheck = false;
        };
      };
    };
  };

  sharedOverlays = system: [
    lixOverlay
    localOverlay
    lupaOverlay
    fastmcpOverlay
    mcpOverlay
    aioboto3Overlay
    direnvOverlay
    grugFarOverlay
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
