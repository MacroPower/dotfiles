{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "1.4.4";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_darwin_arm64.tar.gz";
      hash = "sha256-nFzvjZ+wOk/K+JQhtdX06AsQ1IEeghWUJiq9jBC+JI8=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_darwin_amd64.tar.gz";
      hash = "sha256-f59idBl4Zxyw73MilBraeZLHtNhYLfMwFISn5BByOeg=";
    };
    "aarch64-linux" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_linux_arm64.tar.gz";
      hash = "sha256-Q9liyRbGKDxHHujhL1LRfNoZY7Y/rmcK8ytGPGO1Pyw=";
    };
    "x86_64-linux" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_linux_amd64.tar.gz";
      hash = "sha256-A4J4WDpn80qhjUG0SdOuHCsqa2CLN411oAziD9NIxgM=";
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
