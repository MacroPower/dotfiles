{
  lib,
  stdenv,
  fetchurl,
}:

let
  version = "0.7.0";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_darwin_arm64.tar.gz";
      hash = "sha256-dFYiD/YBLAjgQjN0giTl+X4oufqoMaX8GzLA9vdU5y4=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_darwin_amd64.tar.gz";
      hash = "sha256-TyqnKYfXnUJtpdw7L/BHInd9dp07ur51JtE4v605GZ0=";
    };
    "aarch64-linux" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_linux_arm64.tar.gz";
      hash = "sha256-Lf/MyCSlMXgf1fQ+ej/q7C/pmtk0oihrBv0yaIpRgIs=";
    };
    "x86_64-linux" = {
      url = "https://github.com/minicodemonkey/chief/releases/download/v${version}/chief_${version}_linux_amd64.tar.gz";
      hash = "sha256-jdYbVZcL4hEE7ycNcrBFppx+d5AC3f8q4LZVMoNKr3w=";
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
