{
  pkgs,
  lib,
  config,
  ...
}:

let
  inherit (lib) mkOption types;
  cfg = config.dotfiles.kubernetes;
in
{
  options.dotfiles.kubernetes = {
    extraPackages = mkOption {
      type = types.listOf types.package;
      default = [ ];
      description = "Additional Kubernetes-related packages.";
    };

    extraKrewPlugins = mkOption {
      type = types.listOf types.nonEmptyStr;
      default = [ ];
      description = "Additional kubectl krew plugin names to install.";
    };

    extraHelmPlugins = mkOption {
      type = types.listOf types.package;
      default = [ ];
      description = "Additional Helm plugins to install.";
    };
  };

  config = {
    home = {
      sessionPath = [ "$HOME/.krew/bin" ];

      packages =
        with pkgs;
        [
          kubectl
          kubeconform
          kustomize
          (pkgs.wrapHelm pkgs.kubernetes-helm {
            plugins =
              with pkgs.kubernetes-helmPlugins;
              [
                helm-diff
                helm-unittest
              ]
              ++ [ pkgs.helm-schema ]
              ++ cfg.extraHelmPlugins;
          })
          kubectx
          cilium-cli
          hubble
          krew
          stern
          kubelogin
          fluxcd
          chart-testing
          cmctl
          argocd
          spacectl
        ]
        ++ cfg.extraPackages;
    };

    programs = {
      krewfile = {
        enable = true;
        krewPackage = pkgs.krew;
        indexes = {
          default = "https://github.com/kubernetes-sigs/krew-index.git";
          netshoot = "https://github.com/nilic/kubectl-netshoot.git";
        };
        plugins = [
          "netshoot/netshoot" # https://github.com/nilic/kubectl-netshoot
          "gadget" # inspektor gadget
          "tree" # https://github.com/ahmetb/kubectl-tree
          "blame" # https://github.com/knight42/kubectl-blame
          "get-all" # https://github.com/corneliusweig/ketall
          "graph" # https://github.com/steveteuber/kubectl-graph
          "krew"
        ]
        ++ cfg.extraKrewPlugins;
      };

      kubecolor = {
        enable = true;
        enableAlias = true;
      };
    };
  };
}
