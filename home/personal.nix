{
  pkgs,
  ...
}:

{
  home.packages = with pkgs; [
    discord
    photo-cli
    slack
  ];
}
