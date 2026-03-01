{
  pkgs,
  modulesPath,
  ...
}:

{
  imports = [
    ./common.nix
    /etc/nixos/orbstack.nix
    "${modulesPath}/virtualisation/lxc-container.nix"
  ];

  # Container runtime (isolated from OrbStack's host daemon)
  virtualisation.containerd.enable = true;

  environment.systemPackages = with pkgs; [
    nerdctl
    (writeShellScriptBin "docker" ''exec sudo ${nerdctl}/bin/nerdctl "$@"'')
  ];

  # Networking: OrbStack's orbstack.nix handles DNS and DHCP tuning,
  # but the networkd config itself must be in the main configuration.
  networking.useNetworkd = true;
  networking.dhcpcd.enable = false;
  systemd = {
    network.networks."50-eth0" = {
      matchConfig.Name = "eth0";
      networkConfig = {
        DHCP = "ipv4";
        IPv6AcceptRA = true;
      };
      # OrbStack manages DNS via /opt/orbstack-guest/etc/resolv.conf
      dhcpV4Config.UseDNS = false;
    };

    # OrbStack's orbstack.nix sources profile scripts that may not exist
    tmpfiles.rules = [
      "f /opt/orbstack-guest/etc/profile-early 0644 root root -"
      "f /opt/orbstack-guest/etc/profile-late 0644 root root -"
    ];

    # Unmount OrbStack host filesystem mounts for isolation
    services.unmount-host-mounts = {
      description = "Unmount OrbStack host filesystem mounts for isolation";
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
      };
      script = ''
        ${pkgs.util-linux}/bin/findmnt -rn -o TARGET,SOURCE | sort -r | while read -r target source; do
          case "$source" in
            mac*|machines*) ${pkgs.util-linux}/bin/umount -l "$target" 2>/dev/null || true ;;
          esac
        done
      '';
    };
  };
}
