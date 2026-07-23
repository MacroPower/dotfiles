{ pkgs, ... }:

let
  # terminal-dark renders with the terminal's own ANSI colors, so the
  # stylix palette carries over wherever the terminal is themed.
  presentermConfig = (pkgs.formats.yaml { }).generate "presenterm-config.yaml" {
    defaults.theme = "terminal-dark";
    # Vim-style navigation. tmux owns C-h/j/k/l, C-S-h/j/k/l, and C-b (prefix) at
    # the root table, so none of those keys can reach presenterm inside a tmux pane.
    bindings = {
      next = [
        "j"
        "l"
        "<right>"
        "<down>"
        "<page_down>"
        " "
      ];
      previous = [
        "k"
        "h"
        "<left>"
        "<up>"
        "<page_up>"
      ];
      next_fast = [
        "<c-d>"
        "n"
      ];
      previous_fast = [
        "<c-u>"
        "p"
      ];
      first_slide = [ "gg" ];
      last_slide = [ "G" ];
      go_to_slide = [ "<number>G" ];
      execute_code = [ "<c-e>" ];
      reload = [ "<c-r>" ];
      toggle_slide_index = [ "<c-p>" ];
      toggle_layout_grid = [ "T" ];
      toggle_bindings = [ "?" ];
      close_modal = [ "<esc>" ];
      skip_pauses = [ "s" ];
      exit = [
        "q"
        "<c-c>"
      ];
      suspend = [ "<c-z>" ];
    };
  };
in
{
  home.packages = [ pkgs.presenterm ];

  xdg.configFile."presenterm/config.yaml".source = presentermConfig;
}
