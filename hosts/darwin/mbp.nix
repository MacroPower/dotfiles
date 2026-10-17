{
  username = "jacobcolvin";
  hostname = "Jacobs-MacBook-Pro";

  loginItems = [
    "~/Applications/Home Manager Apps/Caffeine.app"
    "/Applications/Hyperkey.app"
    "/Applications/OrbStack.app"
  ];

  dockExtraApps = [
    "/Applications/Discord.app"
    "~/Applications/Home Manager Apps/Obsidian.app"
  ];

  power = {
    sleep = {
      computer = 15;
      display = 5;
      allowSleepByPowerButton = true;
    };
    restartAfterFreeze = true;
    restartAfterPowerFailure = null;
    disableSleep = false;
  };

  homebrew = {
    taps = [ ];
    brews = [ ];
    casks = [
      "discord"
      "plex"
      "orbstack"
      "filebot"
      "digikam"
    ];
    unsignedCasks = [ "digikam" ];
    masApps = { };
  };

  darwinModule =
    { lib, ... }:
    {
      # mypyc-compiled packages (mypy on the kagimcp py3.12 chain) compile one
      # huge generated C file. At the shared 8 GiB, cc1 gets OOM-killed even
      # with no other jobs. Launch-script-only, so the guest still
      # substitutes. mkOverride 40 beats the shared mkForce (priority 50).
      nix.linux-builder.config.virtualisation.memorySize = lib.mkOverride 40 (32 * 1024); # 32 GiB

      dotfiles.system.darwin.bluetoothAac.enable = true;
      dotfiles.system.darwin.epsonScanV19II.enable = true;
      dotfiles.system.darwin.fork.enable = true;
      dotfiles.system.darwin.fuseT.enable = true;
      dotfiles.system.darwin.hyperkey.enable = true;
      dotfiles.system.darwin.linearmouse.enable = true;
      dotfiles.system.darwin.smbTuning.enable = true;
      dotfiles.system.darwin.spotlight.enable = true;
    };

  homeModule =
    { ... }:
    {
      imports = [
        ../../home/firefox.nix
        ../../home/ghostty.nix
        ../../home/zed.nix
        ../../home/development.nix
        ../../home/kubernetes.nix
        ../../home/claude.nix
        ../../home/displayplacer.nix
        ../../home/personal.nix
        ../../home/obsidian.nix
        ../../home/comfyui.nix
        ../../home/digikam.nix
        ../../home/yt-dlp.nix
      ];
      dotfiles = {
        git = {
          userName = "Jacob Colvin";
          userEmail = "jacobcolvin1@gmail.com";
        };
        claude = {
          fetchAllowlist = false;
          clauth.enable = true;
          agents.go-doc-improver.source = ../../configs/claude/agents/go-doc-improver.md;
          kubeApiDomains = [
            "kmain.cin.macro.network"
            "kmgmt.cin.macro.network"
          ];
          lima = {
            enable = true;
            cpus = 12;
            memory = "32GiB";
            disk = "500GiB";
          };
        };
        comfyui.enable = true;
        ytdlp.enable = true;
      };
    };
}
