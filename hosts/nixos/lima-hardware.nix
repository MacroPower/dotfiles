# Lima guest-agent integration and service stability.
# Called as: import ./lima-hardware.nix nixos-lima
nixos-lima:
{
  config,
  lib,
  pkgs,
  ...
}:
{
  imports = [ "${nixos-lima}/lima.nix" ];

  # Prevent nixos-rebuild switch from restarting lima-init or its
  # dependent lima-guestagent. This allows provisioning scripts
  # (which run inside lima-init) to call switch without killing themselves.
  systemd.services.lima-init.restartIfChanged = lib.mkForce false;
  systemd.services.lima-guestagent.restartIfChanged = lib.mkForce false;

  virtualisation.containerd.enable = true;

  virtualisation.docker = {
    enable = true;
    daemon.settings = {
      containerd = "/run/containerd/containerd.sock";
      features.containerd-snapshotter = true;
    };
  };

  # Upstream virtualisation.docker orders only after network.target
  # and docker.socket. With containerd pointing at an external daemon,
  # dockerd races ahead on boot and crashloops until containerd is up.
  systemd.services.docker = {
    after = [ "containerd.service" ];
    requires = [ "containerd.service" ];
  };

  users.users.${config.dotfiles.system.username}.extraGroups = [ "docker" ];

  # dockerd uses the `moby` containerd namespace; align nerdctl so
  # both CLIs see the same containers/images without extra flags.
  environment.etc."nerdctl/nerdctl.toml".text = ''
    namespace = "moby"
  '';

  environment.systemPackages = with pkgs; [
    iptables
    nerdctl
    pkgsStatic.tini
  ];
}
