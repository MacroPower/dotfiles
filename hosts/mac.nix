{
  self,
  pkgs,
  hostConfig,
  ...
}:

{
  nix.settings.experimental-features = [
    "nix-command"
    "flakes"
  ];

  nixpkgs.config.allowUnfree = true;

  programs.fish.enable = true;

  homebrew = {
    enable = true;
    onActivation = {
      cleanup = "zap";
      upgrade = true;
    };

    taps = [
      "buo/cask-upgrade"
      "dagger/tap"
      "jakehilborn/jakehilborn"
      "macos-fuse-t/cask"
      "photo-cli/photo-cli"
      "kcl-lang/tap"
      "ymtdzzz/tap"
      "macropower/tap"
      "robusta-dev/krr"
      "jacobcolvin/tap"
    ]
    ++ hostConfig.homebrew.extraTaps;

    brews = [
      "dagger"
      "jakehilborn/jakehilborn/displayplacer"
      "photo-cli"
      "kubecolor/tap/kubecolor"
      "ymtdzzz/tap/otel-tui"
      "kcl-lang/tap/kcl"
      "kcl-lang/tap/kcl-lsp"
      "robusta-dev/krr/krr"
      "diskonaut"
    ]
    ++ hostConfig.homebrew.extraBrews;

    casks = [
      "appcleaner"
      "caffeine"
      "drawio"
      "gpg-suite-no-mail"
      "keka"
      "linearmouse"
      "obsidian"
      "rectangle"
      "vlc"
      "fuse-t"
      "ghostty"
      "zed"
      "visual-studio-code"
      "monodraw"
      "db-browser-for-sqlite"
      "fork"
      "wireshark"
      "dotnet-sdk"
      "kat"
    ]
    ++ hostConfig.homebrew.extraCasks;

    inherit (hostConfig.homebrew) masApps;

    caskArgs.no_quarantine = true;
  };

  fonts.packages = with pkgs; [
    nerd-fonts.fira-code
  ];

  system = {
    defaults = {
      finder.AppleShowAllFiles = true;
      NSGlobalDomain."com.apple.swipescrolldirection" = false;
      CustomUserPreferences = {
        "com.apple.desktopservices".DSDontWriteNetworkStores = true;
        NSGlobalDomain = {
          AppleAccentColor = 5;
          AppleHighlightColor = "0.968627 0.831373 1.000000 Purple";
        };
        "com.apple.controlcenter".Sound = 18;
      };
    };

    configurationRevision = self.rev or self.dirtyRev or null;
    primaryUser = hostConfig.username;
    stateVersion = 6;
  };

  power.sleep.computer = "never";
  power.sleep.display = "never";

  environment.etc."nsmb.conf".text = ''
    [default]
      signing_required=no
      streams=yes
      notify_off=yes
      port445=no_netbio
      unix extensions=no
      veto files=/._*/.DS_Store/
      protocol_vers_map=6
  '';

  users.users.${hostConfig.username} = {
    name = hostConfig.username;
    home = "/Users/${hostConfig.username}";
    shell = pkgs.fish;
  };
}
