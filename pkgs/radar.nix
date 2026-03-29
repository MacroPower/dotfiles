{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "1.2.5";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_darwin_arm64.tar.gz";
      hash = "sha256-MHdnLT6nbCi9RJXXVlW2CYRVv6F7oTMt3VKLQMx+uFw=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_darwin_amd64.tar.gz";
      hash = "sha256-kVWtmlbE4fDa+5zRhfdXVqm33VGU/YHt04zZJKuA/SE=";
    };
    "aarch64-linux" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_linux_arm64.tar.gz";
      hash = "sha256-+QsRlUgJ5+VySuXH4QnPmfwE4vSpTj21SPuzjUjHhQs=";
    };
    "x86_64-linux" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_linux_amd64.tar.gz";
      hash = "sha256-tn4FcCFBWd7qTkGRSU3qMdgydCzlMWYSnnLuOgPiFd4=";
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
