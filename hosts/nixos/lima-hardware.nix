# Lima guest-agent integration and service stability.
# Called as: import ./lima-hardware.nix nixos-lima
nixos-lima:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  user = config.dotfiles.system.username;
in
{
  imports = [ "${nixos-lima}/lima.nix" ];

  # Prevent nixos-rebuild switch from restarting lima-init or its
  # dependent lima-guestagent. This allows provisioning scripts
  # (which run inside lima-init) to call switch without killing themselves.
  systemd.services.lima-init.restartIfChanged = lib.mkForce false;
  systemd.services.lima-guestagent.restartIfChanged = lib.mkForce false;

  # Docker (rootful + rootless). Rootless uses rootlesskit's pasta
  # network driver by default; pasta's built-in DNS forwarder
  # (--dns-forward=10.0.2.3) drops queries under rate (upstream:
  # containers/podman#21947). Pointing the daemon at a non-loopback IP
  # other than 10.0.2.3 routes container queries through pasta's
  # general UDP flow tracker instead of the buggy DNS-forwarder.
  # Terrarium's nat_prerouting DNATs any non-loopback DNS query to
  # 127.0.0.1:53 regardless, so the specific IP is cosmetic. Both
  # daemons get the same setting for symmetry even though rootful
  # (kernel bridge) does not hit the pasta bug.
  #
  # Rootless dockerd and an embedded containerd run under the lima
  # user's systemd --user manager. No root socket, no docker group.
  # Sockets do not collide with rootful:
  # - Rootful at /run/docker.sock
  # - Rootless at $XDG_RUNTIME_DIR/docker.sock
  virtualisation.docker = {
    enable = true;
    daemon.settings = {
      dns = [ "1.1.1.1" ];
      # Avoid the 172.17-172.20 default pools so an expanding pool cannot
      # collide with the CNI bridge at 172.20.0.0/16 in terrarium.nix.
      default-address-pools = [
        {
          base = "172.30.0.0/16";
          size = 24;
        }
        {
          base = "172.31.0.0/16";
          size = 24;
        }
      ];
    };
    rootless = {
      enable = true;
      setSocketVariable = true;
      daemon.settings.dns = [ "1.1.1.1" ];
      # Default slirp4netns breaks nested network namespaces, which Dagger's
      # BuildKit workers create per build step which manifests as flaky DNS
      # ("Could not resolve host") inside nested containers. pasta handles
      # nested netns reliably and preserves source IPs on inbound.
      extraPackages = [ pkgs.passt ];
    };
  };

  # dockerd-rootless.sh reads these and assembles --net/--port-driver flags
  # for rootlesskit. No declared NixOS option exists; extend the user unit.
  systemd.user.services.docker.environment = {
    DOCKERD_ROOTLESS_ROOTLESSKIT_NET = "pasta";
    DOCKERD_ROOTLESS_ROOTLESSKIT_PORT_DRIVER = "implicit";
  };

  users.users.${user} = {
    # docker-rootless needs subuid/subgid for newuidmap/newgidmap;
    # NixOS's rootless module does not provision them on its own.
    autoSubUidGidRange = true;
    # Keep the user manager (and dockerd) alive across logout so
    # `limactl shell` reconnects always find the daemon running.
    linger = true;
  };
}
