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

      # Workmux multiplexes SSH over a Lima control socket and each pane/
      # agent RPC channel consumes a session. The default of 10 is hit in
      # bursts across a few worktrees; raise the ceiling for this dev VM.
      services.openssh.settings.MaxSessions = 100;

      # Override login shell to bash. workmux sandbox generates POSIX shell
      # syntax (export VAR=value) and passes it via `limactl shell -- eval`,
      # which uses the login shell. Fish cannot parse POSIX export syntax.
      users.users.${config.dotfiles.system.username}.shell = lib.mkForce pkgs.bash;

      environment.systemPackages = [ pkgs.terrarium ];

      # Cap retained generations so kernels + initrds don't fill the
      # ~250M Lima /boot partition. Five is enough headroom to roll
      # back across a couple of bad rebuilds without hitting ENOSPC
      # during the next bootloader install.
      boot.loader.grub.configurationLimit = 5;

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
      };

      # Boot-time deny-all egress. Terrarium replaces this table with
      # policy-based rules on startup. If terrarium never starts, the
      # deny-all OUTPUT remains in the kernel.
      networking.nftables = {
        enable = true;
        tables.terrarium = {
          family = "inet";
          content = ''
            chain output {
              type filter hook output priority filter; policy drop;
              oifname "lo" accept
              ct state established,related accept
            }
          '';
        };
        # Guard table -- terrarium never touches this, so it survives
        # daemon reloads. Accepts traffic carrying the policy-evaluated
        # fwmark (bit 0x2) set by the terrarium table; drops everything
        # else when the daemon is down (no terrarium table, no mark).
        tables.terrarium-guard = {
          family = "inet";
          content = ''
            chain output {
              type filter hook output priority 10; policy accept;
              ip daddr 127.0.0.0/8 accept
              ip6 daddr ::1 accept
              meta mark & 0x2 == 0x2 accept
              ct state established,related accept
              drop
            }
          '';
        };
      };

      # Inbound filtering via the NixOS firewall. Accept DNATted
      # bridge-container traffic and TPROXY-marked forwarded packets.
      # Loose rpfilter avoids br_netfilter iif mismatches at the
      # nftables level (fib saddr . mark oif exists).
      networking.firewall = {
        enable = true;
        checkReversePath = "loose";
        allowedTCPPorts = [ 22 ];
        extraInputRules = ''
          ct status dnat accept
          meta mark & 0x1 == 0x1 accept
        '';
        # TPROXY-marked forwarded packets use policy routing table 100
        # (local default dev lo), which changes the FIB reverse path
        # result. Without this exception, the rpfilter chain drops them
        # because fib saddr . mark resolves via table 100 instead of main.
        extraReversePathFilterRules = ''
          meta mark & 0x1 == 0x1 accept
        '';
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
        partOf = [ "nftables.service" ];
        wantedBy = [
          "multi-user.target"
          "nftables.service"
        ];
        environment = {
          HOME = "/var/lib/terrarium";
          XDG_DATA_HOME = "/var/lib/terrarium";
          XDG_STATE_HOME = "/var/lib/terrarium";
        };
        serviceConfig = {
          Type = "notify";
          ExecStart = "${pkgs.terrarium}/bin/terrarium daemon --config=/etc/terrarium/config.yaml";
          EnvironmentFile = "/etc/environment";
          RuntimeDirectory = "terrarium";
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
  homeModule =
    { lib, ... }:
    {
      imports = [
        ../../home/development.nix
        ../../home/kubernetes.nix
        ../../home/claude.nix
        ../../home/tmux.nix
      ];
      dotfiles = {
        sops.enable = false;
        git = {
          userName = "Jacob Colvin";
          userEmail = "jacobcolvin1@gmail.com";
        };
        claude = {
          dangerouslySkipPermissions = true;
          remoteControl = true;
          research.useVault = true;
        };
      };
      # Inner tmux uses C-a so it doesn't collide with the macOS-host
      # tmux's C-b prefix when SSHed in from a nested session.
      programs.tmux.prefix = lib.mkForce "C-a";
    };
}
