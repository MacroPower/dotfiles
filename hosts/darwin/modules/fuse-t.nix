{
  config,
  lib,
  ...
}:

let
  cfg = config.dotfiles.system.darwin.fuseT;
in
{
  options.dotfiles.system.darwin.fuseT.enable = lib.mkOption {
    type = lib.types.bool;
    default = false;
    description = ''
      Install FUSE-T (a FUSE-compatible filesystem-in-userspace
      implementation for macOS) via Homebrew. Adds the macos-fuse-t/cask
      tap, the fuse-t cask, and strips quarantine from the unsigned
      install.
    '';
  };

  config = lib.mkIf cfg.enable {
    dotfiles.system.homebrew = {
      taps = [ "macos-fuse-t/cask" ];
      casks = [ "fuse-t" ];
      unsignedCasks = [ "fuse-t" ];
    };

    # Homebrew 6.0 refuses to load casks from non-official taps until they
    # are explicitly trusted (https://docs.brew.sh/Tap-Trust). Trust just the
    # fuse-t cask rather than the whole macos-fuse-t/cask tap, so activation
    # can install it without an interactive `brew trust` prompt.
    nix-homebrew.trust.casks = [ "macos-fuse-t/cask/fuse-t" ];
  };
}
