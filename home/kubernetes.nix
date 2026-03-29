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
          radar
        ]
        ++ lib.optionals pkgs.stdenv.isDarwin [ pkgs.radar-desktop ]
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

      k9s = {
        enable = true;
        settings = {
          k9s = {
            liveViewAutoRefresh = true;
            screenDumpDir = "/tmp/k9s-screen-dumps";
            refreshRate = 1;
            maxConnRetry = 5;
            readOnly = false;
            noExitOnCtrlC = false;
            ui = {
              enableMouse = false;
              headless = false;
              logoless = true;
              crumbsless = false;
              reactive = true;
              noIcons = false;
              defaultsToFullScreen = false;
              skin = "one-dark";
            };
            skipLatestRevCheck = false;
            disablePodCounting = false;
            shellPod = {
              image = "nicolaka/netshoot:b2f26ec9a306e27037573443b63f00e2e94a82dd";
              namespace = "default";
              limits = {
                cpu = "100m";
                memory = "100Mi";
              };
            };
            imageScans = {
              enable = false;
              exclusions = {
                namespaces = [ ];
                labels = { };
              };
            };
            logger = {
              tail = 1000;
              buffer = 10000;
              sinceSeconds = -1;
              textWrap = false;
              showTime = false;
            };
            thresholds = {
              cpu = {
                critical = 90;
                warn = 70;
              };
              memory = {
                critical = 90;
                warn = 70;
              };
            };
          };
        };

        aliases = {
          aliases = {
            dp = "deployments";
            sec = "v1/secrets";
            jo = "jobs";
            cr = "clusterroles";
            crb = "clusterrolebindings";
            ro = "roles";
            rb = "rolebindings";
            np = "networkpolicies";
            es = "externalsecrets";
            ces = "clusterexternalsecrets";
            ess = "secretstores";
            cess = "clustersecretstores";
          };
        };

        hotKeys = {
          hotKeys = {
            shift-1 = {
              shortCut = "Shift-1";
              description = "View Namespaces";
              command = "namespaces";
            };
            shift-2 = {
              shortCut = "Shift-2";
              description = "View Pods";
              command = "pods";
            };
            shift-3 = {
              shortCut = "Shift-3";
              description = "View Worloads";
              command = "workloads";
            };
          };
        };

        plugins = {
          plugins = {
            blame = {
              shortCut = "Ctrl-B";
              description = "Blame";
              scopes = [ "all" ];
              confirm = false;
              background = false;
              command = "bash";
              args = [
                "-c"
                "kubectl blame --context $CONTEXT --namespace $NAMESPACE $RESOURCE_NAME $NAME | less"
              ];
            };
            remove_finalizers = {
              shortCut = "Ctrl-F";
              confirm = true;
              dangerous = true;
              scopes = [ "all" ];
              description = "Remove Finalizers";
              command = "kubectl";
              background = true;
              args = [
                "patch"
                "--context"
                "$CONTEXT"
                "--namespace"
                "$NAMESPACE"
                "$RESOURCE_NAME"
                "$NAME"
                "-p"
                ''{"metadata":{"finalizers":null}}''
                "--type"
                "merge"
              ];
            };
          };
        };

        views = {
          views = {
            "external-secrets.io/v1beta1/externalsecrets" = {
              sortColumn = "NAME:asc";
              columns = [ ];
            };
            "v1/pods" = {
              sortColumn = "NAME:asc";
              columns = [ ];
            };
          };
        };

        skins.one-dark = ../configs/k9s/skins/one-dark.yaml;
      };
    };
  };
}
