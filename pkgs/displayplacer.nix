{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "1.4.0";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/jakehilborn/displayplacer/releases/download/v${version}/displayplacer-apple-v140";
      hash = "sha256-BXLD0pGOR8fguddyOQeGTi6itTudOwI3l2n//PRPfqA=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/jakehilborn/displayplacer/releases/download/v${version}/displayplacer-intel-v140";
      hash = "sha256-E+wDUe14SbIulFl08dSskeyjCziwnslixJf+uCl+rCs=";
    };
  };

  src =
    srcs.${stdenv.hostPlatform.system} or (throw "Unsupported system: ${stdenv.hostPlatform.system}");
in
stdenv.mkDerivation {
  pname = "displayplacer";
  inherit version;

  src = fetchurl {
    inherit (src) url hash;
  };

  dontUnpack = true;
  dontStrip = true;

  installPhase = ''
    install -Dm755 $src $out/bin/displayplacer
  '';

  meta = {
    description = "macOS command line utility to configure multi-display resolutions and arrangements";
    homepage = "https://github.com/jakehilborn/displayplacer";
    license = lib.licenses.mit;
    platforms = builtins.attrNames srcs;
    mainProgram = "displayplacer";
  };
}
