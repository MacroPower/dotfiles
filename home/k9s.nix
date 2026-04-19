{ config, ... }:
{
  config = {
    programs = {
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
              image = "nicolaka/netshoot@sha256:49dd3b2d303468996db4bde350285ea155338fe51b2fb0f44887a19acd3e6847";
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
          svc = "v1/services";
          cm = "v1/configmaps";
          ing = "networking.k8s.io/v1/ingresses";
          sa = "v1/serviceaccounts";
          ev = "v1/events";
          pvc = "v1/persistentvolumeclaims";
          pv = "v1/persistentvolumes";
          cert = "cert-manager.io/v1/certificates";
          ci = "cert-manager.io/v1/clusterissuers";
          cep = "ciliumendpoints";
          cnp = "ciliumnetworkpolicies";
          ccnp = "ciliumclusterwidenetworkpolicies";
        };

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
            description = "View Workloads";
            command = "workloads";
          };
          shift-4 = {
            shortCut = "Shift-4";
            description = "View Services";
            command = "services";
          };
          shift-5 = {
            shortCut = "Shift-5";
            description = "View Deployments";
            command = "deployments";
          };
          shift-6 = {
            shortCut = "Shift-6";
            description = "View Events";
            command = "events";
          };
        };

        plugins = {
          blame = {
            shortCut = "Ctrl-B";
            description = "Blame";
            scopes = [ "all" ];
            confirm = false;
            background = false;
            command = "task";
            args = [
              "-g"
              "k8s:blame"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "RESOURCE_NAME=$RESOURCE_NAME"
              "NAME=$NAME"
            ];
          };
          remove_finalizers = {
            shortCut = "Ctrl-F";
            confirm = true;
            dangerous = true;
            scopes = [ "all" ];
            description = "Remove Finalizers";
            command = "task";
            background = false;
            args = [
              "-g"
              "k8s:remove-finalizers"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "RESOURCE_NAME=$RESOURCE_NAME"
              "NAME=$NAME"
            ];
          };

          # Helm plugins
          helm_values = {
            shortCut = "v";
            confirm = false;
            description = "Values";
            scopes = [ "helm" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "helm:values"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "RELEASE=$COL-NAME"
            ];
          };
          helm_default_values = {
            shortCut = "Shift-V";
            confirm = false;
            description = "Chart Default Values";
            scopes = [ "helm" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "helm:default-values"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "RELEASE=$COL-NAME"
            ];
          };
          helm_diff_previous = {
            shortCut = "Shift-D";
            confirm = false;
            description = "Diff with Previous Revision";
            scopes = [ "helm" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "helm:diff-previous"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "RELEASE=$COL-NAME"
              "REVISION=$COL-REVISION"
            ];
          };
          helm_diff_current = {
            shortCut = "Shift-Q";
            confirm = false;
            description = "Diff with Current Revision";
            scopes = [ "history" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "helm:diff-current"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "RELEASE=$COL-NAME"
              "REVISION=$COL-REVISION"
            ];
          };

          # Debug & networking plugins
          debug = {
            shortCut = "Shift-D";
            description = "Add debug container";
            dangerous = true;
            confirm = true;
            scopes = [ "containers" ];
            command = "task";
            background = false;
            inputs = [
              {
                name = "image";
                label = "Debug image";
                type = "dropdown";
                required = true;
                default = "nicolaka/netshoot:v0.15";
                options = [
                  "nicolaka/netshoot:v0.15"
                  "busybox:1.37"
                  "alpine:3.23"
                  "ubuntu:24.04"
                ];
              }
              {
                name = "profile";
                label = "Debug profile";
                type = "dropdown";
                required = true;
                default = "sysadmin";
                options = [
                  "general"
                  "baseline"
                  "restricted"
                  "netadmin"
                  "sysadmin"
                  "legacy"
                ];
              }
              {
                name = "share_processes";
                label = "Share processes";
                type = "bool";
                required = true;
                default = true;
              }
            ];
            args = [
              "-g"
              "k8s:debug"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "POD=$POD"
              "NAME=$NAME"
            ];
          };
          node_root_shell = {
            shortCut = "Ctrl-N";
            description = "Root shell on node";
            dangerous = true;
            confirm = true;
            scopes = [ "nodes" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "k8s:node-root-shell"
              "CONTEXT=$CONTEXT"
              "NAME=$NAME"
            ];
          };
          watch_events = {
            shortCut = "Shift-E";
            confirm = false;
            description = "Get Events";
            scopes = [ "all" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "k8s:watch-events"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "RESOURCE_NAME=$RESOURCE_NAME"
              "NAME=$NAME"
            ];
          };
          pvc_shell = {
            shortCut = "Ctrl-P";
            description = "Shell on PVC";
            dangerous = true;
            confirm = true;
            scopes = [ "pvc" ];
            command = "task";
            background = false;
            inputs = [
              {
                name = "podname";
                label = "POD name";
                type = "string";
                required = true;
                default = "pvc-shell";
              }
              {
                name = "image";
                label = "Image";
                type = "dropdown";
                required = true;
                default = "nicolaka/netshoot:v0.15";
                options = [
                  "nicolaka/netshoot:v0.15"
                  "ubuntu:24.04"
                ];
              }
              {
                name = "mountpath";
                label = "Mount path";
                type = "string";
                required = true;
                default = "/mnt/data";
              }
            ];
            args = [
              "-g"
              "pvc:shell"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };
          pvc_resize = {
            shortCut = "Ctrl-X";
            description = "Resize PVC";
            dangerous = true;
            confirm = true;
            scopes = [ "pvc" ];
            command = "task";
            background = true;
            inputs = [
              {
                name = "size";
                label = "New size (e.g. 10Gi)";
                type = "string";
                required = true;
              }
            ];
            args = [
              "-g"
              "pvc:resize"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };
          trace_dns = {
            shortCut = "Shift-G";
            description = "Trace DNS requests";
            confirm = false;
            scopes = [
              "containers"
              "pods"
              "nodes"
            ];
            command = "task";
            background = false;
            args = [
              "-g"
              "k8s:trace-dns"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "RESOURCE_NAME=$RESOURCE_NAME"
              "NAME=$NAME"
              "POD=$POD"
            ];
          };

          # External Secrets plugins
          refresh_external_secrets = {
            shortCut = "Shift-R";
            confirm = false;
            scopes = [ "externalsecrets" ];
            description = "Refresh ExternalSecret";
            command = "task";
            background = true;
            args = [
              "-g"
              "externalsecrets:refresh"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };
          refresh_push_secrets = {
            shortCut = "Shift-R";
            confirm = false;
            scopes = [ "pushsecrets" ];
            description = "Refresh PushSecret";
            command = "task";
            background = true;
            args = [
              "-g"
              "externalsecrets:refresh-push"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };

          # Cert-Manager & TLS plugins
          cert_status = {
            shortCut = "Shift-W";
            confirm = false;
            description = "Certificate status";
            scopes = [ "certificates" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "cert:status"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };
          cert_renew = {
            shortCut = "Shift-R";
            confirm = true;
            dangerous = true;
            description = "Certificate renew";
            scopes = [ "certificates" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "cert:renew"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };
          secret_inspect = {
            shortCut = "Shift-I";
            confirm = false;
            description = "Inspect secret";
            scopes = [ "secrets" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "cert:inspect"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };
          secret_openssl_ca = {
            shortCut = "Ctrl-O";
            confirm = false;
            description = "Openssl ca.crt";
            scopes = [ "secrets" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "cert:openssl-ca"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };
          secret_openssl_tls = {
            shortCut = "Ctrl-T";
            confirm = false;
            description = "Openssl tls.crt";
            scopes = [ "secrets" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "cert:openssl-tls"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };

          # Cilium & Hubble plugins
          cilium_endpoint_status = {
            shortCut = "Ctrl-L";
            confirm = false;
            description = "Cilium endpoint status";
            scopes = [ "pods" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "cilium:endpoint-status"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };
          hubble_observe = {
            shortCut = "Shift-H";
            confirm = false;
            description = "Hubble observe flows";
            scopes = [ "pods" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "cilium:hubble-observe"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };
          cilium_identity = {
            shortCut = "Ctrl-Y";
            confirm = false;
            description = "Cilium identity";
            scopes = [ "pods" ];
            command = "task";
            background = false;
            args = [
              "-g"
              "cilium:identity"
              "CONTEXT=$CONTEXT"
              "NAMESPACE=$NAMESPACE"
              "NAME=$NAME"
            ];
          };
        };

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

        skins.one-dark = with config.lib.stylix.colors.withHashtag; {
          k9s = {
            body = {
              fgColor = base05;
              bgColor = "default";
              logoColor = base0B;
            };
            prompt = {
              fgColor = base05;
              bgColor = "default";
              suggestColor = base09;
            };
            info = {
              fgColor = base05;
              sectionColor = base0B;
            };
            help = {
              fgColor = base05;
              bgColor = "default";
              keyColor = base0A;
              numKeyColor = base0D;
              sectionColor = base0E;
            };
            dialog = {
              fgColor = base05;
              bgColor = base00;
              buttonFgColor = base05;
              buttonBgColor = base0B;
              buttonFocusFgColor = base00;
              buttonFocusBgColor = base0D;
              labelFgColor = base09;
              fieldFgColor = base0D;
            };
            frame = {
              border = {
                fgColor = base0B;
                focusColor = base0B;
              };
              menu = {
                fgColor = base05;
                keyColor = base0A;
                numKeyColor = base0A;
              };
              crumbs = {
                fgColor = base00;
                bgColor = base0B;
                activeColor = base0A;
              };
              status = {
                newColor = base0D;
                modifyColor = base0B;
                addColor = base05;
                pendingColor = base09;
                errorColor = base08;
                highlightColor = base0A;
                killColor = base0E;
                completedColor = base05;
              };
              title = {
                fgColor = base0D;
                bgColor = base00;
                highlightColor = base0E;
                counterColor = base05;
                filterColor = base0D;
              };
            };
            views = {
              charts = {
                bgColor = "default";
                defaultDialColors = [
                  base0B
                  base08
                ];
                defaultChartColors = [
                  base0B
                  base08
                ];
              };
              table = {
                fgColor = base0A;
                bgColor = "default";
                cursorFgColor = base00;
                cursorBgColor = base0D;
                markColor = base09;
                header = {
                  fgColor = base05;
                  bgColor = "default";
                  sorterColor = base09;
                };
              };
              xray = {
                fgColor = base0D;
                bgColor = "default";
                cursorColor = base05;
                graphicColor = base09;
                showIcons = false;
              };
              yaml = {
                keyColor = base08;
                colonColor = base05;
                valueColor = base05;
              };
              logs = {
                fgColor = base05;
                bgColor = "default";
                indicator = {
                  fgColor = base0D;
                  bgColor = "default";
                  toggleOnColor = base08;
                  toggleOffColor = base05;
                };
              };
              help = {
                fgColor = base05;
                bgColor = "default";
                indicator.fgColor = base0D;
              };
            };
          };
        };
      };
    };
  };
}
