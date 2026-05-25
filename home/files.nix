{ pkgs, lib, ... }:

{
  home.packages =
    with pkgs;
    [
      fclones
      moreutils
      renameutils
      rnr
      gdu
      gomi
      exiftool
      czkawka
      jpegoptim
      jpeginfo
      oxipng
      pngcheck
      dust
      _7zz
    ]
    ++ lib.optionals pkgs.stdenv.isLinux [
      acl
      attr
      apfs-fuse
      ntfs3g
      exfat
      fuse3
    ];
}
