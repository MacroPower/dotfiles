{ lib, ... }:

let
  inherit (lib) mkOption types;
in
{
  options.dotfiles = {
    username = mkOption {
      type = types.nonEmptyStr;
      description = "System username.";
    };

    homeDirectory = mkOption {
      type = types.nonEmptyStr;
      description = "Absolute path to the user's home directory.";
    };

    hostname = mkOption {
      type = types.nonEmptyStr;
      description = "Hostname for this configuration (used for nixd LSP option discovery).";
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
        description = "Extra fish shell init commands sourced on every shell startup (before interactiveShellInit).";
      };
      extraInteractiveInit = mkOption {
        type = types.lines;
        default = "";
        description = "Extra fish commands sourced only in interactive shells.";
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
      description = "Additional entries merged into xdg.configFile. Each key is a relative path under XDG_CONFIG_HOME.";
    };

    taskSubdirs = mkOption {
      type = types.listOf types.nonEmptyStr;
      default = [ ];
      description = "Task subdirectory names to include in the generated global Taskfile.";
    };

    sshIncludes = mkOption {
      type = types.listOf types.nonEmptyStr;
      default = [ ];
      description = "SSH config Include directives.";
    };

    caCertificateFiles = mkOption {
      type = types.listOf types.path;
      default = [ ];
      description = "PEM certificate files to add to the CA bundle.";
    };

    caBundlePath = mkOption {
      type = types.nullOr types.str;
      default = null;
      internal = true;
      description = "Path to the custom CA bundle (set automatically by ca-certificates module).";
    };

    fonts.features = mkOption {
      type = types.listOf types.nonEmptyStr;
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
