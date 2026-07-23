{ pkgs, ... }:

let
  # terminal-dark renders with the terminal's own ANSI colors, so the
  # stylix palette carries over wherever the terminal is themed.
  presentermConfig = (pkgs.formats.yaml { }).generate "presenterm-config.yaml" {
    defaults.theme = "terminal-dark";
  };
in
{
  home.packages = [ pkgs.presenterm ];

  xdg.configFile."presenterm/config.yaml".source = presentermConfig;
}
