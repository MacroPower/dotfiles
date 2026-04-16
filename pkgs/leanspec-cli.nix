{
  lib,
  stdenv,
  rustPlatform,
  fetchFromGitHub,
  darwinMinVersionHook,
}:

rustPlatform.buildRustPackage {
  pname = "leanspec-cli";
  version = "0.2.28";

  src = fetchFromGitHub {
    owner = "codervisor";
    repo = "lean-spec";
    rev = "v0.2.28";
    hash = "sha256-uLk/mEAI4f7GDZATnMNgAIYs7ZomC0RkOEB6fwpDBXE=";
  };

  cargoHash = "sha256-BJiu74s2rTHDRmch9BgCk82e6s4+SbT2wo2lMKlHhf8=";

  cargoRoot = "rust";
  buildAndTestSubdir = "rust";
  cargoBuildFlags = [
    "-p"
    "leanspec-cli"
  ];
  doCheck = false;

  buildInputs = lib.optionals stdenv.hostPlatform.isDarwin [
    (darwinMinVersionHook "10.12")
  ];

  meta = {
    description = "CLI for LeanSpec spec-driven development";
    homepage = "https://github.com/codervisor/lean-spec";
    license = lib.licenses.mit;
    mainProgram = "lean-spec";
  };
}
