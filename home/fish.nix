{
  pkgs,
  lib,
  config,
  ...
}:

let
  inherit (config.lib.stylix) colors;
  # Produce a Nerd Font glyph from a hex codepoint via JSON unicode escape.
  # Use nf for BMP (4-digit) codepoints; nf2 for supplementary (surrogate pair).
  nf = code: builtins.fromJSON ''"\u${code}"'';
  nf2 = hi: lo: builtins.fromJSON ''"\u${hi}\u${lo}"'';
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
        "$kubernetes"
        "$docker_context"
        "$nix_shell"
        "$direnv"
        "$golang"
        "$nodejs"
        "$python"
        "$ruby"
        "$rust"
        "$terraform"
        "$aws"
        "$azure"
        "$gcloud"
        "$java"
        "$zig"
        "$elixir"
        "$php"
        "$bun"
        "$crystal"
        "$lua"
        "$pulumi"
        "$username"
        "$hostname"
        "$time"
      ];

      character = {
        success_symbol = "[❯](bold #${colors.base0B})";
        error_symbol = "[❯](bold #${colors.base08})";
      };

      directory = {
        style = "bold #${colors.base0D}";
        truncation_symbol = "/.../";
        read_only = " ${nf2 "DB80" "DF3E"}";
      };

      git_branch = {
        style = "#${colors.base0B}";
        format = "[${nf "f418"} $branch(:$remote_branch)]($style) ";
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
      git_commit.tag_symbol = " ${nf "f412"} ";
      git_state.style = "#${colors.base08}";
      git_metrics = {
        disabled = false;
        added_style = "#${colors.base0B}";
        deleted_style = "#${colors.base08}";
      };

      fill = {
        symbol = nf "00b7";
        style = "#${colors.base02}";
      };

      status = {
        disabled = false;
        style = "#${colors.base08}";
        symbol = "${nf "f467"} ";
        format = " [$status]($style)";
      };

      cmd_duration = {
        style = "#${colors.base0A}";
        min_time = 2000;
        format = " [${nf2 "DB86" "DD9F"} $duration]($style)";
      };

      shlvl = {
        disabled = false;
        style = "#${colors.base09}";
        threshold = 2;
        symbol = "${nf "f120"} ";
        format = " [$symbol$shlvl]($style)";
      };

      jobs = {
        style = "#${colors.base0D}";
        symbol = "${nf "f013"} ";
        format = " [$symbol$number]($style)";
      };

      time = {
        disabled = false;
        style = "#${colors.base04}";
        format = " [$time]($style)";
        time_format = "%r";
      };

      username = {
        style_user = "#${colors.base09}";
        style_root = "bold #${colors.base08}";
        show_always = false;
        format = " [$user]($style)";
      };

      hostname = {
        style = "#${colors.base09}";
        ssh_only = true;
        ssh_symbol = "${nf "eb01"} ";
        format = "[@$hostname]($style)";
      };

      # Tool indicators (nerd font symbols, no "via" prefix)
      aws = {
        style = "#${colors.base09}";
        symbol = "${nf "e33d"} ";
        format = " [$symbol($profile )(@$region)]($style)";
      };
      azure = {
        disabled = false;
        style = "#${colors.base0D}";
        symbol = "${nf "ebd8"} ";
        format = " [$symbol($subscription)]($style)";
      };
      bun = {
        style = "#${colors.base0A}";
        symbol = "${nf "e76f"} ";
        format = " [$symbol($version)]($style)";
      };
      crystal = {
        style = "#${colors.base06}";
        symbol = "${nf "e62f"} ";
        format = " [$symbol($version)]($style)";
      };
      direnv = {
        disabled = false;
        style = "#${colors.base0B}";
        symbol = "${nf "f07c"} ";
        denied_msg = "[denied](#${colors.base08})";
        format = " [$symbol$loaded$denied]($style)";
      };
      docker_context = {
        style = "#${colors.base0D}";
        symbol = "${nf "f308"} ";
        format = " [$symbol$context]($style)";
      };
      elixir = {
        style = "#${colors.base0E}";
        symbol = "${nf "e62d"} ";
        format = " [$symbol($version)]($style)";
      };
      gcloud = {
        style = "#${colors.base0D}";
        symbol = "${nf "e7f1"} ";
        format = " [$symbol$account(@$domain)(\\($project\\))]($style)";
      };
      golang = {
        style = "#${colors.base0C}";
        symbol = "${nf "e627"} ";
        format = " [$symbol($version)]($style)";
      };
      java = {
        style = "#${colors.base09}";
        symbol = "${nf "e256"} ";
        format = " [$symbol($version)]($style)";
      };
      kubernetes = {
        disabled = false;
        style = "#${colors.base0D}";
        symbol = "${nf "2638"} ";
        format = " [$symbol$context(\\($namespace\\))]($style)";
      };
      lua = {
        style = "#${colors.base0D}";
        symbol = "${nf "e620"} ";
        format = " [$symbol($version)]($style)";
      };
      nix_shell = {
        style = "#${colors.base0D}";
        symbol = "${nf "f313"} ";
        format = " [$symbol($name)]($style)";
      };
      nodejs = {
        style = "#${colors.base0B}";
        symbol = "${nf "e718"} ";
        format = " [$symbol($version)]($style)";
      };
      pulumi = {
        style = "#${colors.base0A}";
        symbol = "${nf "f1b2"} ";
        format = " [$symbol($stack)]($style)";
      };
      php = {
        style = "#${colors.base0D}";
        symbol = "${nf "e608"} ";
        format = " [$symbol($version)]($style)";
      };
      python = {
        style = "#${colors.base0C}";
        symbol = "${nf "e235"} ";
        format = " [$symbol($version)]($style)";
      };
      ruby = {
        style = "#${colors.base08}";
        symbol = "${nf "e791"} ";
        format = " [$symbol($version)]($style)";
      };
      rust = {
        style = "#${colors.base0F}";
        symbol = "${nf2 "DB85" "DE17"} ";
        format = " [$symbol($version)]($style)";
      };
      terraform = {
        style = "#${colors.base0E}";
        symbol = "${nf2 "db84" "dc62"} ";
        format = " [$symbol$workspace]($style)";
      };
      zig = {
        style = "#${colors.base0A}";
        symbol = "${nf "e6a9"} ";
        format = " [$symbol($version)]($style)";
      };

      # Additional nerd font symbol modules
      buf = {
        style = "#${colors.base0D}";
        symbol = "${nf "f49d"} ";
        format = "[$symbol($version )]($style)";
      };
      c = {
        style = "#${colors.base0D}";
        symbol = "${nf "e61e"} ";
        format = "[$symbol($version )]($style)";
      };
      cmake = {
        style = "#${colors.base08}";
        symbol = "${nf "e794"} ";
        format = "[$symbol($version )]($style)";
      };
      conda = {
        style = "#${colors.base0B}";
        symbol = "${nf "f10c"} ";
        format = "[$symbol($version )]($style)";
      };
      cpp = {
        style = "#${colors.base0D}";
        symbol = "${nf "e61d"} ";
        format = "[$symbol($version )]($style)";
      };
      dart = {
        style = "#${colors.base0C}";
        symbol = "${nf "e798"} ";
        format = "[$symbol($version )]($style)";
      };
      deno = {
        style = "#${colors.base06}";
        symbol = "${nf "e7c0"} ";
        format = "[$symbol($version )]($style)";
      };
      elm = {
        style = "#${colors.base0C}";
        symbol = "${nf "e62c"} ";
        format = "[$symbol($version )]($style)";
      };
      fennel = {
        style = "#${colors.base0A}";
        symbol = "${nf "e6af"} ";
        format = "[$symbol($version )]($style)";
      };
      fortran = {
        style = "#${colors.base0E}";
        symbol = "${nf "e7de"} ";
        format = "[$symbol($version )]($style)";
      };
      fossil_branch = {
        style = "#${colors.base0B}";
        symbol = "${nf "f418"} ";
        format = "[$symbol$branch]($style) ";
      };
      gradle = {
        style = "#${colors.base0B}";
        symbol = "${nf "e660"} ";
        format = "[$symbol($version )]($style)";
      };
      guix_shell = {
        style = "#${colors.base0A}";
        symbol = "${nf "f325"} ";
        format = "[$symbol$state( \\($name\\) )]($style)";
      };
      haskell = {
        style = "#${colors.base0E}";
        symbol = "${nf "e777"} ";
        format = "[$symbol($version )]($style)";
      };
      haxe = {
        style = "#${colors.base09}";
        symbol = "${nf "e666"} ";
        format = "[$symbol($version )]($style)";
      };
      hg_branch = {
        style = "#${colors.base04}";
        symbol = "${nf "f418"} ";
        format = "[$symbol$branch]($style) ";
      };
      julia = {
        style = "#${colors.base0E}";
        symbol = "${nf "e624"} ";
        format = "[$symbol($version )]($style)";
      };
      kotlin = {
        style = "#${colors.base09}";
        symbol = "${nf "e634"} ";
        format = "[$symbol($version )]($style)";
      };
      memory_usage = {
        style = "#${colors.base08}";
        symbol = "${nf2 "DB80" "DF5B"} ";
        format = "[$symbol$ram]($style) ";
      };
      meson = {
        style = "#${colors.base0C}";
        symbol = "${nf2 "DB81" "DD37"} ";
        format = "[$symbol($version )]($style)";
      };
      nim = {
        style = "#${colors.base0A}";
        symbol = "${nf2 "DB80" "DDA5"} ";
        format = "[$symbol($version )]($style)";
      };
      ocaml = {
        style = "#${colors.base09}";
        symbol = "${nf "e67a"} ";
        format = "[$symbol($version )]($style)";
      };
      package = {
        style = "#${colors.base09}";
        symbol = "${nf2 "DB80" "DFD7"} ";
        format = "[$symbol($version )]($style)";
      };
      perl = {
        style = "#${colors.base0D}";
        symbol = "${nf "e67e"} ";
        format = "[$symbol($version )]($style)";
      };
      pijul_channel = {
        style = "#${colors.base0B}";
        symbol = "${nf "f418"} ";
        format = "[$symbol$channel]($style) ";
      };
      pixi = {
        style = "#${colors.base0B}";
        symbol = "${nf2 "DB80" "DFD7"} ";
        format = "[$symbol($version )]($style)";
      };
      rlang = {
        style = "#${colors.base0D}";
        symbol = "${nf2 "DB81" "DFD4"} ";
        format = "[$symbol($version )]($style)";
      };
      scala = {
        style = "#${colors.base08}";
        symbol = "${nf "e737"} ";
        format = "[$symbol($version )]($style)";
      };
      swift = {
        style = "#${colors.base09}";
        symbol = "${nf "e755"} ";
        format = "[$symbol($version )]($style)";
      };
      xmake = {
        style = "#${colors.base0B}";
        symbol = "${nf "e794"} ";
        format = "[$symbol($version )]($style)";
      };

      container = {
        style = "#${colors.base05}";
        symbol = "${nf "f4b7"} ";
        format = "[$symbol]($style)";
      };

      # OS detection symbols
      os = {
        disabled = false;
        style = "#${colors.base05}";
      };
      os.symbols = {
        Alpaquita = "${nf "eaa2"} ";
        Alpine = "${nf "f300"} ";
        AlmaLinux = "${nf "f31d"} ";
        Amazon = "${nf "f270"} ";
        Android = "${nf "f17b"} ";
        AOSC = "${nf "f301"} ";
        Arch = "${nf "f303"} ";
        Artix = "${nf "f31f"} ";
        CachyOS = "${nf "f303"} ";
        CentOS = "${nf "f304"} ";
        Debian = "${nf "f306"} ";
        DragonFly = "${nf "e28e"} ";
        Elementary = "${nf "f309"} ";
        Emscripten = "${nf "f205"} ";
        EndeavourOS = "${nf "f197"} ";
        Fedora = "${nf "f30a"} ";
        FreeBSD = "${nf "f30c"} ";
        Garuda = "${nf2 "DB81" "DED3"} ";
        Gentoo = "${nf "f30d"} ";
        HardenedBSD = "${nf2 "DB81" "DF8C"} ";
        Illumos = "${nf2 "DB80" "DE38"} ";
        Ios = "${nf2 "DB80" "DC37"} ";
        Kali = "${nf "f327"} ";
        Linux = "${nf "f31a"} ";
        Mabox = "${nf "eb29"} ";
        Macos = "${nf "f302"} ";
        Manjaro = "${nf "f312"} ";
        Mariner = "${nf "f1cd"} ";
        MidnightBSD = "${nf "f186"} ";
        Mint = "${nf "f30e"} ";
        NetBSD = "${nf "f024"} ";
        NixOS = "${nf "f313"} ";
        Nobara = "${nf "f380"} ";
        OpenBSD = "${nf2 "DB80" "DE3A"} ";
        openSUSE = "${nf "f314"} ";
        OracleLinux = "${nf2 "DB80" "DF37"} ";
        Pop = "${nf "f32a"} ";
        Raspbian = "${nf "f315"} ";
        Redhat = "${nf "f316"} ";
        RedHatEnterprise = "${nf "f316"} ";
        RockyLinux = "${nf "f32b"} ";
        Redox = "${nf2 "DB80" "DC18"} ";
        Solus = "${nf2 "DB82" "DC33"} ";
        SUSE = "${nf "f314"} ";
        Ubuntu = "${nf "f31b"} ";
        Unknown = "${nf "f22d"} ";
        Void = "${nf "f32e"} ";
        Windows = "${nf2 "DB80" "DF72"} ";
        Zorin = "${nf "f32f"} ";
      };

    };
  };

  programs.fish = {
    enable = true;

    shellInit = ''
      ${config.dotfiles.shell.extraShellInit}
    '';

    interactiveShellInit = ''
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
          printf '%s%s%s%s%s\n' "$left" (set_color ${colors.base02}) (string repeat -n $fill_len -- '${nf "00b7"}') (set_color normal) "$right"
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
      tf = "tofu";
      t = "go-task";
      k = "kubectl";
      kd = "kubectl describe";
      kg = "kubectl get";
      kl = "kubectl logs";
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
