{
  nixos-lima,
  system,
}:
{
  inherit system;
  username = "jacobcolvin";
  hostModule =
    { ... }:
    {
      imports = [
        ./default.nix
        (import ./lima-hardware.nix nixos-lima)
      ];

      networking.hostName = "lima";
    };
  homeModule = {
    imports = [
      ../../home/development.nix
      ../../home/kubernetes.nix
      ../../home/claude.nix
    ];
    dotfiles = {
      git = {
        userName = "Jacob Colvin";
        userEmail = "jacobcolvin1@gmail.com";
      };
      claude.dangerouslySkipPermissions = true;
    };
  };
}
