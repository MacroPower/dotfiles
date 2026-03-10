{
  pkgs,
  lib,
  config,
  ...
}:

let
  cfg = config.dotfiles.zed;
  fontFeatures = config.dotfiles.fonts.features;
  fontFeaturesAttrs = builtins.listToAttrs (
    map (f: {
      name = f;
      value = true;
    }) fontFeatures
  );
in
{
  options.dotfiles.zed.enable = lib.mkEnableOption "Zed editor" // {
    default = true;
  };

  config = lib.mkIf cfg.enable {
    programs.zed-editor = {
      enable = true;
      package = pkgs.zed-bin;

      extensions = [
        # themes & icons
        "material-icon-theme"
        "one-dark-pro"
        # languages
        "nix"
        "csharp"
        "toml"
        "dockerfile"
        "fish"
        "latex"
        "java"
        "scss"
        "git-firefly"
        "sql"
        "csv"
        "ini"
        # infra
        "terraform"
        "helm"
      ];

      userSettings = {
        agent = {
          default_model = {
            provider = "copilot_chat";
            model = "claude-opus-4.6";
          };
          favorite_models = [ ];
          model_parameters = [ ];
        };
        edit_predictions = {
          mode = "subtle";
        };
        ui_font_size = 15.0;
        ui_font_weight = 500.0;
        ui_font_family = config.stylix.fonts.monospace.name;
        ui_font_features = fontFeaturesAttrs;
        buffer_font_size = 14.0;
        buffer_font_weight = 500.0;
        buffer_font_family = config.stylix.fonts.monospace.name;
        buffer_font_features = fontFeaturesAttrs;
        features = {
          edit_prediction_provider = "copilot";
        };
        terminal = {
          font_family = config.stylix.fonts.monospace.name;
          font_features = fontFeaturesAttrs;
        };
        base_keymap = "VSCode";
        vim_mode = false;
        icon_theme = "Material Icon Theme";
        theme = "One Dark Pro";
        wrap_guides = [
          88
          120
        ];
        ssh_connections = [
          {
            host = "nixos-orbstack.orb.local";
            username = "jacobcolvin";
          }
        ];
      };

      userKeymaps = [
        {
          context = "Workspace";
          bindings = {
            "shift shift" = "file_finder::Toggle";
          };
        }
      ];
    };

    home.file.".zed_server" = lib.mkIf pkgs.stdenv.isLinux {
      source = "${pkgs.zed-bin.remote_server}/bin";
      recursive = true;
    };
  };
}
