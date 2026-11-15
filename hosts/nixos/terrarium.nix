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
  # The share of the VM's memory that the processes of one login session
  # can hold. hostModule sets it as the MemoryMax of every session scope,
  # and homeModule names it in the Claude Code host notes.
  sessionMemoryMax = "75%";
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
    let
      inherit (config.dotfiles.system) username;
      userHome = config.home-manager.users.${username}.home;
      # home-manager writes the entries of home.sessionVariables,
      # home.sessionPath, and home.sessionSearchVariables to this script.
      sessionVarsScript = "${userHome.sessionVariablesPackage}/etc/profile.d/hm-session-vars.sh";
    in
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

      # Workmux starts each agent from the user's bash login shell. The agent
      # and every command that it runs inherit the environment of that
      # shell. home-manager loads its session variables only in the shells
      # that it manages, and it manages only fish on this host. Every bash
      # login shell reads /etc/profile, so /etc/profile sources the
      # home-manager script.
      #
      # NixOS puts this text after the lines of /etc/profile that set the
      # system environment. Fish loads the system environment and the
      # home-manager script in the same order, so a bash login shell and
      # fish get the same values. The home-manager values replace the
      # NixOS defaults, such as EDITOR. In PATH, the home.sessionPath
      # directories come first, then /run/wrappers/bin, then the Nix
      # profiles, and /run/current-system/sw/bin comes last.
      #
      # The script holds paths in the home directory of this user, such as
      # XDG_CONFIG_HOME, so the login shells of the other users skip it.
      programs.bash.loginShellInit = ''
        if [ "$USER" = ${lib.escapeShellArg username} ]; then
          . ${sessionVarsScript}
        fi
      '';

      # These assertions fail the evaluation when a bash login shell does
      # not export an entry of home.sessionVariables. The first assertion
      # reads /etc/profile. It also fails when /etc/profile sources the
      # home-manager script before the system environment, because the
      # NixOS script then replaces PATH and EDITOR. The second assertion
      # reads the home-manager script.
      assertions =
        let
          # The line of /etc/profile that sources a script. lib.splitString
          # and lib.hasInfix reject a pattern that carries the context of
          # a store path, so the line drops the context.
          sourceLine = script: builtins.unsafeDiscardStringContext ". ${script}\n";
          # The text of /etc/profile on each side of the line that sources
          # the home-manager script.
          profileParts = lib.splitString (sourceLine sessionVarsScript) config.environment.etc.profile.text;
          # home-manager skips each entry that holds null.
          sessionVariableNames = builtins.attrNames (
            lib.filterAttrs (_: value: value != null) userHome.sessionVariables
          );
          unexported = lib.filter (
            name: !lib.hasInfix "\nexport ${name}=" userHome.sessionVariablesPackage.text
          ) sessionVariableNames;
        in
        [
          {
            assertion =
              builtins.length profileParts == 2
              && lib.hasInfix (sourceLine config.system.build.setEnvironment) (builtins.head profileParts);
            message = ''
              terrarium: /etc/profile does not source the home-manager
              script after the script that sets the NixOS environment.
              Workmux starts each agent from a bash login shell, and that
              shell gets home.sessionVariables and home.sessionPath only
              from /etc/profile. Source this script in
              programs.bash.loginShellInit:
              ${sessionVarsScript}
            '';
          }
          {
            assertion = unexported == [ ];
            message = ''
              terrarium: the home-manager script that /etc/profile sources
              does not export these entries of home.sessionVariables, so
              the agents that workmux starts do not get them:
              ${lib.concatStringsSep ", " unexported}
            '';
          }
        ];

      # The VM has no swap. When its memory runs out, the kernel gives
      # each process a score and kills the process with the highest
      # score. The score is the memory that the process holds, plus the
      # oom_score_adj of the process in thousandths of the VM's memory.
      #
      # The commands of a login session run at an oom_score_adj of 0. The
      # upstream unit starts the user manager at 100, and the manager
      # starts each of its services 100 higher than itself. A user
      # service at 200 therefore outscores a session command until the
      # command holds a fifth of the VM's memory more than the service
      # holds.
      #
      # On a 47 GiB VM, twelve race-test binaries of about 5 GiB each ran
      # in one session. The kernel killed rootless dockerd, the Dagger
      # engine, every other user service, and the user manager before it
      # killed the first test binary. The upstream unit does not set
      # Restart=, so nothing restarted the dead user manager. The
      # `systemctl --user` command and rootless Docker stayed down until
      # `sudo systemctl start` brought the manager back.
      #
      # With an OOMScoreAdjust of -100, the kernel ranks the manager after
      # every process at 0, and the manager starts its services at 0,
      # level with the session commands. The kernel then kills the
      # largest process first. Restart=on-failure starts the manager
      # again when it dies of a signal or exits with an error. Restarts
      # that are 5 seconds apart never reach the default start limit of
      # five starts in 10 seconds, so systemd keeps trying.
      #
      # A switch applies the restart setting to the running manager. The
      # manager and its services keep their old oom_score_adj until the
      # manager starts again. To apply the new value, reboot the VM or
      # run `sudo systemctl restart "user@$(id -u).service"`. The restart
      # stops every user service and every rootless Docker container.
      systemd.services."user@".serviceConfig = {
        OOMScoreAdjust = -100;
        Restart = "on-failure";
        RestartSec = "5s";
      };

      # logind puts each login in a scope named session-<id>.scope, and
      # systemd reads this drop-in into every such scope. A process stays
      # in the scope of the login that started it. Workmux starts each
      # agent with `limactl shell`, and Lima carries every such shell over
      # one SSH connection, so one login and one scope hold every agent.
      #
      # When the processes of one scope need more than MemoryMax, the
      # kernel kills the largest process of that scope. It never kills a
      # process outside the scope to enforce the limit. While one login
      # holds every agent, the limit leaves the rest of the VM's memory to
      # the user services and the system. A second login, such as one that
      # a reconnected SSH connection opens, gets a limit of its own.
      # logind sets OOMPolicy=continue on each session scope, so the
      # other processes of the scope keep running. A switch applies the
      # limit to the running scopes.
      #
      # systemd-oomd does not fit here. It kills a whole cgroup, and the
      # cgroup of the scope holds every agent.
      systemd.units."session-.scope" = {
        overrideStrategy = "asDropin";
        text = ''
          [Scope]
          MemoryMax=${sessionMemoryMax}
        '';
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
          # log, the daemon that redirects UDP datagrams, and the memory
          # limit on login sessions. So the guidance lives here and not in
          # the instructions that every host gets.
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

            Tests that send UDP over loopback, such as the tests of a DNS server, fail with a timeout on this host. Go tests report the timeout as `read udp 127.0.0.1:50210->127.0.0.1:39613: i/o timeout`. The terrarium daemon redirects to Envoy each UDP datagram that a non-root process sends to a port other than 53. Datagrams to an address of the VM itself, such as `127.0.0.1` or `::1`, then never arrive at the sockets that listen for them. TCP connections over loopback work.

            To run a command that sends UDP over loopback, start it in a new network namespace, where the rules of the daemon do not apply:

            ```sh
            unshare -rn sh -c 'ip link set lo up && go test ./...'
            ```

            `unshare -rn` runs the shell as root in a new user namespace and a new network namespace. The loopback interface of a new network namespace is down, and `ip link set lo up` brings it up. Without that step, a connection to `127.0.0.1` fails with `network is unreachable`, and a socket on `::1` fails with `cannot assign requested address`.

            The namespace holds no interface other than its own loopback. Commands in the namespace cannot reach the internet, the Lima host, or a server that listens outside the namespace. So fetch the dependencies of the command before you start it. Unix sockets that have a file path work from inside the namespace, so `nix` and `devbox` still reach the Nix daemon.

            Root in the namespace is your own user outside it, so the files that the command writes belong to your user. Root in the namespace also opens each file of your user whatever the mode of the file, so tests that expect a permission error fail. To run the command as your own user, drop root with a second `unshare`. Keep the double quotes, so that the outer shell fills in your IDs. Inside the namespace, `id -u` prints 0.

            ```sh
            unshare -rn sh -c "ip link set lo up && exec unshare -U --map-user=$(id -u) --map-group=$(id -g) go test ./..."
            ```

            When a commit hook runs tests that send UDP over loopback, run `git commit` in a namespace so that the hook passes. Pass the `git commit` command as arguments after the `sh -c` string, not inside it. The outer shell then expands the commit message. So the quotes in the message, or in a heredoc that builds it, never end the single quotes of the `sh -c` string. The `sh` after the string sets `$0`, and `exec "$@"` runs the arguments after `sh`:

            ```sh
            unshare -rn sh -c 'ip link set lo up && exec "$@"' sh git commit -F msg.txt
            ```

            Hooks that also need the internet fail in the namespace. The Claude Code hook that lints commit messages finds a message only when the shell runs `git` itself. In this command, `git` is an argument of `unshare`, so that hook does not lint the message. Before you commit, run `prose-lint --commit < msg.txt` to lint the message yourself.

            systemd limits the processes of one login to ${sessionMemoryMax} of the VM's memory. The commands of every Claude Code session that the login started count toward the limit. When those processes need more memory than the limit, the kernel kills the largest one with SIGKILL. Shells report the kill as exit status 137, and `go test` reports it as `signal: killed`.

            After the kernel kills a command, list the processes that the kernel killed today to free memory:

            ```sh
            journalctl -k --since today -g 'Killed process'
            ```

            Each line names one killed process and gives the memory that the process held as `anon-rss`. When the processes of your command held most of that memory, do not run the command again unchanged. Run fewer processes at the same time, or lower the memory that each process needs.
          '';
        };
        ytdlp.enable = true;
      };
      # Inner tmux uses C-a so it doesn't collide with the macOS-host
      # tmux's C-b prefix when SSHed in from a nested session.
      programs.tmux.prefix = lib.mkForce "C-a";
    };
}
