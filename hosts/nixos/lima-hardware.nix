# Lima guest-agent integration and service stability.
# Called as: import ./lima-hardware.nix nixos-lima
nixos-lima:
{ lib, pkgs, ... }:
{
  imports = [ "${nixos-lima}/lima.nix" ];

  # Prevent nixos-rebuild switch from restarting lima-init or its
  # dependent lima-guestagent. This allows provisioning scripts
  # (which run inside lima-init) to call switch without killing themselves.
  systemd.services.lima-init.restartIfChanged = lib.mkForce false;
  systemd.services.lima-guestagent.restartIfChanged = lib.mkForce false;

  virtualisation.containerd.enable = true;

  environment.systemPackages = with pkgs; [
    iptables
    nerdctl
    pkgsStatic.tini
    (writeShellScriptBin "docker" ''exec sudo ${nerdctl}/bin/nerdctl "$@"'')
  ];
}
