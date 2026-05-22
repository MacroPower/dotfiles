{
  lib,
  rustPlatform,
  fetchFromGitHub,
}:

let
  version = "0.1.16";
in
rustPlatform.buildRustPackage {
  pname = "git-surgeon";
  inherit version;

  src = fetchFromGitHub {
    owner = "raine";
    repo = "git-surgeon";
    rev = "v${version}";
    hash = "sha256-+0wIByswyAZFTE20VysejdfGTknzCsxRl3GKzPzWQPE=";
  };

  cargoHash = "sha256-Zh6sk1DYamyxEbec0V8ukCnGpyQIDNqd0+Y9x66rmMA=";

  meta = {
    description = "Git primitives for autonomous coding agents";
    homepage = "https://github.com/raine/git-surgeon";
    license = lib.licenses.mit;
    mainProgram = "git-surgeon";
  };
}
