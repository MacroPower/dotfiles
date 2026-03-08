{
  username = "jcolvin";

  homebrew = {
    taps = [ ];
    brews = [ ];
    casks = [ ];
    masApps = { };
  };

  homeModule =
    { pkgs, ... }:
    {
      dotfiles = {
        git = {
          userName = "Jacob Colvin";
          userEmail = "jcolvin@example.com";
        };
        extraHomePackages = with pkgs; [
          azure-cli
        ];
        kubernetes.extraPackages = with pkgs; [
          kubelogin
          fluxcd
        ];
      };
    };
}
