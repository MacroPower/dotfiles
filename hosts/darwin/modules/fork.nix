{
  config,
  lib,
  ...
}:

let
  cfg = config.dotfiles.system.darwin.fork;
in
{
  options.dotfiles.system.darwin.fork.enable = lib.mkOption {
    type = lib.types.bool;
    default = false;
    description = ''
      Install the Fork Git client via Homebrew and disable its
      built-in Sparkle auto-updates (managed by nix-darwin instead).
    '';
  };

  config = lib.mkIf cfg.enable {
    dotfiles.system.homebrew.casks = [ "fork" ];

    system.defaults.CustomUserPreferences."com.DanPristupov.Fork".SUAutomaticallyUpdate = false;
  };
}
