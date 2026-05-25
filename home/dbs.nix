{ pkgs, ... }:

let
  # Linked at ~/.harlequin.toml so harlequin's _search_home() picks it up
  # without depending on $XDG_CONFIG_HOME being exported (which on macOS is
  # only guaranteed in shells that sourced hm-session-vars).
  harlequinConfig = (pkgs.formats.toml { }).generate "harlequin-config.toml" {
    default_profile = "default";
    profiles.default = {
      # Closest dark Textual built-in to our stylix base16 onedark scheme;
      # Textual ships no native one-dark and harlequin has no theme
      # plugin entry point.
      theme = "tokyo-night";
    };
  };

  # Quotes around '\d> ' are load-bearing: configobj strips trailing
  # whitespace from unquoted values, which would drop the space after the
  # prompt's '>' (litecli/main.py:89 + as_bool calls).
  liteclConfig = (pkgs.formats.ini { }).generate "litecli-config" {
    main = {
      key_bindings = "emacs";
      syntax_style = "one-dark";
      prompt = "'\\d> '";
      multi_line = false;
      auto_vertical_output = true;
    };
  };
in

{
  home.packages = with pkgs; [
    harlequin
    litecli
  ];

  xdg.configFile."litecli/config".source = liteclConfig;
  home.file.".harlequin.toml".source = harlequinConfig;
}
