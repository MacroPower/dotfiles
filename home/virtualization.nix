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
  home.file.".lima/_config/default.yaml".text = ''
    audio:
      device: default
      microphone: true
  '';
}
