{
  pkgs,
  lib,
  config,
  ...
}:

let
  tideSrc = pkgs.fishPlugins.tide.src;
  inherit (config.lib.stylix) colors;

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
    # Tide internal color aliases (OneDark via stylix)
    set -g _tide_color_dark_blue ${colors.base0D}    # blue
    set -g _tide_color_dark_green ${colors.base0B}   # green
    set -g _tide_color_gold ${colors.base0A}         # yellow
    set -g _tide_color_green ${colors.base0B}        # green
    set -g _tide_color_light_blue ${colors.base0D}   # blue

    # Tide lean preset (auto-generated from plugin source)
    ${toSetG "${tideSrc}/functions/tide/configure/icons.fish"}
    ${toSetG "${tideSrc}/functions/tide/configure/configs/lean.fish"}

    # Ensure default key bindings during first render
    set -g fish_key_bindings fish_default_key_bindings

    # Overrides on top of lean defaults
    set -g tide_right_prompt_items $tide_right_prompt_items time
    set -g tide_prompt_add_newline_before false
    set -g tide_prompt_icon_connection \u00b7
    set -g tide_time_format %r

    # ── OneDark color overrides (stylix palette) ─────────────────────

    # Character prompt
    set -g tide_character_color_failure ${colors.base08}   # red

    # Git
    set -g tide_git_color_conflicted ${colors.base08}       # red
    set -g tide_git_color_operation ${colors.base08}        # red

    # PWD
    set -g tide_pwd_color_truncated_dirs ${colors.base0C}   # cyan

    # Prompt chrome
    set -g tide_prompt_color_frame_and_connection ${colors.base03}   # muted gray
    set -g tide_prompt_color_separator_same_color ${colors.base04}   # dark foreground

    # Context (user@host)
    set -g tide_context_color_default ${colors.base09}   # orange
    set -g tide_context_color_ssh ${colors.base09}       # orange

    # Status & duration
    set -g tide_status_color_failure ${colors.base08}   # red
    set -g tide_cmd_duration_color ${colors.base0A}     # yellow

    # Time, shell level
    set -g tide_time_color ${colors.base04}    # dark foreground
    set -g tide_shlvl_color ${colors.base09}   # orange

    # Direnv
    set -g tide_direnv_color_denied ${colors.base08}   # red

    # Tool indicators
    set -g tide_aws_color ${colors.base09}            # orange
    set -g tide_bun_color ${colors.base0A}            # yellow
    set -g tide_crystal_color ${colors.base06}        # light foreground
    set -g tide_distrobox_color ${colors.base0E}      # purple
    set -g tide_docker_color ${colors.base0D}         # blue
    set -g tide_elixir_color ${colors.base0E}         # purple
    set -g tide_gcloud_color ${colors.base0D}         # blue
    set -g tide_go_color ${colors.base0C}             # cyan
    set -g tide_java_color ${colors.base09}           # orange
    set -g tide_kubectl_color ${colors.base0D}        # blue
    set -g tide_nix_shell_color ${colors.base0D}      # blue
    set -g tide_node_color ${colors.base0B}           # green
    set -g tide_os_color ${colors.base05}             # foreground
    set -g tide_php_color ${colors.base0D}            # blue
    set -g tide_private_mode_color ${colors.base06}   # light foreground
    set -g tide_pulumi_color ${colors.base0A}         # yellow
    set -g tide_python_color ${colors.base0C}         # cyan
    set -g tide_ruby_color ${colors.base08}           # red
    set -g tide_rustc_color ${colors.base0F}          # dark red
    set -g tide_terraform_color ${colors.base0E}      # purple
    set -g tide_toolbox_color ${colors.base0E}        # purple
    set -g tide_zig_color ${colors.base0A}            # yellow

    # Vi mode
    set -g tide_vi_mode_color_default ${colors.base04}   # dark foreground
    set -g tide_vi_mode_color_insert ${colors.base0B}    # green
    set -g tide_vi_mode_color_replace ${colors.base09}   # orange
    set -g tide_vi_mode_color_visual ${colors.base0E}    # purple

    # Fix low-contrast base16-fish colors using stylix palette
    set -g fish_color_param ${colors.base05}    # foreground
    set -g fish_color_end ${colors.base0E}      # purple

    # Per-host overrides
    ${config.dotfiles.shell.extraTideConfig}
  '';

  programs.fish = {
    enable = true;

    shellInit = ''
      ${config.dotfiles.shell.extraShellInit}
    '';

    interactiveShellInit = ''
      ${config.dotfiles.shell.extraInteractiveInit}
      set --global fish_key_bindings fish_default_key_bindings
    '';

    shellAbbrs = {
      k = "kubectl";
      wk = "watch -n 1 kubectl";
      kx = "kubectx";
      kn = "kubens";
      dig = "doggo";
      ping = "gping";
      lzd = "lazydocker";
      hf = "hyperfine";
      curl = "xh";
      ps = "procs";
      wc = "tokei";
      man = "tldr";
    };

    shellAliases = {
      cat = "bat";
      cd = "z";
      du = "dust";
      top = "btm";
      watch = "viddy";
      w = "viddy";
      traceroute = "trip";
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
    ];
  };

}
