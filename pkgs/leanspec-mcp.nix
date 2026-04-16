{
  lib,
  rustPlatform,
  fetchFromGitHub,
}:

rustPlatform.buildRustPackage {
  pname = "leanspec-mcp";
  version = "0.2.28";

  src = fetchFromGitHub {
    owner = "codervisor";
    repo = "lean-spec";
    rev = "v0.2.28";
    hash = "sha256-uLk/mEAI4f7GDZATnMNgAIYs7ZomC0RkOEB6fwpDBXE=";
  };

  cargoHash = "sha256-BJiu74s2rTHDRmch9BgCk82e6s4+SbT2wo2lMKlHhf8=";

  # leanspec-mcp is excluded from the workspace; move it to members so cargo builds it.
  postPatch = ''
    sed -i '/leanspec-mcp.*deprecated/d' rust/Cargo.toml
    sed -i 's/"leanspec-http",/"leanspec-http", "leanspec-mcp",/' rust/Cargo.toml
  '';

  cargoRoot = "rust";
  buildAndTestSubdir = "rust";
  cargoBuildFlags = [
    "-p"
    "leanspec-mcp"
  ];
  doCheck = false;

  meta = {
    description = "MCP server for LeanSpec spec-driven development";
    homepage = "https://github.com/codervisor/lean-spec";
    license = lib.licenses.mit;
    mainProgram = "leanspec-mcp";
  };
}
