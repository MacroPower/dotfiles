{
  pkgs,
  lib,
  ...
}:

{
  home.file.".zed_server" = lib.mkIf pkgs.stdenv.hostPlatform.isLinux {
    source = "${pkgs.zed-bin.remote_server}/bin";
    recursive = true;
  };
}
