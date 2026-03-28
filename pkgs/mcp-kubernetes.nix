{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "0.0.12";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/Azure/mcp-kubernetes/releases/download/v${version}/mcp-kubernetes-darwin-arm64";
      hash = "sha256-sy5oEBp0vino+3+brE3aK+V7dCSDAnr2volSCr6qlBY=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/Azure/mcp-kubernetes/releases/download/v${version}/mcp-kubernetes-darwin-amd64";
      hash = "sha256-Liz9aXh6XgBYS7rB0/1WYWrS0JYOUDOBUvzAoIczX4g=";
    };
    "aarch64-linux" = {
      url = "https://github.com/Azure/mcp-kubernetes/releases/download/v${version}/mcp-kubernetes-linux-arm64";
      hash = "sha256-PpNhTKAQVIklMDtYU3+cD9mAQywD4LhCtSEJFroe/ls=";
    };
    "x86_64-linux" = {
      url = "https://github.com/Azure/mcp-kubernetes/releases/download/v${version}/mcp-kubernetes-linux-amd64";
      hash = "sha256-bqWiGGqaV6FiUonSmVjSMp6qq5G+yuN/pKuGpFGkByw=";
    };
  };

  src =
    srcs.${stdenv.hostPlatform.system} or (throw "Unsupported system: ${stdenv.hostPlatform.system}");
in
stdenv.mkDerivation {
  pname = "mcp-kubernetes";
  inherit version;

  src = fetchurl {
    inherit (src) url hash;
  };

  dontUnpack = true;
  dontStrip = true;

  installPhase = ''
    install -Dm755 $src $out/bin/mcp-kubernetes
  '';

  meta = {
    description = "MCP server for Kubernetes providing kubectl, Helm, Cilium, and Hubble operations";
    homepage = "https://github.com/Azure/mcp-kubernetes";
    license = lib.licenses.mit;
    platforms = builtins.attrNames srcs;
    mainProgram = "mcp-kubernetes";
    sourceProvenance = with lib.sourceTypes; [ binaryNativeCode ];
  };
}
