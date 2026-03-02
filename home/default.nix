{
  pkgs,
  lib,
  hostConfig,
  ...
}:

{
  imports = [
    ./fish.nix
    ./git.nix
    ./vim.nix
    ./tools.nix
    ./kubernetes.nix
    ./vscode.nix
    ./claude.nix
  ];

  programs.ssh = {
    enable = true;
    enableDefaultConfig = false;
    includes = hostConfig.sshIncludes or [ ];
    extraConfig = "SendEnv COLORTERM";
    matchBlocks."*".addKeysToAgent = "yes";
  };

  programs.ghostty = {
    enable = true;
    package = null; # installed via Homebrew cask on macOS
    enableFishIntegration = true;
    systemd.enable = false;
    settings = {
      window-height = 40;
      window-width = 80;

      foreground = "#abb2bf";
      background = "#23272e";

      cursor-color = "#d0d0d0";
      cursor-text = "#151515";
      selection-background = "#979eab";
      selection-foreground = "#282c34";

      window-titlebar-background = "#282c34";
      window-titlebar-foreground = "#979eab";
      split-divider-color = "#393e48";

      palette = [
        "0=#282c34"
        "1=#e06c75"
        "2=#98c379"
        "3=#e5c07b"
        "4=#61afef"
        "5=#be5046"
        "6=#56b6c2"
        "7=#979eab"
        "8=#393e48"
        "9=#d19a66"
        "10=#56b6c2"
        "11=#e5c07b"
        "12=#61afef"
        "13=#be5046"
        "14=#56b6c2"
        "15=#abb2bf"
      ];

      window-padding-x = 8;
      window-padding-y = "8,0";

      font-family = "FiraCode Nerd Font Mono SemBd";
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

  xdg.configFile = {
    "zed/settings.json".source = ../configs/zed/settings.json;
    "zed/keymap.json".source = ../configs/zed/keymap.json;
    "viddy.toml".source = ../configs/viddy.toml;
    "dlv/config.yml".source = ../configs/dlv/config.yml;
    "neofetch/config.conf".source = ../configs/neofetch/config.conf;
    "gh-copilot/config.yml".source = ../configs/gh-copilot/config.yml;
    "kat/config.yaml".source = ../configs/kat/config.yaml;
    "ccstatusline/settings.json".source = ../configs/ccstatusline/settings.json;
  }
  // hostConfig.extraXdgConfigFiles;

  home = {
    stateVersion = "25.05";

    sessionVariables = {
      EDITOR = "vim";
      XDG_CONFIG_HOME = "$HOME/.config";
      devbox_no_prompt = "true";
    };

    activation = {
      installPython = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        if ! ${pkgs.uv}/bin/uv python find --no-project > /dev/null 2>&1; then
          run ${pkgs.uv}/bin/uv python install --default
        fi
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
        neofetch
        onefetch

        # Languages & runtimes
        go
        uv
        nodejs

        # Dev tools
        nixd
        nil
        claude-code
        chief

        # Fonts
        nerd-fonts.fira-code

        # Python build dependencies
        openssl
        readline
        sqlite
        xz
        zlib
        tcl

      ]
      ++ (map (name: pkgs.${name}) hostConfig.extraHomePackages);

    file."Taskfile.yaml".source = ../configs/task/Taskfile.yaml;

    file.".zed_server" = lib.mkIf pkgs.stdenv.isLinux {
      source = "${pkgs.zed-editor.remote_server}/bin";
      recursive = true;
    };
  };

}
