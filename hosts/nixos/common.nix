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

  nix.gc = {
    automatic = true;
    options = "--delete-older-than 30d";
  };

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
      (builtins.fetchurl {
        url = "https://github.com/MacroPower.keys";
        sha256 = "11f40a2p2dq3f5fb0zk8r3kmim3rjbs9dynhkbrn9j0xrwr0j72d";
      })
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
