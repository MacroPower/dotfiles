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
    playwright-cli = final.callPackage paths.playwright-cli { };
    git-surgeon = final.callPackage paths.git-surgeon { };
    comfyui = final.callPackage paths.comfyui { };
    slugify = final.callPackage paths.slugify { };
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
        # ClaudeProfile's hardcoded skip-permissions flag activates bypass
        # mode and clobbers --permission-mode plan in the pane command.
        # Swap to the opt-in --allow-dangerously-skip-permissions so plan
        # mode survives launch; the agent can still escalate mid-session.
        ../pkgs/workmux-claude-allow-dangerous.patch
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

  # nixpkgs PR #489526 (merged 2026-04-20) switched deno to building
  # rusty_v8 from source instead of fetching the upstream prebuilt
  # static lib. The from-source build fails on aarch64-darwin: during
  # the V8 link step rustc execs build/toolchain/apple/linker_driver.py
  # and trips EPERM (the chromium_build submodule's shebangs aren't
  # reachable by patchShebangs, and python3 is absent from rustc's
  # reduced linker PATH). Other rusty_v8 consumers in nixpkgs (codex,
  # brioche, windmill, ...) still use the prebuilt release asset, so
  # point deno's librusty_v8 input back at the prebuilt and skip the
  # V8 build entirely. Bump `version` and the four shas in lockstep
  # with `pkgs.deno.passthru.librusty_v8.version` when updating nixpkgs.
  #
  # Separately, deno 2.7.13's checkPhase runs uv_compat's
  # tty_reset_mode_restores_termios test, which calls termios reset on
  # a non-TTY fd inside the sandbox and asserts the ECHO flag is
  # restored. With no TTY backing the fd the assertion always fires
  # (left: 0, right: 8). This matches the pattern of the existing
  # "Darwin sandbox issues" skips in deno/package.nix; add ours to
  # checkFlags via overrideAttrs.
  denoOverlay = final: prev: {
    deno =
      (prev.deno.override {
        librusty_v8 = final.fetchurl {
          name = "librusty_v8-147.2.1";
          url = "https://github.com/denoland/rusty_v8/releases/download/v147.2.1/librusty_v8_simdutf_release_${final.stdenv.hostPlatform.rust.rustcTarget}.a.gz";
          hash =
            {
              aarch64-darwin = "sha256-+KRxJX4ba/+c6xEdrjrBqjhW5mMRkI/H9DbmvFoVZ/U=";
              x86_64-darwin = "sha256-PGgufH3EaUhMw/fgGEWW+WSjHWjh7l9xY//oUCvdXLk=";
              aarch64-linux = "sha256-DjSVA3iGMxlBIopqA9woyPW/cDnGHzIP6lcCPxgSOBg=";
              x86_64-linux = "sha256-/oX8Aww6CwIsukfa/Rv/MYSXM3Ku8i19ID8UuXHQIvM=";
            }
            .${final.stdenv.hostPlatform.system};
          meta.sourceProvenance = with final.lib.sourceTypes; [ binaryNativeCode ];
        };
      }).overrideAttrs
        (old: {
          checkFlags =
            (old.checkFlags or [ ])
            ++ final.lib.optionals final.stdenv.hostPlatform.isDarwin [
              "--skip=uv_compat::tests::tty_reset_mode_restores_termios"
            ];
        });
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
    denoOverlay
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
