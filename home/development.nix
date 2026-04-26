{
  pkgs,
  lib,
  config,
  ...
}:

let
  # OSC 52 pbcopy shim for Linux hosts (notably the terrarium lima VM).
  #
  # The chain is: shim -> outer client tty -> SSH -> Ghostty -> macOS clipboard.
  #
  # When inside tmux we deliberately route around tmux's clipboard machinery
  # by writing OSC 52 straight to the attached client's outer tty instead of
  # /dev/tty (the pane PTY). Two reasons:
  #   1. tmux interns the Ms= cap at client-attach time, so a stale session
  #      that started before a fix won't pick it up without detach+reattach.
  #   2. Even with set-clipboard set correctly, OSC 52 from a pane is always
  #      consumed by tmux's parser; if the cached Ms= is wrong the bytes are
  #      silently dropped on re-emit.
  # Writing directly to client_tty puts bytes onto the SSH PTY downstream of
  # tmux entirely, so neither failure mode applies.
  pbcopy-osc52 = pkgs.writeShellScriptBin "pbcopy" ''
    set -eu
    b64=$(${pkgs.coreutils}/bin/base64 -w0 < "''${1:-/dev/stdin}")
    osc() { printf '\e]52;c;%s\a' "$b64"; }

    if [ -n "''${TMUX:-}" ]; then
      client_tty=$(${config.programs.tmux.package}/bin/tmux \
        list-clients -F '#{client_tty}' 2>/dev/null | ${pkgs.coreutils}/bin/head -n1)
      if [ -n "$client_tty" ] && [ -w "$client_tty" ]; then
        osc > "$client_tty"
        exit 0
      fi
    fi

    if osc >/dev/tty 2>/dev/null; then exit 0; fi

    echo "pbcopy: no path to outer terminal (TMUX=''${TMUX:-unset})" >&2
    exit 1
  '';
in

{
  programs = {
    go = {
      enable = true;
      telemetry.mode = "off";
    };

    npm = {
      enable = true;
      package = null; # nodejs is already in home.packages
      settings.prefix = "\${HOME}/.npm";
    };

    uv = {
      enable = true;
      settings = {
        python-downloads = "automatic";
      };
    };
  };

  home = {
    sessionPath = [
      "$HOME/go/bin"
      "$HOME/.npm/bin"
    ];

    sessionVariables = lib.mkIf (config.dotfiles.hostname == "terrarium") {
      UV_PROJECT_ENVIRONMENT = ".venv-linux";
    };

    activation = {
      installPython = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        run ${pkgs.uv}/bin/uv python install --default
      '';
    };

    packages =
      with pkgs;
      [
        # Languages & runtimes
        nodejs
        gcc

        # Python build dependencies
        openssl
        readline
        sqlite
        xz
        zlib
        tcl
      ]
      ++ lib.optionals pkgs.stdenv.isLinux [
        pbcopy-osc52
      ];
  };
}
