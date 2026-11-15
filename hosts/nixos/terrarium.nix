{
  nixos-lima,
  system,
}:
let
  # The terrarium daemon writes the DNS log, and Envoy writes its
  # process log and its access log. hostModule sets each path in the
  # daemon config and rotates every file in this set. homeModule names
  # the access log and its rotated files in the Claude Code host notes.
  logs = {
    dns = "/var/lib/terrarium/dns.log";
    envoy = "/var/lib/terrarium/envoy.log";
    envoyAccess = "/var/lib/terrarium/envoy-access.log";
  };
  # logrotate keeps this many rotated files of each log.
  logRotations = 7;
in
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

      users.users.${config.dotfiles.system.username} = {
        # Override login shell to bash. workmux sandbox generates POSIX shell
        # syntax (export VAR=value) and passes it via `limactl shell -- eval`,
        # which uses the login shell. Fish cannot parse POSIX export syntax.
        shell = lib.mkForce pkgs.bash;
        # Voice capture reads the virtio-sound device that Lima attaches
        # through ~/.lima/_config/default.yaml (home/virtualization.nix).
        # /dev/snd is root:audio 0660, and logind does not grant a seat ACL
        # to SSH sessions such as `limactl shell`.
        extraGroups = [ "audio" ];
      };

      # Claude Code's native recorder loads libasound through nix-ld.
      # arecord from alsa-utils is its fallback recorder and lists capture
      # devices with `arecord -l`.
      programs.nix-ld.libraries = [ pkgs.alsa-lib ];

      environment.systemPackages = [
        pkgs.terrarium
        pkgs.alsa-utils
      ];

      # GRUB copies each menu entry's kernel and initrd onto the ~250M Lima
      # /boot partition, and a kernel plus initrd takes ~92M, so at most two
      # distinct kernels fit. Three generations rarely span more than two
      # kernels; five did (7.2.9, 7.2.10, 7.3) and the install failed with
      # ENOSPC while copying the third kernel.
      boot.loader.grub.configurationLimit = 3;

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
            path = logs.dns;
          };
          envoy = {
            path = logs.envoy;
            accessLog = {
              enabled = true;
              format = "json";
              path = logs.envoyAccess;
            };
          };
          firewall.enabled = true;
        };
      };

      # Nothing else bounds these logs. In five days of uptime the DNS
      # log reached 2.4 GB and the access log 621 MB. The daemon reopens
      # its logs only on a reload, and a reload restarts the DNS proxy
      # and Envoy. So logrotate copies each file and then truncates it in
      # place. Each writer opens its file with O_APPEND, so its next line
      # goes to the start of the truncated file. logrotate loses the
      # lines that arrive between its copy and its truncate.
      # delaycompress leaves the newest rotated file uncompressed, so
      # `agrind -f` reads that file. The logrotate timer fires every
      # hour, and its first run of each day rotates the logs.
      services.logrotate.settings.terrarium = {
        files = builtins.attrValues logs;
        frequency = "daily";
        rotate = logRotations;
        copytruncate = true;
        compress = true;
        delaycompress = true;
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
        ../../home/yt-dlp.nix
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
          fetchAllowlist = false;
          clauth.enable = true;
          toolBundles.destiny.enable = true;
          # This image runs Claude inside the Lima sandbox, where
          # ~/.kube/config is the guest's own config (no host admin
          # creds). Surface it as a local kubectx source so guest-local
          # clusters behave like any normal cluster.
          guestKubeconfigLocal = true;
          # home/claude.nix renders this text as the Host Environment
          # section of ~/.claude/CLAUDE.md. Only this host has the access
          # log, so the guidance lives here and not in the instructions
          # that every host gets.
          hostContext = ''
            This host is the terrarium VM. The terrarium daemon routes each outbound connection of the VM and of its containers through Envoy.

            When a build, a fetch, or a commit hook is slow, read `${logs.envoyAccess}` first. Envoy appends one JSON line when an outbound connection closes, so the log does not show a download in progress. These fields show where a connection went, what it downloaded, and how fast:

            - `time`: the start of the connection, in UTC.
            - `sni`: the TLS server name of a connection to port 443. Plain HTTP requests to port 80 leave `sni` null and name the server in `authority`. TCP connections to other ports and UDP flows leave both fields null.
            - `upstream`: the address and port that Envoy connected to. It is the only field that names the connections that leave both `sni` and `authority` null. Many of those connections go to `127.0.0.1` or to the Lima host gateway at `192.168.5.2`, not to the internet.
            - `tx_bytes`: the bytes that the connection downloaded. `rx_bytes` holds the bytes that the connection uploaded.
            - `duration`: the milliseconds that the connection stayed open.

            This command totals the bytes downloaded from each server since the timestamp. Replace the timestamp with the UTC time that the slow command started.

            ```sh
            agrind -f ${logs.envoyAccess} '* | json | where time >= "2026-01-31T14:05" | sum(tx_bytes) as bytes, count by sni | sort by bytes desc'
            ```

            The output puts every connection that leaves `sni` null in one `None` row. To split that row by destination, group those connections by `authority` and by the host part of `upstream`:

            ```sh
            agrind -f ${logs.envoyAccess} '* | json | where time >= "2026-01-31T14:05" | where isNull(sni) | parse regex "^(?P<host>.*):[0-9]+$" from upstream nodrop | sum(tx_bytes) as bytes, count by authority, host | sort by bytes desc'
            ```

            logrotate rotates the log every day, so `${logs.envoyAccess}` holds only the lines that Envoy wrote after the last rotation. `${logs.envoyAccess}.1` holds the lines of the day that ended at that rotation, and the modification time of `${logs.envoyAccess}.1` is the time of the rotation. `${logs.envoyAccess}.2.gz` to `${logs.envoyAccess}.${toString logRotations}.gz` hold the days before that, newest first.

            When the slow command started before the last rotation, pipe the `.1` file and the live log to the same query. `zcat -f` decompresses the `.gz` files and copies the uncompressed files unchanged, so the same pipe reads every one of these files.

            ```sh
            zcat -f ${logs.envoyAccess}.1 ${logs.envoyAccess} | agrind '* | json | where time >= "2026-01-31T14:05" | sum(tx_bytes) as bytes, count by sni | sort by bytes desc'
            ```
          '';
        };
        ytdlp.enable = true;
      };
      # Inner tmux uses C-a so it doesn't collide with the macOS-host
      # tmux's C-b prefix when SSHed in from a nested session.
      programs.tmux.prefix = lib.mkForce "C-a";
    };
}
