{
  lib,
  rustPlatform,
  applyPatches,
  harper,
  pkg-config,
  zstd,
}:

let
  # prose-lint-rs links harper-core, and rule authors check rules with
  # harper-cli. Both must read a rule the same way, so the crate must lock
  # the harper version of pkgs.harper.
  lock = lib.importTOML ./Cargo.lock;
  lockedHarper = (lib.findFirst (p: p.name == "harper-core") { } lock.package).version or null;

  # The patched harper source of pkgs.harper. The crate depends on its
  # harper-core and harper-comments by path.
  harperSrc = applyPatches {
    name = "harper-${harper.version}-source";
    inherit (harper) src patches;
  };
in
assert lib.assertMsg (lockedHarper == harper.version) ''
  tools/prose-lint/Cargo.lock locks harper-core ${toString lockedHarper}, but pkgs.harper is ${harper.version}.
  Point tools/prose-lint/harper at the source of harper ${harper.version} and update Cargo.lock.
'';
rustPlatform.buildRustPackage {
  pname = "prose-lint-rs";
  version = "0.1.0";

  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.unions [
      ./Cargo.toml
      ./Cargo.lock
      ./src
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

  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck
    got=$("$out/bin/prose-lint-rs" --version)
    if [ "$got" != ${harper.version} ]; then
      echo "prose-lint-rs --version printed '$got', but pkgs.harper is ${harper.version}" >&2
      exit 1
    fi
    "$out/bin/prose-lint-rs"
    runHook postInstallCheck
  '';

  passthru = {
    inherit harperSrc;
  };

  meta = {
    description = "Prose linter built on harper-core";
    homepage = "https://github.com/MacroPower/dotfiles/tree/main/tools/prose-lint";
    license = lib.licenses.asl20;
    mainProgram = "prose-lint-rs";
  };
}
