{
  lib,
  pkgs,
  config,
  ...
}:

let
  cfg = config.dotfiles.ghostty;
  fontFeatures = config.dotfiles.fonts.features;
in
{
  options.dotfiles.ghostty.enable = lib.mkEnableOption "Ghostty terminal" // {
    default = true;
  };

  config = lib.mkIf cfg.enable {
    programs.ghostty = {
      enable = true;
      package = if pkgs.stdenv.hostPlatform.isDarwin then pkgs.ghostty-bin else null;
      systemd.enable = false;
      settings = {
        window-height = 40;
        window-width = 80;

        window-padding-x = 8;
        window-padding-y = "8,0";

        font-style = "SemiBold";
        font-feature = fontFeatures;
        font-size = 14;

        keybind = [
          "global:cmd+grave_accent=toggle_quick_terminal"
        ];
        quick-terminal-screen = "mouse";
        quick-terminal-position = "right";
        # Disable auto-updates (managed by Nix)
        auto-update = "off";
      };
    };
  };
}
