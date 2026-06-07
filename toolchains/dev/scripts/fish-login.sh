#!/bin/sh
# Login shell for root in the published shell image (see ssh.go). sshd
# execs it for interactive sessions and remote commands.
#
# Recreates the environment the image's default command would have
# provided (HOME, home-manager session vars, PATH with nix store paths)
# before handing control to fish. Without it, fish's built-in config
# fails immediately on mkdir/tty/uname because the PATH sshd provides
# is empty. Deliberately not `set -e`: a login shell must not die on
# minor failures.

home=/home/dev
hm_home_path=$home/.local/state/home-manager/gcroots/current-home/home-path

# Pull in home-manager's session vars (XDG_*, NIX_SSL_CERT_FILE, etc.).
# The exact path varies by home-manager version; try the common ones.
for f in \
  "$home/.nix-profile/etc/profile.d/hm-session-vars.sh" \
  "$hm_home_path/etc/profile.d/hm-session-vars.sh" \
  /etc/profile.d/hm-session-vars.sh; do
  # shellcheck disable=SC1090 # path intentionally varies by hm version
  [ -r "$f" ] && . "$f"
done

# sshd already sets HOME from /etc/passwd field 6, but pin it here too
# in case a tool launches us without it.
export HOME=$home
[ -z "$USER" ] && export USER=root

# If hm-session-vars didn't add the home-manager bin dir to PATH (some
# setups manage PATH outside session vars), prepend a known set of dirs
# that contains fish, starship, atuin, mkdir, etc.
case ":${PATH-}:" in
*:"$hm_home_path/bin":*) ;;
*)
  PATH="$hm_home_path/bin:$home/.nix-profile/bin"
  PATH="$PATH:/nix/var/nix/profiles/default/bin:/usr/local/bin:/usr/bin:/bin"
  ;;
esac
export PATH

# User-managed env file. Deployments bind-mount it from the host so env
# vars can change without recreating the container. Parsed line-by-line
# rather than sourced so a malformed line can't fail the whole login.
# Accepts KEY=value and "export KEY=value"; ignores blanks, comments,
# and anything that doesn't match IDENT=...
if [ -e /etc/ssh/env ] && [ ! -f /etc/ssh/env ]; then
  echo "warning: /etc/ssh/env is not a regular file (bind-mount likely created a directory); skipping" >&2
elif [ -r /etc/ssh/env ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
    '' | '#'*) continue ;;
    'export '*) line=${line#export } ;;
    esac
    # shellcheck disable=SC2163 # exporting the KEY=value held in $line
    case "$line" in
    [a-zA-Z_]*=*) export "$line" ;;
    esac
  done </etc/ssh/env
fi

# Hand off to fish. With "-c <cmd>" we're being invoked as
# "<login_shell> -c <SSH_ORIGINAL_COMMAND>" (e.g. "ssh host ls");
# otherwise it's an interactive login session.
FISH=$hm_home_path/bin/fish
if [ "$1" = "-c" ]; then
  exec "$FISH" -c "$2"
else
  exec "$FISH" -l
fi
