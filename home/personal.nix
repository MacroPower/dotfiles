{
  pkgs,
  ...
}:

{
  home.packages = with pkgs; [
    discord
    obsidian
    slack
    talosctl
    tpi
  ];
}
