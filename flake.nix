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
    llm-agents = {
      url = "github:numtide/llm-agents.nix";
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
    homebrew-core = {
      url = "github:homebrew/homebrew-core";
      flake = false;
    };
    homebrew-cask = {
      url = "github:homebrew/homebrew-cask";
      flake = false;
    };
    homebrew-bundle = {
      url = "github:homebrew/homebrew-bundle";
      flake = false;
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

      perSystem =
        { system, ... }:
        {
          treefmt.programs = {
            nixfmt.enable = true;
            deadnix.enable = true;
            statix.enable = true;
            shfmt.enable = true;
            prettier.enable = true;
          };

          checks =
            let
              inherit (nixpkgs) lib;
              filterSystem = lib.filterAttrs (
                _: cfg: (cfg.pkgs.stdenv.hostPlatform.system or cfg.config.nixpkgs.hostPlatform) == system
              );
            in
            lib.mergeAttrsList [
              (lib.mapAttrs' (name: cfg: lib.nameValuePair "${name}_home" cfg.activationPackage) (
                filterSystem self.homeConfigurations
              ))
              (lib.mapAttrs (_: cfg: cfg.config.system.build.toplevel) (
                filterSystem (self.darwinConfigurations // self.nixosConfigurations)
              ))
            ];
        };

      flake =
        let
          inherit
            (import ./lib {
              inherit inputs self;
              paths = {
                home = ./home;
                hostMac = ./hosts/mac.nix;
                hostLinux = ./hosts/linux.nix;
                stylix = ./hosts/stylix.nix;
                chief = ./pkgs/chief.nix;
                linearmouse = ./configs/linearmouse/linearmouse.json;
              };
            })
            mkDarwin
            mkHome
            mkNixOS
            ;
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
                    vscode.extraExtensions =
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
                    kubernetes.extraPackages = with pkgs; [
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
                dotfiles = {
                  git = {
                    userName = "Jacob Colvin";
                    userEmail = "jacobcolvin1@gmail.com";
                  };
                  kubernetes.enable = false;
                  vscode.enable = false;
                  claude.enable = false;
                  ghostty.enable = false;
                  zed.enable = false;
                  development.enable = false;
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
