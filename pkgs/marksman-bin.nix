{
  lib,
  stdenv,
  fetchurl,
  autoPatchelfHook,
  icu,
  zlib,
}:

let
  version = "2026-02-08";

  srcs = {
    "aarch64-linux" = {
      url = "https://github.com/artempyanykh/marksman/releases/download/${version}/marksman-linux-arm64";
      hash = "sha256-244SRSf3+ASOPmyRghucUu8XPZLAHkfSIb8TN6/ZYvs=";
    };
    "x86_64-linux" = {
      url = "https://github.com/artempyanykh/marksman/releases/download/${version}/marksman-linux-x64";
      hash = "sha256-vlCY6CEyGSacR/wNkWpm+jHOBgLslnR1xyImCqvyYIc=";
    };
  };

  src =
    srcs.${stdenv.hostPlatform.system}
      or (throw "marksman-bin: unsupported system ${stdenv.hostPlatform.system}");
in
stdenv.mkDerivation {
  pname = "marksman";
  inherit version;

  src = fetchurl { inherit (src) url hash; };

  dontUnpack = true;
  dontStrip = true;

  nativeBuildInputs = [ autoPatchelfHook ];

  buildInputs = [
    icu
    zlib
    stdenv.cc.cc.lib
  ];

  installPhase = ''
    install -Dm755 $src $out/bin/marksman
  '';

  meta = {
    description = "Language Server for Markdown (upstream self-contained release)";
    homepage = "https://github.com/artempyanykh/marksman";
    license = lib.licenses.mit;
    platforms = builtins.attrNames srcs;
    mainProgram = "marksman";
    sourceProvenance = with lib.sourceTypes; [ binaryNativeCode ];
  };
}
