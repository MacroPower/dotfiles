{
  config,
  pkgs,
  lib,
  ...
}:

{
  imports = [
    ../shared.nix
    ../options.nix
  ];

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
      "ymtdzzz/tap"
      "macropower/tap"
      "robusta-dev/krr"
      "jacobcolvin/tap"
    ]
    ++ config.dotfiles.system.homebrew.taps;

    brews = [
      "jakehilborn/jakehilborn/displayplacer"
      "photo-cli"

      "ymtdzzz/tap/otel-tui"
      "robusta-dev/krr/krr"
    ]
    ++ config.dotfiles.system.homebrew.brews;

    casks = [
      "appcleaner"
      "caffeine"
      "drawio"
      "gpg-suite-no-mail"
      "keka"
      "linearmouse"
      "obsidian"
      "vlc"
      "fuse-t"
      "ghostty"
      "zed"
      "monodraw"
      "db-browser-for-sqlite"
      "fork"
      "wireshark"
      "kat"
    ]
    ++ config.dotfiles.system.homebrew.casks;

    inherit (config.dotfiles.system.homebrew) masApps;

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

        # Pinned Dock apps; optional cask-only apps are included only when
        # their Homebrew cask is present in this host's merged cask list.
        persistent-apps =
          let
            inherit (config.homebrew) casks;
            hasCask = name: builtins.elem name (map (c: if builtins.isString c then c else c.name) casks);
          in
          [
            { app = "/Applications/Ghostty.app"; }
          ]
          ++ lib.optional (hasCask "firefox") { app = "/Applications/Firefox.app"; }
          ++ lib.optional (hasCask "obsidian") { app = "/Applications/Obsidian.app"; }
          ++ lib.optional (hasCask "discord") { app = "/Applications/Discord.app"; }
          ++ [
            { app = "/Applications/Zed.app"; }
            { app = "/Applications/Fork.app"; }
            { app = "/System/Applications/Messages.app"; }
            { app = "/System/Applications/Notes.app"; }
            { app = "/System/Applications/Utilities/Activity Monitor.app"; }
            { app = "/System/Applications/System Settings.app"; }
          ];

        persistent-others = [
          { folder = "/Users/${config.dotfiles.system.username}/Downloads"; }
          { folder = "/Users/${config.dotfiles.system.username}/Documents/Screenshots"; }
        ];
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
        save-selections = true;
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
        "com.apple.symbolichotkeys" =
          let
            # AppleSymbolicHotKeys IDs (from macOS internals)
            hotkeys = {
              showApps = 160;
              dictation = 164;
            };
            # NSEvent modifier flags (from NSEvent.h / CGEvent.h)
            modifiers = {
              command = 1048576; # 0x100000 -- NSEventModifierFlagCommand
            };
            # Unicode character values for AppleSymbolicHotKeys parameters
            chars = {
              nonPrintable = 65535; # 0xFFFF -- sentinel for non-character keys
            };
            # Virtual keycodes (from Events.h / Carbon)
            keycodes = {
              enter = 36; # kVK_Return
            };
          in
          {
            AppleSymbolicHotKeys = {
              # Show Apps: Cmd+Enter
              "${toString hotkeys.showApps}" = {
                enabled = true;
                value = {
                  parameters = [
                    chars.nonPrintable
                    keycodes.enter
                    modifiers.command
                  ];
                  type = "standard";
                };
              };
              # Dictation: Press Either Command Key Twice
              "${toString hotkeys.dictation}" = {
                enabled = true;
                value = {
                  # macOS stores [flag, ~flag] for modifier-type shortcuts;
                  # ~flag as signed 64-bit = -(flag + 1)
                  parameters = [
                    modifiers.command
                    (-(modifiers.command + 1))
                  ];
                  type = "modifier";
                };
              };
            };
          };
        # Disable dictation auto-punctuation
        "com.apple.assistant.support"."Dictation Auto Punctuation Enabled" = false;
      };
    };

    # Transitional option: the user that owns system-level nix-darwin operations
    primaryUser = config.dotfiles.system.username;
    # Apply settings changes without requiring logout
    activationScripts.postActivation.text = ''
      sudo -u ${config.dotfiles.system.username} /System/Library/PrivateFrameworks/SystemAdministration.framework/Resources/activateSettings -u
    '';
    # nix-darwin state version, do not change after initial setup
    stateVersion = 6;
  };

  # Keep the Mac always-on and auto-recover from failures
  power = {
    sleep = {
      computer = "never";
      display = "never";
      # Disable sleep via power button so headless Mac mini stays running
      allowSleepByPowerButton = false;
    };
    # Auto-recover from freezes and power failures
    restartAfterFreeze = true;
    restartAfterPowerFailure = true;
  };

  # SMB client configuration (/etc/nsmb.conf).
  # Optimized for Apple Silicon on a trusted local network.
  # signing_required=no and validate_neg_off=yes trade MITM protection
  # for throughput — re-enable if connecting to untrusted SMB shares.
  environment.etc."nsmb.conf".text = lib.generators.toINI { } {
    default = {
      # Disable SMB packet signing for ~20-30% throughput gain on trusted networks.
      signing_required = "no";
      # Disable NTFS alternate data streams (buggy on Apple Silicon SMB3 stack).
      streams = "no";
      # Suppress change notifications to reduce resource usage.
      notify_off = "yes";
      # Use direct TCP (port 445) only; skip legacy NetBIOS name resolution.
      port445 = "no_netbio";
      # Bitmap: 2=SMB1, 4=SMB2, 6=SMB2+SMB3. Enforce modern protocols only.
      protocol_vers_map = 6;
      # Soft mounts: operations fail gracefully instead of hanging
      # indefinitely when the SMB server becomes unresponsive.
      soft = "yes";
      # Disable multichannel — inconsistently negotiated on Apple Silicon.
      mc_on = "no";
      # When multichannel is re-enabled, prefer wired over Wi-Fi interfaces.
      mc_prefer_wired = "yes";
      # Disable local directory enumeration caching so Finder always fetches
      # current file/folder listings from the server.
      dir_cache_max_cnt = 0;
      # Skip SMB negotiate validation. Reduces overhead on trusted networks
      # (same MITM trade-off as signing_required=no).
      validate_neg_off = "yes";
    };
  };

  # Disable TCP delayed ACK for SMB performance.
  # macOS defaults to delaying ACK packets, which bottlenecks SMB throughput.
  launchd.daemons.tcp-delayed-ack-disable = {
    serviceConfig = {
      ProgramArguments = [
        "/usr/sbin/sysctl"
        "-w"
        "net.inet.tcp.delayed_ack=0"
      ];
      RunAtLoad = true;
      StandardErrorPath = "/dev/null";
      StandardOutPath = "/dev/null";
    };
  };

  # Disable Spotlight indexing on network and external volumes.
  # Watches /Volumes for mount events; also runs at boot (RunAtLoad).
  launchd.daemons.spotlight-volume-blocker = {
    serviceConfig = {
      ProgramArguments = [
        "/bin/sh"
        "-c"
        ''for vol in /Volumes/*/; do [ -d "$vol" ] && /usr/bin/mdutil -i off "$vol" 2>/dev/null; done''
      ];
      WatchPaths = [ "/Volumes" ];
      RunAtLoad = true;
      StandardErrorPath = "/dev/null";
      StandardOutPath = "/dev/null";
    };
  };

  security.pam.services.sudo_local = {
    touchIdAuth = true;
    watchIdAuth = true;
    reattach = true;
  };

  users.users.${config.dotfiles.system.username} = {
    name = config.dotfiles.system.username;
    home = "/Users/${config.dotfiles.system.username}";
    shell = pkgs.fish;
  };
}
