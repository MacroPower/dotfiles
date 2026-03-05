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

  nix.optimise.automatic = true;

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
        # Dock icon size in points
        tilesize = 64;
        # Shorter delay before auto-hidden Dock appears
        autohide-delay = 0.15;
        # Faster Dock show/hide animation
        autohide-time-modifier = 0.15;
        # Faster minimize animation than the default "genie"
        mineffect = "scale";
        # Dim icons of hidden applications
        showhidden = true;
        # Show dot indicators under running apps
        show-process-indicators = true;
        # Group windows by application in Mission Control
        expose-group-apps = true;
        # Speed up Mission Control animations (default: ~0.7s)
        expose-animation-duration = 0.15;
        # Minimize windows into their application icon instead of the Dock
        minimize-to-application = true;
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
        # Show full POSIX path in Finder window title bar
        _FXShowPosixPathInTitle = true;
        # Sort folders before files on Desktop too
        _FXSortFoldersFirstOnDesktop = true;
        # Allow quitting Finder with Cmd+Q
        QuitMenuItem = true;
        # Keep Desktop clean — access drives via Finder sidebar
        ShowExternalHardDrivesOnDesktop = false;
        ShowHardDrivesOnDesktop = false;
        ShowMountedServersOnDesktop = false;
        ShowRemovableMediaOnDesktop = false;
        # New Finder windows open in the home directory (not Recents)
        NewWindowTarget = "Home";
      };

      NSGlobalDomain = {
        # Disable "natural" (inverted) scroll direction
        "com.apple.swipescrolldirection" = false;
        # Show all file extensions in Finder
        AppleShowAllExtensions = true;
        # Disable auto-capitalization
        NSAutomaticCapitalizationEnabled = false;
        # Disable smart dashes (e.g., -- -> —)
        NSAutomaticDashSubstitutionEnabled = false;
        # Disable smart quotes (e.g., normal -> slanted quotes)
        NSAutomaticQuoteSubstitutionEnabled = false;
        # Disable auto-correction
        NSAutomaticSpellingCorrectionEnabled = false;
        # Faster key repeat: lower values = faster (default: 25/6)
        InitialKeyRepeat = 15;
        KeyRepeat = 2;
        # Save to local disk by default, not iCloud
        NSDocumentSaveNewDocumentsToCloud = false;
        # Disable press-and-hold accent picker; enables true key repeat everywhere
        ApplePressAndHoldEnabled = false;
        # Disable double-space to period substitution
        NSAutomaticPeriodSubstitutionEnabled = false;
        # Show expanded Save dialog by default (full file browser)
        NSNavPanelExpandedStateForSaveMode = true;
        NSNavPanelExpandedStateForSaveMode2 = true;
        # Show expanded Print dialog by default
        PMPrintingExpandedStateForPrint = true;
        PMPrintingExpandedStateForPrint2 = true;
        # Full keyboard navigation: Tab moves through all dialog controls
        AppleKeyboardUIMode = 2;
        # Click scrollbar to jump to clicked position instead of paging
        AppleScrollerPagingBehavior = true;
        # Use F1–F12 as standard function keys without holding Fn
        "com.apple.keyboard.fnState" = true;
        # Disable window open/close animations for snappier feel
        NSAutomaticWindowAnimationsEnabled = false;
        # Instant window resize animation (default: 0.2s)
        NSWindowResizeTime = 0.001;
        # Disable inline predictive text suggestions (Sonoma+)
        NSAutomaticInlinePredictionEnabled = false;
        # Move windows by Ctrl+Cmd+click anywhere (like Linux WMs)
        NSWindowShouldDragOnGesture = true;
        # Switch to the Space containing the app's window on activate
        AppleSpacesSwitchOnActivate = true;
        # Prefer tabs when opening documents to reduce window clutter
        AppleWindowTabbingMode = "always";
      };

      # Save screenshots to ~/Documents/Screenshots as lossless PNG
      screencapture = {
        location = "~/Documents/Screenshots";
        type = "png";
      };

      # Require password immediately after screensaver activates
      screensaver = {
        askForPassword = true;
        askForPasswordDelay = 0;
      };

      # Disable quarantine prompts for downloaded applications
      LaunchServices.LSQuarantine = false;

      # Login window hardening
      loginwindow = {
        GuestEnabled = false;
        # Prevent console access via ">console" username at login
        DisableConsoleAccess = true;
      };

      menuExtraClock = {
        # Always show date in the menu bar
        ShowDate = 1;
        # Show day of week alongside the time
        ShowDayOfWeek = true;
        # Show seconds for precise timestamps
        ShowSeconds = true;
      };

      controlcenter = {
        Sound = true;
        BatteryShowPercentage = true;
        Bluetooth = true;
        # Hide Now Playing widget from menu bar
        NowPlaying = false;
      };

      # Window tiling and Stage Manager behavior
      WindowManager = {
        # Disable click-wallpaper-to-reveal-desktop (Sonoma default)
        EnableStandardClickToShowDesktop = false;
        # Native drag-to-edge window tiling (Sequoia+)
        EnableTilingByEdgeDrag = true;
        # Drag window to menu bar to fill screen
        EnableTopTilingByEdgeDrag = true;
        # Hold Option while dragging for tiling placement hints
        EnableTilingOptionAccelerator = true;
        # No gaps between tiled windows (maximize screen real estate)
        EnableTiledWindowMargins = false;
      };

      # Each display gets its own independent set of Spaces (requires logout)
      spaces.spans-displays = false;

      # Don't auto-install macOS updates (prefer manual control for nix-darwin compat)
      SoftwareUpdate.AutomaticallyInstallMacOSUpdates = false;

      CustomUserPreferences = {
        # Don't create .DS_Store files on network or USB volumes
        "com.apple.desktopservices" = {
          DSDontWriteNetworkStores = true;
          DSDontWriteUSBStores = true;
        };
        # System accent and text-selection highlight color: purple
        NSGlobalDomain = {
          AppleAccentColor = 5;
          AppleHighlightColor = "0.968627 0.831373 1.000000 Purple";
        };
        # Disable personalized Apple ads
        "com.apple.AdLib".allowApplePersonalizedAdvertising = false;
        # Suppress crash reporter dialog (logs still collected)
        "com.apple.CrashReporter".DialogType = "none";
        # TextEdit opens in plain text mode by default
        "com.apple.TextEdit".RichText = 0;
        # Hide Recent Tags from Finder sidebar
        "com.apple.finder".ShowRecentTags = false;
      };
    };

    # Record the flake's git revision so `darwin-version` shows what's deployed
    configurationRevision = self.rev or self.dirtyRev or null;
    # Transitional option: the user that owns system-level nix-darwin operations
    primaryUser = hostConfig.username;
    # nix-darwin state version, do not change after initial setup
    stateVersion = 6;
  };

  # Keep the Mac always-on (never sleep computer or display)
  power.sleep.computer = "never";
  power.sleep.display = "never";

  # SMB client configuration (/etc/nsmb.conf).
  # signing_required=no disables SMB packet signing, trading MITM protection
  # for ~20-30% throughput gain on local/trusted networks. Re-enable signing
  # (signing_required=yes) if connecting to untrusted or remote SMB shares.
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
