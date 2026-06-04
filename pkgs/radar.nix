{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "1.7.4";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_darwin_arm64.tar.gz";
      hash = "sha256-8It1TxfIhN5EyrSVVyLmd5jMg7g05gi06lFgi0xLmFU=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_darwin_amd64.tar.gz";
      hash = "sha256-enwDKCRtsPrKMeAbsWfbtWPoqWllgaei6zGeFVdR6eE=";
    };
    "aarch64-linux" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_linux_arm64.tar.gz";
      hash = "sha256-3+KSFZj1DjgJUyDXkfqI/G066DSUb/Gdo6tYQbUriSw=";
    };
    "x86_64-linux" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_linux_amd64.tar.gz";
      hash = "sha256-HLTw8YJA4Zwv3DXe0ntiUIjc1yJZiCGF89Hrc9IbdHA=";
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
