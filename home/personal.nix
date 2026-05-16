{
  pkgs,
  ...
}:

{
  home.packages = with pkgs; [
    photo-cli
    slack
  ];
}
