{
  pkgs,
  lib,
  hostConfig,
  ...
}:

let
  tideSrc = pkgs.fishPlugins.tide.src;

  # Transform Tide preset file lines ("varname value") into fish
  # "set -g varname value" commands at Nix build time.
  toSetG =
    file:
    let
      lines = lib.splitString "\n" (builtins.readFile file);
      nonEmpty = builtins.filter (l: l != "") lines;
    in
    builtins.concatStringsSep "\n" (map (l: "set -g " + l) nonEmpty);
in
{
  # Tide prompt config: generated at build time from Tide's own lean
  # preset and icon files, with a small set of overrides. The 00- prefix
  # ensures this loads before Tide's _tide_init.fish in conf.d.
  xdg.configFile."fish/conf.d/00-tide-config.fish".text = ''
    # Color aliases referenced by lean.fish
    set -g _tide_color_dark_blue 0087AF
    set -g _tide_color_dark_green 5FAF00
    set -g _tide_color_gold D7AF00
    set -g _tide_color_green 5FD700
    set -g _tide_color_light_blue 00AFFF

    # Tide lean preset (auto-generated from plugin source)
    ${toSetG "${tideSrc}/functions/tide/configure/icons.fish"}
    ${toSetG "${tideSrc}/functions/tide/configure/configs/lean.fish"}

    # Ensure default key bindings during first render
    set -g fish_key_bindings fish_default_key_bindings

    # Overrides on top of lean defaults
    set -g tide_right_prompt_items $tide_right_prompt_items time
    set -g tide_prompt_add_newline_before false
    set -g tide_prompt_color_frame_and_connection 444444
    set -g tide_prompt_icon_connection \u00b7
    set -g tide_time_format %r

    # Per-host overrides
    ${hostConfig.shell.extraTideConfig or ""}
  '';

  programs.fish = {
    enable = true;

    shellInit = ''
      ${hostConfig.shell.extraShellInit}
      alias k=kubectl
      alias wk="watch -n 1 kubectl"
      alias kx=kubectx
      alias kn=kubens
    '';

    interactiveShellInit = ''
      ${hostConfig.shell.extraInteractiveInit}
      set --global fish_key_bindings fish_default_key_bindings

      set --global fish_color_autosuggestion 555 brblack
      set --global fish_color_cancel -r
      set --global fish_color_command blue
      set --global fish_color_comment red
      set --global fish_color_cwd green
      set --global fish_color_cwd_root red
      set --global fish_color_end green
      set --global fish_color_error brred
      set --global fish_color_escape brcyan
      set --global fish_color_history_current --bold
      set --global fish_color_host normal
      set --global fish_color_host_remote yellow
      set --global fish_color_normal normal
      set --global fish_color_operator brcyan
      set --global fish_color_param cyan
      set --global fish_color_quote yellow
      set --global fish_color_redirection cyan --bold
      set --global fish_color_search_match white --background=brblack
      set --global fish_color_selection white --bold --background=brblack
      set --global fish_color_status red
      set --global fish_color_user brgreen
      set --global fish_color_valid_path --underline
      set --global fish_pager_color_completion normal
      set --global fish_pager_color_description B3A06D yellow -i
      set --global fish_pager_color_prefix normal --bold --underline
      set --global fish_pager_color_progress brwhite --background=cyan
      set --global fish_pager_color_selected_background -r

      fish_add_path "$HOME/go/bin"
      fish_add_path "$HOME/.npm-packages/bin"
      fish_add_path "$HOME/.krew/bin"
      fish_add_path "$HOME/.local/bin"

    '';

    shellAliases = {
      cat = "bat";
      cd = "z";
      top = "btm";
      watch = "viddy";
      w = "viddy";
      traceroute = "trip";
      kubectl = "kubecolor";
    };

    functions = {
      fish_greeting = "";
    };

    plugins = [
      {
        name = "fzf-fish";
        inherit (pkgs.fishPlugins.fzf-fish) src;
      }
      {
        name = "autopair";
        inherit (pkgs.fishPlugins.autopair) src;
      }
      {
        name = "tide";
        inherit (pkgs.fishPlugins.tide) src;
      }
      {
        name = "plugin-jump";
        src = pkgs.fetchFromGitHub {
          owner = "oh-my-fish";
          repo = "plugin-jump";
          rev = "af285ff91fa9d0d0261b810e09f8a4a05a6b1307";
          hash = "sha256-MVIXBKsfd7rrH7Dh7cksNI29YunqAGZvuZwdfrf1bZQ=";
        };
      }
      {
        name = "fish-kubectl-completions";
        src = pkgs.fetchFromGitHub {
          owner = "evanlucas";
          repo = "fish-kubectl-completions";
          rev = "ced676392575d618d8b80b3895cdc3159be3f628";
          hash = "sha256-OYiYTW+g71vD9NWOcX1i2/TaQfAg+c2dJZ5ohwWSDCc=";
        };
      }
    ];
  };

}
