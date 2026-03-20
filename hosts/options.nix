{ lib, ... }:

{
  options.dotfiles.system = {
    username = lib.mkOption {
      type = lib.types.str;
      description = "The primary user account name.";
    };

    loginItems = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "Absolute paths to .app bundles that should launch at login.";
    };

    extraApps = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = "Nix-installed GUI app names for conditional Dock/login items.";
    };

    power = {
      sleep = {
        computer = lib.mkOption {
          type = lib.types.either lib.types.ints.unsigned (lib.types.enum [ "never" ]);
          default = "never";
          description = "Minutes of inactivity before computer sleeps, or \"never\".";
        };
        display = lib.mkOption {
          type = lib.types.either lib.types.ints.unsigned (lib.types.enum [ "never" ]);
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
        description = "Automatically restart after a power failure. Set to null on devices that don't support it.";
      };
      disableSleep = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Globally disable sleep via com.apple.PowerManagement.SleepDisabled.";
      };
    };

    homebrew = {
      taps = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
      };
      brews = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
      };
      casks = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
      };
      masApps = lib.mkOption {
        type = lib.types.attrsOf lib.types.ints.positive;
        default = { };
      };
    };
  };
}
