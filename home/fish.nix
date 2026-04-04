{
  pkgs,
  lib,
  config,
  ...
}:

let
  inherit (config.lib.stylix) colors;
  inherit (import ../lib/nerdfonts.nix) icons;
in
{
  # Fix low-contrast base16-fish colors using stylix palette
  xdg.configFile."fish/conf.d/00-color-fixes.fish".text = ''
    set -g fish_color_param ${colors.base05}
    set -g fish_color_end ${colors.base0E}
  '';

  programs.starship = {
    enable = true;
    enableFishIntegration = false;

    settings = {
      add_newline = false;

      format = lib.concatStrings [
        "$os"
        "$container"
        "$directory"
        "$git_branch"
        "$git_state"
        "$git_status"
        "$git_metrics"
        "$cmd_duration"
      ];

      right_format = lib.concatStrings [
        "$status"
        "$shlvl"
        "$jobs"
        "$nix_shell"
        "$direnv"
        "$golang"
        "$nodejs"
        "$python"
        "$ruby"
        "$rust"
        "$terraform"
        "$java"
        "$zig"
        "$elixir"
        "$php"
        "$bun"
        "$crystal"
        "$lua"
        "$pulumi"
      ];

      character = {
        success_symbol = "[❯](bold #${colors.base0B})";
        error_symbol = "[❯](bold #${colors.base08})";
      };

      directory = {
        style = "bold #${colors.base0D}";
        truncation_symbol = "/.../";
        read_only = " ${icons.lock}";
      };

      git_branch = {
        style = "#${colors.base0B}";
        format = "[${icons.branch} $branch(:$remote_branch)]($style) ";
      };
      git_status = {
        format = lib.concatStrings [
          "[$ahead_behind](#${colors.base0C})"
          "[$conflicted](#${colors.base08})"
          "[$stashed](#${colors.base0E})"
          "[$deleted](#${colors.base08})"
          "[$renamed](#${colors.base0E})"
          "[$modified](#${colors.base0A})"
          "[$staged](#${colors.base0B})"
          "[$untracked](#${colors.base04})"
        ];
        ahead = "⇡$count ";
        behind = "⇣$count ";
        diverged = "⇕⇡$ahead_count⇣$behind_count ";
        conflicted = "=$count ";
        stashed = "\\$$count ";
        deleted = "✘$count ";
        renamed = "»$count ";
        modified = "*$count ";
        staged = "+$count ";
        untracked = "?$count ";
      };
      git_commit.tag_symbol = " ${icons.gitTag} ";
      git_state.style = "#${colors.base08}";
      git_metrics = {
        disabled = false;
        added_style = "#${colors.base0B}";
        deleted_style = "#${colors.base08}";
      };

      fill = {
        symbol = icons.middleDot;
        style = "#${colors.base02}";
      };

      status = {
        disabled = false;
        style = "#${colors.base08}";
        symbol = "${icons.error} ";
        format = " [$status]($style)";
      };

      cmd_duration = {
        style = "#${colors.base0A}";
        min_time = 2000;
        format = " [${icons.timer} $duration]($style)";
      };

      shlvl = {
        disabled = false;
        style = "#${colors.base09}";
        threshold = 2;
        symbol = "${icons.terminal} ";
        format = " [$symbol$shlvl]($style)";
      };

      jobs = {
        style = "#${colors.base0D}";
        symbol = "${icons.gear} ";
        format = " [$symbol$number]($style)";
      };

      # Tool indicators (nerd font symbols, no "via" prefix)
      bun = {
        style = "#${colors.base0A}";
        symbol = "${icons.bun} ";
        format = " [$symbol($version)]($style)";
      };
      crystal = {
        style = "#${colors.base06}";
        symbol = "${icons.crystal} ";
        format = " [$symbol($version)]($style)";
      };
      direnv = {
        disabled = false;
        style = "#${colors.base0B}";
        symbol = "${icons.direnv} ";
        denied_msg = "[denied](#${colors.base08})";
        format = " [$symbol$loaded$denied]($style)";
      };
      elixir = {
        style = "#${colors.base0E}";
        symbol = "${icons.elixir} ";
        format = " [$symbol($version)]($style)";
      };
      golang = {
        style = "#${colors.base0C}";
        symbol = "${icons.golang} ";
        format = " [$symbol($version)]($style)";
      };
      java = {
        style = "#${colors.base09}";
        symbol = "${icons.java} ";
        format = " [$symbol($version)]($style)";
      };
      lua = {
        style = "#${colors.base0D}";
        symbol = "${icons.lua} ";
        format = " [$symbol($version)]($style)";
      };
      nix_shell = {
        style = "#${colors.base0D}";
        symbol = "${icons.nix} ";
        format = " [$symbol($name)]($style)";
      };
      nodejs = {
        style = "#${colors.base0B}";
        symbol = "${icons.nodejs} ";
        format = " [$symbol($version)]($style)";
      };
      pulumi = {
        style = "#${colors.base0A}";
        symbol = "${icons.pulumi} ";
        format = " [$symbol($stack)]($style)";
      };
      php = {
        style = "#${colors.base0D}";
        symbol = "${icons.php} ";
        format = " [$symbol($version)]($style)";
      };
      python = {
        style = "#${colors.base0C}";
        symbol = "${icons.python} ";
        format = " [$symbol($version)]($style)";
      };
      ruby = {
        style = "#${colors.base08}";
        symbol = "${icons.ruby} ";
        format = " [$symbol($version)]($style)";
      };
      rust = {
        style = "#${colors.base0F}";
        symbol = "${icons.rust} ";
        format = " [$symbol($version)]($style)";
      };
      terraform = {
        style = "#${colors.base0E}";
        symbol = "${icons.terraform} ";
        format = " [$symbol$workspace]($style)";
      };
      zig = {
        style = "#${colors.base0A}";
        symbol = "${icons.zig} ";
        format = " [$symbol($version)]($style)";
      };

      # Additional nerd font symbol modules
      buf = {
        style = "#${colors.base0D}";
        symbol = "${icons.buf} ";
        format = "[$symbol($version )]($style)";
      };
      c = {
        style = "#${colors.base0D}";
        symbol = "${icons.c} ";
        format = "[$symbol($version )]($style)";
      };
      cmake = {
        style = "#${colors.base08}";
        symbol = "${icons.cmake} ";
        format = "[$symbol($version )]($style)";
      };
      conda = {
        style = "#${colors.base0B}";
        symbol = "${icons.conda} ";
        format = "[$symbol($version )]($style)";
      };
      cpp = {
        style = "#${colors.base0D}";
        symbol = "${icons.cpp} ";
        format = "[$symbol($version )]($style)";
      };
      dart = {
        style = "#${colors.base0C}";
        symbol = "${icons.dart} ";
        format = "[$symbol($version )]($style)";
      };
      deno = {
        style = "#${colors.base06}";
        symbol = "${icons.deno} ";
        format = "[$symbol($version )]($style)";
      };
      elm = {
        style = "#${colors.base0C}";
        symbol = "${icons.elm} ";
        format = "[$symbol($version )]($style)";
      };
      fennel = {
        style = "#${colors.base0A}";
        symbol = "${icons.fennel} ";
        format = "[$symbol($version )]($style)";
      };
      fortran = {
        style = "#${colors.base0E}";
        symbol = "${icons.fortran} ";
        format = "[$symbol($version )]($style)";
      };
      fossil_branch = {
        style = "#${colors.base0B}";
        symbol = "${icons.branch} ";
        format = "[$symbol$branch]($style) ";
      };
      gradle = {
        style = "#${colors.base0B}";
        symbol = "${icons.gradle} ";
        format = "[$symbol($version )]($style)";
      };
      guix_shell = {
        style = "#${colors.base0A}";
        symbol = "${icons.guixShell} ";
        format = "[$symbol$state( \\($name\\) )]($style)";
      };
      haskell = {
        style = "#${colors.base0E}";
        symbol = "${icons.haskell} ";
        format = "[$symbol($version )]($style)";
      };
      haxe = {
        style = "#${colors.base09}";
        symbol = "${icons.haxe} ";
        format = "[$symbol($version )]($style)";
      };
      hg_branch = {
        style = "#${colors.base04}";
        symbol = "${icons.branch} ";
        format = "[$symbol$branch]($style) ";
      };
      julia = {
        style = "#${colors.base0E}";
        symbol = "${icons.julia} ";
        format = "[$symbol($version )]($style)";
      };
      kotlin = {
        style = "#${colors.base09}";
        symbol = "${icons.kotlin} ";
        format = "[$symbol($version )]($style)";
      };
      memory_usage = {
        style = "#${colors.base08}";
        symbol = "${icons.memory} ";
        format = "[$symbol$ram]($style) ";
      };
      meson = {
        style = "#${colors.base0C}";
        symbol = "${icons.meson} ";
        format = "[$symbol($version )]($style)";
      };
      nim = {
        style = "#${colors.base0A}";
        symbol = "${icons.nim} ";
        format = "[$symbol($version )]($style)";
      };
      ocaml = {
        style = "#${colors.base09}";
        symbol = "${icons.ocaml} ";
        format = "[$symbol($version )]($style)";
      };
      package = {
        style = "#${colors.base09}";
        symbol = "${icons.package} ";
        format = "[$symbol($version )]($style)";
      };
      perl = {
        style = "#${colors.base0D}";
        symbol = "${icons.perl} ";
        format = "[$symbol($version )]($style)";
      };
      pijul_channel = {
        style = "#${colors.base0B}";
        symbol = "${icons.branch} ";
        format = "[$symbol$channel]($style) ";
      };
      pixi = {
        style = "#${colors.base0B}";
        symbol = "${icons.package} ";
        format = "[$symbol($version )]($style)";
      };
      rlang = {
        style = "#${colors.base0D}";
        symbol = "${icons.rlang} ";
        format = "[$symbol($version )]($style)";
      };
      scala = {
        style = "#${colors.base08}";
        symbol = "${icons.scala} ";
        format = "[$symbol($version )]($style)";
      };
      swift = {
        style = "#${colors.base09}";
        symbol = "${icons.swift} ";
        format = "[$symbol($version )]($style)";
      };
      xmake = {
        style = "#${colors.base0B}";
        symbol = "${icons.xmake} ";
        format = "[$symbol($version )]($style)";
      };

      container = {
        style = "#${colors.base05}";
        symbol = "${icons.container} ";
        format = "[$symbol]($style)";
      };

      # OS detection symbols
      os = {
        disabled = false;
        style = "#${colors.base05}";
      };
      os.symbols = builtins.mapAttrs (_: v: "${v} ") icons.os;

    };
  };

  programs.fish = {
    enable = true;

    shellInit = ''
      ${config.dotfiles.shell.extraShellInit}
    '';

    interactiveShellInit = ''
      # Auto-start tmux for interactive shells (skip if already in tmux,
      # inside an IDE terminal, or tmux isn't available)
      if status is-interactive; and command -q tmux; and not set -q TMUX; and not set -q ZED_TERM; and test -z "$SSH_CONNECTION"
        exec tmux new-session -c "$HOME" \; set-option destroy-unattached on
      end

      ${config.dotfiles.shell.extraInteractiveInit}
      set --global fish_key_bindings fish_default_key_bindings

      # Initialize starship (defines helper functions like enable_transience)
      starship init fish | source

      # Width-aware prompt: joins left+right with fill dots on one line when
      # they fit, or stacks them on two lines when the terminal is too narrow.
      function fish_prompt
        set -l exit_code $status
        set -l pipe_status $pipestatus

        if contains -- --final-rendering $argv; or test "$TRANSIENT" = "1"
          if test "$TRANSIENT" = "1"
            set -g TRANSIENT 0
            printf \e\[0J
          end
          set_color --bold ${colors.base0B}
          printf '❯ '
          set_color normal
          return
        end

        set -l keymap insert
        switch "$fish_key_bindings"
          case fish_hybrid_key_bindings fish_vi_key_bindings
            set keymap "$fish_bind_mode"
        end

        set -l cmd_args \
          --terminal-width="$COLUMNS" \
          --status=$exit_code \
          --pipestatus="$pipe_status" \
          --cmd-duration="$CMD_DURATION" \
          --jobs=(count (jobs -p 2>/dev/null)) \
          --keymap=$keymap

        set -l left (starship prompt $cmd_args 2>/dev/null | string collect)
        set -l right (starship prompt --right $cmd_args 2>/dev/null | string collect)
        set -l left_w (string length --visible -- "$left")
        set -l right_w (string length --visible -- "$right")

        if test (math "$left_w + $right_w + 1") -le $COLUMNS
          set -l fill_len (math "$COLUMNS - $left_w - $right_w")
          printf '%s%s%s%s%s\n' "$left" (set_color ${colors.base02}) (string repeat -n $fill_len -- '${icons.middleDot}') (set_color normal) "$right"
        else
          printf '%s\n%s\n' "$left" (string trim -l -- "$right")
        end

        set_color --bold (test $exit_code -eq 0 && echo ${colors.base0B} || echo ${colors.base08})
        printf '❯ '
        set_color normal
      end

      function fish_right_prompt; end

      enable_transience
    '';

    shellAbbrs = {
      g = "git";
      gs = "git status";
      wm = "workmux";
      tf = "tofu";
      t = "task";
      tg = "task -g";
      k = "kubectl";
      kd = "kubectl describe";
      kg = "kubectl get";
      kl = "kubectl logs";
      wk = "watch -n 1 kubectl";
      wkd = "watch -n 1 kubectl describe";
      wkg = "watch -n 1 kubectl get";
      wkl = "kubectl logs --follow";
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
      nc = "ncat";
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
    ];
  };
}
