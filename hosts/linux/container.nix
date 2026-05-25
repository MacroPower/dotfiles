system: {
  inherit system;
  homeModule =
    { ... }:
    {
      imports = [
        ../../home/development.nix
        ../../home/kubernetes.nix
        ../../home/claude.nix
        ../../home/files.nix
        ../../home/photo-cli.nix
      ];
      dotfiles = {
        username = "dev";
        hostname = "linux";
        homeDirectory = "/home/dev";
        git = {
          userName = "Jacob Colvin";
          userEmail = "jacobcolvin1@gmail.com";
        };
        # /home/dev is on the container's ephemeral overlay; a cross-device
        # fallback into it would silently fill the writable layer instead of
        # the writable bind-mount the source file lives on. Force gomi to
        # error out when no per-mount .Trash-$uid exists.
        gomi.homeFallback = false;
        claude.hostContext = ''
          You're running in a Nix container. Both nix-command and flakes are enabled.

          IMPORTANT: Your working directory is bind mounted storage. Unless stated
          otherwise, all other directories live on the container's ephemeral overlay.
          You may only use these directories for files that do not need persistence.
        '';
      };
    };
}
