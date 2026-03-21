{
  pkgs,
  lib,
  config,
  ...
}:

let
  cfg = config.dotfiles.development;
in
{
  options.dotfiles.development.enable =
    lib.mkEnableOption "development toolchains (Go, Node, Python)"
    // {
      default = true;
    };

  config = lib.mkIf cfg.enable {
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
        dotnet-sdk

        # Dev tools
        gopls
        nixd
        chief

        # Python build dependencies
        openssl
        readline
        sqlite
        xz
        zlib
        tcl
      ];
    };
  };
}
