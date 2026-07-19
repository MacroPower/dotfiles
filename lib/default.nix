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
    hook-router = final.callPackage paths.hook-router { };
    copilot-api-proxy = final.callPackage paths.copilot-api-proxy { };
    mcp-fetch = final.callPackage paths.mcp-fetch { };
    mcp-http-proxy = final.callPackage paths.mcp-http-proxy { };
    cookie = final.callPackage paths.cookie { };
    mcp-kubectx = final.callPackage paths.mcp-kubectx { };
    radar = final.callPackage paths.radar { };
    radar-desktop = final.callPackage paths.radar-desktop { };
    helm-schema = final.callPackage paths.helm-schema { };
    mcp-kagi = final.callPackage paths.mcp-kagi { };
    marksman-bin = final.callPackage paths.marksman-bin { };
    mcp-opentofu = final.callPackage paths.mcp-opentofu { };
    leanspec-cli = final.callPackage paths.leanspec-cli { };
    claude-powerline = final.callPackage paths.claude-powerline { };
    claude-history = final.callPackage paths.claude-history { };
    playwright-cli = final.callPackage paths.playwright-cli { };
    git-surgeon = final.callPackage paths.git-surgeon { };
    comfyui = final.callPackage paths.comfyui { };
    slugify = final.callPackage paths.slugify { };
    mdcopy = final.callPackage paths.mdcopy { };
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

  # Built directly via final.rustPlatform.buildRustPackage (instead of
  # consuming workmux.packages.${system}.default) so the cargoDeps
  # fetches go through our overlaid fetchurl. workmux's own flake
  # evaluates against unmodified nixpkgs, which means the fetchurlOverlay
  # injection would not reach the crate downloads there. Keep the
  # buildRustPackage args in sync with workmux's flake.nix.
  workmuxOverlay = _system: final: _prev: {
    workmux-bin = final.rustPlatform.buildRustPackage {
      pname = "workmux";
      version = workmux.shortRev or workmux.dirtyShortRev or "dev";
      src = workmux;
      cargoLock = {
        lockFile = "${workmux}/Cargo.lock";
        outputHashes = {
          "crossterm-0.29.0" = "sha256-rfAaqGylDaxx3bjmofifnzSh7Hmh21BzHp5fS/w2Z6I=";
        };
      };
      nativeBuildInputs = [
        final.installShellFiles
        final.git
      ]
      # nixpkgs' classic open-source ld64 crashes (Trace/BPT trap: 5) in
      # its stubs pass when linking the mac-notification-sys Objective-C
      # object pulled in via notify-rust. Link with LLVM ld64.lld instead,
      # mirroring nixpkgs' own workaround for starship
      # (NixOS/nixpkgs#540463). Drop once the cctools fix
      # (NixOS/nixpkgs#536365) reaches our nixpkgs pin.
      ++ final.lib.optionals final.stdenv.hostPlatform.isDarwin [ final.llvmPackages.lld ];

      env = final.lib.optionalAttrs final.stdenv.hostPlatform.isDarwin {
        NIX_CFLAGS_LINK = "-fuse-ld=lld";
      };

      # Sandbox network_proxy and rpc tests need to bind TCP listeners,
      # which the Nix build sandbox does not permit.
      doCheck = false;

      # mcp-kubectx (declared in workmux host_commands) needs to read
      # ~/.kube/config. Upstream's host-exec sandbox unconditionally denies
      # ~/.kube. Drop that single entry from DENY_READ_DIRS. Trade-off:
      # every host_commands entry now gets read access to ~/.kube; today
      # that's only mcp-kubectx, so blast radius matches the binary that
      # needs it.
      patches = [
        ../pkgs/workmux-allow-kube-read.patch
        # ClaudeProfile's hardcoded skip-permissions flag activates bypass
        # mode and clobbers --permission-mode plan in the pane command.
        # Swap to the opt-in --allow-dangerously-skip-permissions so plan
        # mode survives launch; the agent can still escalate mid-session.
        ../pkgs/workmux-claude-allow-dangerous.patch
        # Claude Code's built-in sandbox hardcoded-denies writes to
        # .git/config, which blocks workmux's per-worktree metadata writes
        # (branch.<n>.workmux-base, workmux.worktree.<n>.<k>) and breaks
        # `workmux add` mid-flow. Redirect those keys to
        # $GIT_COMMON_DIR/workmux-config -- same directory, different
        # filename, outside the sandbox's protected set.
        ../pkgs/workmux-store-meta-outside-config.patch
      ];

      postInstall = ''
        export HOME=$TMPDIR
        installShellCompletion --cmd workmux \
          --bash <($out/bin/workmux completions bash) \
          --fish <($out/bin/workmux completions fish) \
          --zsh <($out/bin/workmux completions zsh)
      '';
    };
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

  # mcp's sse/ws/streamable_http/integration tests spawn subprocess
  # servers and poll for a TCP port to come up; under heavy local
  # rebuilds in the Nix sandbox dozens of them time out at once and
  # fail the whole build (started with one test, grew to 40+ across
  # four files on later bumps). Same story as fastmcp above: trust
  # upstream CI and skip the check phase entirely.
  mcpOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        mcp = pyprev.mcp.overrideAttrs {
          doInstallCheck = false;
        };
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

  # czkawka's GUI binaries (krokiet, cedinia) link mac-notification-sys via
  # notify-rust and hit the same classic-ld64 stubs-pass crash as workmux
  # (see workmuxOverlay). Same lld workaround; drop both together once the
  # cctools fix (NixOS/nixpkgs#536365) reaches our nixpkgs pin.
  czkawkaOverlay = final: prev: {
    czkawka = prev.czkawka.overrideAttrs (
      old:
      final.lib.optionalAttrs final.stdenv.hostPlatform.isDarwin {
        nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [ final.llvmPackages.lld ];
        env = (old.env or { }) // {
          NIX_CFLAGS_LINK = "-fuse-ld=lld";
        };
      }
    );
  };

  # Building marksman on Linux pulls in dotnetCorePackages.runtime_9_0,
  # which on aarch64-linux routes through the source-built dotnet-vmr-9.0.15
  # (a multi-hour build that aborts with SIGILL in its binary-allowance
  # scanner), and even when the binary runtime is swapped in, the F# build
  # under Apple-Silicon virtualization (Lima/QEMU) crashes the SDK with
  # SIGILL during fsc invocations. Upstream publishes self-contained
  # AOT-compiled binaries that sidestep the whole dotnet build chain; use
  # those on Linux and keep the nixpkgs source build on Darwin where it
  # works without a hitch.
  marksmanOverlay = final: prev: {
    marksman = if prev.stdenv.hostPlatform.isLinux then final.marksman-bin else prev.marksman;
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

  # crates.io's edge (Fastly / Heroku) returns HTTP 403 for any request
  # whose User-Agent header starts with "curl/". Nix's fetchurl invokes
  # curl with its default UA, so every fetchCrate / importCargoLock
  # download fails with the same 403 (verified directly: curl -A "" and
  # -A Mozilla/5.0 both succeed against the same URL). Inject a neutral
  # UA into every fetchurl call. Appending to curlOptsList is safe: the
  # downloaded bytes are unchanged, so existing fixed-output hashes
  # match and substitution from cache.nixos.org still hits.
  fetchurlOverlay = _final: prev: {
    # Preserve fetchurl's attrs (override, resolveUrl, extendDrvArgs, ...)
    # by replacing only its __functor entrypoint. A plain wrapper function
    # would drop those attrs and break callers that do fetchurl.override,
    # fetchurl // { ... }, or attribute lookup against fetchurl.
    #
    # fetchurl accepts either a plain attrset or a fixed-point function
    # (finalAttrs: { ... }); handle both shapes so `args // {...}` does
    # not error against a function.
    fetchurl =
      let
        addOpts =
          a:
          a
          // {
            curlOptsList = (a.curlOptsList or [ ]) ++ [
              "--user-agent"
              "Mozilla/5.0"
            ];
          };
      in
      prev.fetchurl
      // {
        __functor =
          _self: args:
          if builtins.isFunction args then
            prev.fetchurl (final: addOpts (args final))
          else
            prev.fetchurl (addOpts args);
      };
  };

  # cli-helpers 2.10.0 (litecli dep) ships 3 tabular-output tests that
  # assert exact ANSI escape sequences; the bytes have shifted across
  # Pygments versions in nixpkgs unstable, so the assertions fail even
  # though the styling output is functionally correct.
  cliHelpersOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        cli-helpers = pyprev.cli-helpers.overrideAttrs (old: {
          disabledTests = (old.disabledTests or [ ]) ++ [
            "test_style_output"
            "test_style_output_with_newlines"
            "test_style_output_custom_tokens"
          ];
        });
      })
    ];
  };

  # backrefs' test_timeout (tests/test_bregex.py) busy-loops for a fixed
  # wall-clock interval and asserts a TimeoutError fires; on a fast or contended
  # builder the regex sub finishes first and the test fails with "DID NOT
  # RAISE". It is pure timing flake, and the py3.12 build we pull in
  # transitively (via mkdocs-material) isn't on cache.nixos.org, so it builds
  # from source and trips the flake. Deselect just that test rather than
  # disabling the whole install-check phase.
  backrefsOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        backrefs = pyprev.backrefs.overrideAttrs (old: {
          disabledTests = (old.disabledTests or [ ]) ++ [ "test_timeout" ];
        });
      })
    ];
  };

  # inline-snapshot's tests/test_docs.py asserts that code examples embedded
  # in its own docs match freshly generated output byte-for-byte. The
  # generated side is formatted with whatever black version is in nixpkgs, so
  # a black bump shifts line breaks / hl_lines and the docs snapshots fail
  # (assert '...' == '...' diffs on formatting only). We pull the py3.12
  # build transitively (via mcp/fastmcp for kagimcp), which isn't on
  # cache.nixos.org, so it builds from source and trips the skew. Deselect
  # the docs-freshness tests; the functional suite still runs.
  #
  # test_empty_sub_snapshot snapshots pytest's terminal summary line, whose
  # `=` padding is computed from the real elapsed time while the time itself
  # is normalized to <time>. An inner pytest run crossing 10s (6-char
  # "11.23s" vs 5-char "9.99s") shifts the padding by one `=` and fails the
  # comparison, so the test flakes on slow builders.
  inlineSnapshotOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        inline-snapshot = pyprev.inline-snapshot.overrideAttrs (old: {
          disabledTestPaths = (old.disabledTestPaths or [ ]) ++ [ "tests/test_docs.py" ];
          disabledTests = (old.disabledTests or [ ]) ++ [ "test_empty_sub_snapshot" ];
        });
      })
    ];
  };

  # geoip2's tests/webservice_test.py starts a werkzeug HTTP server bound to
  # "localhost", which the darwin build sandbox cannot resolve ("nodename nor
  # servname provided, or not known"), so all 52 webservice tests error with
  # SystemExit before running. Pulled transitively on the py3.14 chain
  # (mcp-nixos -> fastmcp -> py-key-value-aio -> moto -> django -> geoip2)
  # and not on cache.nixos.org. Deselect the server-backed tests; the
  # database-reader tests still run.
  geoip2Overlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        geoip2 = pyprev.geoip2.overrideAttrs (old: {
          disabledTestPaths = (old.disabledTestPaths or [ ]) ++ [ "tests/webservice_test.py" ];
        });
      })
    ];
  };

  # opentelemetry-instrumentation-requests' TestURLLib3InstrumentorWithRealSocket
  # tests (tests/test_requests_ip_support.py) bind a real HTTPServer socket,
  # which the darwin build sandbox denies (PermissionError on socket.bind).
  # Pulled transitively via azure-cli on py3.14 and not on cache.nixos.org.
  # Deselect the real-socket class; the mocket/httpretty suites still run.
  otelRequestsOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        opentelemetry-instrumentation-requests =
          pyprev.opentelemetry-instrumentation-requests.overrideAttrs
            (old: {
              disabledTests = (old.disabledTests or [ ]) ++ [
                "TestURLLib3InstrumentorWithRealSocket"
              ];
            });
      })
    ];
  };

  # syrupy's xdist_two fixture param carries xfail("Not currently compatible
  # with xdist"), promoted to strict by `xfail_strict = true` in its
  # pyproject.toml — but the xfail nondeterministically passes, and a strict
  # xfail that passes is reported as FAILED [XPASS(strict)]. Same kagimcp
  # py3.12 chain as inline-snapshot above, so it builds from source and
  # trips the flake. nixpkgs' pytestCheckHook attrs (disabledTests) are
  # inert here because syrupy's derivation runs a hand-written checkPhase
  # (`invoke test`), so demote the marker to non-strict in the source: an
  # XPASS then reports as xpassed instead of failing the build.
  syrupyOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        syrupy = pyprev.syrupy.overrideAttrs (old: {
          postPatch = (old.postPatch or "") + ''
            substituteInPlace tests/conftest.py \
              --replace-fail \
                'marks=pytest.mark.xfail(reason="Not currently compatible with xdist"),' \
                'marks=pytest.mark.xfail(reason="Not currently compatible with xdist", strict=False),'
          '';
        });
      })
    ];
  };

  sharedOverlays = system: [
    fetchurlOverlay
    lixOverlay
    localOverlay
    lupaOverlay
    fastmcpOverlay
    mcpOverlay
    aioboto3Overlay
    backrefsOverlay
    inlineSnapshotOverlay
    geoip2Overlay
    otelRequestsOverlay
    syrupyOverlay
    direnvOverlay
    czkawkaOverlay
    marksmanOverlay
    grugFarOverlay
    cliHelpersOverlay
    (nurJacobColvinOverlay system)
    ryceeOverlay
    (workmuxOverlay system)
    llm-agents.overlays.shared-nixpkgs
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
