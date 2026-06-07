system: {
  inherit system;
  homeModule =
    { pkgs, ... }:
    {
      imports = [
        ../../home/development.nix
        ../../home/kubernetes.nix
        ../../home/claude.nix
        ../../home/files.nix
        ../../home/photo-cli.nix
      ];
      dotfiles = {
        # sshd and ssh-keygen for the published shell image, which can
        # serve SSH via the baked-in sshd-entrypoint helper (see
        # toolchains/dev/ssh.go). Client config comes from
        # home/default.nix; openssh here makes the server explicit
        # instead of leaning on the lix base image's profile. This
        # config also feeds the sandbox image, which carries the few
        # extra megabytes as the price of sharing one home config;
        # openssh's heavy deps (openssl, zlib) are already in the
        # closure either way.
        extraHomePackages = [ pkgs.openssh ];
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
