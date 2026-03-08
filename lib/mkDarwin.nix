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
  homeModule,
}:
inputs.nix-darwin.lib.darwinSystem {
  modules = [
    paths.hostMac
    {
      dotfiles.system = {
        inherit username homebrew loginItems;
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
          "buo/homebrew-cask-upgrade" = inputs.homebrew-cask-upgrade;
          "jakehilborn/homebrew-jakehilborn" = inputs.homebrew-jakehilborn;
          "macos-fuse-t/homebrew-cask" = inputs.homebrew-fuse-t;
          "photo-cli/homebrew-photo-cli" = inputs.homebrew-photo-cli;
          "ymtdzzz/homebrew-tap" = inputs.homebrew-ymtdzzz;
          "macropower/homebrew-tap" = inputs.homebrew-macropower;
          "robusta-dev/homebrew-krr" = inputs.homebrew-krr;
          "jacobcolvin/homebrew-tap" = inputs.homebrew-jacobcolvin;
        };
      };
    }
    # Sync nix-homebrew taps into nix-darwin's homebrew.taps so
    # `brew bundle --cleanup` doesn't try to untap them.
    ({ config, ... }: {
      homebrew.taps = builtins.attrNames config.nix-homebrew.taps;
    })
    inputs.home-manager.darwinModules.home-manager
    inputs.stylix.darwinModules.stylix
    sharedStylixConfig
    {
      nixpkgs.hostPlatform = "aarch64-darwin";
      nixpkgs.overlays = sharedOverlays;
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
              extraTideConfig = ''
                set -g tide_left_prompt_items os $tide_left_prompt_items
                set -g tide_os_icon \ue711
              '';
            };
            extraXdgConfigFiles = {
              "linearmouse/linearmouse.json".source = paths.linearmouse;
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
