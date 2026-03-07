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

    extraK8sPackages = mkOption {
      type = types.listOf types.package;
      default = [ ];
      description = "Additional Kubernetes-related packages.";
    };

    extraKrewPlugins = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = "Additional kubectl krew plugins.";
    };

    extraXdgConfigFiles = mkOption {
      type = types.attrsOf types.anything;
      default = { };
      description = "Additional XDG config files merged into xdg.configFile.";
    };

    extraVscodeExtensions = mkOption {
      type = types.uniq (types.functionTo (types.listOf types.package));
      default = _marketplace: [ ];
      description = "Function taking marketplace to return extra VS Code extensions.";
    };

    extraVscodeKubernetesSettings = mkOption {
      type = types.attrsOf types.str;
      default = { };
      description = "Additional VS Code Kubernetes extension settings.";
    };

    sshIncludes = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = "SSH config Include directives.";
    };

    claude = {
      dangerouslySkipPermissions = mkOption {
        type = types.bool;
        default = false;
        description = "Whether to skip Claude Code permission prompts.";
      };
      extraSettings = mkOption {
        type = types.attrsOf types.anything;
        default = { };
        description = "Additional settings merged into Claude Code settings.json.";
      };
    };
  };
}
