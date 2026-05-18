{
  pkgs,
  lib,
  config,
  ...
}:

let
  cfg = config.dotfiles.ytdlp;
in
{
  options.dotfiles.ytdlp = {
    enable = lib.mkEnableOption "yt-dlp with video-archive defaults";
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ pkgs.yt-dlp ];

    xdg.configFile."yt-dlp/config".source = ../configs/yt-dlp/config;
  };
}
