{
  pkgs,
  hostConfig,
  lib,
  ...
}:

{
  nix.settings.experimental-features = [
    "nix-command"
    "flakes"
  ];
  nixpkgs.config.allowUnfree = true;
  environment.enableAllTerminfo = true;

  programs.fish.enable = true;

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
    openssh.authorizedKeys.keyFiles = [
      (builtins.fetchurl "https://github.com/MacroPower.keys")
    ];
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
