{
  description = "jacobcolvin dotfiles";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
    nix-darwin = {
      url = "github:nix-darwin/nix-darwin";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    home-manager = {
      url = "github:nix-community/home-manager";
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
    workmux = {
      url = "github:raine/workmux";
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
    rycee-nur = {
      url = "gitlab:rycee/nur-expressions";
      flake = false;
    };
    nur-jacobcolvin = {
      url = "git+https://nur.jacobcolvin.com";
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
    homebrew-fuse-t = {
      url = "github:macos-fuse-t/homebrew-cask";
      flake = false;
    };
    nixos-lima = {
      url = "github:nixos-lima/nixos-lima";
      inputs.nixpkgs.follows = "nixpkgs";
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
        {
          system,
          ...
        }:
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

          packages =
            let
              inherit (nixpkgs) lib;
              arch = builtins.head (lib.splitString "-" system);
            in
            lib.optionalAttrs (lib.hasSuffix "-linux" system) {
              terrarium-image = self.nixosConfigurations."terrarium-${arch}".config.system.build.images.qemu-efi;
            };

        };

      flake =
        let
          inherit
            (import ./lib {
              inherit inputs self;
              paths = {
                home = ./home;
                hostMac = ./hosts/darwin/default.nix;
                hostLinux = ./hosts/linux/default.nix;
                stylix = ./lib/stylix.nix;
                chief = ./pkgs/chief.nix;
                displayplacer = ./pkgs/displayplacer.nix;
                otel-tui = ./pkgs/otel-tui.nix;
                zed-bin = ./pkgs/zed.nix;
                photo-cli = ./pkgs/photo-cli.nix;
                rtk-bin = ./pkgs/rtk-bin.nix;
                mcp-kubernetes = ./pkgs/mcp-kubernetes.nix;
                mcp-git = ./tools/mcp-git/package.nix;
                hook-router = ./tools/hook-router/package.nix;
                mcp-fetch = ./tools/mcp-fetch/package.nix;
                radar = ./pkgs/radar.nix;
                radar-desktop = ./pkgs/radar-desktop.nix;
                helm-schema = ./pkgs/helm-schema.nix;
                mcp-kagi = ./pkgs/mcp-kagi.nix;
                mcp-argocd = ./pkgs/mcp-argocd.nix;
                mcp-opentofu = ./pkgs/mcp-opentofu.nix;
                leanspec-mcp = ./pkgs/leanspec-mcp.nix;
                leanspec-cli = ./pkgs/leanspec-cli.nix;
                claude-powerline = ./pkgs/claude-powerline.nix;
                claude-history = ./pkgs/claude-history.nix;
                git-surgeon = ./pkgs/git-surgeon.nix;
                no-new-privs = ./tools/no-new-privs/package.nix;
                krewfileModule = ./lib/krewfile-module.nix;
              };
            })
            mkDarwin
            mkHome
            mkNixOS
            ;
        in
        {
          darwinConfigurations = {
            "jacobcolvin@Jacobs-Mac-mini" = mkDarwin (import ./hosts/darwin/mac-mini.nix);
            "jacobcolvin@Jacobs-MacBook-Pro" = mkDarwin (import ./hosts/darwin/mbp.nix);
          };

          nixosConfigurations =
            let
              mkLimaConfig =
                file: system:
                mkNixOS (
                  import file {
                    inherit (inputs) nixos-lima;
                    inherit system;
                  }
                );
            in
            {
              "nixos-orbstack" = mkNixOS (import ./hosts/nixos/orbstack.nix);
              "nixos-truenas" = mkNixOS (import ./hosts/nixos/truenas.nix);
              "terrarium-aarch64" = mkLimaConfig ./hosts/nixos/terrarium.nix "aarch64-linux";
              "terrarium-x86_64" = mkLimaConfig ./hosts/nixos/terrarium.nix "x86_64-linux";
            };

          homeConfigurations = {
            "dev@aarch64-linux" = mkHome (import ./hosts/linux/container.nix "aarch64-linux");
            "dev@x86_64-linux" = mkHome (import ./hosts/linux/container.nix "x86_64-linux");
          };

          inventory = import ./lib/inventory.nix {
            inherit self;
            inherit (nixpkgs) lib;
          };
        };
    };
}
