{
  nixos-lima,
  system,
}:
{
  inherit system;
  username = "jacobcolvin";
  hostModule =
    { pkgs, ... }:
    {
      imports = [
        ./default.nix
        (import ./lima-hardware.nix nixos-lima)
      ];

      networking.hostName = "terrarium";

      environment.systemPackages = [ pkgs.terrarium ];

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
          ExecStart = "${pkgs.terrarium}/bin/terrarium daemon --config=\${TERRARIUM_CONFIG}";
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
