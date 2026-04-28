{
  pkgs,
  lib,
  config,
  ...
}:

let
  cfg = config.dotfiles.comfyui;

  comfyuiPkg = pkgs.comfyui.override {
    inherit (cfg) dataDir port;
  };
in
{
  options.dotfiles.comfyui = {
    enable = lib.mkEnableOption "ComfyUI photo-restoration toolkit";

    dataDir = lib.mkOption {
      type = lib.types.str;
      default = "${config.home.homeDirectory}/comfyui";
      description = ''
        Where ComfyUI is cloned and the venv lives. Must contain no '.' in
        any path component: ComfyUI-SUPIR's sgm/util.py resolves relative
        imports against the install's absolute path, and any '.' in that
        path causes Python's relative-import resolver to split incorrectly
        (e.g. ~/.local/share/comfyui mis-splits at .local and tries to
        import '/Users/<you>/' as a module). Avoiding ~/Documents also
        keeps the venv out of iCloud sync.
      '';
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8188;
      description = "Port the comfyui launcher binds (127.0.0.1 only).";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = config.programs.uv.enable;
        message = "dotfiles.comfyui requires programs.uv.enable in your home config.";
      }
    ];

    home.packages = [ comfyuiPkg ];

    home.activation.setupComfyUI = lib.hm.dag.entryAfter [ "writeBoundary" ] ''
      ${comfyuiPkg}/bin/comfyui-update || true
    '';
  };
}
