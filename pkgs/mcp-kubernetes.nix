{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "0.0.13";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/Azure/mcp-kubernetes/releases/download/v${version}/mcp-kubernetes-darwin-arm64";
      hash = "sha256-RtG6VhIodpbD5CwMNnAyxuvW8Rw1Vzk0BoBDyCHBcOE=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/Azure/mcp-kubernetes/releases/download/v${version}/mcp-kubernetes-darwin-amd64";
      hash = "sha256-GzCMx66xR5h5e/YkgWr4aYUabFYZN5W4bpMhM2lCrXg=";
    };
    "aarch64-linux" = {
      url = "https://github.com/Azure/mcp-kubernetes/releases/download/v${version}/mcp-kubernetes-linux-arm64";
      hash = "sha256-1l9wZethNre46JhyGCaF4vyKQ0MkBMjnsWHgsjIbOpk=";
    };
    "x86_64-linux" = {
      url = "https://github.com/Azure/mcp-kubernetes/releases/download/v${version}/mcp-kubernetes-linux-amd64";
      hash = "sha256-gDXmQijfSmRIeyzCKESgqU6mLU6VQj2m/XR3aoC0WdQ=";
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
