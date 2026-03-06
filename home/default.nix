{
  pkgs,
  lib,
  config,
  ...
}:

{
  imports = [
    ./options.nix
    ./stylix.nix
    ./fish.nix
    ./git.nix
    ./vim.nix
    ./tools.nix
    ./kubernetes.nix
    ./vscode.nix
    ./claude.nix
    ./secrets.nix
  ];

  programs = {
    ssh = {
      enable = true;
      enableDefaultConfig = false;
      includes = config.dotfiles.sshIncludes;
      extraConfig = "SendEnv COLORTERM";
      matchBlocks."*".addKeysToAgent = "yes";
    };

    ghostty = {
      enable = true;
      package = null; # installed via Homebrew cask on macOS
      enableFishIntegration = true;
      systemd.enable = false;
      settings = {
        window-height = 40;
        window-width = 80;

        window-padding-x = 8;
        window-padding-y = "8,0";

        font-style = "SemiBold";
        font-feature = [
          "ss01"
          "ss03"
          "ss04"
          "ss06"
        ];
        font-size = 14;

        keybind = [
          "global:cmd+grave_accent=toggle_quick_terminal"
        ];
        quick-terminal-screen = "mouse";
        quick-terminal-position = "right";
      };
    };

    fastfetch = {
      enable = true;
      settings = {
        modules = [
          "title"
          "separator"
          "os"
          "host"
          "kernel"
          "uptime"
          "packages"
          "shell"
          "display"
          "de"
          "wm"
          "terminal"
          "terminalfont"
          "cpu"
          "gpu"
          "memory"
          "break"
          "colors"
        ];
      };
    };

    go = {
      enable = true;
      telemetry.mode = "off";
    };

    npm = {
      enable = true;
      package = null; # nodejs is already in home.packages
    };

    uv = {
      enable = true;
      settings = {
        python-downloads = "manual";
      };
    };

    zed-editor = {
      enable = true;
      package = null; # installed via Homebrew cask on macOS

      userSettings = {
        agent = {
          default_model = {
            provider = "copilot_chat";
            model = "claude-opus-4.6";
          };
          favorite_models = [ ];
          model_parameters = [ ];
        };
        edit_predictions = {
          mode = "subtle";
        };
        ui_font_size = 15.0;
        ui_font_weight = 500.0;
        ui_font_family = config.stylix.fonts.monospace.name;
        ui_font_features = {
          ss01 = true;
          ss03 = true;
          ss04 = true;
          ss06 = true;
        };
        buffer_font_size = 14.0;
        buffer_font_weight = 500.0;
        buffer_font_family = config.stylix.fonts.monospace.name;
        buffer_font_features = {
          ss01 = true;
          ss03 = true;
          ss04 = true;
          ss06 = true;
        };
        features = {
          edit_prediction_provider = "copilot";
        };
        terminal = {
          font_family = config.stylix.fonts.monospace.name;
          font_features = {
            ss01 = true;
            ss03 = true;
            ss04 = true;
            ss06 = true;
          };
        };
        base_keymap = "VSCode";
        vim_mode = false;
        icon_theme = "Material Icon Theme";
        theme = "One Dark Pro";
        wrap_guides = [
          88
          120
        ];
        ssh_connections = [
          {
            host = "nixos-orbstack.orb.local";
            username = "jacobcolvin";
          }
        ];
      };

      userKeymaps = [
        {
          context = "Workspace";
          bindings = {
            "shift shift" = "file_finder::Toggle";
          };
        }
      ];
    };
  };

  xdg.configFile = {
    "viddy.toml".source = ../configs/viddy.toml;
    "dlv/config.yml".source = ../configs/dlv/config.yml;
    "gh-copilot/config.yml".source = ../configs/gh-copilot/config.yml;
    "kat/config.yaml".source = ../configs/kat/config.yaml;
    "ccstatusline/settings.json".source = ../configs/ccstatusline/settings.json;
  }
  // config.dotfiles.extraXdgConfigFiles;

  home = {
    stateVersion = "25.05";
    preferXdgDirectories = true;

    sessionPath = [
      "$HOME/go/bin"
      "$HOME/.npm/bin"
      "$HOME/.krew/bin"
      "$HOME/.local/bin"
    ];

    sessionVariables = {
      EDITOR = "vim";
      XDG_CONFIG_HOME = "$HOME/.config";
      FLAKE = "${config.home.homeDirectory}/repos/dotfiles";
      devbox_no_prompt = "true";
    };

    activation = {
      installPython = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        run ${pkgs.uv}/bin/uv python install --default
      '';
    };

    packages =
      with pkgs;
      [
        # Core utilities (GNU replacements for macOS BSD defaults)
        coreutils-full
        diffutils
        findutils
        gawk
        gnugrep
        gnupatch
        gnused
        gnutar
        gnumake
        gzip
        less
        rsync
        wget
        util-linux
        tree
        dos2unix

        # Media & graphics
        imagemagick
        ffmpeg
        graphviz

        # System tools
        qemu
        nmap
        ddrescue
        arping
        onefetch

        # Languages & runtimes
        nodejs
        dotnet-sdk

        # Dev tools
        nixd
        nil
        claude-code
        chief

        # Python build dependencies
        openssl
        readline
        sqlite
        xz
        zlib
        tcl

      ]
      ++ config.dotfiles.extraHomePackages;

    file."Taskfile.yaml".source = ../configs/task/Taskfile.yaml;

    file.".zed_server" = lib.mkIf pkgs.stdenv.isLinux {
      source = "${pkgs.zed-editor.remote_server}/bin";
      recursive = true;
    };
  };

}
