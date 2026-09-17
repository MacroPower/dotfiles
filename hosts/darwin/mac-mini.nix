{
  username = "jacobcolvin";
  hostname = "Jacobs-Mac-mini";

  loginItems = [
    "~/Applications/Home Manager Apps/Caffeine.app"
    "/Applications/Hyperkey.app"
    "/Applications/OrbStack.app"
  ];

  dockExtraApps = [
    "/Applications/Discord.app"
    "~/Applications/Home Manager Apps/Obsidian.app"
  ];

  homebrew = {
    taps = [ ];
    brews = [ ];
    casks = [
      "discord"
      "plex"
      "orbstack"
      "filebot"
    ];
    masApps = { };
  };

  darwinModule = _: {
    dotfiles.system.darwin.bluetoothAac.enable = true;
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
      ];
      dotfiles = {
        git = {
          userName = "Jacob Colvin";
          userEmail = "jacobcolvin1@gmail.com";
        };
        claude = {
          remoteControl = true;
          agents.go-doc-improver.source = ../../configs/claude/agents/go-doc-improver.md;
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
