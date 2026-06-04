{
  lib,
  rustPlatform,
  fetchFromGitHub,
}:

let
  version = "0.1.17";
in
rustPlatform.buildRustPackage {
  pname = "git-surgeon";
  inherit version;

  src = fetchFromGitHub {
    owner = "raine";
    repo = "git-surgeon";
    rev = "v${version}";
    hash = "sha256-SeXHYZwhwvkYxFHW694Cp1VKKeehxgOdfKqShuPI7M4=";
  };

  cargoHash = "sha256-PbhASsdDxmVcIzV+oHIbpX70zjSeNvkwGcbhQRi88rE=";

  meta = {
    description = "Git primitives for autonomous coding agents";
    homepage = "https://github.com/raine/git-surgeon";
    license = lib.licenses.mit;
    mainProgram = "git-surgeon";
  };
}
