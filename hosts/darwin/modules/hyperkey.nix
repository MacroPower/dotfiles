{
  config,
  lib,
  ...
}:

let
  cfg = config.dotfiles.system.darwin.hyperkey;
in

{
  options.dotfiles.system.darwin.hyperkey.enable = lib.mkOption {
    type = lib.types.bool;
    default = false;
    description = ''
      Install the Hyperkey app (remaps Caps Lock to a Hyper key:
      Cmd+Ctrl+Opt+Shift) and apply its preferred defaults.
    '';
  };

  config = lib.mkIf cfg.enable {
    dotfiles.system.homebrew.casks = [ "hyperkey" ];

    system.defaults.CustomUserPreferences."com.knollsoft.Hyperkey" = {
      SUEnableAutomaticChecks = false;
      capsLockRemapped = 2;
      keyRemap = 1;
      hyperFlags = 1966080;
      physicalKeycode = 57;
      applyToClick = 2;
      hideMenuBarIcon = 1;
      launchOnLogin = 1;
      quickHyperKeycode = 0;
    };
  };
}
