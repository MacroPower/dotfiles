{ lib, ... }:

{
  options.dotfiles.system = {
    username = lib.mkOption {
      type = lib.types.nonEmptyStr;
      description = "The primary user account name.";
    };

    loginItems = lib.mkOption {
      type = lib.types.listOf lib.types.nonEmptyStr;
      default = [ ];
      description = "Paths to .app bundles that should launch at login.";
    };

    dockExtraApps = lib.mkOption {
      type = lib.types.listOf lib.types.nonEmptyStr;
      default = [ ];
      description = "Additional .app paths to pin to the Dock.";
    };

    power = {
      sleep = {
        computer = lib.mkOption {
          type = lib.types.either lib.types.ints.positive (lib.types.enum [ "never" ]);
          default = "never";
          description = "Minutes of inactivity before computer sleeps, or \"never\".";
        };
        display = lib.mkOption {
          type = lib.types.either lib.types.ints.positive (lib.types.enum [ "never" ]);
          default = "never";
          description = "Minutes of inactivity before display sleeps, or \"never\".";
        };
        allowSleepByPowerButton = lib.mkOption {
          type = lib.types.bool;
          default = false;
          description = "Whether pressing the power button puts the machine to sleep.";
        };
      };
      restartAfterFreeze = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Automatically restart after a system freeze.";
      };
      restartAfterPowerFailure = lib.mkOption {
        type = lib.types.nullOr lib.types.bool;
        default = true;
        description = "Automatically restart after a power failure. Null disables the setting entirely (for hardware that lacks the capability).";
      };
      disableSleep = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Globally disable sleep via com.apple.PowerManagement.SleepDisabled.";
      };
    };

    homebrew = {
      taps = lib.mkOption {
        type = lib.types.listOf lib.types.nonEmptyStr;
        default = [ ];
        description = "Additional Homebrew tap repositories.";
      };
      brews = lib.mkOption {
        type = lib.types.listOf lib.types.nonEmptyStr;
        default = [ ];
        description = "Homebrew formulae to install.";
      };
      casks = lib.mkOption {
        type = lib.types.listOf lib.types.nonEmptyStr;
        default = [ ];
        description = "Homebrew casks to install.";
      };
      masApps = lib.mkOption {
        type = lib.types.attrsOf lib.types.ints.positive;
        default = { };
        description = "Mac App Store apps as name-to-ID pairs.";
      };
    };
  };
}
