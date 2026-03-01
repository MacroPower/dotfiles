{ ... }:

{
  imports = [
    ./common.nix
  ];

  # Networking: Jailmaker systemd-nspawn containers use host0 as
  # the virtual ethernet interface connected to the host bridge.
  networking.useNetworkd = true;
  networking.dhcpcd.enable = false;
  systemd.network.networks."50-host0" = {
    matchConfig.Name = "host0";
    networkConfig = {
      DHCP = "ipv4";
      IPv6AcceptRA = true;
    };
  };
}
