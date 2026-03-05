{
  pkgs,
  hostConfig,
  lib,
  ...
}:

{
  imports = [ ../shared.nix ];

  environment.enableAllTerminfo = true;

  programs.nh = {
    enable = true;
    flake = "/home/${hostConfig.username}/repos/dotfiles";
  };

  programs.nix-ld = {
    enable = true;
    libraries = with pkgs; [
      readline
      sqlite
      tcl
      libffi
      ncurses
    ];
  };

  users.users.${hostConfig.username} = {
    isNormalUser = true;
    home = "/home/${hostConfig.username}";
    shell = pkgs.fish;
    extraGroups = [ "wheel" ];
    openssh.authorizedKeys.keyFiles = [ ../../keys/authorized_keys ];
  };

  services.openssh = {
    enable = lib.mkForce true;
    settings = {
      PasswordAuthentication = false;
      KbdInteractiveAuthentication = false;
      PermitRootLogin = "no";
      AcceptEnv = [ "COLORTERM" ];
    };
  };

  security.sudo.keepTerminfo = true;

  security.sudo.extraRules = [
    {
      users = [ hostConfig.username ];
      commands = [
        {
          command = "ALL";
          options = [ "NOPASSWD" ];
        }
      ];
    }
  ];

  time.timeZone = lib.mkDefault "America/New_York";

  system.stateVersion = "25.05";
}
