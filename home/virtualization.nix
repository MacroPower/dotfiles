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
  # later. The VZ driver reads "default" as virtio-sound. QEMU reads it as
  # CoreAudio playback and ignores microphone.
  #
  # The port rule keeps Lima from forwarding the guest port of aven's sync
  # proxy (home/aven.nix) back to the host. The Mac's own aven server
  # needs that port on the host. A forward that starts before the aven
  # server loops the proxy into itself and holds the server's port.
  home.file.".lima/_config/default.yaml".text = ''
    audio:
      device: default
      microphone: true
    portForwards:
      - guestPort: 3746
        ignore: true
  '';
}
