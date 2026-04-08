{
  config,
  pkgs,
  lib,
  ...
}:

{
  imports = [
    ../shared.nix
    ../options.nix
  ];

  environment.enableAllTerminfo = true;

  programs.nh = {
    enable = true;
    flake = "/home/${config.dotfiles.system.username}/repos/dotfiles";
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

  users.users.${config.dotfiles.system.username} = {
    isNormalUser = true;
    home = "/home/${config.dotfiles.system.username}";
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
      ClientAliveInterval = 60;
      ClientAliveCountMax = 3;
    };
  };

  security.pki.certificateFiles = config.dotfiles.system.caCertificateFiles;

  security.sudo.keepTerminfo = true;

  security.sudo.extraRules = [
    {
      users = [ config.dotfiles.system.username ];
      commands = [
        {
          command = "ALL";
          options = [ "NOPASSWD" ];
        }
      ];
    }
  ];

  time.timeZone = lib.mkDefault "America/New_York";

  system.stateVersion = lib.mkDefault "25.05";
}
