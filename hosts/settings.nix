{ pkgs, ... }:
{
  nix.settings = {
    experimental-features = [
      "nix-command"
      "flakes"
    ];
    trusted-users = [ "root" ];
    allowed-users = [ "root" ] ++ (if pkgs.stdenv.isDarwin then [ "@admin" ] else [ "@wheel" ]);
    sandbox = if pkgs.stdenv.isLinux then true else "relaxed";
    substituters = [ "https://cache.nixos.org" ];
    trusted-substituters = [ "https://cache.nixos.org" ];
    require-sigs = true;
    trusted-public-keys = [ "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY=" ];
    allow-import-from-derivation = false;
    accept-flake-config = false;
    fallback = true;
    connect-timeout = 10;
    download-attempts = 3;
  };
}
