{
  pkgs,
  lib,
  config,
  osConfig ? { },
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

  hasOsConfig = osConfig ? networking;
  isDarwin = hasOsConfig && pkgs.stdenv.isDarwin;
  inherit (config.dotfiles) username hostname;
  flakeExpr = "builtins.getFlake (toString ./.)";
  # NixOS configs are keyed by hostname alone; darwin and home-manager use username@hostname.
  isNixOS = hasOsConfig && !isDarwin;
  configName =
    let
      name = if isNixOS then hostname else "${username}@${hostname}";
    in
    ''"${name}"'';
  configType =
    if isDarwin then
      "darwinConfigurations"
    else if isNixOS then
      "nixosConfigurations"
    else
      "homeConfigurations";

  nixdOptions =
    let
      flakeConfig = "(${flakeExpr}).${configType}.${configName}";
    in
    if hasOsConfig then
      {
        ${if isDarwin then "darwin" else "nixos"} = {
          expr = "${flakeConfig}.options";
        };
        home-manager = {
          expr = "${flakeConfig}.options.home-manager.users.type.getSubOptions []";
        };
      }
    else
      {
        home-manager = {
          expr = "${flakeConfig}.options";
        };
      };
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
        languages = {
          Nix = {
            language_servers = [
              "nixd"
              "!nil"
            ];
          };
        };
        lsp = {
          nixd = {
            settings = {
              nixpkgs = {
                expr = "(${flakeExpr}).${configType}.${configName}.pkgs";
              };
              formatting = {
                command = [ "nixfmt" ];
              };
              options = nixdOptions;
            };
          };
        };
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
          provider = "copilot";
        };
        ui_font_size = 15.0;
        ui_font_weight = 500.0;
        ui_font_family = config.stylix.fonts.monospace.name;
        ui_font_features = fontFeaturesAttrs;
        buffer_font_size = 14.0;
        buffer_font_weight = 500.0;
        buffer_font_family = config.stylix.fonts.monospace.name;
        buffer_font_features = fontFeaturesAttrs;
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
