{ pkgs, ... }:

{
  programs.vim = {
    enable = true;
    settings = {
      expandtab = true;
      tabstop = 2;
      shiftwidth = 2;
      number = true;
      mouse = "a";
      ignorecase = true;
      smartcase = true;
      hidden = true;
      undofile = true;
      undodir = [ "$HOME/.vim/undodir" ];
    };
    plugins = with pkgs.vimPlugins; [
      # Appearance
      onedark-vim
      vim-airline
      vim-airline-themes

      # Navigation
      fzf-vim
      vim-tmux-navigator

      # Git
      vim-fugitive
      vim-gitgutter

      # Editing
      vim-surround
      vim-commentary
      vim-repeat

      # Learning
      vim-which-key

      # Language support
      vim-polyglot
    ];
    extraConfig = builtins.readFile ../configs/vim/extra.vim;
  };

}
