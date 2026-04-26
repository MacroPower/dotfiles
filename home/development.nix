{
  pkgs,
  lib,
  config,
  ...
}:

let
  # OSC 52 pbcopy shim for Linux hosts (notably the workmux lima VM).
  # Raw OSC 52 goes to /dev/tty; tmux (with `set-clipboard on` + the `Ms=`
  # override in home/tmux.nix) intercepts and re-emits upstream to Ghostty.
  pbcopy-osc52 = pkgs.writeShellScriptBin "pbcopy" ''
    set -eu
    data=$(${pkgs.coreutils}/bin/base64 -w0 < "''${1:-/dev/stdin}")
    if ! printf '\e]52;c;%s\a' "$data" >/dev/tty 2>/dev/null; then
      echo "pbcopy: no controlling tty; OSC 52 clipboard unavailable" >&2
      exit 1
    fi
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
