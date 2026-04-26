{
  pkgs,
  lib,
  config,
  ...
}:

let
  taskDir = ../configs/task;
  taskSubdirs = [
    "ssh"
    "k8s"
    "helm"
    "pvc"
    "externalsecrets"
    "cert"
    "cilium"
  ]
  ++ config.dotfiles.taskSubdirs;
  globalTaskfile = pkgs.writeText "Taskfile.yaml" (
    lib.concatStrings (
      [
        ''
          version: "3"

          tasks:
            default:
              cmd: task -g -l

          includes:
        ''
      ]
      ++ map (name: "  ${name}:\n    taskfile: ./task/${name}/Taskfile.yaml\n") taskSubdirs
    )
  );
in

{
  imports = [
    ./options.nix
    ./ca-certificates.nix
    ./stylix.nix
    ./fish.nix
    ./files.nix
    ./git.nix
    ./gpg.nix
    ./neovim.nix
    ./tools.nix
    ./secrets.nix
  ];

  programs = {
    # Disable expensive man-cache rebuild triggered by fish enabling generateCaches
    man.generateCaches = false;

    ssh = {
      enable = true;
      enableDefaultConfig = false;
      includes = config.dotfiles.sshIncludes;
      extraConfig = ''
        SendEnv COLORTERM
        KexAlgorithms sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org
        Ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com
        MACs hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com
        HostKeyAlgorithms ssh-ed25519-cert-v01@openssh.com,ssh-ed25519,sk-ssh-ed25519-cert-v01@openssh.com,sk-ssh-ed25519@openssh.com
      '';
      matchBlocks."*" = {
        addKeysToAgent = "yes";
        serverAliveInterval = 60;
        serverAliveCountMax = 3;
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
  };

  dconf.enable = false;

  xdg.enable = true;

  xdg.configFile = {
    "viddy.toml".source = ../configs/viddy.toml;
    "dlv/config.yml".source = ../configs/dlv/config.yml;
    "gh-copilot/config.yml".source = ../configs/gh-copilot/config.yml;
    "kat/config.yaml".source = ../configs/kat/config.yaml;
    "terrarium/config.yaml".source = ../configs/terrarium/config.yaml;
  }
  // config.dotfiles.extraXdgConfigFiles;

  home = {
    stateVersion = "26.05";
    preferXdgDirectories = true;

    sessionPath = [
      "$HOME/.local/bin"
    ];

    sessionVariables = {
      COLORTERM = "truecolor";
      EDITOR = "nvim";
      VISUAL = "nvim";
      HOMEBREW_NO_AUTO_UPDATE = "1";
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
        openssl
        socat
        tcpdump
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
        ipset
        iputils
        ipvsadm
        nftables
        procps
        strace
        terrarium
      ]
      ++ config.dotfiles.extraHomePackages;

    file = {
      "Taskfile.yaml".source = globalTaskfile;
    }
    // builtins.listToAttrs (
      map (name: {
        name = "task/${name}/Taskfile.yaml";
        value.source = taskDir + "/${name}/Taskfile.yaml";
      }) taskSubdirs
    );
  };

}
