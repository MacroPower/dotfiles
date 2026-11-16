# This function builds a check for system.checks. The check fails the
# build of the system when the daily cleanup of /tmp deletes a file
# that a tmpfiles rule of the configuration must keep.
#
# The check runs systemd-tmpfiles with the stock rule for /tmp and the
# rules of the configuration, as /etc/tmpfiles.d holds them. faketime
# sets the clock of the run 11 days ahead, and NO_FAKE_STAT keeps the
# real times on the files. So every file is 11 days old to the cleanup,
# and the cleanup deletes each file that no rule protects.
#
# Under --root, systemd-tmpfiles reads the user and the group of each
# rule from the `/etc/passwd` and `/etc/group` files of the root. It
# exits 65 when a rule names a user or a group that those files lack. A
# module that adds a rule for its own user then fails the check. So the
# check writes every user and every group of the configuration to the
# fake root. The cleanup reads only the names, so each entry carries
# the id 0.
{
  config,
  lib,
  pkgs,
}:
{
  # The name of the check.
  name,
  # The files that the cleanup must keep, as absolute paths in /tmp.
  files,
  # The words that name the tree of the files in the message of a
  # failure, such as `a clone`.
  tree,
}:
let
  inherit (config.dotfiles.system) username;
in
pkgs.runCommand name
  {
    nativeBuildInputs = [ pkgs.libfaketime ];
    # NixOS sets the source to a glob over the directory.
    tmpfilesDir = lib.removeSuffix "/*" config.environment.etc."tmpfiles.d".source;
    passwd = lib.concatMapStrings (u: "${u.name}:x:0:0::/:/\n") (lib.attrValues config.users.users);
    group = lib.concatMapStrings (g: "${g.name}:x:0:\n") (lib.attrValues config.users.groups);
    # This rule names the primary user of the host and its group. So the
    # run fails when the fake root lacks that user or that group. It
    # fails that way even when every other rule names only root. The
    # rule sets no age, so the cleanup leaves its path alone.
    owners = "d /var/lib/owners 0755 ${username} ${config.users.users.${username}.group} -\n";
    passAsFile = [
      "passwd"
      "group"
      "owners"
    ];
  }
  ''
    files=(${lib.escapeShellArgs files})
    mkdir -p root/etc root/tmp/other
    cp "$passwdPath" root/etc/passwd
    cp "$groupPath" root/etc/group
    cp "$ownersPath" owners.conf
    touch root/tmp/other/file
    for file in "''${files[@]}"; do
      mkdir -p "root$(dirname "$file")"
      touch "root$file"
    done
    NO_FAKE_STAT=1 faketime -f +11d ${config.systemd.package}/bin/systemd-tmpfiles \
      --root="$PWD/root" --clean "$tmpfilesDir/tmp.conf" "$tmpfilesDir/00-nixos.conf" "$PWD/owners.conf"
    if [ -e root/tmp/other ]; then
      echo "The cleanup kept an old file in /tmp, so this check cannot tell what the cleanup deletes." >&2
      exit 1
    fi
    for file in "''${files[@]}"; do
      if [ ! -e "root$file" ]; then
        echo "The cleanup of /tmp deleted $file from ${tree}." >&2
        exit 1
      fi
    done
    touch "$out"
  ''
