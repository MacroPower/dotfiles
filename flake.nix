{
  description = "jacobcolvin dotfiles";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    nix-darwin = {
      url = "github:nix-darwin/nix-darwin";
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
    stylix = {
      url = "github:nix-community/stylix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    nix-index-database = {
      url = "github:nix-community/nix-index-database";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    sops-nix = {
      url = "github:Mic92/sops-nix";
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
      stylix,
      nix-index-database,
      sops-nix,
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

      sharedStylixConfig =
        { pkgs, ... }:
        {
          stylix = {
            enable = true;
            autoEnable = true;
            polarity = "dark";

            base16Scheme = "${pkgs.base16-schemes}/share/themes/onedark.yaml";
            override = {
              base00 = "23272e"; # darker background
            };

            fonts = {
              monospace = {
                package = pkgs.nerd-fonts.fira-code;
                name = "FiraCode Nerd Font Mono";
              };
              sansSerif = {
                package = pkgs.fira;
                name = "Fira Sans";
              };
              serif = {
                package = pkgs.merriweather;
                name = "Merriweather";
              };
              emoji = {
                package = pkgs.noto-fonts-color-emoji;
                name = "Noto Color Emoji";
              };
              sizes.terminal = 14;
            };
          };
        };

      mkDarwin =
        {
          username,
          homebrew ? { },
          homeModule,
        }:
        nix-darwin.lib.darwinSystem {
          specialArgs = {
            inherit self;
            hostConfig = {
              inherit username;
              inherit homebrew;
            };
          };
          modules = [
            ./hosts/mac.nix
            home-manager.darwinModules.home-manager
            stylix.darwinModules.stylix
            sharedStylixConfig
            {
              nixpkgs.hostPlatform = "aarch64-darwin";
              nixpkgs.overlays = sharedOverlays;
              home-manager = {
                useGlobalPkgs = true;
                useUserPackages = true;
                backupFileExtension = "bak";
                sharedModules = [
                  sops-nix.homeManagerModules.sops
                  nix-index-database.homeModules.nix-index
                ];
                extraSpecialArgs = { inherit krewfile; };
                users.${username} = {
                  imports = [
                    ./home
                    homeModule
                  ];
                };
              };
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
                    extraKrewPlugins = [
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
                };
            }
          ];
        };

      mkHome =
        { system, homeModule }:
        home-manager.lib.homeManagerConfiguration {
          pkgs = import nixpkgs {
            inherit system;
            config.allowUnfree = true;
            overlays = sharedOverlays;
          };
          extraSpecialArgs = { inherit krewfile; };
          modules = [
            sops-nix.homeManagerModules.sops
            nix-index-database.homeModules.nix-index
            stylix.homeModules.stylix
            sharedStylixConfig
            ./hosts/linux.nix
            ./home
            homeModule
          ];
        };

      mkNixOS =
        {
          system,
          hostModule,
          username,
          homeModule,
        }:
        nixpkgs.lib.nixosSystem {
          specialArgs = {
            hostConfig = { inherit username; };
          };
          modules = [
            hostModule
            home-manager.nixosModules.home-manager
            stylix.nixosModules.stylix
            sharedStylixConfig
            {
              nixpkgs.hostPlatform = system;
              nixpkgs.overlays = sharedOverlays;
              home-manager = {
                useGlobalPkgs = true;
                useUserPackages = true;
                backupFileExtension = "bak";
                sharedModules = [
                  sops-nix.homeManagerModules.sops
                  nix-index-database.homeModules.nix-index
                ];
                extraSpecialArgs = { inherit krewfile; };
                users.${username} = {
                  imports = [
                    ./home
                    homeModule
                  ];
                };
              };
            }
            # NixOS-specific home-manager defaults
            {
              home-manager.users.${username} = {
                dotfiles = {
                  inherit username;
                  homeDirectory = "/home/${username}";
                  shell.extraTideConfig = ''
                    set -g tide_left_prompt_items os $tide_left_prompt_items
                    set -g tide_os_icon \ue843
                  '';
                };
              };
            }
          ];
        };
    in
    {
      formatter = {
        aarch64-darwin = nixpkgs.legacyPackages.aarch64-darwin.nixfmt;
        aarch64-linux = nixpkgs.legacyPackages.aarch64-linux.nixfmt;
        x86_64-linux = nixpkgs.legacyPackages.x86_64-linux.nixfmt;
      };

      darwinConfigurations = {
        "jacobcolvin@Jacobs-Mac-mini" = mkDarwin {
          username = "jacobcolvin";

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

          homeModule =
            { pkgs, ... }:
            {
              dotfiles = {
                git = {
                  userName = "Jacob Colvin";
                  userEmail = "jacobcolvin1@gmail.com";
                };
                extraHomePackages = with pkgs; [ talosctl ];
                extraVscodeExtensions =
                  marketplace: with marketplace; [
                    wakatime.vscode-wakatime
                  ];
              };
            };
        };

        "jcolvin@Corporate-Mac" = mkDarwin {
          username = "jcolvin";

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

          homeModule =
            { pkgs, ... }:
            {
              dotfiles = {
                git = {
                  userName = "Jacob Colvin";
                  userEmail = "jcolvin@example.com";
                };
                extraHomePackages = with pkgs; [
                  azure-cli
                  fluxcd
                ];
                extraK8sPackages = with pkgs; [
                  kubelogin
                  fluxcd
                ];
              };
            };
        };
      };

      nixosConfigurations = {
        "nixos-orbstack" = mkNixOS {
          system = "aarch64-linux";
          hostModule = ./hosts/nixos/orbstack.nix;
          username = "jacobcolvin";
          homeModule = {
            dotfiles = {
              git = {
                userName = "Jacob Colvin";
                userEmail = "jacobcolvin1@gmail.com";
              };
              claude.dangerouslySkipPermissions = true;
            };
          };
        };

        "nixos-truenas" = mkNixOS {
          system = "x86_64-linux";
          hostModule = ./hosts/nixos/truenas.nix;
          username = "jacobcolvin";
          homeModule = {
            dotfiles.git = {
              userName = "Jacob Colvin";
              userEmail = "jacobcolvin1@gmail.com";
            };
          };
        };
      };

      homeConfigurations = {
        "jacobcolvin@linux" = mkHome {
          system = "aarch64-linux";
          homeModule = {
            dotfiles = {
              username = "jacobcolvin";
              homeDirectory = "/home/jacobcolvin";
              git = {
                userName = "Jacob Colvin";
                userEmail = "jacobcolvin1@gmail.com";
              };
              shell.extraTideConfig = ''
                set -g tide_left_prompt_items os $tide_left_prompt_items
                set -g tide_os_icon \uebc6
              '';
            };
          };
        };
      };
    };
}
