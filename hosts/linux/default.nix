{
  pkgs,
  lib,
  config,
  ...
}:

let
  # Stylix requires a package per font slot to satisfy its module type,
  # but the dev container is headless: terminal rendering happens on the
  # host and nothing inside the container reads fontconfig. An empty
  # derivation keeps the type happy without dragging in the font files.
  emptyFont = pkgs.runCommand "empty-font" { } "mkdir -p $out/share/fonts";
in
{
  home.username = config.dotfiles.username;
  home.homeDirectory = config.dotfiles.homeDirectory;

  nix.package = pkgs.lixPackageSets.stable.lix;
  nix.settings = import ../settings.nix {
    inherit pkgs;
    inherit (config.dotfiles) username;
  };

  # The default glibcLocales is a single ~225 MB archive of every locale.
  # Nothing in the container reads anything but en_US.UTF-8.
  i18n.glibcLocales = pkgs.glibcLocales.override {
    allLocales = false;
    locales = [ "en_US.UTF-8/UTF-8" ];
  };

  stylix.fonts = {
    monospace.package = lib.mkForce emptyFont;
    sansSerif.package = lib.mkForce emptyFont;
    serif.package = lib.mkForce emptyFont;
    emoji.package = lib.mkForce emptyFont;
  };
}
