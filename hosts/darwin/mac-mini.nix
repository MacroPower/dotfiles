{
  username = "jacobcolvin";
  hostname = "Jacobs-Mac-mini";

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
      ];
      dotfiles = {
        git = {
          userName = "Jacob Colvin";
          userEmail = "jacobcolvin1@gmail.com";
        };
        claude = {
          remoteControl = true;
          extraAgents.go-doc-improver = ../../configs/claude/agents/go-doc-improver.md;
          kubeApiDomains = [
            "kmain.cin.macro.network"
            "kmgmt.cin.macro.network"
          ];
          lima = {
            enable = true;
            cpus = 8;
            memory = "8GiB";
            disk = "120GiB";
          };
        };
      };
    };
}
