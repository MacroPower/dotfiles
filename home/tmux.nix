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

  tmuxObsidianTask = pkgs.writeShellApplication {
    name = "tmux-obsidian-task";
    runtimeInputs = with pkgs; [
      fd
      gum
    ];
    text = ''
      vaults_dir="${config.dotfiles.obsidian.vaultsDir}"
      if [ ! -d "$vaults_dir" ]; then
        echo "No vaults found in $vaults_dir"
        sleep 1
        exit 1
      fi

      vault=$(fd --type d --max-depth 1 --base-directory "$vaults_dir" | gum choose --header "Select vault")
      [ -z "$vault" ] && exit 0

      task=$(gum input --placeholder "Task..." --header "Add task to daily note ($vault)")
      [ -z "$task" ] && exit 0

      obsidian daily:append content="- [ ] $task" vault="$vault" silent
      echo "Added to $vault: $task"
      sleep 0.5
    '';
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

  tmuxHints = pkgs.writeShellApplication {
    name = "tmux-hints";
    runtimeInputs = [
      pkgs.coreutils
      pkgs.ncurses
      pkgs.tmux
    ];
    text = ''
      # Stylix colors (injected at build time)
      color_accent="${colors.base0B}"
      color_active="${colors.base0E}"
      color_dim="${colors.base04}"
      color_key="${colors.base0D}"
    ''
    + builtins.readFile ../scripts/tmux-hints.sh;
  };

  tmuxVimPopup = pkgs.writeShellApplication {
    name = "tmux-vim-popup";
    runtimeInputs = [
      pkgs.tmux
      pkgs.coreutils
    ];
    text = builtins.readFile ../scripts/tmux-vim-popup.sh;
  };

  tmuxHintsToggle = pkgs.writeShellApplication {
    name = "tmux-hints-toggle";
    runtimeInputs = [
      pkgs.coreutils
      pkgs.tmux
    ];
    text = builtins.readFile ../scripts/tmux-hints-toggle.sh;
  };

  tmux-floax = pkgs.tmuxPlugins.mkTmuxPlugin {
    pluginName = "tmux-floax";
    rtpFilePath = "floax.tmux";
    version = "0-unstable-2026-02-24";
    src = pkgs.fetchFromGitHub {
      owner = "omerxx";
      repo = "tmux-floax";
      rev = "133f526793d90d2caa323c47687dd5544a2c704b";
      hash = "sha256-9Hb9dn2qHF6KcIhtogvycX3Z0MoQrLPLCzZXtjGlPHw=";
    };
  };

  floaxScripts = "${tmux-floax}/share/tmux-plugins/tmux-floax/scripts";

  tmuxFloaxRun = pkgs.writeShellApplication {
    name = "tmux-floax-run";
    excludeShellChecks = [ "SC1091" ];
    runtimeInputs = [ pkgs.tmux ];
    text = ''
      # Source floax utils for set_bindings, unset_bindings, and env vars
      # shellcheck source=/dev/null
      source "${floaxScripts}/utils.sh"

      # Suppress workmux sidebar hooks for the lifetime of this popup.
      # They fire on new-session/new-window and would attach a sidebar to the
      # floax session. Hooks are restored (with || true) after the popup closes.
      _workmux_bin=$(tmux show-hooks -g 2>/dev/null \
        | sed -n 's|^after-new-session\[99\] run-shell -b "\(.*\) _sidebar-sync.*|\1|p' || true)
      if [ -n "$_workmux_bin" ]; then
        tmux set-hook -gu 'after-new-session[99]' 2>/dev/null || true
        tmux set-hook -gu 'after-new-window[99]' 2>/dev/null || true
      fi

      WIDTH="$FLOAX_WIDTH"
      HEIGHT="$FLOAX_HEIGHT"

      while [[ "''${1-}" == --* ]]; do
        case "$1" in
          --width) WIDTH="$2"; shift 2 ;;
          --height) HEIGHT="$2"; shift 2 ;;
          *) break ;;
        esac
      done

      NAME="''${1:?usage: tmux-floax-run [--width W] [--height H] <name> [command...]}"
      shift
      COMMAND="''${*:-}"

      # Toggle off if already in this session
      if [ "$(tmux display-message -p '#{session_name}')" = "$NAME" ]; then
        unset_bindings
        tmux detach-client
        exit 0
      fi

      # Store context for floax scripts (embed, menu, etc.)
      tmux setenv -g FLOAX_SESSION_NAME "$NAME"
      tmux setenv -g ORIGIN_SESSION "$(tmux display -p '#{session_name}')"
      tmux setenv -g FLOAX_TITLE " $NAME "

      set_bindings

      CURRENT_PATH="$(tmux display-message -p '#{pane_current_path}')"

      if ! tmux has-session -t "$NAME" 2>/dev/null; then
        if [ -n "$COMMAND" ]; then
          tmux new-session -d -s "$NAME" -c "$CURRENT_PATH" -e FLOAX=1 "$COMMAND"
        else
          tmux new-session -d -s "$NAME" -c "$CURRENT_PATH" -e FLOAX=1
        fi
        tmux set-option -t "$NAME" status off
      elif [ -z "$COMMAND" ] && [ "''${FLOAX_CHANGE_PATH:-}" = "true" ]; then
        # Auto-cd for persistent sessions (scratch)
        session_path="$(tmux display -t "$NAME" -p '#{pane_current_path}' 2>/dev/null || echo "")"
        if [ -n "$session_path" ] && [ "$session_path" != "$CURRENT_PATH" ]; then
          tmux send-keys -R -t "$NAME" " cd \"$CURRENT_PATH\"" C-m
        fi
      fi

      # Show floating popup (blocks until dismissed)
      tmux popup \
        -S "fg=$FLOAX_BORDER_COLOR" \
        -s "fg=$FLOAX_TEXT_COLOR" \
        -T " $NAME " \
        -w "$WIDTH" -h "$HEIGHT" \
        -b rounded -E \
        "tmux attach-session -t '$NAME'" || true

      # Cleanup: destroy leftover command sessions (e.g. after embed moves the
      # window out, leaving only the placeholder). Persistent sessions (no
      # COMMAND) are kept alive across toggles.
      if [ -n "$COMMAND" ] && tmux has-session -t "$NAME" 2>/dev/null; then
        tmux kill-session -t "$NAME"
      fi

      # Restore workmux hooks (with error tolerance for non-workmux windows)
      if [ -n "$_workmux_bin" ]; then
        _sync="$_workmux_bin _sidebar-sync --window #{window_id} 2>/dev/null || true"
        tmux set-hook -g 'after-new-session[99]' "run-shell -b \"$_sync\""
        tmux set-hook -g 'after-new-window[99]' "run-shell -b \"$_sync\""
      fi

      # Remove root-table bindings and reset session name for menu
      unset_bindings
      tmux setenv -g FLOAX_SESSION_NAME scratch
    '';
  };

  # Shared tmux binding definitions -- single source of truth for both
  # direct keybindings and which-key menu items.
  #
  # Fields:
  #   group     - hint category (general, windows, panes, etc.)
  #   name      - display name (present = appears in which-key)
  #   key       - key shared by direct bind and which-key
  #   cmd       - tmux command string
  #   repeat    - (optional) -r flag for direct binding
  #   transient - (optional) transient which-key menu item
  #   table     - (optional) "root" for -n, or key table name for -T
  b = {
    # General
    commandPrompt = {
      group = "general";
      name = "Command prompt";
      key = ":";
      cmd = "command-prompt";
    };
    lastWindow = {
      group = "general";
      name = "Last window";
      key = "tab";
      cmd = ''run-shell "workmux last-agent"'';
    };
    reloadConfig = {
      group = "general";
      name = "Reload config";
      key = "r";
      cmd = ''source-file ~/.config/tmux/tmux.conf \; display-message "Config reloaded"'';
    };
    clearScreen = {
      group = "general";
      name = "Clear scrollback";
      key = "C";
      cmd = "clear-history";
    };
    hintsToggle = {
      group = "general";
      name = "Hints sidebar";
      key = "?";
      cmd = ''run-shell "tmux-hints-toggle"'';
    };
    listKeys = {
      group = "general";
      name = "+Keys";
      key = "I";
      cmd = "list-keys -N";
    };

    # Windows
    newWindow = {
      group = "windows";
      name = "New window";
      key = "c";
      cmd = ''new-window -c "#{pane_current_path}"'';
    };
    splitH = {
      group = "windows";
      name = "Split horizontal";
      key = "d";
      cmd = ''split-window -h -c "#{pane_current_path}"'';
    };
    splitV = {
      group = "windows";
      name = "Split vertical";
      key = "v";
      cmd = ''split-window -v -c "#{pane_current_path}"'';
    };
    swapWindowPrev = {
      group = "windows";
      name = "Swap prev";
      key = "[";
      cmd = "swap-window -t -1 \\; select-window -t -1";
      repeat = true;
    };
    swapWindowNext = {
      group = "windows";
      name = "Swap next";
      key = "]";
      cmd = "swap-window -t +1 \\; select-window -t +1";
      repeat = true;
    };
    renameWindow = {
      group = "windows";
      name = "Rename";
      key = "R";
      cmd = ''command-prompt -I "#W" "rename-window -- \"%%\""'';
    };
    killWindow = {
      group = "windows";
      name = "Kill";
      key = "X";
      cmd = ''confirm -p "Kill window #W? (y/n)" kill-window'';
    };
    nextWindow = {
      group = "windows";
      name = "Next";
      key = "n";
      cmd = "next-window";
      repeat = true;
    };
    prevWindow = {
      group = "windows";
      name = "Previous";
      key = "b";
      cmd = "previous-window";
      repeat = true;
    };

    # Layouts
    layoutNext = {
      group = "layouts";
      name = "Next";
      key = "=";
      cmd = "next-layout";
      transient = true;
    };
    layoutTiled = {
      group = "layouts";
      name = "Tiled";
      key = "t";
      cmd = "select-layout tiled";
    };
    layoutH = {
      group = "layouts";
      name = "Horizontal";
      key = "-";
      cmd = "select-layout even-horizontal";
    };
    layoutV = {
      group = "layouts";
      name = "Vertical";
      key = "V";
      cmd = "select-layout even-vertical";
    };
    layoutMainH = {
      group = "layouts";
      name = "Main horizontal";
      key = "U";
      cmd = "select-layout main-horizontal";
    };
    layoutMainV = {
      group = "layouts";
      name = "Main vertical";
      key = "Y";
      cmd = "select-layout main-vertical";
    };
    layoutSpread = {
      group = "layouts";
      name = "Spread evenly";
      key = "_";
      cmd = "select-layout -E";
    };

    # Panes (navigation via vim-tmux-navigator: C-hjkl)
    resizeLeft = {
      group = "panes";
      name = "Left";
      key = "H";
      cmd = "resize-pane -L 5";
      repeat = true;
      transient = true;
    };
    resizeDown = {
      group = "panes";
      name = "Down";
      key = "J";
      cmd = "resize-pane -D 5";
      repeat = true;
      transient = true;
    };
    resizeUp = {
      group = "panes";
      name = "Up";
      key = "K";
      cmd = "resize-pane -U 5";
      repeat = true;
      transient = true;
    };
    resizeRight = {
      group = "panes";
      name = "Right";
      key = "L";
      cmd = "resize-pane -R 5";
      repeat = true;
      transient = true;
    };
    swapPanePrev = {
      group = "panes";
      name = "Swap prev";
      key = "[";
      cmd = "swap-pane -U";
      repeat = true;
    };
    swapPaneNext = {
      group = "panes";
      name = "Swap next";
      key = "]";
      cmd = "swap-pane -D";
      repeat = true;
    };
    zoomPane = {
      group = "panes";
      name = "Zoom";
      key = "z";
      cmd = "resize-pane -Z";
    };
    breakPane = {
      group = "panes";
      name = "Break pane";
      key = "B";
      cmd = "break-pane";
    };
    grabPane = {
      group = "panes";
      name = "Grab/join pane";
      key = "g";
      cmd = ''choose-window "join-pane -h -s \"%%\""'';
    };
    movePane = {
      group = "panes";
      name = "Send to window";
      key = "m";
      cmd = ''command-prompt -p "send pane to:" "join-pane -h -t \"%%\""'';
    };
    killPane = {
      group = "panes";
      name = "Kill pane";
      key = "x";
      cmd = "kill-pane";
    };
    displayPanes = {
      group = "panes";
      name = "Display numbers";
      key = "i";
      cmd = "display-panes";
    };
    rotatePanes = {
      group = "panes";
      name = "Rotate";
      key = "a";
      cmd = "rotate-window";
      transient = true;
    };
    markPane = {
      group = "panes";
      name = "Mark";
      key = "u";
      cmd = ''select-pane -m \; display-message "Pane marked"'';
    };
    respawnPane = {
      group = "panes";
      name = "Respawn";
      key = "Z";
      cmd = ''confirm -p "Respawn pane? (y/n)" "respawn-pane -k"'';
    };

    # Sessions
    seshPicker = {
      group = "sessions";
      name = "Switcher (sesh)";
      key = "s";
      cmd = ''display-popup -E -T " sesh " -w 70% -h 70% tmux-sesh-picker'';
    };
    sessionTree = {
      group = "sessions";
      name = "Session tree";
      key = "w";
      cmd = "choose-tree -Zs";
    };
    sessionRename = {
      group = "sessions";
      name = "Rename";
      key = "$";
      cmd = ''command-prompt -I "#S" "rename-session -- \"%%\""'';
    };
    newSession = {
      group = "sessions";
      name = "New";
      key = "N";
      cmd = "new-session -c ~";
    };
    detach = {
      group = "sessions";
      name = "Detach";
      key = "q";
      cmd = "detach";
    };

    # Floax-based popups (persistent sessions, embeddable via C-M-e)
    scratchPopup = {
      group = "popups";
      name = "Scratch terminal";
      key = "\`";
      cmd = ''run-shell "tmux-floax-run scratch"'';
    };
    scratchMenu = {
      group = "popups";
      name = "Floax menu";
      key = "~";
      cmd = ''run-shell "${floaxScripts}/menu.sh"'';
    };
    gituiPopup = {
      group = "popups";
      name = "gitui";
      key = "G";
      cmd = ''run-shell "tmux-floax-run --width 90% --height 90% gitui gitui"'';
    };
    lazydockerPopup = {
      group = "popups";
      name = "lazydocker";
      key = "D";
      cmd = ''run-shell "tmux-floax-run --width 90% --height 90% lazydocker lazydocker"'';
    };
    obsidianTask = {
      group = "popups";
      name = "Add task (obsidian)";
      key = "o";
      cmd = ''run-shell "tmux-floax-run --width 60% --height 30% obsidian tmux-obsidian-task"'';
    };

    # Pickers stay as display-popup (need parent pane context)
    filePicker = {
      group = "popups";
      name = "File picker";
      key = "F";
      cmd = ''display-popup -E -T " files " -w 80% -h 80% -d "#{pane_current_path}" tmux-file-picker --git-root'';
    };

    # Workmux
    workmuxDash = {
      group = "workmux";
      name = "Dashboard";
      key = "S";
      cmd = ''run-shell "tmux-floax-run --width 100 --height 30 wm-dash workmux dashboard"'';
    };
    workmuxSidebar = {
      group = "workmux";
      name = "Sidebar";
      key = "T";
      cmd = ''run-shell "workmux sidebar"'';
    };
    workmuxLast = {
      group = "workmux";
      name = "Last done";
      key = "W";
      cmd = ''run-shell "workmux last-done"'';
    };

    # Toggles
    toggleSync = {
      group = "toggles";
      name = "Sync panes";
      key = "y";
      cmd = ''set-window-option synchronize-panes \; display-message "sync #{?synchronize-panes,ON,OFF}"'';
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
        b.nextWindow
        b.prevWindow
        { separator = true; }
        {
          name = "+Layout";
          key = "l";
          menu = [
            b.layoutNext
            b.layoutTiled
            b.layoutH
            b.layoutV
            b.layoutMainH
            b.layoutMainV
            { separator = true; }
            b.layoutSpread
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
        b.displayPanes
        b.rotatePanes
        b.zoomPane
        { separator = true; }
        b.breakPane
        b.grabPane
        b.movePane
        b.markPane
        b.respawnPane
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
        b.scratchMenu
        b.gituiPopup
        b.lazydockerPopup
        b.filePicker
        b.obsidianTask
      ];
    }
    {
      name = "+Workmux";
      key = "M";
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
        b.toggleSync
        b.hintsToggle
      ];
    }
    { separator = true; }
    b.clearScreen
    b.hintsToggle
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

  # Hint generation -- builds a flat cheat sheet from binding data
  hintGroups = [
    {
      id = "general";
      label = "GENERAL";
    }
    {
      id = "windows";
      label = "WINDOWS";
    }
    {
      id = "panes";
      label = "PANES";
    }
    {
      id = "layouts";
      label = "LAYOUTS";
    }
    {
      id = "sessions";
      label = "SESSIONS";
    }
    {
      id = "popups";
      label = "POPUPS";
    }
    {
      id = "workmux";
      label = "WORKMUX";
    }
    {
      id = "toggles";
      label = "TOGGLES";
    }
  ];

  entriesForGroup = gid: lib.filter (e: (e.group or "") == gid) (lib.attrValues b);

  padRight =
    width: str:
    str + builtins.substring 0 (lib.max 0 (width - builtins.stringLength str)) "                    ";

  # Display symbols for special key names in hints
  keySymbols = {
    tab = "⇥";
  };

  displayKey = k: keySymbols.${k} or k;

  # Display width of a key (1 for symbol replacements, byte length for ASCII)
  keyDisplayWidth =
    k:
    let
      dk = displayKey k;
    in
    if keySymbols ? ${k} then 1 else builtins.stringLength dk;

  fmtHintEntry =
    e:
    let
      dk = displayKey e.key;
      # Compensate for multi-byte UTF-8 symbols that are only 1 column wide
      padWidth = 6 + (builtins.stringLength dk - keyDisplayWidth e.key);
    in
    "  ${padRight padWidth dk}${lib.toLower e.name}";

  fmtHintGroup =
    { id, label }:
    let
      entries = entriesForGroup id;
    in
    lib.optionalString (entries != [ ])
      "\n${label}\n${lib.concatMapStringsSep "\n" fmtHintEntry entries}";

  tmuxHintsText = "tmux\n" + lib.concatMapStrings fmtHintGroup hintGroups + "\n";

  tmuxHintsFile = pkgs.writeText "tmux-hints.txt" tmuxHintsText;

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
  programs.tmux = {
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
          set -g @logging_key "O"
          set -g @screen-capture-key "P"
          set -g @save-complete-history-key "A"
          set -g @clear-history-key "Q"
        '';
      }
      {
        plugin = tmux-thumbs;
        extraConfig = ''
          set -g @thumbs-key f
          set -g @thumbs-upcase-command 'tmux-vim-popup "{}"'

          # Hint styling
          set -g @thumbs-hint-fg-color "#${tmux.bg}"
          set -g @thumbs-hint-bg-color "#${tmux.highlight}"
          set -g @thumbs-contrast enabled
          set -g @thumbs-position off_left
          set -g @thumbs-reverse enabled
        '';
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
        plugin = tmux-floax;
        extraConfig = ''
          set -g @floax-bind 'F13'
          set -g @floax-bind-menu 'F14'
          set -g @floax-width '80%'
          set -g @floax-height '80%'
          set -g @floax-border-color '#${tmux.muted}'
          set -g @floax-text-color '#${tmux.dim}'
          set -g @floax-change-path 'true'
          set -g @floax-title ' scratch '
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
        extraConfig = ''set -g @tmux-fzf-launch-key "E"'';
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
          "#[fg=#{?client_prefix,##${tmux.accentAlt},##${tmux.accent}},bg=#${tmux.surface}]${plr}"
          "#[fg=#${tmux.dark},bg=#{?client_prefix,##${tmux.accentAlt},##${tmux.accent}},bold] %I:%M:%S %p "
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
      set -g automatic-rename-format "#{?#{m:.*-wrapped,#{pane_current_command}},#{s/^\\.//;s/-wrapped$//:pane_current_command},#{pane_current_command}}#{?pane_in_mode, (#{pane_mode}),}#{?pane_dead,[dead],}"

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

  xdg.configFile."tmux/plugins/tmux-which-key/config.yaml".source = tmuxWhichKeyConfig;
  xdg.configFile."hints/tmux.txt".source = tmuxHintsFile;
  xdg.configFile."hints/vim.txt".source = ../configs/hints/vim.txt;

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
    tmuxObsidianTask
    tmuxHints
    tmuxHintsToggle
    tmuxVimPopup
    tmuxFloaxRun
    pkgs.sesh
  ];
}
