{ config, pkgs, ... }:
let
  inherit (config.stylix.inputs) tinted-schemes;
in
{
  stylix = {
    enable = true;
    autoEnable = true;
    polarity = "dark";

    base16Scheme = "${tinted-schemes}/base16/onedark.yaml";
    override = {
      base00 = "23272e"; # darker background
    };

    fonts = {
      monospace = {
        package = pkgs.nerd-fonts.fira-code;
        name = "FiraCode Nerd Font Mono";
      };
      sansSerif = {
        package = pkgs.fira;
        name = "Fira Sans";
      };
      serif = {
        package = pkgs.merriweather;
        name = "Merriweather";
      };
      emoji = {
        package = pkgs.noto-fonts-color-emoji;
        name = "Noto Color Emoji";
      };
      sizes.terminal = 14;
    };
  };
}
