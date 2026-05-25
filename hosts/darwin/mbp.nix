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

  darwinModule = _: {
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
          extraAgents.go-doc-improver = ../../configs/claude/agents/go-doc-improver.md;
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
