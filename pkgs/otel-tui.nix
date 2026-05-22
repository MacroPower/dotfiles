{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "0.7.3";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/ymtdzzz/otel-tui/releases/download/v${version}/otel-tui_Darwin_arm64.tar.gz";
      hash = "sha256-ntX8cLJtLKkBzlYLv4lu2xaFFEWrDrKnUWEt2dtUVZU=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/ymtdzzz/otel-tui/releases/download/v${version}/otel-tui_Darwin_x86_64.tar.gz";
      hash = "sha256-kKZ30SOo/R3KeT0JL+Cn55icn/o1cA39+6gKwdQqy7A=";
    };
    "aarch64-linux" = {
      url = "https://github.com/ymtdzzz/otel-tui/releases/download/v${version}/otel-tui_Linux_arm64.tar.gz";
      hash = "sha256-yKc4zhpL2MtDb8TL6pm6lDOBI9dEHTol+EFKWYL3ve0=";
    };
    "x86_64-linux" = {
      url = "https://github.com/ymtdzzz/otel-tui/releases/download/v${version}/otel-tui_Linux_x86_64.tar.gz";
      hash = "sha256-CKXwt4psjh4sr1vshpbZWwBUFVq63OOgUAX0vM6h2eQ=";
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
