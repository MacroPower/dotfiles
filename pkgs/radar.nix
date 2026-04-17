{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "1.4.8";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_darwin_arm64.tar.gz";
      hash = "sha256-wzcVVAWfTW2QhE02gUvlHcKkfXwsOBR2gfxDZT1jAdg=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_darwin_amd64.tar.gz";
      hash = "sha256-f/S5gPSwQPeh0l7Mojk2eeeUcWICkVzFW8ELSWClMU8=";
    };
    "aarch64-linux" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_linux_arm64.tar.gz";
      hash = "sha256-ondNmhwu5ntq9BOS/sRQ/ONvjHxWlRB5ehhZ68+5RCM=";
    };
    "x86_64-linux" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_linux_amd64.tar.gz";
      hash = "sha256-3l/qY/FsQy3SU6n1Faspo3sK24iY1xWoJDxJkgGxkb4=";
    };
  };

  src =
    srcs.${stdenv.hostPlatform.system} or (throw "Unsupported system: ${stdenv.hostPlatform.system}");
in
stdenv.mkDerivation {
  pname = "radar";
  inherit version;

  src = fetchurl {
    inherit (src) url hash;
  };

  sourceRoot = ".";
  dontStrip = true;

  installPhase = ''
    install -Dm755 kubectl-radar $out/bin/kubectl-radar
  '';

  meta = {
    description = "Modern Kubernetes visibility — topology, event timeline, and service traffic";
    homepage = "https://github.com/skyhook-io/radar";
    license = lib.licenses.asl20;
    platforms = builtins.attrNames srcs;
    mainProgram = "kubectl-radar";
  };
}
