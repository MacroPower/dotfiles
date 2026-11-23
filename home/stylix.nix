{ lib, ... }:

{
  stylix.targets = {
    # Neovim: themed by navarasu/onedark.nvim plugin + lualine
    neovim.enable = lib.mkDefault false;
    # Zed: themed by "One Dark Pro" built-in theme
    zed.enable = lib.mkDefault false;
    # GitUI: themed by configs/gitui/theme.ron
    gitui.enable = lib.mkDefault false;
    # K9s: themed by skins.one-dark in k9s.nix
    k9s.enable = lib.mkDefault false;
    # Starship: themed explicitly in fish.nix with stylix palette
    starship.enable = lib.mkDefault false;
    # Tmux: themed explicitly in tools.nix with stylix palette
    tmux.enable = lib.mkDefault false;
    # Wayprompt: not installed. Home-manager removed programs.wayprompt,
    # but stylix's target still sets it, which fails an assertion.
    # Drop this line once stylix removes the target.
    wayprompt.enable = lib.mkDefault false;
  };
}
