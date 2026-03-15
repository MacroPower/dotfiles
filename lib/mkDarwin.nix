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
  homebrew ? { },
  loginItems ? [ ],
  extraApps ? [ ],
  homeModule,
}:
inputs.nix-darwin.lib.darwinSystem {
  modules = [
    paths.hostMac
    {
      dotfiles.system = {
        inherit
          username
          homebrew
          loginItems
          extraApps
          ;
      };
      system.configurationRevision = self.rev or self.dirtyRev or null;
    }
    inputs.mac-app-util.darwinModules.default
    # home-manager 25.11+ copies apps via rsync (targets.darwin.copyApps),
    # so they work natively with Spotlight, Dock, and Gatekeeper -- no
    # mac-app-util trampolines needed for HM apps.
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
          dotfiles = {
            inherit username;
            homeDirectory = "/Users/${username}";
            extraHomePackages = with pkgs; [
              terminal-notifier
              gtk4
              librsvg
              libheif
              libraw
              dav1d
              drawio
              wireshark
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
            vscode.extraKubernetesSettings = {
              "vscode-kubernetes.helm-path.mac" = "/opt/homebrew/bin/helm";
              "vscode-kubernetes.kubectl-path.mac" = "/opt/homebrew/bin/kubectl";
              "vscode-kubernetes.minikube-path.mac" = "/opt/homebrew/bin/minikube";
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
