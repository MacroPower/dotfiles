{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "0.6.1";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_darwin_arm64.tar.gz";
      hash = "sha256-oK2bxBWWSLUPNYSQRtu5lJWAWng4NCUkH1WN2/15Aic=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_darwin_amd64.tar.gz";
      hash = "sha256-JqXY3Tv7j5yuU9vmhsG4PHIWIQQvuXiAPAV+CEFAzeo=";
    };
    "aarch64-linux" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_linux_arm64.tar.gz";
      hash = "sha256-HWf1JekNnl2g92nHUX9cXsSF344Z32SWOuVCN7UspiA=";
    };
    "x86_64-linux" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_linux_amd64.tar.gz";
      hash = "sha256-W4WKvfHLqbHXbE2ul+ca4sqz//xQ/hJ9SGcCL5VyCvU=";
    };
  };

  src =
    srcs.${stdenv.hostPlatform.system} or (throw "Unsupported system: ${stdenv.hostPlatform.system}");
in
stdenv.mkDerivation {
  pname = "chief";
  inherit version;

  src = fetchurl {
    inherit (src) url hash;
  };

  sourceRoot = ".";

  installPhase = ''
    install -Dm755 chief $out/bin/chief
  '';

  meta = {
    description = "AI project manager that breaks work into tasks and runs Claude Code in a loop";
    homepage = "https://github.com/minicodemonkey/chief";
    license = lib.licenses.mit;
    platforms = builtins.attrNames srcs;
    mainProgram = "chief";
  };
}
