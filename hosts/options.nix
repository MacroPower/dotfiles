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
