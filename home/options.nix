{ lib, ... }:

let
  inherit (lib) mkOption types;
in
{
  options.dotfiles = {
    username = mkOption {
      type = types.str;
      description = "System username.";
    };

    homeDirectory = mkOption {
      type = types.str;
      description = "Path to the user's home directory.";
    };

    git = {
      userName = mkOption {
        type = types.str;
        default = "";
        description = "Git user.name value.";
      };
      userEmail = mkOption {
        type = types.str;
        default = "";
        description = "Git user.email value.";
      };
    };

    shell = {
      extraShellInit = mkOption {
        type = types.lines;
        default = "";
        description = "Extra fish shell init commands.";
      };
      extraInteractiveInit = mkOption {
        type = types.lines;
        default = "";
        description = "Extra fish interactive shell init commands.";
      };
      extraTideConfig = mkOption {
        type = types.lines;
        default = "";
        description = "Extra Tide prompt configuration.";
      };
    };

    extraHomePackages = mkOption {
      type = types.listOf types.package;
      default = [ ];
      description = "Additional packages to install in home.packages.";
    };

    extraXdgConfigFiles = mkOption {
      type = types.attrsOf types.anything;
      default = { };
      description = "Additional XDG config files merged into xdg.configFile.";
    };

    sshIncludes = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = "SSH config Include directives.";
    };

    fonts.features = mkOption {
      type = types.listOf types.str;
      default = [
        "ss01"
        "ss03"
        "ss04"
        "ss06"
      ];
      description = "OpenType font features to enable across editors and terminals.";
    };
  };
}
