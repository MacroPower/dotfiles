{
  inputs,
  self,
  paths,
}:
let
  inherit (inputs)
    nur-jacobcolvin
    llm-agents
    dagger
    sops-nix
    nix-index-database
    krewfile
    ;

  localOverlay = final: _prev: {
    aven = final.callPackage paths.aven { };
    chief = final.callPackage paths.chief { };
    clauth = final.callPackage paths.clauth { };
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
    playwright-cli = final.callPackage paths.playwright-cli { };
    comfyui = final.callPackage paths.comfyui { };
    slugify = final.callPackage paths.slugify { };
    mdcopy = final.callPackage paths.mdcopy { };
    prose-weirpack = final.callPackage paths.prose-weirpack { };
    prose-lint = final.callPackage paths.prose-lint { };
  };

  # kat, kclipper, and kcl-lsp are GoReleaser-generated packages that take a
  # bare `system` argument. callPackage fills it from the deprecated
  # `pkgs.system` alias, which warns on every evaluation. Pass the system
  # explicitly.
  nurJacobColvinOverlay =
    system: _final: prev:
    let
      nurPkgs = nur-jacobcolvin.packages.${system};
    in
    nurPkgs
    // prev.lib.genAttrs [
      "kat"
      "kclipper"
      "kcl-lsp"
    ] (name: nurPkgs.${name}.override { inherit system; });

  lixOverlay = final: prev: {
    # nixpkgs passes -Dmimalloc=disabled to every Lix build, but only Lix
    # 2.96 and later define that Meson option, so Lix 2.95 fails to
    # configure. NixOS/nixpkgs#573942 limits the flag to 2.96. Drop this
    # override once nixpkgs-unstable includes that change.
    lixPackageSets = prev.lixPackageSets.extend (
      _: lprev: {
        lix_2_95 = lprev.lix_2_95.overrideScope (
          _: sprev: {
            lix = sprev.lix.overrideAttrs (old: {
              mesonFlags = prev.lib.remove "-Dmimalloc=disabled" old.mesonFlags;
            });
          }
        );
      }
    );

    # The Lix scope builds nixpkgs-review and nix-fast-build by overriding
    # the top-level packages of the same name. Inheriting them from the
    # scope makes each one depend on itself, so override the prev versions
    # the same way the scope does.
    inherit (final.lixPackageSets.stable) nix-eval-jobs;
    nixpkgs-review = prev.nixpkgs-review.override { nix = final.lixPackageSets.stable.lix; };
    nix-fast-build = prev.nix-fast-build.override { inherit (final) nix-eval-jobs; };
  };

  # Stock devbox reads packages with builtins.fetchClosure, which Lix 2.95
  # removed, and devbox 0.18.3 and later refuse to start on that Lix. This
  # patch is jetify-com/devbox#2997 rebased onto 0.18.4. It makes the
  # generated flake use builtins.appendContext and deletes the Lix check.
  # Drop it once a devbox release includes that change.
  devboxOverlay = _final: prev: {
    devbox = prev.devbox.overrideAttrs (old: {
      patches = (old.patches or [ ]) ++ [ ../pkgs/devbox-lix-append-context.patch ];
    });
  };

  # spacectl's MCP server covers stack runs (local preview, run logs) but
  # only module metadata. This patch adds local_preview_module and
  # get_module_run_logs, the module twins of local_preview and
  # get_stack_run_logs, so Claude can preview modules without calling
  # `spacectl module` directly.
  #
  # The stack local_preview tool takes a `path` argument that filters an
  # archive of the server's working directory and skips the walk up to the
  # git root. It works only when Claude Code starts at the repository root
  # and `path` names a subdirectory. Every other `path` uploads an empty
  # archive or one without the stack's project root. The second patch
  # removes the argument, so local_preview always uploads the repository
  # that contains the server's working directory. Drop it when upstream
  # makes `path` choose the directory to upload.
  #
  # nixpkgs ships spacectl 1.26.0. 1.29.1 requires Go 1.27, newer than
  # nixpkgs' default Go. The Nix sandbox points SSL_CERT_FILE at a missing
  # file, and 1.29.1 loads SSL_CERT_FILE as its CA bundle. preCheck unsets
  # it so the registry credentials tests pass. Drop the version, src,
  # vendorHash, buildGoModule, and preCheck overrides when nixpkgs ships
  # spacectl 1.29.1 or later.
  spacectlOverlay = final: prev: {
    spacectl = (prev.spacectl.override { buildGoModule = final.buildGo127Module; }).overrideAttrs (
      finalAttrs: old: {
        version = "1.29.1";
        src = final.fetchFromGitHub {
          owner = "spacelift-io";
          repo = "spacectl";
          tag = "v${finalAttrs.version}";
          hash = "sha256-G/WtGQ/bDeqArAi9ld/1TnjPmu6n1zQZwsbMUG4NCho=";
        };
        vendorHash = "sha256-DRkrrePtUipTy79ZmA1QddTOvry0KRRh/G9UHa4HLPE=";
        patches = (old.patches or [ ]) ++ [
          ../pkgs/spacectl-module-mcp-tools.patch
          ../pkgs/spacectl-stack-local-preview-no-path.patch
        ];
        preCheck = (old.preCheck or "") + ''
          unset SSL_CERT_FILE
        '';
      }
    );
  };

  # Lima 2.3.0-beta.1 adds `audio.microphone`, which passes the Mac's
  # microphone into a VZ guest as a virtio-sound capture stream. Claude
  # Code voice mode in the terrarium VM needs it. Drop this override when
  # nixpkgs ships lima 2.3.0 or later.
  limaOverlay = final: prev: {
    lima = prev.lima.overrideAttrs (
      finalAttrs: _old: {
        version = "2.3.0-beta.1";
        src = final.fetchFromGitHub {
          owner = "lima-vm";
          repo = "lima";
          tag = "v${finalAttrs.version}";
          hash = "sha256-CPvHlbdJ5tI4M+sxWLFpCH/aXA0ilqBqSv2F1FZlr9U=";
        };
        vendorHash = "sha256-FP6piTL4SMPZUXaMiNUuZRTn2uraGi6ok1o6gYUvlcs=";
      }
    );
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

  # Expose llm-agents' own package set (built against its pinned nixpkgs)
  # rather than overlays.shared-nixpkgs, so the packages substitute from
  # cache.numtide.com instead of rebuilding against our nixpkgs.
  llmAgentsOverlay = system: _final: _prev: {
    llm-agents = llm-agents.packages.${system};
  };

  # workmux from llm-agents plus local patches. The patches force a local
  # build, so workmux never substitutes from cache.numtide.com.
  workmuxOverlay = system: final: _prev: {
    workmux-bin = llm-agents.packages.${system}.workmux.overrideAttrs (old: {
      # nixpkgs' classic open-source ld64 crashes (Trace/BPT trap: 5) in
      # its stubs pass when linking the mac-notification-sys Objective-C
      # object pulled in via notify-rust. Link with LLVM ld64.lld instead,
      # mirroring nixpkgs' own workaround for starship
      # (NixOS/nixpkgs#540463). Drop once the cctools fix
      # (NixOS/nixpkgs#536365) reaches llm-agents' nixpkgs pin.
      nativeBuildInputs =
        old.nativeBuildInputs
        ++ final.lib.optionals final.stdenv.hostPlatform.isDarwin [
          llm-agents.inputs.nixpkgs.legacyPackages.${system}.llvmPackages.lld
        ];
      env =
        (old.env or { })
        // final.lib.optionalAttrs final.stdenv.hostPlatform.isDarwin {
          NIX_CFLAGS_LINK = "-fuse-ld=lld";
        };

      # mcp-kubectx (declared in workmux host_commands) needs to read
      # ~/.kube/config. Upstream's host-exec sandbox unconditionally denies
      # ~/.kube. Drop that single entry from DENY_READ_DIRS. Trade-off:
      # every host_commands entry now gets read access to ~/.kube; today
      # that's only mcp-kubectx, so blast radius matches the binary that
      # needs it.
      patches = (old.patches or [ ]) ++ [
        ../pkgs/workmux-allow-kube-read.patch
        # ClaudeProfile's hardcoded skip-permissions flag starts every pane
        # in bypass mode. Swap to the opt-in
        # --allow-dangerously-skip-permissions so panes start in the
        # permissions.defaultMode that home/claude.nix sets for the host.
        ../pkgs/workmux-claude-allow-dangerous.patch
        # Claude Code's built-in sandbox hardcoded-denies writes to
        # .git/config, which blocks workmux's per-worktree metadata writes
        # (branch.<n>.workmux-base, workmux.worktree.<n>.<k>) and breaks
        # `workmux add` mid-flow. Redirect those keys to
        # $GIT_COMMON_DIR/workmux-config -- same directory, different
        # filename, outside the sandbox's protected set.
        ../pkgs/workmux-store-meta-outside-config.patch
        # The Lima supervisor passes the RPC token and every
        # env_passthrough value as `export` statements in the limactl
        # arguments. ps shows those arguments to every user on the host
        # and in the guest. Write the secrets to a private file on the
        # per-VM state mount instead. The guest shell reads the file and
        # deletes it.
        ../pkgs/workmux-env-file-for-secrets.patch
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
  #
  # fastmcp 3.x does not support mcp 2.x, and nixpkgs marks fastmcp-slim
  # broken when its mcp argument is 2.x or later. That breaks kagimcp and
  # mcp-nixos. Build fastmcp-slim against mcp_1, the same workaround
  # nixpkgs applies to music-assistant. Nothing else in the fastmcp
  # closure depends on mcp, so the override does not pull in a second
  # mcp version. Drop it when nixpkgs ships fastmcp 4.x.
  fastmcpOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (pyfinal: pyprev: {
        fastmcp = pyprev.fastmcp.overrideAttrs {
          doInstallCheck = false;
        };
        fastmcp-slim = pyprev.fastmcp-slim.override {
          mcp = pyfinal.mcp_1;
        };
      })
    ];
  };

  # mcp's sse/ws/streamable_http/integration tests spawn subprocess
  # servers and poll for a TCP port to come up; under heavy local
  # rebuilds in the Nix sandbox dozens of them time out at once and
  # fail the whole build (started with one test, grew to 40+ across
  # four files on later bumps). Same story as fastmcp above: trust
  # upstream CI and skip the check phase entirely. mcp_1 carries the same
  # test suite and gets the same treatment.
  mcpOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        mcp = pyprev.mcp.overrideAttrs {
          doInstallCheck = false;
        };
        mcp_1 = pyprev.mcp_1.overrideAttrs {
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

  # aiohttp's test_heartbeat_does_not_timeout_while_receiving_large_frame
  # (added in aio-libs/aiohttp#12030, "reset ws heartbeat on inbound data")
  # drives a mocked event-loop clock and asserts no heartbeat PING is sent
  # while a large frame is still streaming in. It leans on exact call_soon vs
  # timer callback ordering, so under the loaded Nix sandbox it intermittently
  # errors "Heartbeat PING sent while data was still being received". nixpkgs
  # runs the suite via pytestCheckHook, so disabledTests (-k "not ...") is the
  # right knob; skip just this one case rather than the whole check phase.
  # Scoped to python 3.12: only the kagimcp chain builds aiohttp from
  # source. The python 3.14 aiohttp is on cache.nixos.org, so its tests
  # never run locally -- but patching it would fork the drv of anything
  # whose build env pulls aiohttp (e.g. lix's python test env) and lose
  # those substitutions.
  aiohttpOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        _pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          aiohttp = pyprev.aiohttp.overrideAttrs (old: {
            disabledTests = (old.disabledTests or [ ]) ++ [
              "test_heartbeat_does_not_timeout_while_receiving_large_frame"
            ];
          });
        }
      )
    ];
  };

  # httpx2's TestKeepalivePing tests assert a 0.1s keepalive ping timer
  # fires within a 0.2s wall-clock sleep against a mock stream; under a
  # loaded build sandbox the timer misses the window and ping_received
  # stays 0. Scoped to python 3.12: only the kagimcp chain builds httpx2
  # from source; the python 3.14 build comes from cache.nixos.org.
  httpx2Overlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        _pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          httpx2 = pyprev.httpx2.overrideAttrs (old: {
            disabledTests = (old.disabledTests or [ ]) ++ [ "TestKeepalivePing" ];
          });
        }
      )
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

  # Hydra has no cached atuin build for aarch64-linux at the nixpkgs pin,
  # so terrarium builds it locally. A timing test in atuin-common fails
  # when the builder is busy, so skip the check phase there. Every other
  # platform keeps the stock package, which cache.nixos.org serves. Drop
  # this when Hydra caches atuin for aarch64-linux.
  atuinOverlay = _final: prev: {
    atuin =
      if prev.stdenv.hostPlatform.isAarch64 && prev.stdenv.hostPlatform.isLinux then
        prev.atuin.overrideAttrs { doCheck = false; }
      else
        prev.atuin;
  };

  # harlequin's test_a_timed_out_run_closes_its_connection_once_the_work_stopped
  # runs a query with --timeout 0.1 against a stuck mock adapter and asserts
  # the connection closed after the worker left. Under a loaded build sandbox
  # the worker misses the window and the test fails. cache.nixos.org has no
  # harlequin build for aarch64-linux at the nixpkgs pin, so terrarium runs
  # the suite locally; skip just this case there. Every other platform keeps
  # the stock package so its cached build still substitutes.
  harlequinOverlay = _final: prev: {
    harlequin =
      if prev.stdenv.hostPlatform.isAarch64 && prev.stdenv.hostPlatform.isLinux then
        prev.harlequin.overrideAttrs (old: {
          disabledTests = (old.disabledTests or [ ]) ++ [
            "test_a_timed_out_run_closes_its_connection_once_the_work_stopped"
          ];
        })
      else
        prev.harlequin;
  };

  # czkawka's GUI binaries (krokiet, cedinia) link mac-notification-sys via
  # notify-rust and hit the same classic-ld64 stubs-pass crash as workmux
  # (see workmuxOverlay). Same lld workaround; drop each once the cctools fix
  # (NixOS/nixpkgs#536365) reaches the nixpkgs pin it builds against.
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

  # harper builds its curated dictionary from the rune files on every
  # start, which takes about 160ms of each harper-cli run. prose-lint runs
  # harper-cli on every file Claude writes, so the cost adds up. The patch
  # lets a build embed the finished dictionary. The overlay builds harper
  # once with the patch, dumps the dictionary with that build, and builds
  # again with HARPER_PRECOMPUTED_DICT naming the dump. The patch also
  # changes Cargo.lock, so the overlay fetches the vendored crates again.
  # When nixpkgs bumps harper, the patch might stop applying. Rebase it on
  # the new tag, or drop this overlay.
  harperOverlay = final: prev: {
    harper =
      let
        patch = ../pkgs/harper-precomputed-dict.patch;
        unembedded = prev.harper.overrideAttrs (old: {
          patches = (old.patches or [ ]) ++ [ patch ];
          cargoDeps = final.rustPlatform.fetchCargoVendor {
            inherit (old) src;
            name = "${old.pname}-${old.version}";
            patches = [ patch ];
            hash = "sha256-JTA+S+BcitX7WJJNoluV85zi/CTGK+pQikO4Fj2+Om8=";
          };
          # The embedded build runs the tests.
          doCheck = false;
          doInstallCheck = false;
        });
        dictionary = final.runCommand "harper-curated-dictionary" { } ''
          ${unembedded}/bin/harper-cli dump-curated-dictionary "$out"
        '';
      in
      unembedded.overrideAttrs (old: {
        env = (old.env or { }) // {
          HARPER_PRECOMPUTED_DICT = "${dictionary}";
        };
        doCheck = prev.harper.doCheck;
        doInstallCheck = prev.harper.doInstallCheck;
        passthru = (old.passthru or { }) // {
          inherit dictionary unembedded;
        };
      });
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

  # curl-impersonate's dylib keeps upstream's @rpath install name on
  # darwin, so every consumer linking it records an @rpath reference with
  # no LC_RPATH to resolve it and dies at load time -- notably
  # python3Packages.curl-cffi (a yt-dlp dependency) fails its
  # pythonImportsCheck with "Library not loaded:
  # @rpath/libcurl-impersonate.4.dylib ... no LC_RPATH's found".
  # Backport of nixpkgs 9da1a5ec ("curl-impersonate: fix dylib install
  # name on darwin"), which landed hours after this nixpkgs pin: run
  # fixDarwinDylibNames so the install name becomes the absolute store
  # path. Drop on the next nixpkgs bump.
  curlImpersonateOverlay = final: prev: {
    curl-impersonate = prev.curl-impersonate.overrideAttrs (
      old:
      prev.lib.optionalAttrs prev.stdenv.hostPlatform.isDarwin {
        nativeBuildInputs = (old.nativeBuildInputs or [ ]) ++ [ final.fixDarwinDylibNames ];
      }
    );
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
    # (finalAttrs: { ... }); handle both forms so `args // {...}` does
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
  # Scoped to python 3.12 (the kagimcp chain that builds from source).
  # Unscoped, this forked python 3.14 inline-snapshot, which forked
  # pydantic (test dep), then yarl, then aiohttp, then lix's python test
  # env -- pushing lix itself off cache.nixos.org into a local build.
  inlineSnapshotOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        _pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          inline-snapshot = pyprev.inline-snapshot.overrideAttrs (old: {
            disabledTestPaths = (old.disabledTestPaths or [ ]) ++ [ "tests/test_docs.py" ];
            disabledTests = (old.disabledTests or [ ]) ++ [ "test_empty_sub_snapshot" ];
          });
        }
      )
    ];
  };

  # py-key-value-aio (fastmcp's storage-backend library, pulled via kagimcp
  # and mcp-nixos) drags aioboto3, moto, cfn-lint, django, and geoip2 into
  # its check-only closure — moto alone runs a 30+ minute test suite, and
  # several of those packages have sandbox-hostile or flaky tests of their
  # own. The store backends are test-only there; skip the suite and the
  # whole chain drops out of the build closure. Upstream CI validates
  # releases.
  pyKeyValueAioOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        py-key-value-aio = pyprev.py-key-value-aio.overridePythonAttrs (_: {
          doCheck = false;
        });
      })
    ];
  };

  # cyclopts (fastmcp's CLI framework, pulled via both kagimcp and
  # mcp-nixos -> fastmcp) runs a large pytest suite -- shell-completion
  # integration tests that spawn bash/fish/zsh subprocesses dominate the
  # ~20-minute wall clock. It builds from source on our python chains
  # (not on cache.nixos.org), so the suite runs on every closure rebuild.
  # pytestCheckHook lands in installCheckPhase, which buildPythonPackage
  # gates on doInstallCheck = (doCheck or true); overriding doCheck here
  # flips that gate off and drops the check-only inputs. Upstream CI
  # validates releases.
  cycloptsOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        cyclopts = pyprev.cyclopts.overridePythonAttrs (_: {
          doCheck = false;
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

  # syrupy's xdist_two fixture param carries xfail("Not currently compatible
  # with xdist"), promoted to strict by `xfail_strict = true` in its
  # pyproject.toml — but the xfail nondeterministically passes, and a strict
  # xfail that passes is reported as FAILED [XPASS(strict)]. Same kagimcp
  # py3.12 chain as inline-snapshot above, so it builds from source and
  # trips the flake. nixpkgs' pytestCheckHook attrs (disabledTests) are
  # inert here because syrupy's derivation runs a hand-written checkPhase
  # (`invoke test`), so demote the marker to non-strict in the source: an
  # XPASS then reports as xpassed instead of failing the build.
  # Scoped to python 3.12: only the kagimcp chain builds from source and
  # trips the flake. Patching every python scope would fork the drv of any
  # package whose test suite pulls in syrupy (e.g. arrow-cpp via its
  # python3.14 test env) and lose their cache.nixos.org substitutions.
  syrupyOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        _pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          syrupy = pyprev.syrupy.overrideAttrs (old: {
            postPatch = (old.postPatch or "") + ''
              substituteInPlace tests/conftest.py \
                --replace-fail \
                  'marks=pytest.mark.xfail(reason="Not currently compatible with xdist"),' \
                  'marks=pytest.mark.xfail(reason="Not currently compatible with xdist", strict=False),'
            '';
          });
        }
      )
    ];
  };

  # rich's test_brokenpipeerror pipes `python -m rich` into `head -1` and
  # asserts python exits 1 from a BrokenPipeError. When rich writes its whole
  # demo into the pipe buffer before head closes the read end, python never
  # hits the broken pipe and exits 0, so the test fails. It failed on every
  # run on the terrarium VM. Deselect just that test; the rest of the suite
  # still runs. Scoped to python 3.12, the kagimcp chain that builds rich
  # from source, so the override leaves other python scopes' drvs untouched.
  richOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        _pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          rich = pyprev.rich.overrideAttrs (old: {
            disabledTests = (old.disabledTests or [ ]) ++ [ "test_brokenpipeerror" ];
          });
        }
      )
    ];
  };

  # anyio's test_tls_connectable wraps the server side of a TLS stream
  # with a hostname. Python 3.12.15 rejects that in wrap_bio with
  # "server_hostname can only be specified in client mode", and python
  # 3.14 accepts it. The streams the failed test leaves open then fail
  # two unrelated later tests with unraisable exception warnings.
  # Scoped to python 3.12, where only the kagimcp chain builds anyio
  # from source. The python 3.14 build comes from cache.nixos.org.
  anyioOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        _pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          anyio = pyprev.anyio.overrideAttrs (old: {
            disabledTests = (old.disabledTests or [ ]) ++ [ "test_tls_connectable" ];
          });
        }
      )
    ];
  };

  # uvloop's test_call_at asserts a timer fires within 70 ms of its
  # deadline. On a loaded linux-builder VM it took 80 ms and failed the
  # build. Scoped to python 3.12, where only the kagimcp chain builds
  # uvloop from source, so other python scopes keep their cached drvs.
  uvloopOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        _pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          uvloop = pyprev.uvloop.overrideAttrs (old: {
            disabledTestPaths = (old.disabledTestPaths or [ ]) ++ [
              "tests/test_base.py::TestBaseUV::test_call_at"
            ];
          });
        }
      )
    ];
  };

  # sse-starlette's suite times pings, timeouts, and disconnects against
  # wall-clock deadlines. On a loaded linux-builder VM a different test
  # fails each run (an extra ping, a cancel-scope deadline). Skip the
  # check phase and trust upstream CI. nixpkgs omits starlette from
  # dependencies and only gets it through fastapi in the check inputs, so
  # add it back or pythonRuntimeDepsCheck fails. Scoped to python 3.12
  # like uvloopOverlay above.
  sseStarletteOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          sse-starlette = pyprev.sse-starlette.overridePythonAttrs (old: {
            doCheck = false;
            dependencies = old.dependencies ++ [ pyfinal.starlette ];
          });
        }
      )
    ];
  };

  # psutil's test_heap_info asserts that glibc reports mmap-backed heap
  # bytes in use. A python 3.12 test worker on the terrarium VM holds
  # none, so mmap_used is 0 and the test fails. Scoped to python 3.12,
  # where only the kagimcp chain builds psutil from source. nixpkgs
  # already skips psutil's checks on darwin.
  psutilOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        _pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          psutil = pyprev.psutil.overrideAttrs (old: {
            disabledTests = (old.disabledTests or [ ]) ++ [ "test_heap_info" ];
          });
        }
      )
    ];
  };

  # isort's check phase pulls pylama, vulture, pint, uncertainties, and
  # scipy into the kagimcp closure through fastapi and inline-snapshot.
  # None of them is on cache.nixos.org for python 3.12, so scipy builds
  # from source and runs its suite. Its test_unitarity draws unseeded
  # random matrices and checks them against a 1e-15 tolerance, which
  # one draw missed on the terrarium VM. Skip isort's suite so the
  # whole chain drops out of the build closure. Upstream CI validates
  # releases. Scoped to python 3.12, because the python 3.14 isort
  # comes from cache.nixos.org.
  isortOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        _pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          isort = pyprev.isort.overridePythonAttrs (_: {
            doCheck = false;
          });
        }
      )
    ];
  };

  # pyjwt's test_fetch_data_rejects_redirect_without_contacting_destination
  # binds a ThreadingHTTPServer on 127.0.0.1, which the darwin build
  # sandbox denies (PermissionError on socket.bind). Deselect that test.
  # The rest of the suite still runs. Scoped to python 3.12, where only
  # the kagimcp chain builds pyjwt from source. The python 3.14 build
  # comes from cache.nixos.org.
  pyjwtOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (
        _pyfinal: pyprev:
        prev.lib.optionalAttrs (pyprev.python.pythonVersion == "3.12") {
          pyjwt = pyprev.pyjwt.overrideAttrs (old: {
            disabledTests = (old.disabledTests or [ ]) ++ [
              "test_fetch_data_rejects_redirect_without_contacting_destination"
            ];
          });
        }
      )
    ];
  };

  # nixpkgs ships invoke 3, and no fabric release supports it, so nixpkgs
  # marks fabric 3.2.3 broken (`fabric/fabric#2376`). azure-cli depends on
  # fabric, so every host that installs azure-cli fails to evaluate. Build
  # the unreleased fabric commit that adds invoke 3 support, the same fix
  # NixOS/nixpkgs#576874 proposes. Drop this overlay when that PR reaches
  # nixpkgs-unstable.
  fabricOverlay = _final: prev: {
    pythonPackagesExtensions = prev.pythonPackagesExtensions ++ [
      (_pyfinal: pyprev: {
        fabric = pyprev.fabric.overridePythonAttrs (old: {
          version = "3.2.3-unstable-2026-04-07";
          src = prev.fetchFromGitHub {
            owner = "fabric";
            repo = "fabric";
            rev = "cc098ad475dbbe2e10aba72692d83aee58919570";
            hash = "sha256-Z1tGcjdLrzbqcbS6XwapSgDNpyLbBiO21yeqi7XhbpQ=";
          };
          meta = old.meta // {
            broken = false;
          };
        });
      })
    ];
  };

  sharedOverlays = system: [
    fetchurlOverlay
    curlImpersonateOverlay
    devboxOverlay
    spacectlOverlay
    limaOverlay
    lixOverlay
    localOverlay
    lupaOverlay
    fastmcpOverlay
    mcpOverlay
    aioboto3Overlay
    aiohttpOverlay
    httpx2Overlay
    backrefsOverlay
    inlineSnapshotOverlay
    cycloptsOverlay
    geoip2Overlay
    pyKeyValueAioOverlay
    syrupyOverlay
    richOverlay
    anyioOverlay
    uvloopOverlay
    sseStarletteOverlay
    psutilOverlay
    isortOverlay
    pyjwtOverlay
    fabricOverlay
    direnvOverlay
    atuinOverlay
    harlequinOverlay
    czkawkaOverlay
    harperOverlay
    marksmanOverlay
    grugFarOverlay
    cliHelpersOverlay
    (nurJacobColvinOverlay system)
    ryceeOverlay
    (workmuxOverlay system)
    (llmAgentsOverlay system)
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
