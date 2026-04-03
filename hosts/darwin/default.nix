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
      "macos-fuse-t/cask"
    ]
    ++ config.dotfiles.system.homebrew.taps;

    inherit (config.dotfiles.system.homebrew) brews;

    casks = [
      "fork"
      "fuse-t"
      "linearmouse"
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
        # Enable bounce animation when launching apps
        launchanim = true;
        # Dock position on screen
        orientation = "bottom";
        # Show all apps (not just active ones)
        static-only = false;
        # Hot corners (1 = disabled)
        wvous-tl-corner = 1;
        wvous-tr-corner = 1;
        wvous-bl-corner = 1;
        wvous-br-corner = 1;
        # Faster minimize animation than the default "genie"
        mineffect = "scale";
        # Dim icons of hidden applications
        showhidden = true;
        # Show dot indicators under running apps
        show-process-indicators = true;
        # Group windows by application in Mission Control
        expose-group-apps = true;
        # Instant Mission Control animations (default: ~0.7s)
        expose-animation-duration = 0.001;
        # Minimize windows into their application icon instead of the Dock
        minimize-to-application = true;

        # Pinned Dock apps, sorted alphabetically by name.
        persistent-apps =
          let
            resolveHome = p: builtins.replaceStrings [ "~" ] [ "/Users/${config.dotfiles.system.username}" ] p;
            unsorted = [
              "~/Applications/Home Manager Apps/Ghostty.app"
              "~/Applications/Home Manager Apps/Firefox.app"
              "~/Applications/Home Manager Apps/Zed.app"
              "/Applications/Fork.app"
              "/System/Applications/Messages.app"
              "/System/Applications/Notes.app"
              "/System/Applications/Utilities/Activity Monitor.app"
              "/System/Applications/System Settings.app"
            ]
            ++ config.dotfiles.system.dockExtraApps;
          in
          map resolveHome (lib.sort (a: b: lib.toLower (baseNameOf a) < lib.toLower (baseNameOf b)) unsorted);

        persistent-others =
          let
            resolveHome = p: builtins.replaceStrings [ "~" ] [ "/Users/${config.dotfiles.system.username}" ] p;
          in
          map
            (
              a:
              a
              // {
                folder = a.folder // {
                  path = resolveHome a.folder.path;
                };
              }
            )
            [
              {
                folder = {
                  path = "~/Downloads";
                  arrangement = "date-added";
                  showas = "fan";
                };
              }
              {
                folder = {
                  path = "~/Documents/screenshots";
                  arrangement = "date-added";
                  showas = "fan";
                };
              }
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

      trackpad = {
        # Two-finger tap/click for right-click
        TrackpadRightClick = true;
        # Three-finger drag (accessibility feature, avoids click-and-hold)
        TrackpadThreeFingerDrag = false;
        # Launchpad gesture with four-finger pinch
        TrackpadFourFingerPinchGesture = 2;
      };

      NSGlobalDomain = {
        # Enable "natural" (inverted) scroll direction
        "com.apple.swipescrolldirection" = true;
        # Show all file extensions in Finder
        AppleShowAllExtensions = true;
        # Dark mode
        AppleInterfaceStyle = "Dark";
        # Don't auto-switch between light and dark mode
        AppleInterfaceStyleSwitchesAutomatically = false;
        # Medium font smoothing for non-Apple external displays
        AppleFontSmoothing = 2;
        # Freedom units
        AppleMeasurementUnits = "Inches";
        AppleMetricUnits = 0;
        AppleTemperatureUnit = "Fahrenheit";
        # Medium sidebar icon size
        NSTableViewDefaultSizeMode = 2;
        # Spring-loading: hover a dragged file over a folder to open it
        "com.apple.springing.enabled" = true;
        "com.apple.springing.delay" = 0.1;
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
        location = "/Users/${config.dotfiles.system.username}/Documents/screenshots";
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

      # Globe/Fn key opens emoji picker (note: dictation bound to cmd)
      hitoolbox.AppleFnUsageType = "Show Emoji & Symbols";

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
        # Keep desktop icons visible (Stage Manager)
        HideDesktop = false;
        StandardHideDesktopIcons = false;
        # Keep widgets visible (Stage Manager)
        StageManagerHideWidgets = false;
        StandardHideWidgets = false;
      };

      # Each display gets its own independent set of Spaces (requires logout)
      spaces.spans-displays = false;

      # Don't auto-install macOS updates (prefer manual control for nix-darwin compat)
      SoftwareUpdate.AutomaticallyInstallMacOSUpdates = false;

      # Reduce visual effects for snappier UI and less distraction
      universalaccess = {
        # Skip animation when switching Spaces, opening Mission Control, etc.
        reduceMotion = true;
        # Use solid backgrounds instead of translucent sidebars and menus
        reduceTransparency = true;
      };

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
          # Don't minimize on title bar double-click
          AppleMiniaturizeOnDoubleClick = false;
          # Web Inspector available in right-click context menus
          WebKitDeveloperExtras = true;
          # Disable web view auto-correct
          WebAutomaticSpellingCorrectionEnabled = false;
          # Disable non-essential UI animations
          QLPanelAnimationDuration = 0;
          NSToolbarFullScreenAnimationDuration = 0;
          NSBrowserColumnAnimationSpeedMultiplier = 0;
          NSDocumentRevisionsWindowTransformAnimation = 0;
        };
        # Disable personalized Apple ads and ad tracking
        "com.apple.AdLib" = {
          allowApplePersonalizedAdvertising = false;
          allowIdentifierForAdvertising = false;
        };
        # Show AirPlay in menu bar even when not in use
        "com.apple.airplay".showInMenuBarIfPresent = true;
        # Fine-grained menu bar item visibility
        "com.apple.controlcenter" = {
          "NSStatusItem Visible AirDrop" = true;
          "NSStatusItem Visible Battery" = true;
          "NSStatusItem Visible Bluetooth" = true;
          "NSStatusItem Visible Clock" = true;
          "NSStatusItem Visible FocusModes" = true;
          "NSStatusItem Visible Sound" = true;
          "NSStatusItem Visible WiFi" = true;
        };
        # Disable proofreading popup in Dictionary
        "com.apple.Dictionary".ProofreadingEnabled = false;
        # Eliminate Launchpad/springboard animations
        "com.apple.dock" = {
          springboard-hide-duration = 0;
          springboard-page-duration = 0;
          springboard-show-duration = 0;
        };
        # Show location icon in menu bar
        "com.apple.locationmenu".StatusBarIconEnabled = true;
        # Notification banners visible for 5 seconds
        "com.apple.notificationcenterui".bannerTime = 5;
        # Prevent sleep at the OS level when disableSleep is set
        "com.apple.PowerManagement".SleepDisabled =
          if config.dotfiles.system.power.disableSleep then 1 else 0;
        # Show all preference panes in System Settings
        "com.apple.systempreferences".ShowAllMode = true;
        # TextEdit opens in plain text mode by default
        "com.apple.TextEdit".RichText = 0;
        # Hide Recent Tags from Finder sidebar
        "com.apple.finder".ShowRecentTags = false;
        "com.apple.symbolichotkeys" =
          let
            # AppleSymbolicHotKeys IDs (from macOS internals)
            hotkeys = {
              selectPrevInputSource = 60;
              selectNextInputSource = 61;
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
              # Disable input source switching (frees Ctrl+Space for tmux prefix)
              "${toString hotkeys.selectPrevInputSource}" = {
                enabled = false;
              };
              "${toString hotkeys.selectNextInputSource}" = {
                enabled = false;
              };
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
        # Prevent Photos from auto-launching when a camera, phone, or SD card is connected
        "com.apple.ImageCapture".disableHotPlug = true;
        # Suppress the "use this disk for backups?" prompt when new drives are mounted
        # and exclude system files from backups for faster/smaller snapshots
        "com.apple.TimeMachine" = {
          DoNotOfferNewDisksForBackup = true;
          SkipSystemFiles = true;
        };
        # Disable App Store auto updates
        "com.apple.commerce".AutoUpdate = false;
        # Disable Sparkle auto-updates for Homebrew casks (managed by nix-darwin)
        "dev.kdrag0n.MacVirt".SUAutomaticallyUpdate = false;
        "com.lujjjh.LinearMouse".SUAutomaticallyUpdate = false;
        "com.DanPristupov.Fork".SUAutomaticallyUpdate = false;

        # Force the maximum AAC bitpool (80) for Bluetooth audio negotiation,
        # preventing the codec from dropping to lower quality under contention
        "com.apple.BluetoothAudioAgent" = {
          "Apple Bitpool Max (editable)" = 80;
          "Apple Bitpool Min (editable)" = 80;
          "Apple Initial Bitpool (editable)" = 80;
          "Apple Initial Bitpool Min (editable)" = 80;
          "Negotiated Bitpool" = 80;
          "Negotiated Bitpool Max" = 80;
          "Negotiated Bitpool Min" = 80;
        };

        # Lower-level Bluetooth daemon settings: pin AAC at 320 kbps,
        # raise the packet ceiling, and enable both AAC and AptX codecs
        bluetoothaudiod = {
          "AAC Bitrate" = 320;
          "AAC max packet size" = 644;
          "Apple Bitpool Max" = 80;
          "Apple Bitpool Min" = 80;
          "Apple Initial Bitpool Min" = 80;
          "Apple Initial Bitpool" = 80;
          "Enable AAC codec" = true;
          "Enable AptX codec" = true;
          "Negotiated Bitpool Max" = 80;
          "Negotiated Bitpool Min" = 80;
          "Negotiated Bitpool" = 80;
        };

        # Activity Monitor opens its main window on launch, shows
        # real-time CPU graph in the Dock icon, and sorts by CPU descending
        "com.apple.ActivityMonitor" = {
          OpenMainWindow = true;
          # 5 = CPU usage graph in Dock icon (0=app, 2=history, 3=network, 6=all)
          IconType = 5;
          SortColumn = "CPUUsage";
          # 0 = descending (highest CPU first)
          SortDirection = 0;
        };

        # Disk Utility: show all devices, partitions, debug menu, and advanced image options
        "com.apple.DiskUtility" = {
          DUDebugMenuEnabled = true;
          DUShowEveryPartition = true;
          SidebarShowAllDevices = true;
          advanced-image-options = true;
        };

        # Enable AirDrop discovery over Ethernet and other non-default interfaces
        "com.apple.NetworkBrowser".BrowseAllInterfaces = true;

        # Skip checksum verification when mounting .dmg disk images (faster mounts)
        "com.apple.frameworks.diskimages" = {
          skip-verify = true;
          skip-verify-locked = true;
          skip-verify-remote = true;
        };

        # Don't reopen previous windows/apps after a restart or re-login
        "com.apple.loginwindow".TALLogoutSavesState = false;

        # Make Help Viewer windows non-floating so they behave like normal windows
        "com.apple.helpviewer".DevMode = true;

        # Spotlight: disable web suggestions and all other noisy/expensive categories
        "com.apple.spotlight".orderedItems =
          let
            spotlightCategories = {
              APPLICATIONS = true; # .app bundles
              BOOKMARKS = false; # Safari and browser bookmarks
              CONTACT = true; # Contacts / address book entries
              DIRECTORIES = false; # Folder names
              DOCUMENTS = false; # Pages, Word, plain text, etc.
              EVENT_TODO = true; # Calendar events and Reminders
              FONTS = false; # Installed font families
              IMAGES = false; # Photos, screenshots, graphics
              MENU_CONVERSION = true; # Unit and currency conversions
              MENU_DEFINITION = true; # Dictionary definitions
              MENU_EXPRESSION = true; # Calculator / math expressions
              MENU_OTHER = false; # Miscellaneous results
              MENU_SPOTLIGHT_SUGGESTIONS = false; # Siri / Apple suggestions
              MENU_WEBSEARCH = false; # Web search suggestions
              MESSAGES = false; # iMessage / SMS history
              MOVIES = false; # Video files
              MUSIC = false; # Audio files and Apple Music
              PDF = false; # PDF documents
              PRESENTATIONS = false; # Keynote, PowerPoint slides
              SOURCE = false; # Source code files
              SPREADSHEETS = false; # Numbers, Excel sheets
              SYSTEM_PREFS = true; # System Settings panes
            };
          in
          map (name: {
            inherit name;
            enabled = spotlightCategories.${name};
          }) (builtins.attrNames spotlightCategories);

        # Disable dictation auto-punctuation
        "com.apple.assistant.support"."Dictation Auto Punctuation Enabled" = false;
      };

      # System-wide preferences (written to /Library/Preferences/, requires root)
      CustomSystemPreferences = {
        # Prevent Gatekeeper from silently re-enabling itself every 30 days
        "com.apple.security".GKAutoRearm = false;
        # Check for macOS/security updates daily instead of weekly,
        # download in background, and auto-install critical patches
        "com.apple.SoftwareUpdate" = {
          AutomaticCheckEnabled = true;
          # 1 = daily (default is 7)
          ScheduleFrequency = 1;
          # Download updates in the background
          AutomaticDownload = 1;
          # Auto-install XProtect, MRT, and system data files
          CriticalUpdateInstall = 1;
        };
      };
    };

    # Silence the boot chime on startup
    startup.chime = false;

    # Transitional option: the user that owns system-level nix-darwin operations
    primaryUser = config.dotfiles.system.username;
    # Apply settings changes without requiring logout
    activationScripts.postActivation.text = ''
      sudo -u ${config.dotfiles.system.username} /System/Library/PrivateFrameworks/SystemAdministration.framework/Resources/activateSettings -u
    '';

    # Ensure the custom screenshot directory exists
    activationScripts.screenshotDir.text = ''
      sudo -u ${config.dotfiles.system.username} mkdir -p /Users/${config.dotfiles.system.username}/Documents/screenshots
    '';

    # Declaratively manage macOS login items via System Events.
    # Adds items from dotfiles.system.loginItems and removes any
    # previously-managed items that are no longer in the list.
    activationScripts.loginItems.text =
      let
        user = config.dotfiles.system.username;
        resolveHome = p: builtins.replaceStrings [ "~" ] [ "/Users/${user}" ] p;
        items = map resolveHome config.dotfiles.system.loginItems;
      in
      lib.optionalString (items != [ ]) ''
        echo "managing login items..." >&2
        MANAGED_ITEMS=(${lib.concatMapStringsSep " " (p: ''"${p}"'') items})

        # Get current login item names
        current_items=$(sudo -u ${user} osascript -e \
          'tell application "System Events" to get the name of every login item' 2>/dev/null || echo "")

        for app in "''${MANAGED_ITEMS[@]}"; do
          app_name=$(/usr/bin/basename "$app" .app)
          if [ -d "$app" ]; then
            if ! echo "$current_items" | /usr/bin/grep -q "$app_name"; then
              echo "  adding login item: $app_name" >&2
              sudo -u ${user} osascript -e \
                "tell application \"System Events\" to make login item at end with properties {path:\"$app\", hidden:false}" 2>/dev/null || true
            fi
          fi
        done
      '';

    # nix-darwin state version, do not change after initial setup
    stateVersion = 6;
  };

  # macOS application-level firewall with stealth mode
  networking.applicationFirewall = {
    # Enable the built-in ALF (Application Layer Firewall)
    enable = true;
    # Don't respond to ICMP probes or closed-port connection attempts,
    # making the machine invisible to casual port scans
    enableStealthMode = true;
    # Allow connections for apps signed by Apple (system services, App Store apps)
    allowSigned = true;
    # Allow connections for apps signed with a valid developer certificate
    allowSignedApp = true;
  };

  power = {
    sleep = {
      inherit (config.dotfiles.system.power.sleep) computer display allowSleepByPowerButton;
    };
    inherit (config.dotfiles.system.power) restartAfterFreeze restartAfterPowerFailure;
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

  security.pki.certificateFiles = config.dotfiles.system.caCertificateFiles;

  security.pam.services.sudo_local = {
    touchIdAuth = true;
    watchIdAuth = true;
    reattach = true;
  };

  environment.shells = [ pkgs.fish ];

  users.knownUsers = [ config.dotfiles.system.username ];
  users.users.${config.dotfiles.system.username} = {
    uid = 501;
    name = config.dotfiles.system.username;
    home = "/Users/${config.dotfiles.system.username}";
    shell = pkgs.fish;
  };
}
