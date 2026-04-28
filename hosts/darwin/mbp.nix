{
  username = "jacobcolvin";
  hostname = "Jacobs-MacBook-Pro";

  loginItems = [
    "/Applications/LinearMouse.app"
    "~/Applications/Home Manager Apps/Caffeine.app"
    "/Applications/Hyperkey.app"
    "/Applications/OrbStack.app"
  ];

  dockExtraApps = [
    "~/Applications/Home Manager Apps/Discord.app"
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
      "plex"
      "orbstack"
      "filebot"
    ];
    masApps = { };
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
      ];
      dotfiles = {
        git = {
          userName = "Jacob Colvin";
          userEmail = "jacobcolvin1@gmail.com";
        };
        claude = {
          remoteControl = true;
          fetchAllowlist = false;
          extraAgents.go-doc-improver = ../../configs/claude/agents/go-doc-improver.md;
          lima = {
            enable = true;
            cpus = 12;
            memory = "32GiB";
            disk = "500GiB";
          };
        };
        comfyui.enable = true;
      };
    };
}
