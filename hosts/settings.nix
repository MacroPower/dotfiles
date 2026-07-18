{ pkgs, username }:
{
  experimental-features = [
    "nix-command"
    "flakes"
  ];
  trusted-users = [
    "root"
    username
  ];
  allowed-users = [ "root" ] ++ (if pkgs.stdenv.isDarwin then [ "@admin" ] else [ "@wheel" ]);
  sandbox = if pkgs.stdenv.isLinux then true else "relaxed";
  substituters = [ "https://cache.nixos.org" ];
  trusted-substituters = [ "https://cache.nixos.org" ];
  trusted-public-keys = [ "cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY=" ];
  require-sigs = true;
  # Fetch cached outputs even for derivations that set allowSubstitutes = false.
  # The nix-darwin linux-builder VM pulls in trivial aarch64-linux files (e.g.
  # /etc/nix/registry.json) marked no-substitute; on macOS those can only build
  # once the builder VM is already running, a bootstrap deadlock. The outputs
  # are generic and live in cache.nixos.org, so substituting them is safe.
  always-allow-substitutes = true;
  allow-import-from-derivation = false;
  accept-flake-config = false;
  fallback = true;
  connect-timeout = 10;
  download-attempts = 3;
}
