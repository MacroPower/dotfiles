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
    mcp-destiny = final.callPackage paths.mcp-destiny { };
    leanspec-cli = final.callPackage paths.leanspec-cli { };
    claude-json-sync = final.callPackage paths.claude-json-sync { };
    claude-powerline = final.callPackage paths.claude-powerline { };
    playwright-cli = final.callPackage paths.playwright-cli { };
    comfyui = final.callPackage paths.comfyui { };
    slugify = final.callPackage paths.slugify { };
    mdcopy = final.callPackage paths.mdcopy { };
    expire-claude-tmp = final.callPackage paths.expire-claude-tmp { };
    expire-clones = final.callPackage paths.expire-clones { };
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
  # patch is jetify-com/devbox#2997 rebased onto 0.22.0. It makes the
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
  # The third patch adds run_stack_task, the MCP twin of `spacectl stack
  # task`. It runs a shell command on a stack's worker and returns the
  # task's logs.
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
          ../pkgs/spacectl-stack-task-mcp-tool.patch
        ];
        preCheck = (old.preCheck or "") + ''
          unset SSL_CERT_FILE
        '';
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

      # mcp-kubectx-host, the only entry of workmux host_commands, reads
      # ~/.kube/config. Upstream's host-exec sandbox unconditionally denies
      # ~/.kube. Drop that single entry from DENY_READ_DIRS. So every
      # host_commands entry gets read access to ~/.kube, and the list
      # must hold only commands that a guest cannot make run its own code.
      patches = (old.patches or [ ]) ++ [
        ../pkgs/workmux-allow-kube-read.patch
        # Upstream's host-exec runs each host command other than its
        # builtins in the toolchain of the worktree. It wraps the command
        # in `nix develop` for a flake.nix, or in `devbox run` for a
        # devbox.json. The guest writes the worktree, so the wrapper runs
        # code of the guest on the host, with the read access to ~/.kube
        # above. Run every host command without the wrapper. The guest
        # agent still starts in the toolchain, because that wrapper runs
        # in the guest.
        ../pkgs/workmux-host-exec-no-toolchain.patch
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
        # workmux mounts the per-VM state directory under the Lima guest
        # home, `/home/<user>.guest` on Lima 2.1.0 and later. It puts the
        # `shims/bin` directory of that mount on the PATH of the guest
        # agent as `$HOME/.workmux-state/shims/bin`. The terrarium image
        # gives the guest user the home `/home/<user>`, so that entry
        # names a missing directory. The guest then finds none of the
        # shims, such as the xclip and wl-paste shims that paste a host
        # image into Claude Code. Put the mount path itself on PATH. The
        # patch calls the function that
        # workmux-env-file-for-secrets.patch adds, so it must follow that
        # patch.
        ../pkgs/workmux-guest-shim-path.patch
      ];
    });
  };

  # git-surgeon 0.1.17 marks the commit that `split` edits with a sed
  # script that matches the first 7 characters of its SHA. Git writes
  # longer SHAs in the rebase todo of a large repository, so the rebase
  # runs to completion and split resets the branch tip instead of the
  # target. The patch matches the todo SHA as a prefix, checks that the
  # rebase paused at the target, and restores the repository when a
  # split fails. The patch forces a local build. Drop this overlay when
  # upstream ships the fix.
  gitSurgeonOverlay = system: _final: _prev: {
    git-surgeon = llm-agents.packages.${system}.git-surgeon.overrideAttrs (old: {
      patches = (old.patches or [ ]) ++ [ ../pkgs/git-surgeon-split-restore.patch ];
    });
  };

  # harper builds its curated dictionary from the rune files on every
  # start, which takes about 160ms of each harper-cli run. prose-lint runs
  # harper-cli on every file Claude writes, so the cost adds up.
  # harper-precomputed-dict.patch lets a build embed the finished
  # dictionary. The overlay builds harper once with the patches, dumps the
  # dictionary with that build, and builds again with
  # HARPER_PRECOMPUTED_DICT naming the dump.
  #
  # harper 3.0.0 aborted on most Haskell files, because its
  # tree-sitter-haskell 0.23.1 corrupts the heap. The install check of the
  # embedded build lints a comment in a Haskell file, so a harper that
  # aborts there fails the build.
  #
  # The patch changes Cargo.lock, so the overlay fetches the vendored
  # crates again. When nixpkgs bumps harper, the patch might stop applying.
  # Rebase it on the new tag, or drop it.
  harperOverlay = final: prev: {
    harper =
      let
        patches = [ ../pkgs/harper-precomputed-dict.patch ];
        unembedded = prev.harper.overrideAttrs (old: {
          patches = (old.patches or [ ]) ++ patches;
          cargoDeps = final.rustPlatform.fetchCargoVendor {
            inherit (old) src;
            name = "${old.pname}-${old.version}";
            inherit patches;
            hash = "sha256-pIt9QXHWifbSCTTo+0PX/e+K3La5FIB0695nw/QAcNE=";
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
        # harper-cli exits 1 when it finds a lint, and 134 when it aborts.
        postInstallCheck = (old.postInstallCheck or "") + ''
          printf '%s\n' 'module Main where' "" '-- This is is a comment.' \
            'main :: IO ()' 'main = pure ()' >check.hs
          status=0
          "$out/bin/harper-cli" lint --format compact check.hs >check.out || status=$?
          cat check.out
          if [ "$status" -ne 1 ] || ! grep -q '^check.hs:3:.*RepeatedWords' check.out; then
            echo "harper-cli did not lint the comment in check.hs (exit $status)" >&2
            exit 1
          fi
        '';
        passthru = (old.passthru or { }) // {
          inherit dictionary unembedded;
        };
      });
  };

  # nixpkgs builds marksman against dotnetCorePackages.runtime_9_0, the
  # source-built dotnet runtime. On aarch64-linux that build runs for hours
  # and aborts with SIGILL in its binary-allowance scanner. Even with the
  # binary runtime swapped in, the F# build under Apple-Silicon
  # virtualization (Lima/QEMU) crashes the SDK with SIGILL during fsc
  # invocations. On aarch64-darwin the cache drops the runtime whenever
  # swift rebuilds, which leaves a multi-hour local build that Hydra itself
  # failed. Upstream publishes self-contained AOT-compiled binaries that
  # sidestep the whole dotnet build chain, so every host uses those.
  marksmanOverlay = final: _prev: {
    marksman = final.marksman-bin;
  };

  # The Firefox 159 binary from nixpkgs' firefox-bin does not launch on
  # the macOS hosts, so they pin Firefox 158.0.1. The firefox-bin
  # derivation reads its version and download table from the `generated`
  # argument. This overlay swaps in the 158.0.1 en-US entries, with the
  # hashes from Mozilla's SHA256SUMS for that release. Remove the pin
  # after a 159 or later firefox-bin launches on macOS. NixOS/nixpkgs#591542
  # signs the relocated binaries and should fix the launch.
  firefoxBinOverlay = _final: prev: {
    firefox-bin-unwrapped = prev.firefox-bin-unwrapped.override {
      generated = {
        version = "158.0.1";
        sources = [
          {
            url = "https://archive.mozilla.org/pub/firefox/releases/158.0.1/mac/en-US/Firefox%20158.0.1.dmg";
            locale = "en-US";
            arch = "mac";
            sha256 = "1d7aa96b51fc9110185047de18228ac87238fba5e42bba53c9d696f88878cba6";
          }
          {
            url = "https://archive.mozilla.org/pub/firefox/releases/158.0.1/linux-x86_64/en-US/firefox-158.0.1.tar.xz";
            locale = "en-US";
            arch = "linux-x86_64";
            sha256 = "dce45470139242cf294eb6ada3d192a9bb334696842b02b71ade448a139eb41a";
          }
          {
            url = "https://archive.mozilla.org/pub/firefox/releases/158.0.1/linux-aarch64/en-US/firefox-158.0.1.tar.xz";
            locale = "en-US";
            arch = "linux-aarch64";
            sha256 = "7f880a6607b9608fc35f8814f7fbe7163c71e9680a8d3c0b27f35ff2fcfcb653";
          }
        ];
      };
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

  # An overlay that changes a nixpkgs package gives it a new store path,
  # and so does every package that depends on it. cache.nixos.org does
  # not hold the new paths, so a switch builds all of them locally. A
  # switch fetches each package that Hydra builds and never runs its
  # tests. So do not override such a package only to skip a flaky test. Run
  # `task nix:build-plan` to list what each host builds locally.
  sharedOverlays = system: [
    fetchurlOverlay
    devboxOverlay
    spacectlOverlay
    lixOverlay
    localOverlay
    harperOverlay
    marksmanOverlay
    firefoxBinOverlay
    grugFarOverlay
    (nurJacobColvinOverlay system)
    ryceeOverlay
    (workmuxOverlay system)
    (gitSurgeonOverlay system)
    (llmAgentsOverlay system)
    dagger.overlays.default
  ];

  # The overlays above that change a nixpkgs package in place, through
  # override, overrideAttrs, a patch, or a replacement src. The others add
  # packages from this repo or from another input. `task nix:build-plan`
  # diffs the nixpkgs-side derivation behind every attribute these touch
  # between two revisions. nixpkgs can add a patch or swap a src under an
  # overlay without moving the version, and the overlay then fails to
  # build. The fabric overlay broke that way when nixpkgs adopted the
  # invoke 3 patch it carried.
  nixpkgsModifyingOverlays = [
    fetchurlOverlay
    devboxOverlay
    spacectlOverlay
    lixOverlay
    harperOverlay
    firefoxBinOverlay
    grugFarOverlay
  ];

  # Attribute paths those overlays override, such as "harper" or
  # "python3Packages.fabric". The function applies each overlay to plain
  # nixpkgs and reads only the names of its result, so nothing builds or
  # evaluates deeply. It applies python package extensions to every python
  # package set in turn, because several extensions act only on one
  # interpreter version. Each name appears under the first set that
  # produced it.
  overlayTargets =
    system:
    let
      pkgs = import inputs.nixpkgs { inherit system; };
      lib = inputs.nixpkgs.lib;
      results = map (overlay: overlay pkgs pkgs) nixpkgsModifyingOverlays;
      topLevel = lib.concatMap (
        result: lib.filter (name: name != "pythonPackagesExtensions") (builtins.attrNames result)
      ) results;
      newExtensions = lib.concatMap (
        result:
        lib.drop (builtins.length pkgs.pythonPackagesExtensions) (result.pythonPackagesExtensions or [ ])
      ) results;
      pythonSets = lib.filter (set: pkgs ? ${set}) [
        "python3Packages"
        "python312Packages"
        "python313Packages"
      ];
      namesIn =
        set:
        lib.concatMap (extension: builtins.attrNames (extension pkgs.${set} pkgs.${set})) newExtensions;
      pythonTargets = lib.foldl' (
        acc: set:
        let
          seen = map (target: lib.last (lib.splitString "." target)) acc;
          fresh = lib.filter (name: !(builtins.elem name seen)) (namesIn set);
        in
        acc ++ map (name: "${set}.${name}") fresh
      ) [ ] pythonSets;
    in
    lib.unique (topLevel ++ pythonTargets);

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
  inherit overlayTargets;
}
