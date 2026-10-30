{ pkgs, ... }:
{
  home.packages = with pkgs; [
    lima
    qemu
  ];

  # Lima fills every instance's unset fields from this file each time the
  # instance starts. Workmux generates its own config for each sandbox VM
  # and does not expose Lima's audio fields. This file reaches both those
  # VMs and terrarium.
  #
  # The audio fields attach the Mac's speakers and microphone as a
  # virtio-sound device so voice mode in Claude Code works inside the
  # VMs. Lima marks them experimental, and microphone needs Lima 2.3.0 or
  # later (see limaOverlay in lib/default.nix). The VZ driver reads
  # "default" as virtio-sound. QEMU reads it as CoreAudio playback and
  # ignores microphone.
  #
  # The port rules keep Lima from forwarding aven's guest ports
  # (home/aven.nix) back to the host, where the Mac's own aven processes
  # need them. 3746 is the sync proxy: a forward that starts before the
  # aven server loops the proxy into itself and holds the server's port.
  # 47631 is the daemon's wake address, which both daemons bind.
  home.file.".lima/_config/default.yaml".text = ''
    audio:
      device: default
      microphone: true
    portForwards:
      - guestPort: 3746
        ignore: true
      - guestPort: 47631
        ignore: true
  '';
}
