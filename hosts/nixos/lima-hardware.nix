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
}
