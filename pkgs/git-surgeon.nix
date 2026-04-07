{
  lib,
  rustPlatform,
  fetchFromGitHub,
}:

let
  version = "0.1.14";
in
rustPlatform.buildRustPackage {
  pname = "git-surgeon";
  inherit version;

  src = fetchFromGitHub {
    owner = "raine";
    repo = "git-surgeon";
    rev = "v${version}";
    hash = "sha256-5Ac4pdxB8FJbGGNc+gi+E+KHQgur3DTeF1IpboYdQJA=";
  };

  cargoHash = "sha256-PdywtdBMwCRNoiUUNmfE/yATI0snWHrhJJVW0sMpUAc=";

  meta = {
    description = "Git primitives for autonomous coding agents";
    homepage = "https://github.com/raine/git-surgeon";
    license = lib.licenses.mit;
    mainProgram = "git-surgeon";
  };
}
