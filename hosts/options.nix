{ lib, ... }:

{
  options.dotfiles.system = {
    username = lib.mkOption {
      type = lib.types.str;
      description = "The primary user account name.";
    };

    homebrew = lib.mkOption {
      type = lib.types.attrs;
      default = { };
      description = "Host-specific Homebrew configuration (extraTaps, extraBrews, extraCasks, masApps).";
    };
  };
}
