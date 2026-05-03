{
  lib,
  rustPlatform,
  fetchFromGitHub,
  pkg-config,
  libxcb,
  stdenv,
}:

let
  version = "0.1.56";
in
rustPlatform.buildRustPackage {
  pname = "claude-history";
  inherit version;

  src = fetchFromGitHub {
    owner = "raine";
    repo = "claude-history";
    rev = "v${version}";
    hash = "sha256-D3S09Ztyjc9mVCxyN/8lWiIPg9rtzkjCCmBjG7QxInA=";
  };

  cargoHash = "sha256-+Wpk+WQLMCNa/Au7u1QdHx92X6peUfHQhPf5VGzXrd8=";

  nativeBuildInputs = [ pkg-config ];

  buildInputs = lib.optionals stdenv.hostPlatform.isLinux [
    libxcb
  ];

  # Upstream 0.1.56 ships a duplicate `mod tests` block in src/config.rs
  # that prevents the test binary from compiling. The release build is
  # unaffected, so trust upstream CI and skip the check phase here.
  doCheck = false;

  meta = {
    description = "Fuzzy-search Claude Code conversation history from the terminal";
    homepage = "https://github.com/raine/claude-history";
    license = lib.licenses.mit;
    mainProgram = "claude-history";
  };
}
