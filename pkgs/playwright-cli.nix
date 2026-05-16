{
  lib,
  stdenv,
  buildNpmPackage,
  fetchFromGitHub,
  makeWrapper,
  # Chromium runtime deps -- mirrors the `buildInputs + appendRunpaths` of
  # nixpkgs' pkgs/development/web/playwright/chromium.nix at the same pin.
  # We do NOT use nixpkgs' chromium binary itself (revision-locked to
  # 1208 while alpha 1.61 expects 1224); we only borrow its dynamic-link
  # dep closure to populate LD_LIBRARY_PATH so the alpha chromium that
  # `playwright-cli install-browser chromium` downloads into
  # ~/.cache/ms-playwright can exec on NixOS. Net cost on a typical
  # NixOS host is small (most libs are already in the system closure);
  # on a minimal headless host it's a few MiB of net-new paths.
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
# cli-client/program')` shim -- we need npm to actually populate
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

  meta = {
    description = "Playwright CLI with SKILLS for browser automation by coding agents";
    homepage = "https://github.com/microsoft/playwright-cli";
    license = lib.licenses.asl20;
    mainProgram = "playwright-cli";
  };
}
