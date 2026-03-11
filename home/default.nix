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
    ./gpg.nix
    ./vim.nix
    ./tools.nix
    ./kubernetes.nix
    ./vscode.nix
    ./claude.nix
    ./secrets.nix
    ./ghostty.nix
    ./displayplacer.nix
    ./zed.nix
    ./firefox.nix
    ./development.nix
  ];

  programs = {
    # Disable expensive man-cache rebuild triggered by fish enabling generateCaches
    man.generateCaches = false;

    ssh = {
      enable = true;
      enableDefaultConfig = false;
      includes = config.dotfiles.sshIncludes;
      extraConfig = "SendEnv COLORTERM";
      matchBlocks."*".addKeysToAgent = "yes";
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
  };

  dconf.enable = false;

  xdg.enable = true;

  xdg.configFile = {
    "viddy.toml".source = ../configs/viddy.toml;
    "dlv/config.yml".source = ../configs/dlv/config.yml;
    "gh-copilot/config.yml".source = ../configs/gh-copilot/config.yml;
    "kat/config.yaml".source = ../configs/kat/config.yaml;
  }
  // config.dotfiles.extraXdgConfigFiles;

  home = {
    stateVersion = "26.05";
    preferXdgDirectories = true;

    sessionPath = [
      "$HOME/.local/bin"
    ];

    sessionVariables = {
      EDITOR = "vim";
      devbox_no_prompt = "true";
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

        # NUR packages (nur.jacobcolvin.com)
        kat
        kclipper
        kcl-lsp

        # System tools
        qemu
        nmap
        ddrescue
        arping
        onefetch
        otel-tui
        azure-cli

        # Networking & debugging
        curl
        file
        fping
        iperf3
        nmap
        openssl
        socat
        tcpdump
        termshark
        websocat
        swaks
        grpcurl
        dhcping
        net-snmp
        gobgpd
        oha
        speedtest-go
        bandwhich

      ]
      ++ lib.optionals pkgs.stdenv.isLinux [
        conntrack-tools
        dnsmasq
        envoy-bin
        ethtool
        iproute2
        iptables
        ipset
        iputils
        ipvsadm
        procps
        strace
      ]
      ++ config.dotfiles.extraHomePackages;

    file."Taskfile.yaml".source = ../configs/task/Taskfile.yaml;
  };

}
