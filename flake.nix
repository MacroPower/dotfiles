{
  description = "jacobcolvin dotfiles";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    nix-darwin = {
      url = "github:LnL7/nix-darwin/master";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    nix-vscode-extensions = {
      url = "github:nix-community/nix-vscode-extensions";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    flake-utils.url = "github:numtide/flake-utils";
    krewfile = {
      url = "github:brumhard/krewfile";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };
    claude-code = {
      url = "github:sadjow/claude-code-nix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };
    dagger = {
      url = "github:dagger/nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      nix-darwin,
      home-manager,
      nix-vscode-extensions,
      krewfile,
      claude-code,
      dagger,
      ...
    }:
    let
      localOverlay = final: _prev: {
        chief = final.callPackage ./pkgs/chief.nix { };
      };

      sharedOverlays = [
        localOverlay
        nix-vscode-extensions.overlays.default
        claude-code.overlays.default
        dagger.overlays.default
      ];

      applyHostDefaults =
        hostConfig:
        {
          homeDirectory = "/home/${hostConfig.username}";
          shell = {
            extraShellInit = "";
            extraInteractiveInit = "";
            extraTideConfig = "";
          };
          extraHomePackages = [ ];
          extraK8sPackages = [ ];
          extraKrewPlugins = [ ];
          extraXdgConfigFiles = { };
          extraVscodeExtensions = _marketplace: [ ];
          extraVscodeKubernetesSettings = { };
          claude = { };
        }
        // hostConfig;

      mkHomeManagerConfig = fullHostConfig: {
        useGlobalPkgs = true;
        useUserPackages = true;
        backupFileExtension = "bak";
        extraSpecialArgs = {
          inherit krewfile;
          hostConfig = fullHostConfig;
        };
        users.${fullHostConfig.username} = import ./home;
      };

      mkDarwin =
        hostConfig:
        let
          baseConfig = applyHostDefaults hostConfig;
          fullHostConfig = baseConfig // {
            homeDirectory = "/Users/${hostConfig.username}";
            extraHomePackages = baseConfig.extraHomePackages ++ [
              "terminal-notifier"
              "gtk4"
              "librsvg"
              "libheif"
              "libraw"
              "dav1d"
            ];
            extraKrewPlugins = baseConfig.extraKrewPlugins ++ [
              "sniff" # https://github.com/eldadru/ksniff
              "access-matrix" # https://github.com/corneliusweig/rakkess
              "cyclonus" # https://github.com/mattfenwick/kubectl-cyclonus
            ];
            shell = {
              extraShellInit = ''
                eval "$(/opt/homebrew/bin/brew shellenv)"
              '';
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
              "rectangle/RectangleConfig.json".source = ./configs/rectangle/RectangleConfig.json;
              "linearmouse/linearmouse.json".source = ./configs/linearmouse/linearmouse.json;
            };
            extraVscodeKubernetesSettings = {
              "vscode-kubernetes.helm-path.mac" = "/opt/homebrew/bin/helm";
              "vscode-kubernetes.kubectl-path.mac" = "/opt/homebrew/bin/kubectl";
              "vscode-kubernetes.minikube-path.mac" = "/opt/homebrew/bin/minikube";
            };
            sshIncludes = [
              "~/.config/colima/ssh_config"
              "~/.orbstack/ssh/config"
            ];
          };
        in
        nix-darwin.lib.darwinSystem {
          system = "aarch64-darwin";
          specialArgs = {
            inherit self;
            hostConfig = fullHostConfig;
          };
          modules = [
            ./hosts/mac.nix
            home-manager.darwinModules.home-manager
            {
              nixpkgs.overlays = sharedOverlays;
              home-manager = mkHomeManagerConfig fullHostConfig;
            }
          ];
        };

      mkHome =
        { system, hostConfig }:
        let
          fullHostConfig = applyHostDefaults hostConfig;
        in
        home-manager.lib.homeManagerConfiguration {
          pkgs = import nixpkgs {
            inherit system;
            config.allowUnfree = true;
            overlays = sharedOverlays;
          };
          inherit (mkHomeManagerConfig fullHostConfig) extraSpecialArgs;
          modules = [
            ./hosts/linux.nix
            ./home
          ];
        };

      mkNixOS =
        {
          system,
          hostModule,
          hostConfig,
        }:
        let
          nixosDefaults = {
            shell = {
              extraShellInit = "";
              extraInteractiveInit = "";
              extraTideConfig = ''
                set -g tide_left_prompt_items os $tide_left_prompt_items
                set -g tide_os_icon \ue843
              '';
            };
          };
          fullHostConfig = applyHostDefaults (nixosDefaults // hostConfig);
        in
        nixpkgs.lib.nixosSystem {
          inherit system;
          specialArgs = {
            hostConfig = fullHostConfig;
          };
          modules = [
            hostModule
            home-manager.nixosModules.home-manager
            {
              nixpkgs.overlays = sharedOverlays;
              home-manager = mkHomeManagerConfig fullHostConfig;
            }
          ];
        };
    in
    {
      darwinConfigurations = {
        "jacobcolvin@Jacobs-Mac-mini" = mkDarwin {
          username = "jacobcolvin";

          git = {
            userName = "Jacob Colvin";
            userEmail = "jacobcolvin1@gmail.com";
          };

          homebrew = {
            extraTaps = [ ];
            extraBrews = [ ];
            extraCasks = [
              "firefox"
              "discord"
              "plex"
              "orbstack"
              "slack"
              "filebot"
            ];
            masApps = { };
          };

          extraHomePackages = [ "talosctl" ];
          extraK8sPackages = [ ];
          extraVscodeExtensions =
            marketplace: with marketplace; [
              wakatime.vscode-wakatime
            ];
        };

        "jcolvin@Corporate-Mac" = mkDarwin {
          username = "jcolvin";

          git = {
            userName = "Jacob Colvin";
            userEmail = "jcolvin@example.com";
          };

          homebrew = {
            extraTaps = [
              "azure/kubelogin"
              "fluxcd/tap"
            ];
            extraBrews = [
              "azure-cli"
              "azure/kubelogin/kubelogin"
              "fluxcd/tap/flux"
            ];
            extraCasks = [ ];
            masApps = { };
          };

          extraHomePackages = [
            "azure-cli"
            "fluxcd"
          ];
          extraK8sPackages = [
            "kubelogin"
            "fluxcd"
          ];
          extraVscodeExtensions = _marketplace: [ ];
        };
      };

      nixosConfigurations = {
        "nixos-orbstack" = mkNixOS {
          system = "aarch64-linux";
          hostModule = ./hosts/nixos/orbstack.nix;
          hostConfig = {
            username = "jacobcolvin";
            git = {
              userName = "Jacob Colvin";
              userEmail = "jacobcolvin1@gmail.com";
            };
            claude = {
              dangerouslySkipPermissions = true;
            };
          };
        };

        "nixos-truenas" = mkNixOS {
          system = "x86_64-linux";
          hostModule = ./hosts/nixos/truenas.nix;
          hostConfig = {
            username = "jacobcolvin";
            git = {
              userName = "Jacob Colvin";
              userEmail = "jacobcolvin1@gmail.com";
            };
          };
        };
      };

      homeConfigurations = {
        "jacobcolvin@linux" = mkHome {
          system = "aarch64-linux";
          hostConfig = {
            username = "jacobcolvin";
            git = {
              userName = "Jacob Colvin";
              userEmail = "jacobcolvin1@gmail.com";
            };
            shell = {
              extraShellInit = "";
              extraInteractiveInit = "";
              extraTideConfig = ''
                set -g tide_left_prompt_items os $tide_left_prompt_items
                set -g tide_os_icon \uebc6
              '';
            };
          };
        };
      };
    };
}
