{
  username = "jacobcolvin";

  loginItems = [
    "/Applications/LinearMouse.app"
    "/Applications/Caffeine.app"
    "/Applications/OrbStack.app"
  ];

  homebrew = {
    taps = [ ];
    brews = [ ];
    casks = [
      "firefox"
      "discord"
      "plex"
      "orbstack"
      "slack"
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
        extraHomePackages = with pkgs; [ talosctl ];
        vscode.extraExtensions =
          marketplace: with marketplace; [
            wakatime.vscode-wakatime
          ];
      };
    };
}
