{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "0.7.1";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/ymtdzzz/otel-tui/releases/download/v${version}/otel-tui_Darwin_arm64.tar.gz";
      hash = "sha256-ezD3pKlN+R+sWQj8LMol8Vblh3vkOK06sFk0s1FebR4=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/ymtdzzz/otel-tui/releases/download/v${version}/otel-tui_Darwin_x86_64.tar.gz";
      hash = "sha256-zbHAM8J+tx9EmxXuvl1tHeHvbdhEpPR44AZyyUnQSvo=";
    };
    "aarch64-linux" = {
      url = "https://github.com/ymtdzzz/otel-tui/releases/download/v${version}/otel-tui_Linux_arm64.tar.gz";
      hash = "sha256-IlPjPEfVbMjQOs+gbmsoDOmv32wp/1J2vXSmREW0Bmg=";
    };
    "x86_64-linux" = {
      url = "https://github.com/ymtdzzz/otel-tui/releases/download/v${version}/otel-tui_Linux_x86_64.tar.gz";
      hash = "sha256-V929JyMB/N/QuwMWGhSP7oAnGHHG+B8zImMpbgB8UR0=";
    };
  };

  src =
    srcs.${stdenv.hostPlatform.system} or (throw "Unsupported system: ${stdenv.hostPlatform.system}");
in
stdenv.mkDerivation {
  pname = "otel-tui";
  inherit version;

  src = fetchurl {
    inherit (src) url hash;
  };

  sourceRoot = ".";
  dontStrip = true;

  installPhase = ''
    install -Dm755 otel-tui $out/bin/otel-tui
  '';

  meta = {
    description = "Terminal OpenTelemetry viewer";
    homepage = "https://github.com/ymtdzzz/otel-tui";
    license = lib.licenses.mit;
    platforms = builtins.attrNames srcs;
    mainProgram = "otel-tui";
  };
}
