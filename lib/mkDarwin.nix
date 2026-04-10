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
    # `brew bundle --cleanup` doesn't try to untap them.
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
    # Darwin-specific home-manager defaults
    {
      home-manager.users.${username} =
        { pkgs, ... }:
        {
          imports = [
            ../home/k9s.nix
            ../home/tmux.nix
            ../home/virtualization.nix
            ../home/wireshark.nix
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
  ];
}
