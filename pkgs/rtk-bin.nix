{
  lib,
  stdenv,
  fetchurl,
  makeWrapper,
  jq,
  autoPatchelfHook,
}:

let
  version = "0.42.2";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/rtk-ai/rtk/releases/download/v${version}/rtk-aarch64-apple-darwin.tar.gz";
      hash = "sha256-WjspTWiGRlv5HOudccFGt/bYWxwgSJfS75KXxRBYZ9Q=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/rtk-ai/rtk/releases/download/v${version}/rtk-x86_64-apple-darwin.tar.gz";
      hash = "sha256-+e5c57rwN732nsD/VOms2ASQ80MrLNM0mzzXqt2q/E8=";
    };
    "aarch64-linux" = {
      url = "https://github.com/rtk-ai/rtk/releases/download/v${version}/rtk-aarch64-unknown-linux-gnu.tar.gz";
      hash = "sha256-DlG4pLL91ywktsEDRZx/9BK1+NZ+EOw+Z9H/9C23Doo=";
    };
    "x86_64-linux" = {
      url = "https://github.com/rtk-ai/rtk/releases/download/v${version}/rtk-x86_64-unknown-linux-musl.tar.gz";
      hash = "sha256-F64lkv5tz8YtC8T66onOUg2BiAFYH/CoOUM19yyRFU0=";
    };
  };

  src =
    srcs.${stdenv.hostPlatform.system} or (throw "Unsupported system: ${stdenv.hostPlatform.system}");

  hookScript = fetchurl {
    url = "https://raw.githubusercontent.com/rtk-ai/rtk/v${version}/hooks/claude/rtk-rewrite.sh";
    hash = "sha256-dCQY1wco/DskAy+4riw5oTFpwrkyGL7aI7+vmT1nv+U=";
  };
in
stdenv.mkDerivation {
  pname = "rtk-bin";
  inherit version;

  src = fetchurl {
    inherit (src) url hash;
  };

  sourceRoot = ".";
  dontStrip = true;

  nativeBuildInputs = [
    makeWrapper
  ]
  ++ lib.optionals (stdenv.hostPlatform.isLinux && stdenv.hostPlatform.isAarch64) [
    autoPatchelfHook
  ];

  buildInputs = lib.optionals (stdenv.hostPlatform.isLinux && stdenv.hostPlatform.isAarch64) [
    stdenv.cc.cc.lib
  ];

  installPhase = ''
    install -Dm755 rtk $out/bin/rtk
    install -Dm755 ${hookScript} $out/libexec/rtk/hooks/rtk-rewrite.sh
    wrapProgram $out/libexec/rtk/hooks/rtk-rewrite.sh \
      --prefix PATH : ${lib.makeBinPath [ jq ]}:$out/bin
  '';

  meta = {
    description = "CLI proxy that reduces LLM token consumption by 60-90% on common dev commands";
    homepage = "https://github.com/rtk-ai/rtk";
    license = lib.licenses.mit;
    platforms = builtins.attrNames srcs;
    mainProgram = "rtk";
    sourceProvenance = with lib.sourceTypes; [ binaryNativeCode ];
  };
}
