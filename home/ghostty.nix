{
  pkgs,
  config,
  ...
}:

let
  fontFeatures = config.dotfiles.fonts.features;
in
{
  config = {
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
          "cmd+d=unbind"
          "cmd+shift+d=unbind"
        ];
        quick-terminal-screen = "mouse";
        quick-terminal-position = "right";
        # Disable auto-updates (managed by Nix)
        auto-update = "off";
      };
    };
  };
}
