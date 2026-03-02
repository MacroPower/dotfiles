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

  xdg.configFile = {
    "ghostty/config".source = ../configs/ghostty/config;
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
