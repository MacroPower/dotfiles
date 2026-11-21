{
  inputs,
  self,
  paths,
  sharedOverlays,
  sharedStylixConfig,
  mkHomeManagerBlock,
}:
{
  username,
  hostname,
  homebrew ? { },
  loginItems ? [ ],
  dockExtraApps ? [ ],
  power ? { },
  caCertificateFiles ? [ ],
  homeModule,
  darwinModule ? { },
}:
inputs.nix-darwin.lib.darwinSystem {
  modules = [
    paths.hostMac
    {
      networking.hostName = hostname;
      dotfiles.system = {
        inherit
          username
          homebrew
          loginItems
          dockExtraApps
          caCertificateFiles
          ;
        inherit power;
      };
      system.configurationRevision = self.rev or self.dirtyRev or null;
    }
    inputs.nix-homebrew.darwinModules.nix-homebrew
    {
      nix-homebrew = {
        enable = true;
        enableRosetta = true;
        autoMigrate = true;
        user = username;
        mutableTaps = false;
        taps = {
          "homebrew/homebrew-core" = inputs.homebrew-core;
          "homebrew/homebrew-cask" = inputs.homebrew-cask;
          "homebrew/homebrew-bundle" = inputs.homebrew-bundle;
          "macos-fuse-t/homebrew-cask" = inputs.homebrew-fuse-t;
        };
      };
    }
    # Sync nix-homebrew taps into nix-darwin's homebrew.taps so
    # `brew bundle --cleanup` does not try to untap them.
    (
      { config, ... }:
      {
        homebrew.taps = builtins.attrNames config.nix-homebrew.taps;
      }
    )
    inputs.home-manager.darwinModules.home-manager
    inputs.stylix.darwinModules.stylix
    sharedStylixConfig
    {
      nixpkgs.hostPlatform = "aarch64-darwin";
      nixpkgs.overlays = sharedOverlays "aarch64-darwin";
      home-manager = mkHomeManagerBlock { inherit username homeModule; };
    }
    # workmux mounts the repos directory of the macOS host into the
    # terrarium VM at its macOS path. The macOS hosts share one terrarium
    # image. So the VM names that mount only when its dotfiles.reposDir
    # equals the path of each macOS host that runs it.
    (
      { config, ... }:
      let
        home = config.home-manager.users.${username};
        terrarium = self.nixosConfigurations.terrarium-aarch64.config;
        guestReposDir =
          terrarium.home-manager.users.${terrarium.dotfiles.system.username}.dotfiles.reposDir;
      in
      {
        assertions = [
          {
            # A homeModule that leaves out home/claude.nix has no
            # dotfiles.claude option, and that host runs no VM.
            assertion = !(home.dotfiles.claude.lima.enable or false) || home.dotfiles.reposDir == guestReposDir;
            message = ''
              dotfiles.reposDir is ${home.dotfiles.reposDir} on ${hostname}
              and ${guestReposDir} on the terrarium VM. The macOS hosts
              share one terrarium image. So each macOS host that enables
              dotfiles.claude.lima must set the path that
              hosts/nixos/terrarium.nix sets.'';
          }
        ];
      }
    )
    # Darwin-specific home-manager defaults
    {
      home-manager.users.${username} =
        { pkgs, ... }:
        {
          imports = [
            ../home/k9s.nix
            ../home/kube-radar.nix
            # Declares dotfiles.kubernetes.extraKrewPlugins, which this
            # module sets.
            ../home/kubernetes.nix
            ../home/tmux.nix
            ../home/virtualization.nix
            ../home/wireshark.nix
            ../home/caffeine.nix
          ];
          dotfiles = {
            inherit username hostname caCertificateFiles;
            homeDirectory = "/Users/${username}";
            taskSubdirs = [ "displays" ];
            extraHomePackages = with pkgs; [
              terminal-notifier
              gtk4
              librsvg
              libheif
              libraw
              dav1d
              drawio
              appcleaner
              caffeine
              keka
              monodraw
              vlc-bin
            ];
            kubernetes.extraKrewPlugins = [
              "sniff" # https://github.com/eldadru/ksniff
              "access-matrix" # https://github.com/corneliusweig/rakkess
              "cyclonus" # https://github.com/mattfenwick/kubectl-cyclonus
            ];
            shell = {
              extraInteractiveInit = ''
                # OrbStack integration
                source ~/.orbstack/shell/init2.fish 2>/dev/null || :
              '';
            };
            sshIncludes = [
              "~/.config/colima/ssh_config"
              "~/.orbstack/ssh/config"
            ];
          };
        };
    }
    darwinModule
  ];
}
