{
  pkgs,
  lib,
  config,
  ...
}:

let
  inherit (lib) mkOption mkEnableOption types;
  cfg = config.dotfiles.vscode;
  marketplace = pkgs.vscode-marketplace;
  fontLigatures = builtins.concatStringsSep ", " (map (f: "'${f}'") config.dotfiles.fonts.features);
in
{
  options.dotfiles.vscode = {
    enable = mkEnableOption "VS Code" // {
      default = true;
    };

    extraExtensions = mkOption {
      # uniq: function-typed options cannot be meaningfully merged across modules
      type = types.uniq (types.functionTo (types.listOf types.package));
      default = _marketplace: [ ];
      description = "Function taking a vscode-marketplace attrset and returning a list of extra VS Code extensions.";
    };

    extraKubernetesSettings = mkOption {
      type = types.attrsOf types.str;
      default = { };
      description = "Additional VS Code Kubernetes extension settings.";
    };
  };

  config = lib.mkIf cfg.enable {
    programs.vscode = {
      enable = true;
      mutableExtensionsDir = false;
      argvSettings = {
        enable-crash-reporter = false;
      };

      profiles.default = {
        extensions =
          with marketplace;
          [
            bierner.markdown-mermaid
            eamodio.gitlens
            editorconfig.editorconfig
            esbenp.prettier-vscode
            github.copilot
            github.copilot-chat
            golang.go
            grafana.vscode-jsonnet
            hashicorp.terraform
            kcl.kcl-vscode-extension
            kokakiwi.vscode-just
            ms-kubernetes-tools.vscode-kubernetes-tools
            ms-python.debugpy
            ms-python.python
            ms-python.vscode-pylance
            ms-python.vscode-python-envs
            pkief.material-icon-theme
            redhat.vscode-yaml
            stkb.rewrap
            streetsidesoftware.code-spell-checker
            task.vscode-task
            zhuangtongfa.material-theme
          ]
          ++ (cfg.extraExtensions marketplace);

        userSettings = {
          "$schema" = "vscode://schemas/settings/user";
          "workbench.colorTheme" = "One Dark Pro Darker";
          "workbench.iconTheme" = "material-icon-theme";
          "material-icon-theme.files.associations" = {
            "*.jsonnet" = "json";
            "*.libsonnet" = "raml";
            ".deployment" = "settings";
          };
          "material-icon-theme.folders.associations" = {
            alerts = "event";
            wiki = "resource";
            ".sonarqube" = "secure";
            dashboards = "benchmark";
            panels = "components";
            "github.com" = "github";
            mixin = "plugin";
          };
          "explorer.confirmDragAndDrop" = false;
          "editor.fontFamily" = "'${config.stylix.fonts.monospace.name}', monospace";
          "editor.fontWeight" = "500";
          "editor.fontLigatures" = fontLigatures;
          "editor.fontSize" = 14;
          "terminal.integrated.fontFamily" = "'${config.stylix.fonts.monospace.name}', monospace";
          "terminal.integrated.fontSize" = 14;
          "vs-kubernetes" = {
            "vs-kubernetes.crd-code-completion" = "enabled";
          }
          // cfg.extraKubernetesSettings;
          "editor.rulers" = [
            80
            120
          ];
          "[vue]" = {
            "editor.defaultFormatter" = "esbenp.prettier-vscode";
          };
          "[typescript]" = {
            "editor.defaultFormatter" = "esbenp.prettier-vscode";
          };
          "[yaml]" = {
            "editor.defaultFormatter" = "esbenp.prettier-vscode";
          };
          "[json]" = {
            "editor.defaultFormatter" = "esbenp.prettier-vscode";
          };
          "[jsonc]" = {
            "editor.defaultFormatter" = "esbenp.prettier-vscode";
          };
          "cSpell.allowCompoundWords" = true;
          "cSpell.reportUnknownWords" = false;
          "cSpell.userWords" = [ "stretchr" ];
          "files.exclude" = {
            "**/.git" = false;
            "**/.git/[^h]*" = true;
            "**/.svn" = true;
            "**/.hg" = true;
            "**/.DS_Store" = true;
            "**/Thumbs.db" = true;
          };
          "github.copilot.enable" = {
            "*" = true;
            plaintext = false;
            scminput = false;
          };
          "github.copilot.chat.codesearch.enabled" = true;
          "github.copilot.chat.generateTests.codeLens" = true;
          "github.copilot.chat.agent.thinkingTool" = true;
          "github.copilot.nextEditSuggestions.enabled" = true;
          "github.copilot.nextEditSuggestions.fixes" = true;
          "editor.inlineSuggest.edits.showCollapsed" = true;
          "editor.fontVariations" = false;
          "github.copilot.chat.agent.terminal.allowList" = {
            task = true;
            mkdir = true;
            touch = true;
            echo = true;
            ls = true;
            cat = true;
            grep = true;
            "devbox run -- task" = true;
          };
          "accessibility.voice.speechTimeout" = 0;
        };
      };
    };
  };
}
