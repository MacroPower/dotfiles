{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "0.23.0";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/dadav/helm-schema/releases/download/${version}/helm-schema_${version}_Darwin_arm64.tar.gz";
      hash = "sha256-OybdtZJ0vCvjW7TVhnHKF4s6I+hS+9g/4H2w0m6eZa8=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/dadav/helm-schema/releases/download/${version}/helm-schema_${version}_Darwin_x86_64.tar.gz";
      hash = "sha256-MP357lUnlvCEtfWwwRb/v5Hrk4Ife7hU2jM1ZjTlWQw=";
    };
    "aarch64-linux" = {
      url = "https://github.com/dadav/helm-schema/releases/download/${version}/helm-schema_${version}_Linux_arm64.tar.gz";
      hash = "sha256-WlOEiQQ0GtYKA7yhqYhSwXmkPqvXkgKnnIXCOK8Trjo=";
    };
    "x86_64-linux" = {
      url = "https://github.com/dadav/helm-schema/releases/download/${version}/helm-schema_${version}_Linux_x86_64.tar.gz";
      hash = "sha256-3dhHso+gp1cPvlIFo+1ofQvk1xfGN3daliT72c0Ky64=";
    };
  };

  src =
    srcs.${stdenv.hostPlatform.system} or (throw "Unsupported system: ${stdenv.hostPlatform.system}");
in
stdenv.mkDerivation {
  pname = "helm-schema";
  inherit version;

  src = fetchurl {
    inherit (src) url hash;
  };

  sourceRoot = ".";
  dontStrip = true;

  installPhase = ''
        install -dm755 $out/helm-schema/bin
        install -m755 helm-schema $out/helm-schema/bin/
        cat > $out/helm-schema/plugin.yaml <<'EOF'
    name: "schema"
    version: "${version}"
    usage: "generate jsonschemas for your helm charts"
    description: "generate jsonschemas for your helm charts"
    command: "$HELM_PLUGIN_DIR/bin/helm-schema"
    EOF
  '';

  meta = {
    description = "Generate JSON schemas from Helm charts";
    homepage = "https://github.com/dadav/helm-schema";
    license = lib.licenses.mit;
    platforms = builtins.attrNames srcs;
  };
}
