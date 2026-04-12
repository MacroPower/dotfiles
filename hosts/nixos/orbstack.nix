{
  system = "aarch64-linux";
  username = "jacobcolvin";
  hostModule =
    {
      pkgs,
      modulesPath,
      ...
    }:
    {
      imports = [
        ./default.nix
        ./orbstack-hardware.nix
        "${modulesPath}/virtualisation/lxc-container.nix"
      ];

      networking = {
        hostName = "nixos-orbstack";

        # Networking: OrbStack's orbstack.nix handles DNS and DHCP tuning,
        # but the networkd config itself must be in the main configuration.
        useNetworkd = true;
        dhcpcd.enable = false;
      };

      # Container runtime (isolated from OrbStack's host daemon)
      virtualisation.containerd.enable = true;

      environment.systemPackages = with pkgs; [
        nerdctl
        pkgsStatic.tini
        (stdenvNoCC.mkDerivation {
          name = "docker-nerdctl-wrapper";
          dontUnpack = true;
          installPhase = ''
            install -Dm755 ${writeShellScript "docker" ''exec sudo ${nerdctl}/bin/nerdctl "$@"''} $out/bin/docker

            mkdir -p $out/share/fish/vendor_completions.d
            sed 's/^complete -c nerdctl/complete -c docker/' \
              "${nerdctl}/share/fish/vendor_completions.d/nerdctl.fish" \
              > $out/share/fish/vendor_completions.d/docker.fish

            mkdir -p $out/share/bash-completion/completions
            sed 's/nerdctl/docker/g' \
              "${nerdctl}/share/bash-completion/completions/nerdctl" \
              > $out/share/bash-completion/completions/docker

            mkdir -p $out/share/zsh/site-functions
            sed 's/nerdctl/docker/g' \
              "${nerdctl}/share/zsh/site-functions/_nerdctl" \
              > $out/share/zsh/site-functions/_docker
          '';
        })
      ];

      systemd = {
        # In LXC containers, systemd-hostnamed only sets the static hostname from
        # /etc/hostname, and cannot call sethostname() to update the kernel hostname.
        # OrbStack pre-sets the kernel hostname to "nixos", so we override it here.
        services.set-hostname = {
          description = "Set kernel hostname to match networking.hostName";
          wantedBy = [ "multi-user.target" ];
          after = [ "systemd-hostnamed.service" ];
          serviceConfig.Type = "oneshot";
          script = ''
            echo "nixos-orbstack" > /proc/sys/kernel/hostname
          '';
        };

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

        # Mount empty tmpfs over OrbStack guest binaries for isolation
        services.mask-guest-bins = {
          description = "Mount empty tmpfs over OrbStack guest binaries for isolation";
          wantedBy = [ "multi-user.target" ];
          before = [ "multi-user.target" ];
          serviceConfig = {
            Type = "oneshot";
            RemainAfterExit = true;
          };
          script = ''
            ${pkgs.util-linux}/bin/mount -t tmpfs -o ro,noexec tmpfs /opt/orbstack-guest/bin
          '';
        };

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
    };
  homeModule = {
    imports = [
      ../../home/development.nix
      ../../home/kubernetes.nix
      ../../home/claude.nix
      ../../home/zed-remote.nix
    ];
    dotfiles = {
      git = {
        userName = "Jacob Colvin";
        userEmail = "jacobcolvin1@gmail.com";
      };
      claude = {
        dangerouslySkipPermissions = true;
        extraAgents.go-doc-improver = ../../configs/claude/agents/go-doc-improver.md;
      };
    };
  };
}
