{
  lib,
  rustPlatform,
  applyPatches,
  harper,
  clippy,
  pkg-config,
  zstd,
}:

let
  # prose-lint links harper-core, and rule authors check rules with
  # harper-cli. Both must read a rule the same way, so the crate must lock
  # the harper version of pkgs.harper.
  lock = lib.importTOML ./Cargo.lock;
  lockedHarper = (lib.findFirst (p: p.name == "harper-core") { } lock.package).version or null;

  # The patched harper source of pkgs.harper. The crate depends on its
  # harper-core by path.
  harperSrc = applyPatches {
    name = "harper-${harper.version}-source";
    inherit (harper) src patches;
  };

  # The rules that the binary embeds, and the fixtures that the tests
  # lint.
  harperConfig = lib.fileset.toSource {
    root = ../../configs/harper;
    fileset = lib.fileset.unions [
      ../../configs/harper/rules
      ../../configs/harper/fixtures
    ];
  };

  # Returns each entry of each list that the last group of `pattern`
  # finds in `file`, such as `"sh", "bash"` in `Syntax::grammar(&["sh",
  # "bash"], ...)`.
  tableEntries =
    file: pattern:
    lib.concatMap (
      groups:
      lib.filter (e: e != "") (
        map (e: lib.removeSuffix "\"" (lib.removePrefix "\"" (lib.trim e))) (
          lib.splitString "," (lib.last groups)
        )
      )
    ) (lib.filter lib.isList (builtins.split pattern (builtins.readFile file)));

  # The extensions of the files that the binary lints. The prose hook of
  # home/claude.nix builds its path glob from them. The list comes from
  # MARKDOWN_EXTENSIONS of src/files.rs and SYNTAXES of src/comments.rs.
  # The extension_tables test of files.rs reads both tables with patterns
  # that match the same text.
  extensions =
    tableEntries ./src/files.rs "MARKDOWN_EXTENSIONS: &[[]&str] = &[[]([^]]*)]"
    ++ tableEntries ./src/comments.rs "Syntax::(grammar|scan)[(]&[[]([^]]*)]";
in
assert lib.assertMsg (lib.all (e: lib.elem e extensions) [
  "md"
  "go"
]) "tools/prose-lint/package.nix found no extension table in the source of the crate.";
assert lib.assertMsg (lockedHarper == harper.version) ''
  tools/prose-lint/Cargo.lock locks harper-core ${toString lockedHarper}, but pkgs.harper is ${harper.version}.
  Point tools/prose-lint/harper at the source of harper ${harper.version} and update Cargo.lock.
'';
rustPlatform.buildRustPackage (finalAttrs: {
  pname = "prose-lint";
  version = "0.1.0";

  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.unions [
      ./Cargo.toml
      ./Cargo.lock
      ./build.rs
      ./comment-checks.txt
      ./src
      (lib.fileset.maybeMissing ./tests)
    ];
  };

  cargoLock.lockFile = ./Cargo.lock;

  postUnpack = ''
    ln -s ${harperSrc} "$sourceRoot/harper"
  '';

  nativeBuildInputs = [ pkg-config ];
  buildInputs = [ zstd ];

  # harper-core embeds this file as its curated dictionary, so the binary
  # does not build the dictionary on each start.
  env.HARPER_PRECOMPUTED_DICT = "${harper.dictionary}";
  env.PROSE_LINT_HARPER = "${harperConfig}";

  # The check phase runs cargo test.
  doCheck = true;

  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck
    got=$("$out/bin/prose-lint" --version)
    if [ "$got" != ${harper.version} ]; then
      echo "prose-lint --version printed '$got', but pkgs.harper is ${harper.version}" >&2
      exit 1
    fi
    echo 'The cache starts warm.' | "$out/bin/prose-lint" --commit
    runHook postInstallCheck
  '';

  passthru = {
    inherit harperSrc extensions;

    # Runs clippy on every target of the crate and fails on each warning.
    # The package itself does not run clippy, so a new lint of a later
    # clippy does not break the build of a host. CheckClippy of the nix
    # toolchain builds this test in the commit hook.
    tests.clippy = finalAttrs.finalPackage.overrideAttrs (old: {
      pname = "${old.pname}-clippy";
      nativeBuildInputs = old.nativeBuildInputs ++ [ clippy ];
      # Clippy lints only the crate, so it does not need the embedded
      # dictionary. Without the dictionary, the test does not depend on the
      # patched harper build, which cache.nixos.org does not hold. So the
      # commit hook does not build that harper.
      env = removeAttrs old.env [ "HARPER_PRECOMPUTED_DICT" ];
      buildPhase = ''
        runHook preBuild
        cargo clippy --all-targets --offline -- -D warnings
        runHook postBuild
      '';
      doCheck = false;
      doInstallCheck = false;
      installPhase = ''
        touch "$out"
      '';
    });
  };

  meta = {
    description = "Prose linter built on harper-core";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/prose-lint";
    license = lib.licenses.asl20;
    mainProgram = "prose-lint";
  };
})
