{
  username = "jcolvin";

  homebrew = {
    taps = [ ];
    brews = [ ];
    casks = [ ];
    masApps = { };
  };

  homeModule = _: {
    dotfiles = {
      git = {
        userName = "Jacob Colvin";
        userEmail = "jcolvin@example.com";
      };
    };
  };
}
