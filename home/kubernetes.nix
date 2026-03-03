{
  pkgs,
  krewfile,
  hostConfig,
  ...
}:

{
  imports = [
    krewfile.homeManagerModules.krewfile
  ];

  home.packages =
    with pkgs;
    [
      kubectl
      kubeconform
      kustomize
      kubernetes-helm
      kubectx
      cilium-cli
      go-task
      krew
    ]
    ++ (map (name: pkgs.${name}) hostConfig.extraK8sPackages);

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
        "stern" # https://github.com/stern/stern
        "krew"
      ]
      ++ hostConfig.extraKrewPlugins;
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

      skins.one-dark =
        let
          foreground = "#abb2bf";
          background = "#23272e";
          black = "#080808";
          blue = "#61afef";
          green = "#98c379";
          grey = "#abb2bf";
          orange = "#ffb86c";
          purple = "#c678dd";
          red = "#e06370";
          yellow = "#e5c07b";
          yellow_bright = "#d19a66";
        in
        {
          inherit
            foreground
            background
            black
            blue
            green
            grey
            orange
            purple
            red
            yellow
            yellow_bright
            ;
          k9s = {
            body = {
              fgColor = foreground;
              bgColor = background;
              logoColor = green;
            };
            prompt = {
              fgColor = foreground;
              bgColor = background;
              suggestColor = orange;
            };
            info = {
              fgColor = grey;
              sectionColor = green;
            };
            help = {
              fgColor = foreground;
              bgColor = background;
              keyColor = yellow;
              numKeyColor = blue;
              sectionColor = purple;
            };
            dialog = {
              fgColor = black;
              bgColor = background;
              buttonFgColor = foreground;
              buttonBgColor = green;
              buttonFocusFgColor = black;
              buttonFocusBgColor = blue;
              labelFgColor = orange;
              fieldFgColor = blue;
            };
            frame = {
              border = {
                fgColor = green;
                focusColor = green;
              };
              menu = {
                fgColor = grey;
                keyColor = yellow;
                numKeyColor = yellow;
              };
              crumbs = {
                fgColor = black;
                bgColor = green;
                activeColor = yellow;
              };
              status = {
                newColor = blue;
                modifyColor = green;
                addColor = grey;
                pendingColor = orange;
                errorColor = red;
                highlightColor = yellow;
                killColor = purple;
                completedColor = grey;
              };
              title = {
                fgColor = blue;
                bgColor = background;
                highlightColor = purple;
                counterColor = foreground;
                filterColor = blue;
              };
            };
            views = {
              charts = {
                bgColor = background;
                defaultDialColors = [
                  green
                  red
                ];
                defaultChartColors = [
                  green
                  red
                ];
              };
              table = {
                fgColor = yellow;
                bgColor = background;
                cursorFgColor = black;
                cursorBgColor = blue;
                markColor = yellow_bright;
                header = {
                  fgColor = grey;
                  bgColor = background;
                  sorterColor = orange;
                };
              };
              xray = {
                fgColor = blue;
                bgColor = background;
                cursorColor = foreground;
                graphicColor = yellow_bright;
                showIcons = false;
              };
              yaml = {
                keyColor = red;
                colonColor = grey;
                valueColor = grey;
              };
              logs = {
                fgColor = grey;
                bgColor = background;
                indicator = {
                  fgColor = blue;
                  bgColor = background;
                  toggleOnColor = red;
                  toggleOffColor = grey;
                };
              };
              help = {
                fgColor = grey;
                bgColor = background;
                indicator = {
                  fgColor = blue;
                };
              };
            };
          };
        };
    };
  };
}
