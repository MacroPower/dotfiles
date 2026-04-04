{
  pkgs,
  lib,
  config,
  ...
}:

let
  inherit (config.lib.stylix) colors;

  inherit (import ../lib/nerdfonts.nix) icons;
  inherit (icons) pl plr;

  # Semantic color aliases (One Dark via Stylix base16)
  tmux = {
    bg = "282C34"; # onedark background, matches airline_c guibg
    fg = colors.base05;
    accent = colors.base0B; # green (airline section A)
    accentAlt = colors.base09; # prefix active
    active = colors.base0D; # blue (secondary)
    dim = colors.base04;
    dark = colors.base00;
    muted = colors.base03;
    surface = colors.base02;
    highlight = colors.base0A;
  };

  tmuxStatusContext = pkgs.writeShellApplication {
    name = "tmux-status-context";
    runtimeInputs = [ pkgs.yq-go ];
    text = ''
      # Source env vars forwarded by tmux update-environment
      eval "$(tmux show-environment -s 2>/dev/null | grep -v '^unset ')" || true

      out=""

      # Kubernetes context
      cfg="''${KUBECONFIG:-}"
      cfg="''${cfg%%:*}"
      cfg="''${cfg:-$HOME/.kube/config}"
      if [ -f "$cfg" ]; then
        ctx=$(yq '.current-context // ""' "$cfg" 2>/dev/null || true)
        if [ -n "$ctx" ]; then
          ns=$(yq '.contexts[] | select(.name == "'"$ctx"'") | .context.namespace // ""' "$cfg" 2>/dev/null || true)
          k8s="${icons.kubernetes} $ctx"
          [ -n "$ns" ] && k8s="$k8s($ns)"
          out="$out#[fg=#${colors.base0D}] $k8s "
        fi
      fi

      # Docker context (skip if empty or "default")
      dctx="''${DOCKER_CONTEXT:-}"
      if [ -n "$dctx" ] && [ "$dctx" != "default" ]; then
        out="$out#[fg=#${colors.base0D}] ${icons.docker} $dctx "
      fi

      # AWS
      if [ -n "''${AWS_PROFILE:-}" ]; then
        aws_seg="${icons.aws} ''${AWS_PROFILE}"
        [ -n "''${AWS_DEFAULT_REGION:-}" ] && aws_seg="$aws_seg@''${AWS_DEFAULT_REGION}"
        out="$out#[fg=#${colors.base09}] $aws_seg "
      fi

      # Azure
      if [ -n "''${AZURE_SUBSCRIPTION:-}" ]; then
        out="$out#[fg=#${colors.base0D}] ${icons.azure} ''${AZURE_SUBSCRIPTION} "
      fi

      # GCloud
      gcloud_account="''${CLOUDSDK_CORE_ACCOUNT:-}"
      gcloud_project="''${CLOUDSDK_CORE_PROJECT:-}"
      if [ -n "$gcloud_account" ] || [ -n "$gcloud_project" ]; then
        gc="${icons.gcloud} "
        [ -n "$gcloud_account" ] && gc="$gc$gcloud_account"
        [ -n "$gcloud_project" ] && gc="$gc($gcloud_project)"
        out="$out#[fg=#${colors.base0D}] $gc "
      fi

      printf '%s' "$out"
    '';
  };
in
{
  programs = {
    bat = {
      enable = true;
      config = {
        style = "numbers,changes,header";
        pager = "less -FR";
      };
    };

    eza = {
      enable = true;
      icons = "auto";
      colors = "auto";
      git = true;
      extraOptions = [
        "--group-directories-first"
        "--header"
        "--all"
      ];
    };

    fzf.enable = true;

    zoxide.enable = true;

    direnv = {
      enable = true;
      nix-direnv.enable = true;
      config.global = {
        hide_env_diff = true;
        warn_timeout = "30s";
      };
    };

    gh = {
      enable = true;
      extensions = [ ];
      settings = {
        git_protocol = "ssh";
        editor = "vim";
      };
    };

    bottom.enable = true;

    tmux = {
      enable = true;
      mouse = true;
      prefix = "C-Space";
      baseIndex = 1;
      historyLimit = 50000;
      escapeTime = 0;
      terminal = "tmux-256color";
      keyMode = "vi";
      aggressiveResize = true;
      plugins = with pkgs.tmuxPlugins; [
        sensible
        urlview
        vim-tmux-navigator
        {
          plugin = yank;
          extraConfig = "set -g @yank_selection_mouse 'clipboard'";
        }
        {
          plugin = tmux-thumbs;
          extraConfig = "set -g @thumbs-key F";
        }
        {
          plugin = extrakto;
          extraConfig = ''
            set -g @extrakto_key "e"
            set -g @extrakto_copy_key "tab"
            set -g @extrakto_insert_key "enter"
          '';
        }
        {
          plugin = resurrect;
          extraConfig = ''
            set -g @resurrect-capture-pane-contents 'on'
            set -g @resurrect-strategy-vim 'session'
            set -g @resurrect-processes '"~btm" "~lazydocker" "~gitui"'
          '';
        }
        {
          plugin = continuum;
          extraConfig = ''
            set -g @continuum-restore 'on'
            set -g @continuum-save-interval '10'
          '';
        }
      ];
      extraConfig = ''
        # --- Theme ---

        # Status bar layout
        set -g status-position bottom
        set -g status-interval 1
        set -g status-left-length 40
        set -g status-right-length 120
        set -g status-style "bg=#${tmux.bg},fg=#${tmux.fg}"
        set -g window-status-separator ""

        # Status left: session name (orange when prefix is active, blue otherwise)
        set -g status-left "${
          lib.concatStrings [
            "#[fg=#${tmux.dark}]"
            "#[bg=#{?client_prefix,##${tmux.accentAlt},##${tmux.accent}}]"
            "#[bold] #S "
          ]
        }"

        # Status right: context segments + user@host + time
        set -g status-right "${
          lib.concatStrings [
            "#[fg=#${tmux.dim},bg=#${tmux.bg}]"
            "#(tmux-status-context)"
            "#[fg=#${tmux.surface},bg=#${tmux.bg}]${plr}"
            "#[fg=#${tmux.fg},bg=#${tmux.surface}] #(whoami)@#h "
            "#[fg=#${tmux.accent},bg=#${tmux.surface}]${plr}"
            "#[fg=#${tmux.dark},bg=#${tmux.accent},bold] %I:%M:%S %p "
          ]
        }"

        # Window tabs
        # First window gets the session-to-bg arrow via window_start_flag
        set -g window-status-format "${
          lib.concatStrings [
            "#{?window_start_flag,"
            "#[fg=#{?client_prefix,##${tmux.accentAlt},##${tmux.accent}}]"
            "#[bg=#${tmux.bg}]#[nobold]${pl},}"
            "#[fg=#${tmux.dim},bg=#${tmux.bg}] #I #W#{?window_zoomed_flag, Z,}#{?@workmux_status, #{@workmux_status},} "
          ]
        }"
        set -g window-status-current-format "${
          lib.concatStrings [
            "#{?window_start_flag,"
            "#[fg=#{?client_prefix,##${tmux.accentAlt},##${tmux.accent}}]"
            "#[bg=#${tmux.active}]#[nobold]${pl},"
            "#[fg=#${tmux.bg}]#[bg=#${tmux.active}]${pl}}"
            "#[fg=#${tmux.dark},bg=#${tmux.active},bold] #I #W#{?window_zoomed_flag, Z,}#{?@workmux_status, #{@workmux_status},} "
            "#[fg=#${tmux.active},bg=#${tmux.bg}]${pl}"
          ]
        }"

        # Borders
        set -g pane-border-style "fg=#${tmux.muted}"
        set -g pane-active-border-style "fg=#${tmux.accent}"
        set -g popup-border-style "fg=#${tmux.muted}"
        set -g popup-border-lines rounded

        # Messages
        set -g message-style "bg=#${tmux.surface},fg=#${tmux.fg}"
        set -g message-command-style "bg=#${tmux.surface},fg=#${tmux.fg}"

        # Copy mode / selection
        set -g mode-style "bg=#${tmux.surface},fg=#${tmux.highlight}"
        set -g copy-mode-match-style "bg=#${tmux.surface},fg=#${tmux.highlight}"
        set -g copy-mode-current-match-style "bg=#${tmux.highlight},fg=#${tmux.dark}"

        # --- General settings ---

        # Forward session env vars for tmux-status-context script
        set -ga update-environment COLORTERM
        set -ga update-environment AWS_PROFILE
        set -ga update-environment AWS_DEFAULT_REGION
        set -ga update-environment AZURE_SUBSCRIPTION
        set -ga update-environment CLOUDSDK_CORE_ACCOUNT
        set -ga update-environment CLOUDSDK_CORE_PROJECT
        set -ga update-environment DOCKER_CONTEXT
        set -ga update-environment KUBECONFIG

        set -g allow-passthrough all
        set -g set-clipboard on
        set -g renumber-windows on
        set -g focus-events on
        set -g display-time 3000
        set -g display-panes-time 3000
        set -g set-titles on
        set -g set-titles-string "#S / #W"

        # --- Keybindings ---

        # Reload config
        bind r source-file ~/.config/tmux/tmux.conf \; display-message "Config reloaded"

        # Better split/window bindings (keep CWD)
        bind c new-window -c "#{pane_current_path}"
        bind d split-window -h -c "#{pane_current_path}"
        bind D split-window -v -c "#{pane_current_path}"

        # Pane navigation with vi keys
        bind h select-pane -L
        bind j select-pane -D
        bind k select-pane -U
        bind l select-pane -R

        # Pane resizing with vi keys
        bind -r H resize-pane -L 5
        bind -r J resize-pane -D 5
        bind -r K resize-pane -U 5
        bind -r L resize-pane -R 5

        # Pane swapping
        bind -r < swap-pane -U
        bind -r > swap-pane -D

        # Window reordering
        bind -r P swap-window -t -1\; select-window -t -1
        bind -r N swap-window -t +1\; select-window -t +1

        # Break/join panes
        bind B break-pane
        bind G choose-window 'join-pane -h -s "%%"'

        # Quick window switching (workmux last-agent is a superset of last-window)
        bind Tab run-shell "workmux last-agent"

        # Kill pane without confirmation
        bind x kill-pane

        # Toggle synchronize-panes
        bind Y set-window-option synchronize-panes\; display-message "sync #{?synchronize-panes,ON,OFF}"

        # Toggle mouse mode (for native terminal selection)
        bind m set-option -g mouse \; display-message "Mouse #{?mouse,ON,OFF}"

        # Move pane to window (complements B=break, G=grab)
        bind M command-prompt -p "send pane to window:" "join-pane -h -t '%%'"

        # Session switcher (sesh + fzf)
        bind S display-popup -E -w 60% -h 60% "sesh connect \"$(sesh list | fzf --reverse --no-sort --border-label ' sesh ' --prompt '> ')\""

        # Popup scratch terminal
        bind ` display-popup -E -w 80% -h 80% -d "#{pane_current_path}"

        # Popup TUI apps
        bind C-g display-popup -E -w 90% -h 90% -d "#{pane_current_path}" "gitui"
        bind D display-popup -E -w 90% -h 90% "lazydocker"

        # Workmux
        bind C-s display-popup -E -h 30 -w 100 "workmux dashboard"
        bind C-t run-shell "workmux sidebar"
        bind C-l run-shell "workmux last-done"

        # vi-style copy mode (yank plugin handles 'y' for clipboard integration)
        bind -T copy-mode-vi v send-keys -X begin-selection
        bind -T copy-mode-vi C-v send-keys -X rectangle-toggle
        bind -T copy-mode-vi Escape send-keys -X cancel

        # Incremental search in copy mode
        bind -T copy-mode-vi / command-prompt -i -I "#{pane_search_string}" -p "(search down)" "send -X search-forward-incremental \"%%%\""
        bind -T copy-mode-vi ? command-prompt -i -I "#{pane_search_string}" -p "(search up)" "send -X search-backward-incremental \"%%%\""

        # --- Terminal support ---

        # Ghostty
        set -g extended-keys on
        set -as terminal-features ',xterm-ghostty:RGB'
        set -as terminal-features ',xterm-ghostty:extkeys'
        set -as terminal-features ',xterm-ghostty:sync'
        set -as terminal-features ',xterm-ghostty:strike'
        set -as terminal-overrides ',xterm-ghostty:Ss=\E[%p1%d q:Se=\E[2 q'

        # Generic undercurl/colored underline support
        set -as terminal-overrides ',*:Smulx=\E[4::%p1%dm'
        set -as terminal-overrides ',*:Setulc=\E[58::2::%p1%{65536}%/%d::%p1%{256}%/%{255}%&%d::%p1%{255}%&%d%;m'
      '';
    };

    ripgrep = {
      enable = true;
      arguments = [
        "--smart-case"
        "--hidden"
        "--glob=!.git"
      ];
    };

    nix-index.enable = true;
    nix-index-database.comma.enable = true;

    nix-your-shell = {
      enable = true;
      nix-output-monitor.enable = true;
    };

    carapace = {
      enable = true;
      ignoreCase = true;
    };

    fd.enable = true;

    yazi = {
      enable = true;
      shellWrapperName = "y";
    };

    atuin = {
      enable = true;
      daemon.enable = true;
      flags = [ "--disable-up-arrow" ];
      settings = {
        update_check = false;
        style = "auto";
        keymap_mode = "auto";

        # Scope searches to the current host by default
        filter_mode = "host";
        # Auto-filter by git repo when inside one
        workspaces = true;
        # Search bar at the top (fzf-like)
        invert = true;

        # Keep history clean, exclude trivial commands
        history_filter = [
          "^cd$"
          "^ls$"
          "^clear$"
          "^exit$"
          "^pwd$"
        ];

        # Track subcommands for better `atuin stats`
        stats.common_subcommands = [
          "cargo"
          "docker"
          "git"
          "go"
          "kubectl"
          "nix"
          "npm"
          "systemctl"
          "tmux"
          "task"
          "dagger"
          "gh"
          "brew"
        ];
      };
    };

    jq.enable = true;
    trippy.enable = true;
    lazydocker.enable = true;
    difftastic.enable = true;
    docker-cli.enable = true;

    nh = {
      enable = true;
      flake = "${config.home.homeDirectory}/repos/dotfiles";
    };

    tealdeer = {
      enable = true;
      settings.updates.auto_update = true;
    };

    gitui = {
      enable = true;
      theme = builtins.readFile ../configs/gitui/theme.ron;
    };
  };

  home.packages = [
    tmuxStatusContext
  ]
  ++ (with pkgs; [
    go-task
    yq-go
    viddy
    doppler
    dagger
    nvd
    sesh

    nurl
    sops
    age
    dust
    hyperfine
    sd
    procs
    xh
    doggo
    dive
    cosign
    tokei
    gping
    photo-cli
    editorconfig-checker
    lefthook
    tfswitch
    tflint
  ]);
}
