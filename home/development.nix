{
  pkgs,
  lib,
  ...
}:

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

    activation = {
      installPython = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
        run ${pkgs.uv}/bin/uv python install --default
      '';
    };

    packages = with pkgs; [
      # Languages & runtimes
      nodejs
      gcc

      # Dev tools
      gopls
      nixd

      # Python build dependencies
      openssl
      readline
      sqlite
      xz
      zlib
      tcl
    ];
  };
}
