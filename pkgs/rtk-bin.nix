{
  lib,
  stdenv,
  fetchurl,
  makeWrapper,
  jq,
  autoPatchelfHook,
}:

let
  version = "0.38.0";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/rtk-ai/rtk/releases/download/v${version}/rtk-aarch64-apple-darwin.tar.gz";
      hash = "sha256-OJbIxD0CZB3arYjpGpVpIz815Ok4o794gmVtxzko+Xo=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/rtk-ai/rtk/releases/download/v${version}/rtk-x86_64-apple-darwin.tar.gz";
      hash = "sha256-8Fv2JYI5iYz1dGf7BDeZUn6+PVJY6T8pD4TIt/QQ41k=";
    };
    "aarch64-linux" = {
      url = "https://github.com/rtk-ai/rtk/releases/download/v${version}/rtk-aarch64-unknown-linux-gnu.tar.gz";
      hash = "sha256-LhcfHRx2CGu0R+Ny2TMoadLNPMEGwIxuX70QKxLpGtk=";
    };
    "x86_64-linux" = {
      url = "https://github.com/rtk-ai/rtk/releases/download/v${version}/rtk-x86_64-unknown-linux-musl.tar.gz";
      hash = "sha256-m6+zVkUPsPZqfy1o0EaNGx4nAWPxYgV05npMj4FtlhA=";
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
