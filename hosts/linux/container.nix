system: {
  inherit system;
  homeModule = {
    imports = [
      ../../home/development.nix
      ../../home/kubernetes.nix
      ../../home/claude.nix
      ../../home/files.nix
    ];
    dotfiles = {
      username = "dev";
      hostname = "linux";
      homeDirectory = "/home/dev";
      git = {
        userName = "Jacob Colvin";
        userEmail = "jacobcolvin1@gmail.com";
      };
    };
  };
}
