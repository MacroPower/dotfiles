{
  lib,
  stdenv,
  fetchurl,
  runCommand,
  jq,
}:

let
  version = "1.7.4";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_darwin_arm64.tar.gz";
      hash = "sha256-8It1TxfIhN5EyrSVVyLmd5jMg7g05gi06lFgi0xLmFU=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_darwin_amd64.tar.gz";
      hash = "sha256-enwDKCRtsPrKMeAbsWfbtWPoqWllgaei6zGeFVdR6eE=";
    };
    "aarch64-linux" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_linux_arm64.tar.gz";
      hash = "sha256-3+KSFZj1DjgJUyDXkfqI/G066DSUb/Gdo6tYQbUriSw=";
    };
    "x86_64-linux" = {
      url = "https://github.com/skyhook-io/radar/releases/download/v${version}/radar_v${version}_linux_amd64.tar.gz";
      hash = "sha256-HLTw8YJA4Zwv3DXe0ntiUIjc1yJZiCGF89Hrc9IbdHA=";
    };
  };

  src =
    srcs.${stdenv.hostPlatform.system} or (throw "Unsupported system: ${stdenv.hostPlatform.system}");
in
stdenv.mkDerivation {
  pname = "radar";
  inherit version;

  src = fetchurl {
    inherit (src) url hash;
  };

  sourceRoot = ".";
  dontStrip = true;

  installPhase = ''
    install -Dm755 kubectl-radar $out/bin/kubectl-radar
  '';

  # Runs pkgs/radar-update-test.sh, which tests pkgs/radar-update.sh on
  # copies of this file and of pkgs/radar-desktop.nix. The update script
  # finds the version and each hash of a package file by the text around
  # them. So the test also fails after an edit of either file that moves
  # them out of the lines that the script reads. The test does not reach
  # the network.
  #
  # To build the test by hand, run:
  #
  #   nix build --no-link --impure --expr '((builtins.getFlake (toString ./.))
  #     .inputs.nixpkgs.legacyPackages.${builtins.currentSystem}.callPackage
  #     ./pkgs/radar.nix { }).tests'
  passthru.tests.update-script = runCommand "radar-update-test" { nativeBuildInputs = [ jq ]; } ''
    mkdir pkgs
    cp ${./radar-update-test.sh} pkgs/radar-update-test.sh
    cp ${./radar-update.sh} pkgs/radar-update.sh
    cp ${./radar.nix} pkgs/radar.nix
    cp ${./radar-desktop.nix} pkgs/radar-desktop.nix
    bash pkgs/radar-update-test.sh
    touch $out
  '';

  meta = {
    description = "Modern Kubernetes visibility — topology, event timeline, and service traffic";
    homepage = "https://github.com/skyhook-io/radar";
    license = lib.licenses.asl20;
    platforms = builtins.attrNames srcs;
    mainProgram = "kubectl-radar";
  };
}
