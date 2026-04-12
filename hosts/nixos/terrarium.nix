{
  nixos-lima,
  system,
}:
{
  inherit system;
  username = "jacobcolvin";
  hostModule =
    {
      pkgs,
      lib,
      config,
      ...
    }:
    {
      imports = [
        ./default.nix
        (import ./lima-hardware.nix nixos-lima)
      ];

      networking.hostName = "terrarium";

      # Override login shell to bash. workmux sandbox generates POSIX shell
      # syntax (export VAR=value) and passes it via `limactl shell -- eval`,
      # which uses the login shell. Fish cannot parse POSIX export syntax.
      users.users.${config.dotfiles.system.username}.shell = lib.mkForce pkgs.bash;

      environment.systemPackages = [ pkgs.terrarium ];

      # br_netfilter makes bridged (container) traffic traverse netfilter
      # hooks so terrarium's nftables rules can see it.
      boot.kernelModules = [ "br_netfilter" ];

      boot.kernel.sysctl = {
        # Let bridged traffic pass through iptables/ip6tables chains.
        "net.bridge.bridge-nf-call-iptables" = 1;
        "net.bridge.bridge-nf-call-ip6tables" = 1;
        # Allow DNAT to 127.0.0.1 on dynamically-created bridge/veth interfaces.
        "net.ipv4.conf.default.route_localnet" = 1;
        # Standard IP forwarding for container networking.
        "net.ipv4.ip_forward" = 1;
        # Loose reverse-path filter -- br_netfilter causes rpfilter mismatches
        # because bridged packets enter L3 hooks with veth as iif but routes
        # point to the bridge master.
        "net.ipv4.conf.all.rp_filter" = 2;
        "net.ipv4.conf.default.rp_filter" = 2;
      };

      # Boot-time deny-all firewall. This table loads before terrarium
      # starts and blocks all non-loopback traffic. Terrarium replaces it
      # with policy-based rules on startup. If terrarium never starts,
      # crashes, or is stopped, the deny-all rules remain in the kernel.
      networking.nftables = {
        enable = true;
        tables.terrarium = {
          family = "inet";
          content = ''
            chain input {
              type filter hook input priority filter; policy drop;
              iifname "lo" accept
              ct state established,related accept
              # Allow SSH from Lima host for guest agent communication.
              tcp dport 22 accept
            }
            chain output {
              type filter hook output priority filter; policy drop;
              oifname "lo" accept
              ct state established,related accept
            }
          '';
        };
      };

      # Envoy user (UID 1001) matching Terrarium's default.
      users.users.envoy = {
        isSystemUser = true;
        uid = 1001;
        group = "envoy";
      };
      users.groups.envoy = { };

      # Terrarium VM-wide network filter daemon.
      systemd.services.terrarium = {
        description = "Terrarium VM-wide network filter daemon";
        after = [
          "network-online.target"
          "nftables.service"
        ];
        wants = [ "network-online.target" ];
        wantedBy = [ "multi-user.target" ];
        environment = {
          HOME = "/var/lib/terrarium";
          XDG_DATA_HOME = "/var/lib/terrarium";
          XDG_STATE_HOME = "/var/lib/terrarium";
        };
        serviceConfig = {
          Type = "notify";
          ExecStart = "${pkgs.terrarium}/bin/terrarium daemon --config=/etc/terrarium/config.yaml";
          EnvironmentFile = "/etc/environment";
          StateDirectory = "terrarium";
          WatchdogSec = "30s";
          Restart = "always";
          RestartSec = "5s";
          StartLimitBurst = 0;
        };
      };

      # Terrarium egress policy, applied on every boot.
      environment.etc."terrarium/config.yaml".text = builtins.toJSON {
        logging = {
          dns = {
            enabled = true;
            format = "json";
            path = "/var/lib/terrarium/dns.log";
          };
          envoy = {
            path = "/var/lib/terrarium/envoy.log";
            accessLog = {
              enabled = true;
              format = "json";
              path = "/var/lib/terrarium/envoy-access.log";
            };
          };
          firewall.enabled = true;
        };
      };

      # CNI bridge network for containerd workloads.
      environment.etc."cni/net.d/10-bridge.conflist".text = builtins.toJSON {
        cniVersion = "1.0.0";
        name = "bridge";
        plugins = [
          {
            type = "bridge";
            bridge = "cni0";
            isGateway = true;
            ipMasq = false;
            ipam = {
              type = "host-local";
              ranges = [
                [
                  {
                    subnet = "172.20.0.0/16";
                    gateway = "172.20.0.1";
                  }
                ]
              ];
              routes = [ { dst = "0.0.0.0/0"; } ];
            };
            dns = {
              nameservers = [ "1.1.1.1" ];
            };
          }
          {
            type = "portmap";
            capabilities = {
              portMappings = true;
            };
          }
          { type = "loopback"; }
        ];
      };

      # Ensure the CA certificate directory exists for terrarium's
      # installCA, which copies the MITM CA cert here at runtime.
      systemd.tmpfiles.rules = [
        "d /usr/local/share/ca-certificates 0755 root root -"
      ];
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
