{ pkgs, ... }:
{
  config = {
    home.packages = [
      pkgs.radar
      pkgs.radar-desktop
    ];
  };
}
