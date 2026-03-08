{
  modulesPath,
  ...
}:

{
  imports = [
    ./common.nix
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
}
