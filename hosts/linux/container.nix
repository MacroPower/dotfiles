system: {
  inherit system;
  homeModule = {
    dotfiles = {
      username = "dev";
      hostname = "linux";
      homeDirectory = "/home/dev";
      git = {
        userName = "Jacob Colvin";
        userEmail = "jacobcolvin1@gmail.com";
      };
      firefox.enable = false;
      ghostty.enable = false;
      vscode.enable = false;
      zed.enable = false;
    };
  };
}
