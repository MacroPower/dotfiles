{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "0.8.0";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_darwin_arm64.tar.gz";
      hash = "sha256-EGKe7/Zu4cafdC0jx759Qs56HupQXYR+SxCk8NxKbHY=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_darwin_amd64.tar.gz";
      hash = "sha256-XO4ewG/GPFCOSAKbPUcJ80HSJjP1jk2TglqCqVx0hPw=";
    };
    "aarch64-linux" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_linux_arm64.tar.gz";
      hash = "sha256-sOqYg4WjGG5sRM+Wo102BYmfxTZVHhc6dKNvXDYZjiw=";
    };
    "x86_64-linux" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_linux_amd64.tar.gz";
      hash = "sha256-zwtLW0y9OyfeBlvTJImTj0b0CtFv8IuONUJ0mGJV89w=";
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
  dontStrip = true;

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
