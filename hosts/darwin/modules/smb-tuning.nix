{
  config,
  lib,
  ...
}:

let
  cfg = config.dotfiles.system.darwin.smbTuning;
in

{
  options.dotfiles.system.darwin.smbTuning.enable = lib.mkOption {
    type = lib.types.bool;
    default = false;
    description = ''
      SMB client throughput tuning for Apple Silicon on a trusted LAN.
      Writes /etc/nsmb.conf (disables SMB signing, negotiate
      validation, multichannel, dir-cache; restricts to SMB2/3 over
      direct TCP; soft mounts) and ships a launchd daemon that sets
      net.inet.tcp.delayed_ack=0 at boot. Trades some MITM protection
      for ~20-30% SMB throughput; leave disabled when connecting to
      untrusted SMB shares.
    '';
  };

  config = lib.mkIf cfg.enable {
    # SMB client configuration (/etc/nsmb.conf).
    # Optimized for Apple Silicon on a trusted local network.
    # signing_required=no and validate_neg_off=yes trade MITM protection
    # for throughput — re-enable if connecting to untrusted SMB shares.
    environment.etc."nsmb.conf".text = lib.generators.toINI { } {
      default = {
        # Disable SMB packet signing for ~20-30% throughput gain on trusted networks.
        signing_required = "no";
        # Disable NTFS alternate data streams (buggy on Apple Silicon SMB3 stack).
        # Tradeoff: with streams off, macOS stores xattrs / resource forks in
        # `._` AppleDouble companion files on the share. Two independent ways
        # to eliminate them if it ever matters:
        #   - Server-side: enable vfs_fruit on the TrueNAS share
        #     (fruit:metadata=stream, fruit:resource=stream — usually the
        #     default for shares created with the Apple/Time Machine preset).
        #     This works regardless of the client `streams` value.
        #   - Client-side: flip this to "yes" and re-test SMB3 stability.
        streams = "no";
        # Suppress change notifications to reduce resource usage.
        notify_off = "yes";
        # Use direct TCP (port 445) only; skip legacy NetBIOS name resolution.
        port445 = "no_netbio";
        # Bitmap: 2=SMB1, 4=SMB2, 6=SMB2+SMB3. Enforce modern protocols only.
        protocol_vers_map = 6;
        # Soft mounts: operations fail gracefully instead of hanging
        # indefinitely when the SMB server becomes unresponsive.
        soft = "yes";
        # Disable multichannel — inconsistently negotiated on Apple Silicon.
        mc_on = "no";
        # When multichannel is re-enabled, prefer wired over Wi-Fi interfaces.
        mc_prefer_wired = "yes";
        # Disable local directory enumeration caching so Finder always fetches
        # current file/folder listings from the server.
        dir_cache_max_cnt = 0;
        # Skip SMB negotiate validation. Reduces overhead on trusted networks
        # (same MITM trade-off as signing_required=no).
        validate_neg_off = "yes";
      };
    };

    # Disable TCP delayed ACK for SMB performance.
    # macOS defaults to delaying ACK packets, which bottlenecks SMB throughput.
    launchd.daemons.tcp-delayed-ack-disable = {
      serviceConfig = {
        ProgramArguments = [
          "/usr/sbin/sysctl"
          "-w"
          "net.inet.tcp.delayed_ack=0"
        ];
        RunAtLoad = true;
        StandardErrorPath = "/dev/null";
        StandardOutPath = "/dev/null";
      };
    };
  };
}
