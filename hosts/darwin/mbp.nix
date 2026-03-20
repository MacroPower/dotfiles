{
  username = "jacobcolvin";

  loginItems = [
    "/Applications/LinearMouse.app"
    "~/Applications/Home Manager Apps/Caffeine.app"
    "/Applications/OrbStack.app"
  ];

  extraApps = [
    "discord"
    "obsidian"
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
          discord
          obsidian
          slack
        ];
        vscode.extraExtensions =
          marketplace: with marketplace; [
            wakatime.vscode-wakatime
          ];
      };
    };
}
