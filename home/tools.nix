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

  tmuxSeshPicker = pkgs.writeShellApplication {
    name = "tmux-sesh-picker";
    runtimeInputs = with pkgs; [
      sesh
      fzf
    ];
    text = ''
      selected=$(sesh list | fzf \
        --reverse --no-sort \
        --border-label ' sesh ' \
        --prompt '> ' \
        --header 'sessions' \
        --preview 'tmux capture-pane -ep -t {} 2>/dev/null || echo "(no preview)"' \
        --preview-window 'right:50%:wrap')
      [ -n "$selected" ] && sesh connect "$selected"
    '';
  };

  tmuxFilePicker = pkgs.writeShellApplication {
    name = "tmux-file-picker";
    excludeShellChecks = [ "SC2086" ];
    runtimeInputs = with pkgs; [
      fzf
      fd
      bat
      tree
      zoxide
      coreutils-full
      git
    ];
    text = builtins.readFile ../scripts/tmux-file-picker.sh;
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

  # Shared tmux binding definitions -- single source of truth for both
  # direct keybindings and which-key menu items.
  #
  # Fields:
  #   name      - display name (present = appears in which-key)
  #   key       - key shared by direct bind and which-key
  #   cmd       - tmux command string
  #   repeat    - (optional) -r flag for direct binding
  #   transient - (optional) transient which-key menu item
  #   table     - (optional) "root" for -n, or key table name for -T
  b = {
    # Top-level
    commandPrompt = {
      name = "Command prompt";
      key = ":";
      cmd = "command-prompt";
    };
    lastWindow = {
      name = "Last window";
      key = "tab";
      cmd = ''run-shell "workmux last-agent"'';
    };
    reloadConfig = {
      name = "Reload config";
      key = "r";
      cmd = ''source-file ~/.config/tmux/tmux.conf \; display-message "Config reloaded"'';
    };
    clearScreen = {
      name = "Clear screen";
      key = "C-l";
      cmd = ''send-keys C-l \; run-shell "sleep 0.1" \; clear-history'';
    };
    listKeys = {
      name = "+Keys";
      key = "?";
      cmd = "list-keys -N";
    };

    # Windows
    newWindow = {
      name = "New window";
      key = "c";
      cmd = ''new-window -c "#{pane_current_path}"'';
    };
    splitH = {
      name = "Split horizontal";
      key = "d";
      cmd = ''split-window -h -c "#{pane_current_path}"'';
    };
    splitV = {
      name = "Split vertical";
      key = "v";
      cmd = ''split-window -v -c "#{pane_current_path}"'';
    };
    swapWindowPrev = {
      name = "Swap prev";
      key = "[";
      cmd = "swap-window -t -1 \\; select-window -t -1";
      repeat = true;
    };
    swapWindowNext = {
      name = "Swap next";
      key = "]";
      cmd = "swap-window -t +1 \\; select-window -t +1";
      repeat = true;
    };
    renameWindow = {
      name = "Rename";
      key = "C-r";
      cmd = ''command-prompt -I "#W" "rename-window -- \"%%\""'';
    };
    killWindow = {
      name = "Kill";
      key = "C-x";
      cmd = ''confirm -p "Kill window #W? (y/n)" kill-window'';
    };

    # Layouts
    layoutNext = {
      name = "Next";
      key = "=";
      cmd = "next-layout";
      transient = true;
    };
    layoutTiled = {
      name = "Tiled";
      key = "t";
      cmd = "select-layout tiled";
    };
    layoutH = {
      name = "Horizontal";
      key = "-";
      cmd = "select-layout even-horizontal";
    };
    layoutV = {
      name = "Vertical";
      key = "C-v";
      cmd = "select-layout even-vertical";
    };

    # Panes
    paneLeft = {
      name = "Left";
      key = "h";
      cmd = "select-pane -L";
    };
    paneDown = {
      name = "Down";
      key = "j";
      cmd = "select-pane -D";
    };
    paneUp = {
      name = "Up";
      key = "k";
      cmd = "select-pane -U";
    };
    paneRight = {
      name = "Right";
      key = "l";
      cmd = "select-pane -R";
    };
    resizeLeft = {
      name = "Left";
      key = "H";
      cmd = "resize-pane -L 5";
      repeat = true;
      transient = true;
    };
    resizeDown = {
      name = "Down";
      key = "J";
      cmd = "resize-pane -D 5";
      repeat = true;
      transient = true;
    };
    resizeUp = {
      name = "Up";
      key = "K";
      cmd = "resize-pane -U 5";
      repeat = true;
      transient = true;
    };
    resizeRight = {
      name = "Right";
      key = "L";
      cmd = "resize-pane -R 5";
      repeat = true;
      transient = true;
    };
    swapPanePrev = {
      name = "Swap prev";
      key = "[";
      cmd = "swap-pane -U";
      repeat = true;
    };
    swapPaneNext = {
      name = "Swap next";
      key = "]";
      cmd = "swap-pane -D";
      repeat = true;
    };
    zoomPane = {
      name = "Zoom";
      key = "z";
      cmd = "resize-pane -Z";
    };
    breakPane = {
      name = "Break pane";
      key = "C-b";
      cmd = "break-pane";
    };
    grabPane = {
      name = "Grab/join pane";
      key = "g";
      cmd = ''choose-window "join-pane -h -s \"%%\""'';
    };
    movePane = {
      name = "Send to window";
      key = "m";
      cmd = ''command-prompt -p "send pane to:" "join-pane -h -t \"%%\""'';
    };
    killPane = {
      name = "Kill pane";
      key = "x";
      cmd = "kill-pane";
    };

    # Sessions
    seshPicker = {
      name = "Switcher (sesh)";
      key = "s";
      cmd = ''display-popup -E -T " sesh " -w 70% -h 70% tmux-sesh-picker'';
    };
    sessionTree = {
      name = "Session tree";
      key = "w";
      cmd = "choose-tree -Zs";
    };
    sessionRename = {
      name = "Rename";
      key = "n";
      cmd = ''command-prompt -I "#S" "rename-session -- \"%%\""'';
    };
    newSession = {
      name = "New";
      key = "C-n";
      cmd = "new-session -c ~";
    };
    detach = {
      name = "Detach";
      key = "q";
      cmd = "detach";
    };

    # Popups
    scratchPopup = {
      name = "Scratch terminal";
      key = "\`";
      cmd = ''display-popup -E -T " scratch " -w 80% -h 80% -d "#{pane_current_path}"'';
    };
    gituiPopup = {
      name = "gitui";
      key = "C-g";
      cmd = ''display-popup -E -T " gitui " -w 90% -h 90% -d "#{pane_current_path}" gitui'';
    };
    lazydockerPopup = {
      name = "lazydocker";
      key = "C-d";
      cmd = ''display-popup -E -T " lazydocker " -w 90% -h 90% lazydocker'';
    };

    # Workmux
    workmuxDash = {
      name = "Dashboard";
      key = "C-s";
      cmd = ''display-popup -E -T " workmux " -h 30 -w 100 "workmux dashboard"'';
    };
    workmuxSidebar = {
      name = "Sidebar";
      key = "C-t";
      cmd = ''run-shell "workmux sidebar"'';
    };
    workmuxLast = {
      name = "Last done";
      key = "C-l";
      cmd = ''run-shell "workmux last-done"'';
    };

    # Toggles
    toggleStatus = {
      name = "Status bar";
      key = "b";
      cmd = "set-option -g status";
    };
    toggleSync = {
      name = "Sync panes";
      key = "y";
      cmd = ''set-window-option synchronize-panes \; display-message "sync #{?synchronize-panes,ON,OFF}"'';
    };
    toggleMouse = {
      name = "Mouse";
      key = "m";
      cmd = ''set -g mouse \; display-message "Mouse #{?mouse,ON,OFF}"'';
    };

    # Direct-only (no name -> no which-key entry)
    altWindow1 = {
      key = "M-1";
      cmd = "select-window -t 1";
      table = "root";
    };
    altWindow2 = {
      key = "M-2";
      cmd = "select-window -t 2";
      table = "root";
    };
    altWindow3 = {
      key = "M-3";
      cmd = "select-window -t 3";
      table = "root";
    };
    altWindow4 = {
      key = "M-4";
      cmd = "select-window -t 4";
      table = "root";
    };
    altWindow5 = {
      key = "M-5";
      cmd = "select-window -t 5";
      table = "root";
    };
    altWindow6 = {
      key = "M-6";
      cmd = "select-window -t 6";
      table = "root";
    };
    altWindow7 = {
      key = "M-7";
      cmd = "select-window -t 7";
      table = "root";
    };
    altWindow8 = {
      key = "M-8";
      cmd = "select-window -t 8";
      table = "root";
    };
    altWindow9 = {
      key = "M-9";
      cmd = "select-window -t 9";
      table = "root";
    };
    altSplitH = {
      key = "M-d";
      cmd = ''split-window -h -c "#{pane_current_path}"'';
      table = "root";
    };
    altSplitV = {
      key = "M-v";
      cmd = ''split-window -v -c "#{pane_current_path}"'';
      table = "root";
    };
    altNewWindow = {
      key = "M-c";
      cmd = ''new-window -c "#{pane_current_path}"'';
      table = "root";
    };
    altZoom = {
      key = "M-z";
      cmd = "resize-pane -Z";
      table = "root";
    };
    altLastWindow = {
      key = "M-Tab";
      cmd = ''run-shell "workmux last-agent"'';
      table = "root";
    };
    altScratch = {
      key = "M-o";
      cmd = ''display-popup -E -T " scratch " -w 80% -h 80% -d "#{pane_current_path}"'';
      table = "root";
    };
    altSeshPicker = {
      key = "M-f";
      cmd = ''display-popup -E -T " sesh " -w 70% -h 70% tmux-sesh-picker'';
      table = "root";
    };
    altFilePicker = {
      key = "M-p";
      cmd = ''display-popup -E -T " files " -w 80% -h 80% -d "#{pane_current_path}" tmux-file-picker --git-root'';
      table = "root";
    };
    altPrevWindow = {
      key = "M-h";
      cmd = "previous-window";
      table = "root";
    };
    altNextWindow = {
      key = "M-l";
      cmd = "next-window";
      table = "root";
    };
  };

  # Which-key menu hierarchy -- references b.* entries above
  whichKeyMenus = [
    b.commandPrompt
    b.lastWindow
    b.reloadConfig
    { separator = true; }
    {
      name = "+Windows";
      key = "w";
      menu = [
        b.newWindow
        b.splitH
        b.splitV
        { separator = true; }
        b.sessionTree
        b.swapWindowPrev
        b.swapWindowNext
        { separator = true; }
        {
          name = "+Layout";
          key = "l";
          menu = [
            b.layoutNext
            b.layoutTiled
            b.layoutH
            b.layoutV
          ];
        }
        b.renameWindow
        b.killWindow
      ];
    }
    {
      name = "+Panes";
      key = "p";
      menu = [
        b.paneLeft
        b.paneDown
        b.paneUp
        b.paneRight
        { separator = true; }
        {
          name = "+Resize";
          key = "r";
          menu = [
            b.resizeLeft
            b.resizeDown
            b.resizeUp
            b.resizeRight
          ];
        }
        b.swapPanePrev
        b.swapPaneNext
        b.zoomPane
        { separator = true; }
        b.breakPane
        b.grabPane
        b.movePane
        b.killPane
      ];
    }
    {
      name = "+Sessions";
      key = "s";
      menu = [
        b.seshPicker
        b.sessionTree
        b.sessionRename
        b.newSession
        b.detach
      ];
    }
    {
      name = "+Popups";
      key = "o";
      menu = [
        b.scratchPopup
        b.gituiPopup
        b.lazydockerPopup
      ];
    }
    {
      name = "+Workmux";
      key = "C-w";
      menu = [
        b.workmuxDash
        b.workmuxSidebar
        b.workmuxLast
      ];
    }
    {
      name = "+Toggles";
      key = "t";
      menu = [
        b.toggleStatus
        b.toggleSync
        b.toggleMouse
      ];
    }
    { separator = true; }
    b.clearScreen
    b.listKeys
  ];

  # Generate a tmux `bind` line from a binding record
  mkBindLine =
    entry:
    let
      tableFlag =
        if entry ? table && entry.table == "root" then
          "-n "
        else if entry ? table then
          "-T ${entry.table} "
        else
          "";
      repeatFlag = if entry.repeat or false then "-r " else "";
    in
    "bind ${repeatFlag}${tableFlag}${entry.key} ${entry.cmd}";

  # Generate all direct bind lines from the binding attrset
  bindLines = lib.concatMapStringsSep "\n" mkBindLine (lib.attrValues b);

  # Convert a binding record or menu node to a which-key YAML item
  mkWhichKeyItem =
    item:
    if item ? separator then
      { separator = true; }
    else if item ? menu then
      {
        inherit (item) name key;
        menu = map mkWhichKeyItem item.menu;
      }
    else
      {
        inherit (item) name key;
        command = item.cmd;
      }
      // lib.optionalAttrs (item.transient or false) { transient = true; };

  tmuxWhichKeyConfig = (pkgs.formats.yaml { }).generate "config.yaml" {
    command_alias_start_index = 200;
    keybindings.prefix_table = "C-b";
    title = {
      style = "align=centre,bold";
      prefix = "tmux";
      prefix_style = "fg=green,align=centre,bold";
    };
    position = {
      x = "R";
      y = "P";
    };
    custom_variables = [ ];
    macros = [ ];
    items = map mkWhichKeyItem whichKeyMenus;
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
      prefix = "C-b";
      mouse = true;
      baseIndex = 1;
      historyLimit = 50000;
      escapeTime = 0;
      terminal = "tmux-256color";
      keyMode = "vi";
      aggressiveResize = true;
      plugins = with pkgs.tmuxPlugins; [
        sensible
        vim-tmux-navigator
        {
          plugin = yank;
          extraConfig = "set -g @yank_selection_mouse 'clipboard'";
        }
        open
        {
          plugin = fuzzback;
          extraConfig = ''
            set -g @fuzzback-bind /
            set -g @fuzzback-popup 1
            set -g @fuzzback-popup-size '90%'
          '';
        }
        {
          plugin = logging;
          extraConfig = ''
            set -g @logging-path "$HOME/.local/share/tmux/logging"
            set -g @logging_key "C-o"
          '';
        }
        {
          plugin = tmux-thumbs;
          extraConfig = "set -g @thumbs-key f";
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
          plugin = tmux-which-key;
          extraConfig = ''
            set -g @tmux-which-key-xdg-enable 1
          '';
        }
        {
          plugin = tmux-fzf;
          extraConfig = ''set -g @tmux-fzf-launch-key "C-f"'';
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
            set -g @continuum-restore 'off'
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
            "#{?synchronize-panes,#[fg=##${tmux.dark}]#[bg=##${tmux.highlight}] SYNC ,}"
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
            "#{?window_activity_flag,#[fg=##${tmux.highlight}],#[fg=##${tmux.dim}]}"
            "#[bg=#${tmux.bg}] #I #W#{?window_zoomed_flag, Z,}#{?@workmux_status, #{@workmux_status},} "
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

        set -g window-status-activity-style "none"

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
        set -g detach-on-destroy no-detached
        set -g wrap-search off
        set -g monitor-activity on
        set -g visual-activity off
        set -g activity-action other
        set -g display-time 3000
        set -g display-panes-time 3000
        set -g pane-base-index 1
        set -g set-titles on
        set -g set-titles-string "#S / #W"

        # Scrollbars disabled - modal scrollbars cause text reflow on width change
        set -g pane-scrollbars off

        # --- Keybindings (generated from shared binding definitions) ---
        ${bindLines}

        # vi-style copy mode (yank plugin handles 'y' for clipboard integration)
        bind -T copy-mode-vi v send-keys -X begin-selection
        bind -T copy-mode-vi C-v send-keys -X rectangle-toggle
        bind -T copy-mode-vi Y send-keys -X select-line \; send-keys -X copy-selection-and-cancel
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
        set -as terminal-features ',xterm-ghostty:sixel'
        set -as terminal-overrides ',xterm-ghostty:Ss=\E[%p1%d q:Se=\E[2 q'

        # Hyperlink (OSC 8) support
        set -as terminal-features ',*:hyperlinks'

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

  xdg.configFile."tmux/plugins/tmux-which-key/config.yaml".source = tmuxWhichKeyConfig;

  # Ensure tmux-which-key's init.tmux is writable so the plugin can rebuild it
  home.activation.tmuxWhichKeyPermissions = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
    f="$HOME/.local/share/tmux/plugins/tmux-which-key/init.tmux"
    if [ -f "$f" ] && [ ! -w "$f" ]; then
      chmod u+w "$f"
    fi
  '';

  home.packages = [
    tmuxStatusContext
    tmuxSeshPicker
    tmuxFilePicker
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
    lefthook
    tfswitch
    devbox
    angle-grinder
  ]);
}
