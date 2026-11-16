{
  lib,
  stdenv,
  buildNpmPackage,
  fetchFromGitHub,
  makeWrapper,
  runCommand,
  jq,
  # Chromium runtime deps. The list mirrors the `buildInputs +
  # appendRunpaths` of nixpkgs' pkgs/development/web/playwright/chromium.nix
  # at the same pin. We do not use nixpkgs' chromium binary itself, which
  # is revision-locked to 1208, and alpha 1.61 expects 1224. We borrow
  # only its dynamic-link dep closure to populate LD_LIBRARY_PATH. The
  # alpha chromium that `playwright-cli install-browser chromium`
  # downloads into ~/.cache/ms-playwright can then exec on NixOS. Net
  # cost on a typical NixOS host is small, because most libs are already
  # in the system closure. On a minimal headless host, it is a few MiB
  # of net-new paths.
  alsa-lib,
  at-spi2-atk,
  atk,
  cairo,
  cups,
  dbus,
  expat,
  glib,
  gobject-introspection,
  libgbm,
  libGL,
  libx11,
  libxcb,
  libxcomposite,
  libxdamage,
  libxext,
  libxfixes,
  libxkbcommon,
  libxrandr,
  nspr,
  nss,
  pango,
  pciutils,
  systemd,
  vulkan-loader,
}:

# Unlike pkgs/claude-powerline.nix (fetchurl + mkDerivation + tar), the
# playwright-cli bin is a one-line `require('playwright-core/lib/tools/
# cli-client/program')` shim, so we need npm to populate
# node_modules/playwright-core. The alpha playwright dep predates
# anything in nixpkgs, so we cannot symlink pkgs.playwright-test the
# way playwright-mcp does upstream.
#
# Sourced from GitHub (not the npm tarball) because upstream's
# .npmignore strips package-lock.json, which buildNpmPackage requires.
# `rev` is the gitHead reported by `npm view @playwright/cli@<ver>`.
buildNpmPackage {
  pname = "playwright-cli";
  version = "0.1.13";

  src = fetchFromGitHub {
    owner = "microsoft";
    repo = "playwright-cli";
    rev = "3a1bafc8b4e973c72d0364eb5b427d1ce0aa8317";
    hash = "sha256-hHK/GR5Drlt+e0L9kyNmn+ht1PCrVH6WrVbxGB1Wsxg=";
  };

  npmDepsHash = "sha256-Ulp6IttsZcOOA7LaYDpVKkBYbe2j4RFG8lJARWifOSk=";

  dontNpmBuild = true;

  nativeBuildInputs = lib.optionals stdenv.hostPlatform.isLinux [ makeWrapper ];

  postFixup = lib.optionalString stdenv.hostPlatform.isLinux ''
    wrapProgram $out/bin/playwright-cli \
      --prefix LD_LIBRARY_PATH : ${
        lib.makeLibraryPath [
          alsa-lib
          at-spi2-atk
          atk
          cairo
          cups
          dbus
          expat
          glib
          gobject-introspection
          libgbm
          libGL
          libx11
          libxcb
          libxcomposite
          libxdamage
          libxext
          libxfixes
          libxkbcommon
          libxrandr
          nspr
          nss
          pango
          pciutils
          stdenv.cc.cc.lib
          systemd
          vulkan-loader
        ]
      }
  '';

  # Runs pkgs/playwright-cli-update-test.sh, which tests
  # pkgs/playwright-cli-update.sh on a copy of this file. The update
  # script finds the version, the commit, and each hash of this file by
  # the text around them. So the test also fails after an edit of this
  # file that moves them out of the lines that the script reads. The
  # test does not reach the network.
  #
  # To build the test by hand, run:
  #
  #   nix build --no-link --impure --expr '((builtins.getFlake (toString ./.))
  #     .inputs.nixpkgs.legacyPackages.${builtins.currentSystem}.callPackage
  #     ./pkgs/playwright-cli.nix { }).tests'
  passthru.tests.update-script =
    runCommand "playwright-cli-update-test" { nativeBuildInputs = [ jq ]; }
      ''
        mkdir pkgs
        cp ${./playwright-cli-update-test.sh} pkgs/playwright-cli-update-test.sh
        cp ${./playwright-cli-update.sh} pkgs/playwright-cli-update.sh
        cp ${./playwright-cli.nix} pkgs/playwright-cli.nix
        bash pkgs/playwright-cli-update-test.sh
        touch $out
      '';

  meta = {
    description = "Playwright CLI with SKILLS for browser automation by coding agents";
    homepage = "https://github.com/microsoft/playwright-cli";
    license = lib.licenses.asl20;
    mainProgram = "playwright-cli";
  };
}
