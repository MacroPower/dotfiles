{
  system = "x86_64-linux";
  username = "jacobcolvin";
  hostModule =
    { modulesPath, ... }:
    {
      imports = [
        ./default.nix
        "${modulesPath}/virtualisation/lxc-container.nix"
      ];

      # Networking: TrueNAS LXC containers use host0 as
      # the virtual ethernet interface connected to the host bridge.
      networking.useNetworkd = true;
      networking.dhcpcd.enable = false;
      networking.useHostResolvConf = false;
      systemd.network.networks."50-host0" = {
        matchConfig.Name = "host0";
        networkConfig = {
          DHCP = "ipv4";
          IPv6AcceptRA = true;
        };
      };
    };
  homeModule = {
    # home/fish.nix reads dotfiles.claude.searchRewrite, whose option is
    # defined in home/claude.nix; every host imports it. Without this the
    # home config fails to evaluate ("attribute 'claude' missing").
    imports = [
      ../../home/claude.nix
    ];
    dotfiles = {
      git = {
        userName = "Jacob Colvin";
        userEmail = "jacobcolvin1@gmail.com";
      };
    };
  };
}
