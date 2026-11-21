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
  # This homeModule imports no module of home/, so the host gets only
  # the modules that home/default.nix imports. The evaluation of this
  # host fails when one of those modules reads an option of a module
  # that only other hosts import, such as home/claude.nix. Declare that
  # option in home/options.nix instead of importing the other module
  # here.
  homeModule =
    { config, ... }:
    {
      dotfiles = {
        # The repositories of this host, such as the dotfiles clone that
        # nh switches from, live under ~/repos.
        reposDir = "${config.dotfiles.homeDirectory}/repos";
        git = {
          userName = "Jacob Colvin";
          userEmail = "jacobcolvin1@gmail.com";
        };
      };
    };
}
