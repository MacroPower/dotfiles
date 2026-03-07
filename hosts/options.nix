{ lib, ... }:

{
  options.dotfiles.system = {
    username = lib.mkOption {
      type = lib.types.str;
      description = "The primary user account name.";
    };

    homebrew = {
      extraTaps = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
      };
      extraBrews = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
      };
      extraCasks = lib.mkOption {
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
