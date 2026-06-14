#!/bin/sh
# Env-driven runtime init for the published shell image (see ssh.go).
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
# Re-link them against the current generation before anything reads them
# -- in particular before the MCP re-merge below reads ~/.config/mcp.
[ -x /usr/local/bin/relink-managed ] && /usr/local/bin/relink-managed || true

# --- persistence volumes ------------------------------------------------
# When the runtime mounts /commandhistory or /claude-state (named
# volumes in compose deployments), redirect the session state that
# should survive container recreates into them. Both are skipped when
# the mounts are absent, so a plain "docker run" keeps stock behavior.
if [ -d /commandhistory ]; then
  mkdir -p "$home/.local/share/fish"
  [ -e /commandhistory/fish_history ] || : >/commandhistory/fish_history
  ln -sf /commandhistory/fish_history "$home/.local/share/fish/fish_history"
fi
if [ -d /claude-state ]; then
  # Seed the volume from the image's copy on first start only; after
  # that the volume is the source of truth.
  if [ ! -f /claude-state/claude.json ] && [ -f "$home/.claude.json" ]; then
    cp "$home/.claude.json" /claude-state/claude.json
  fi
  rm -f "$home/.claude.json"
  ln -sf /claude-state/claude.json "$home/.claude.json"

  # Re-merge MCP server definitions into the persisted claude.json. The file
  # survives image updates via /claude-state, but its .mcpServers[].command
  # values embed /nix/store paths that go stale on a new image. relink-managed
  # (above) has already healed ~/.config/mcp/mcp.json to the current store, so
  # re-run the same merge home/claude.nix's syncClaudeJson does at build time.
  # Keep this jq expression in sync with home/claude.nix. Write through the
  # symlink to the real /claude-state file so the redirect is preserved.
  # Resilient under `set -e`: every jq step is guarded so a malformed file
  # can't abort the entrypoint before the main process execs.
  mcp_config="$home/.config/mcp/mcp.json"
  if command -v jq >/dev/null 2>&1 && [ -f "$mcp_config" ]; then
    claude_real=$(readlink -f "$home/.claude.json" 2>/dev/null || echo /claude-state/claude.json)
    if jq empty "$claude_real" 2>/dev/null; then
      existing=$(cat "$claude_real" 2>/dev/null) || existing='{}'
    else
      existing='{}'
    fi
    mcp_servers=$(jq '.mcpServers // {}' "$mcp_config" 2>/dev/null) || mcp_servers='{}'
    if updated=$(printf '%s' "$existing" | jq --argjson mcp "$mcp_servers" \
      '.mcpServers = (.mcpServers // {} | to_entries | map(select(.key as $k | $mcp | has($k) | not)) | from_entries) * $mcp' 2>/dev/null) &&
      tmp=$(mktemp "$claude_real.tmp.XXXXXX" 2>/dev/null); then
      printf '%s\n' "$updated" >"$tmp"
      chmod 600 "$tmp"
      mv "$tmp" "$claude_real"
    fi
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
