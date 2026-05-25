{
  pkgs,
  ...
}:

{
  imports = [ ./photo-cli.nix ];

  home.packages = with pkgs; [
    slack
  ];
}
