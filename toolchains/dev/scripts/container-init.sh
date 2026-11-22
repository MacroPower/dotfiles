#!/bin/sh
# Env-driven runtime init for the published shell image (see ssh.go).
# The Dagger dev container starts each terminal session through it (see
# wrapWithContainerInit in main.go), for the MCP server sync of the
# /claude-state volume.
#
#   PERSIST_OWNER      "uid:gid". Gives /commandhistory and /claude-state,
#                      with every file in them, to that user and group,
#                      and writes the files there as that user. Set it
#                      when the command drops to a user other than root,
#                      as terrarium init does in the Dagger sandbox (see
#                      SandboxBase in main.go). That user can read and
#                      write the persisted state, and root does not write
#                      into the volumes of that user. Requires setpriv.
#
#   IDMAPPED_MOUNTS    One "path=uid:gid" per line. Bind-mounts each path
#                      over itself with a UID/GID translation so writes by
#                      container-root land on the host as the declared
#                      owner. Requires util-linux >= 2.39 (--map-users),
#                      kernel >= 5.12, a filesystem with idmapped-mount
#                      support (ZFS >= 2.2), and CAP_SYS_ADMIN. Binding
#                      the path over itself is intentional -- it layers
#                      the idmap onto subsequent accesses through it.
#
#   GOMI_TRASH_MOUNTS  One absolute mountpoint per line. Pre-creates
#                      $path/.Trash-$uid/{files,info} (mode 0700) so gomi
#                      has a usable external trash on each writable bind
#                      mount: deletes stay on the same filesystem (atomic
#                      rename) instead of copying into the container
#                      overlay. gomi never creates these dirs itself, and
#                      with home_fallback disabled (hosts/linux/
#                      container.nix) a missing trash dir is an explicit
#                      error instead of a silent spill into $HOME.
#
# With no environment variables and no persistence volumes this is a
# pure pass-through exec.
#
# Usage: container-init <command> [args...]
set -e

home=/home/dev

# --- heal managed symlinks ----------------------------------------------
# A persisted ~/.claude or ~/.config volume shadows the image's freshly
# baked home-manager symlinks with an older image's (now-dangling) ones.
# Re-link them against the current generation before anything reads them.
# The MCP server sync of the /claude-state volume reads ~/.config/mcp.
[ -x /usr/local/bin/relink-managed ] && /usr/local/bin/relink-managed || true

# --- persistence volumes ------------------------------------------------
# When the runtime mounts /commandhistory or /claude-state, redirect the
# session state that should survive container recreates into them.
# Compose deployments mount them as named volumes, and the Dagger dev
# container mounts them as cache volumes. The script skips both when the
# mounts are absent, so a plain "docker run" keeps stock behavior.
#
# With PERSIST_OWNER set, the session user owns the volumes and can
# replace a file in them with a link. root follows such a link when it
# writes the file. A write of root therefore lets the session change any
# file that the link names. The steps below write into the volumes
# through as_owner, which runs them as that user. The chown runs at each
# start, so it also covers a volume that root created or wrote. The
# Dagger engine creates each cache volume with an owner of root.
#
# root replaces the links in the home directory with ln -n. Each of
# them names a file in a volume, such as fish_history. The session can
# turn that file into a link to a directory. keep_regular then removes
# that link, but a concurrent session can plant the link again before
# ln runs. Without -n, ln follows both links and creates its new link
# as root in the directory that the session named.
#
# The session can also leave a regular file that takes hours to read,
# such as a sparse file of many terabytes. A sparse file costs no disk
# space. fish reads the whole fish_history when it loads its history,
# and the MCP server sync reads the whole claude.json and
# mcp-servers.json. keep_small moves each of these files out of the way
# when it is larger than 256 MiB. A concurrent session can still put a
# FIFO or such a file at claude.json or mcp-servers.json after
# keep_regular and keep_small check it. timeout stops the claude.json
# seed and the MCP server sync after 60 seconds, so the start goes on
# to the command.
owner=${PERSIST_OWNER-}
if [ -n "$owner" ]; then
  case $owner in
  *:*:* | :* | *:) owner= ;;
  *:*) ;;
  *) owner= ;;
  esac
  if [ -z "$owner" ]; then
    echo "fatal: PERSIST_OWNER is not uid:gid: $PERSIST_OWNER" >&2
    exit 1
  fi
  if ! command -v setpriv >/dev/null 2>&1; then
    echo "fatal: PERSIST_OWNER needs setpriv" >&2
    exit 1
  fi
  # chown -R changes each link in the volumes and not the file that the
  # link names.
  #
  # The session can take its own write permission from a directory in a
  # volume. keep_regular then cannot remove a FIFO that the session
  # leaves there, and the claude.json seed, the MCP server sync, and fish
  # block on it. chmod -R gives the owner read and write permission on
  # each file and directory in the volumes again. It skips the links that
  # it finds there. It adds only owner bits, so a file that a race puts
  # in its way gives no user more than that user can give itself.
  #
  # A failed chown or chmod prints a warning and does not stop the
  # entrypoint.
  for dir in /commandhistory /claude-state; do
    [ -d "$dir" ] || continue
    chown -R "$owner" "$dir" ||
      echo "warning: cannot give $dir to $owner" >&2
    chmod -R u+rwX "$dir" ||
      echo "warning: cannot let $owner write $dir" >&2
  done
fi

# as_owner CMD [ARGS...] runs CMD as the PERSIST_OWNER user, or as the
# user of this script when the variable is unset.
as_owner() {
  if [ -n "$owner" ]; then
    setpriv --reuid="${owner%%:*}" --regid="${owner#*:}" --clear-groups "$@"
  else
    "$@"
  fi
}

# keep_regular PATH removes PATH when it is a link or anything other than
# a regular file. A link redirects each write of the steps below to the
# file that it names. A FIFO blocks the claude.json seed, the MCP server
# sync, and fish. claude-json-sync writes each other file under a name
# that mktemp picks. A FIFO at another path in the volumes therefore
# does not block the sync.
keep_regular() {
  if [ -L "$1" ] || { [ -e "$1" ] && [ ! -f "$1" ]; }; then
    echo "warning: removing $1, which is not a regular file" >&2
    as_owner rm -rf "$1" || echo "warning: cannot remove $1" >&2
  fi
}

# keep_small PATH moves PATH to PATH.oversized when PATH is larger than
# 256 MiB. It replaces whatever stands at PATH.oversized, so the volume
# keeps one such copy of each file. The steps below then create
# fish_history and claude.json again, and the MCP server sync writes a
# new mcp-servers.json. The copy stays in the volume, so the user can
# read it or delete it.
keep_small() {
  size=$(stat -c %s "$1" 2>/dev/null) || return 0
  [ "$size" -gt 268435456 ] || return 0
  echo "warning: moving $1, which is larger than 256 MiB, to $1.oversized" >&2
  if ! { as_owner rm -rf "$1.oversized" && as_owner mv -fT "$1" "$1.oversized"; }; then
    echo "warning: cannot move $1" >&2
  fi
}

if [ -d /commandhistory ]; then
  mkdir -p "$home/.local/share/fish"
  keep_regular /commandhistory/fish_history
  keep_small /commandhistory/fish_history
  if [ ! -e /commandhistory/fish_history ]; then
    as_owner touch /commandhistory/fish_history ||
      echo "warning: cannot create /commandhistory/fish_history" >&2
  fi
  ln -sfn /commandhistory/fish_history "$home/.local/share/fish/fish_history"
fi
if [ -d /claude-state ]; then
  keep_regular /claude-state/claude.json
  keep_small /claude-state/claude.json
  keep_regular /claude-state/mcp-servers.json
  keep_small /claude-state/mcp-servers.json

  # Seed the volume from the image's copy on first start only; after
  # that the volume is the source of truth.
  if [ ! -f /claude-state/claude.json ] && [ -f "$home/.claude.json" ]; then
    as_owner timeout 60 cp "$home/.claude.json" /claude-state/claude.json ||
      echo "warning: cannot seed /claude-state/claude.json" >&2
  fi
  rm -f "$home/.claude.json"
  ln -sfn /claude-state/claude.json "$home/.claude.json"

  # Sync the MCP servers of the persisted claude.json with the build. The
  # file outlives image updates and rebuilds in /claude-state, but its
  # .mcpServers[].command values embed /nix/store paths of the build that
  # wrote it. ~/.config/mcp/mcp.json names the store paths of the current
  # build. In the published shell image, relink-managed has healed the
  # link against the current generation. The Dagger dev container mounts
  # no volume over ~/.config/mcp, so its link comes from the activation
  # of its own build. Run claude-json-sync, the tool that home/claude.nix's
  # syncClaudeJson runs at build time. The tool also removes each server
  # that an earlier build declared and this build does not declare.
  #
  # The record of the servers that the tool wrote lives in /claude-state,
  # because it describes the file there. The record under ~/.local/state
  # describes the copy of claude.json that the activation wrote.
  # claude-json-sync replaces the file at the path that it gets, so pass
  # the path in the volume and keep the ~/.claude.json redirect.
  # A failed sync prints a warning and does not abort the entrypoint
  # under `set -e` before the main process execs.
  mcp_config="$home/.config/mcp/mcp.json"
  if command -v claude-json-sync >/dev/null 2>&1 && [ -f "$mcp_config" ]; then
    as_owner timeout 60 claude-json-sync --servers "$mcp_config" \
      --record /claude-state/mcp-servers.json /claude-state/claude.json ||
      echo "warning: the MCP server sync of /claude-state/claude.json failed or took more than 60 seconds" >&2
  fi
fi

if [ -n "${IDMAPPED_MOUNTS-}" ]; then
  # Hard-fail early if util-linux is too old to know --map-users;
  # otherwise mount errors out with a confusing "unknown option".
  if ! mount --help 2>&1 | grep -q -- '--map-users'; then
    echo "fatal: 'mount' lacks --map-users (need util-linux >= 2.39)" >&2
    exit 1
  fi
  while IFS= read -r line || [ -n "$line" ]; do
    # Strip leading/trailing whitespace so YAML block-scalar indenting
    # and stray CRs don't produce confusing failures.
    line=$(printf '%s' "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    case "$line" in '' | '#'*) continue ;; esac
    case "$line" in
    *=*) ;;
    *)
      echo "fatal: IDMAPPED_MOUNTS entry missing '=': $line" >&2
      exit 1
      ;;
    esac
    path=${line%%=*}
    spec=${line#*=}
    case "$spec" in
    *:*) ;;
    *)
      echo "fatal: IDMAPPED_MOUNTS entry missing ':' in uid:gid: $line" >&2
      exit 1
      ;;
    esac
    uid=${spec%%:*}
    gid=${spec#*:}
    if [ -z "$path" ] || [ -z "$uid" ] || [ -z "$gid" ]; then
      echo "fatal: IDMAPPED_MOUNTS entry has empty field: $line" >&2
      exit 1
    fi
    if ! mount --bind --map-users "0:$uid:1" --map-groups "0:$gid:1" \
      "$path" "$path"; then
      echo "fatal: idmapped bind mount failed for $path -> $uid:$gid" >&2
      exit 1
    fi
    echo "idmapped: $path -> $uid:$gid"
  done <<IDMAP_EOF
$IDMAPPED_MOUNTS
IDMAP_EOF
fi

# Run after IDMAPPED_MOUNTS so the trash dir's in-container uid 0
# translates to the bind's mapped host uid.
if [ -n "${GOMI_TRASH_MOUNTS-}" ]; then
  uid=$(id -u)
  while IFS= read -r mp || [ -n "$mp" ]; do
    mp=$(printf '%s' "$mp" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    case "$mp" in '' | '#'*) continue ;; esac
    if [ ! -d "$mp" ]; then
      echo "warning: GOMI_TRASH_MOUNTS path missing, skipping: $mp" >&2
      continue
    fi
    # -m only applies to the deepest dirs (the chmod below pins the
    # trash root's mode explicitly so it isn't left at the umask
    # default).
    # shellcheck disable=SC2174
    mkdir -m 0700 -p "$mp/.Trash-$uid/files" "$mp/.Trash-$uid/info"
    chmod 0700 "$mp/.Trash-$uid"
    echo "trash dir ready: $mp/.Trash-$uid"
  done <<TRASH_EOF
$GOMI_TRASH_MOUNTS
TRASH_EOF
fi

if [ "$#" -eq 0 ]; then
  echo "usage: container-init <command> [args...]" >&2
  exit 64
fi
exec "$@"
