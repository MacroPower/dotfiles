{
  description = "jacobcolvin dotfiles";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
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
    krewfile = {
      url = "github:brumhard/krewfile";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    claude-code = {
      url = "github:sadjow/claude-code-nix";
      inputs.nixpkgs.follows = "nixpkgs";
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
    nix-homebrew = {
      url = "github:zhaofengli/nix-homebrew";
    };
    treefmt-nix = {
      url = "github:numtide/treefmt-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    inputs@{
      self,
      nixpkgs,
      flake-parts,
      nix-darwin,
      home-manager,
      nix-vscode-extensions,
      krewfile,
      claude-code,
      dagger,
      stylix,
      nix-index-database,
      sops-nix,
      nix-homebrew,
      treefmt-nix,
      ...
    }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      imports = [
        treefmt-nix.flakeModule
      ];

      systems = [
        "aarch64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];

      perSystem = _: {
        treefmt.programs = {
          nixfmt.enable = true;
          shfmt.enable = true;
          prettier.enable = true;
        };
      };

      flake =
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

          sharedStylixConfig = import ./hosts/stylix.nix;

          hmSharedModules = [
            sops-nix.homeManagerModules.sops
            nix-index-database.homeModules.nix-index
            krewfile.homeManagerModules.krewfile
          ];

          mkHomeManagerBlock =
            { username, homeModule }:
            {
              useGlobalPkgs = true;
              useUserPackages = true;
              backupFileExtension = "bak";
              sharedModules = hmSharedModules;
              users.${username} = {
                imports = [
                  ./home
                  homeModule
                ];
              };
            };

          mkDarwin =
            {
              username,
              homebrew ? { },
              homeModule,
            }:
            nix-darwin.lib.darwinSystem {
              modules = [
                ./hosts/mac.nix
                {
                  dotfiles.system = {
                    inherit username;
                    inherit homebrew;
                  };
                  system.configurationRevision = self.rev or self.dirtyRev or null;
                }
                nix-homebrew.darwinModules.nix-homebrew
                {
                  nix-homebrew = {
                    enable = true;
                    enableRosetta = true;
                    autoMigrate = true;
                    user = username;
                  };
                }
                home-manager.darwinModules.home-manager
                stylix.darwinModules.stylix
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
              modules = hmSharedModules ++ [
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
              modules = [
                hostModule
                { dotfiles.system = { inherit username; }; }
                home-manager.nixosModules.home-manager
                stylix.nixosModules.stylix
                sharedStylixConfig
                {
                  nixpkgs.hostPlatform = system;
                  nixpkgs.overlays = sharedOverlays;
                  home-manager = mkHomeManagerBlock { inherit username homeModule; };
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
                extraTaps = [ ];
                extraBrews = [ ];
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
    };
}
