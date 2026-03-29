{
  lib,
  stdenvNoCC,
  fetchurl,
  unzip,
}:

let
  version = "1.2.5";
in
stdenvNoCC.mkDerivation {
  pname = "radar-desktop";
  inherit version;

  src = fetchurl {
    url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar-desktop_v${version}_darwin_universal.zip";
    hash = "sha256-kZL5TmLceOp9N6AmFshYcQX5quOlNWP9AlIzii8gLGc=";
  };

  sourceRoot = ".";
  dontStrip = true;

  nativeBuildInputs = [ unzip ];

  installPhase = ''
    mkdir -p $out/Applications
    cp -R Radar.app $out/Applications/
  '';

  meta = {
    description = "Radar desktop app — Kubernetes visibility with topology, event timeline, and service traffic";
    homepage = "https://github.com/skyhook-io/radar";
    license = lib.licenses.asl20;
    platforms = [
      "aarch64-darwin"
      "x86_64-darwin"
    ];
  };
}
