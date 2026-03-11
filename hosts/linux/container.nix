{
  system = "aarch64-linux";
  homeModule = {
    dotfiles = {
      username = "dev";
      homeDirectory = "/home/dev";
      git = {
        userName = "Jacob Colvin";
        userEmail = "jacobcolvin1@gmail.com";
      };
      shell.extraTideConfig = ''
        set -g tide_left_prompt_items os $tide_left_prompt_items
        set -g tide_os_icon \uebc6
      '';
    };
  };
}
