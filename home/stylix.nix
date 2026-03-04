{ lib, ... }:

{
  stylix.targets = {
    # Vim: themed by joshdick/onedark.vim plugin + vim-airline
    vim.enable = lib.mkDefault false;
    # VSCode: themed by "One Dark Pro Darker" extension
    vscode.enable = lib.mkDefault false;
    # Zed: themed by "One Dark Pro" built-in theme
    zed.enable = lib.mkDefault false;
    # GitUI: themed by configs/gitui/theme.ron
    gitui.enable = lib.mkDefault false;
    # K9s: themed by skins.one-dark in kubernetes.nix
    k9s.enable = lib.mkDefault false;
  };
}
