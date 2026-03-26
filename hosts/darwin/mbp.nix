{
  username = "jacobcolvin";
  hostname = "Jacobs-MacBook-Pro";

  loginItems = [
    "/Applications/LinearMouse.app"
    "~/Applications/Home Manager Apps/Caffeine.app"
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
    { pkgs, ... }:
    {
      dotfiles = {
        git = {
          userName = "Jacob Colvin";
          userEmail = "jacobcolvin1@gmail.com";
        };
        extraHomePackages = with pkgs; [
          talosctl
          tpi
          discord
          obsidian
          slack
        ];
        claude.extraAgents.go-doc-improver = ../../configs/claude/agents/go-doc-improver.md;
        vscode.extraExtensions =
          marketplace: with marketplace; [
            wakatime.vscode-wakatime
          ];
      };
    };
}
