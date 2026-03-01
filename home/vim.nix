_:

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
    };
    extraConfig = builtins.readFile ../configs/vim/extra.vim;
  };

  home.file.".vim/colors/onedark.vim".source = ../configs/vim/colors/onedark.vim;
}
