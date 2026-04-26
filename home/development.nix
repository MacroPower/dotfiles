{
  pkgs,
  lib,
  config,
  ...
}:

let
  # OSC 52 pbcopy shim for Linux hosts (notably the workmux lima VM).
  # Two paths: write OSC 52 directly to /dev/tty when we have one, otherwise
  # hand the bytes to tmux via `load-buffer -w` so its set-clipboard machinery
  # re-emits OSC 52 upstream via the `Ms=` override in home/tmux.nix.
  pbcopy-osc52 = pkgs.writeShellScriptBin "pbcopy" ''
    set -eu
    input="''${1:-/dev/stdin}"
    tmpfile=$(${pkgs.coreutils}/bin/mktemp)
    trap 'rm -f "$tmpfile"' EXIT
    ${pkgs.coreutils}/bin/cat "$input" > "$tmpfile"

    # Direct path: shell with a controlling tty (interactive SSH, login shell).
    b64=$(${pkgs.coreutils}/bin/base64 -w0 < "$tmpfile")
    if printf '\e]52;c;%s\a' "$b64" >/dev/tty 2>/dev/null; then exit 0; fi

    # Fallback: tmux's server runs us without a controlling tty (copy-pipe,
    # run-shell, etc.). Route through tmux so its set-clipboard machinery
    # re-emits OSC 52 to the outer terminal via the Ms= override.
    if [ -n "''${TMUX:-}" ]; then
      ${config.programs.tmux.package}/bin/tmux load-buffer -w - < "$tmpfile"
      exit 0
    fi

    echo "pbcopy: no controlling tty and not in tmux; OSC 52 unavailable" >&2
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
