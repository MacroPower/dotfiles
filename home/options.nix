{ lib, config, ... }:

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

    extraInventoryPackages = mkOption {
      type = types.listOf types.package;
      default = [ ];
      description = ''
        Additional packages to surface in the README inventory only. Use for
        derivations referenced transitively by Nix store path (e.g. MCP stdio
        servers invoked from program config) that should still be documented
        as installed tools. These are not added to home.packages.
      '';
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

    findExcludes = mkOption {
      type = types.listOf types.str;
      default = [
        ".git"
        ".worktrees"
        ".claude/worktrees"
      ];
      description = ''
        Directories that the fish `find` function leaves out of each
        search. home/fish.nix reads the list on every host, so this
        file declares the option. On a host that imports
        home/claude.nix, hook-router leaves the same directories out of
        the `rg` and `bfs` commands that its search rewrite writes. An
        entry without a slash names a directory, and an entry with a
        slash names the end of a path.
      '';
    };

    caCertificateFiles = mkOption {
      type = types.listOf types.path;
      default = [ ];
      description = "PEM certificate files to add to the CA bundle.";
    };

    sops.enable = mkOption {
      type = types.bool;
      default = true;
      description = "Whether to enable sops-nix secret decryption. Disable for hosts that receive secrets via environment variables.";
    };

    gomi.homeFallback = mkOption {
      type = types.bool;
      default = true;
      description = ''
        Whether gomi falls back to the home trash when the source file
        is on a filesystem that has no usable .Trash-$uid directory.
        Disable on hosts where a cross-device fallback would be
        destructive -- e.g. inside containers whose home directory
        lives on an ephemeral overlay.
      '';
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

    reposDir = mkOption {
      type = types.nonEmptyStr;
      default = "${config.dotfiles.homeDirectory}/Documents/repos";
      description = ''
        Absolute path of the directory that holds the repositories of
        this host. A host sets it in its file under hosts/. The nh flake
        path is the dotfiles clone in this directory, and the direnv
        config of the terrarium VM trusts every project in it. On a host
        that imports home/claude.nix, the Claude Code sandbox and the
        git MCP server allow the directory, workmux mounts it into the
        Lima sandbox, and the wm-spawn skill dispatches agents to the
        repositories in it. The DOTFILES_REPOS_DIR variable of Claude
        Code names it for the scripts of skills, such as the
        aven-ultraclose script that finds the other repositories of a
        run there. A Lima guest sees the directory of the macOS
        host at its macOS path, so the guest sets that path. The macOS
        hosts share one terrarium image. So each macOS host that enables
        dotfiles.claude.lima must set the path that
        hosts/nixos/terrarium.nix sets, and lib/mkDarwin.nix fails the
        evaluation of a macOS host whose path differs.
      '';
    };

    obsidian.vaultsDir = mkOption {
      type = types.nonEmptyStr;
      default = "/Users/${config.dotfiles.username}/Library/Mobile Documents/iCloud~md~obsidian/Documents";
      description = "Absolute path to the Obsidian vaults directory. Anchored at /Users/ so a mounted view inside a Lima sandbox matches the Darwin host path.";
    };

    tmux.socketPath = mkOption {
      type = types.nullOr types.nonEmptyStr;
      default = null;
      description = "Full tmux socket file path passed via -S flag. Overrides the default /tmp/tmux-<UID>/default. On macOS use canonical /private/tmp paths.";
    };
  };
}
