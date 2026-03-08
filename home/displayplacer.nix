{
  lib,
  pkgs,
  config,
  ...
}:

let
  cfg = config.dotfiles.displayplacer;
in
{
  options.dotfiles.displayplacer.enable = lib.mkEnableOption "displayplacer" // {
    default = pkgs.stdenv.hostPlatform.isDarwin;
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ pkgs.displayplacer ];
  };
}
