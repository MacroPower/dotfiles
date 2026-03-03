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

  nix.gc = {
    automatic = true;
    options = "--delete-older-than 30d";
  };

  nixpkgs.config.allowUnfree = true;

  programs.fish.enable = true;

  homebrew = {
    enable = true;
    enableFishIntegration = true;
    onActivation = {
      cleanup = "zap";
      upgrade = true;
    };

    taps = [
      "buo/cask-upgrade"
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
      "jakehilborn/jakehilborn/displayplacer"
      "photo-cli"

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

  system = {
    defaults = {
      # Dock: auto-hide and clean appearance
      dock = {
        autohide = true;
        # Don't show recently opened apps in a separate Dock section
        show-recents = false;
        # Don't rearrange Spaces based on most recent use
        mru-spaces = false;
        tilesize = 48;
      };

      finder = {
        AppleShowAllFiles = true;
        # Don't warn when changing file extensions
        FXEnableExtensionChangeWarning = false;
        ShowPathbar = true;
        ShowStatusBar = true;
        # Sort folders before files in Finder windows
        _FXSortFoldersFirst = true;
        # Search the current folder by default (not "This Mac")
        FXDefaultSearchScope = "SCcf";
        # Default to list view in Finder windows
        FXPreferredViewStyle = "Nlsv";
      };

      NSGlobalDomain = {
        "com.apple.swipescrolldirection" = false;
        # Show all file extensions in Finder
        AppleShowAllExtensions = true;
        # Disable automatic text corrections
        NSAutomaticCapitalizationEnabled = false;
        # Disable smart dashes (e.g., -- → —)
        NSAutomaticDashSubstitutionEnabled = false;
        # Disable smart quotes (e.g., " → ")
        NSAutomaticQuoteSubstitutionEnabled = false;
        NSAutomaticSpellingCorrectionEnabled = false;
        # Faster key repeat: lower values = faster (default: 25/6)
        InitialKeyRepeat = 15;
        KeyRepeat = 2;
        # Save to local disk by default, not iCloud
        NSDocumentSaveNewDocumentsToCloud = false;
      };

      # Enable tap-to-click, right-click, and three-finger drag
      trackpad = {
        Clicking = true;
        TrackpadRightClick = true;
        TrackpadThreeFingerDrag = true;
      };

      screencapture = {
        location = "~/Screenshots";
        # Save screenshots as PNG for lossless quality
        type = "png";
        # Remove drop shadow from window screenshots
        disable-shadow = true;
      };

      # Require password immediately after screensaver activates
      screensaver = {
        askForPassword = true;
        askForPasswordDelay = 0;
      };

      # Disable guest account login
      loginwindow.GuestEnabled = false;

      # Show 24-hour time in the menu bar clock
      menuExtraClock.Show24Hour = true;

      controlcenter = {
        Sound = true;
        BatteryShowPercentage = true;
        Bluetooth = true;
      };

      CustomUserPreferences = {
        # Don't create .DS_Store files on network volumes
        "com.apple.desktopservices".DSDontWriteNetworkStores = true;
        NSGlobalDomain = {
          AppleAccentColor = 5;
          AppleHighlightColor = "0.968627 0.831373 1.000000 Purple";
        };
      };
    };

    configurationRevision = self.rev or self.dirtyRev or null;
    primaryUser = hostConfig.username;
    stateVersion = 6;
  };

  # Application firewall: filter inbound connections, allow signed apps
  networking.applicationFirewall = {
    enable = true;
    enableStealthMode = true;
    allowSigned = true;
    allowSignedApp = true;
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
