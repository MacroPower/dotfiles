#!/bin/sh
# Re-link home-manager-managed files into $HOME against the CURRENT image's
# generation (see ssh.go). Heals stale nix-store symlinks left behind by a
# bind-mounted/persisted config dir (e.g. ~/.claude or ~/.config) that shadows
# the freshly-baked links: after an image update the persisted links point at
# /nix/store paths the new image no longer has.
#
# This reimplements home-manager's linkGeneration file-linking step, and ONLY
# that step, so it stays daemon-free -- no `nix-build` probe, no profile
# mutation, no onChange hooks (all of which the baked `activate` script does and
# which are wasteful or fatal against the published image's stripped, read-only
# /nix). Reference: home-manager modules/files.nix (linkNewGen, insertFile,
# cleanOldGen).
#
# Only paths the current generation OWNS are touched: its home-files/ tree, plus
# pre-existing dead links whose target matches home-manager's ownership glob.
# Non-managed user state (~/.claude/projects, ~/.claude/todos, conversation
# history, ...) is never enumerated and never modified.
#
# Caveat: the generation gcroot under ~/.local/state/home-manager MUST come from
# the fresh image layer. Persisting ~/.local/state (or all of $HOME) makes
# current-home itself stale, so this heal would resolve the OLD generation and
# do nothing useful. Persist specific config dirs (~/.claude, ~/.config), not
# ~/.local/state.
#
# Deliberately not `set -e`: one bad entry must not abort the heal.
set -u

home=/home/dev
gcroot="$home/.local/state/home-manager/gcroots/current-home"

# Resolve the current generation, then its home-files tree. home-manager links
# each leaf at the RESOLVED store path of home-files, not at the gcroot symlink
# (files.nix does `newGenFiles="$(readlink -e .../home-files)"` before
# `ln -Tsf`), so mirror that to land on /nix/store/<hash>-home-manager-files/...
gen=$(readlink -f "$gcroot" 2>/dev/null) || gen=
hf=
[ -n "$gen" ] && hf=$(readlink -e "$gen/home-files" 2>/dev/null)
if [ -z "$hf" ] || [ ! -d "$hf" ]; then
  # No managed tree (bare `docker run fish -l`, or a non-home-manager base).
  # Quietly do nothing.
  exit 0
fi

# Pass 1: materialize the real merged directories (~/.claude, ~/.config, and
# `recursive = true` sources like ~/.zed_server, which home-manager lays down
# with lndir as a real dir of per-file symlinks) so leaf links have a parent. A
# stale symlink sitting where a real dir belongs is replaced; a real dir is kept
# (it may hold user state).
find "$hf" -type d | while IFS= read -r d; do
  rel=${d#"$hf"}
  rel=${rel#/}
  [ -n "$rel" ] || continue
  dest="$home/$rel"
  [ -L "$dest" ] && rm -f "$dest"
  mkdir -p "$dest"
done

# Pass 2: recreate every managed leaf. home-manager enumerates both symlinks and
# regular files (`-type f -o -type l`): a source whose executable bit differs
# from the expected mode is materialized by `cp` (a real file) rather than a
# symlink.
find "$hf" \( -type f -o -type l \) | while IFS= read -r src; do
  rel=${src#"$hf"}
  rel=${rel#/}
  [ -n "$rel" ] || continue
  dest="$home/$rel"
  # A real directory at a leaf slot means user state lives where a managed file
  # is expected; do not clobber it.
  if [ -d "$dest" ] && [ ! -L "$dest" ]; then
    echo "relink-managed: $dest is a real dir at a managed slot, skipping" >&2
    continue
  fi
  mkdir -p "$(dirname "$dest")"
  if [ -L "$src" ]; then
    # -n replaces an existing dir-symlink instead of writing through it into the
    # old target directory.
    ln -sfn "$hf/$rel" "$dest"
  else
    rm -f "$dest"
    cp -f "$hf/$rel" "$dest"
  fi
done

# Pass 3: orphan cleanup (mirrors home-manager's cleanOldGen). A path managed by
# the old image but dropped in the new one leaves a dangling link in the
# persisted volume. Within each managed parent dir, drop symlinks that
# home-manager owns (target matches *-home-manager-files/*) but no longer
# resolve. This only ever removes home-manager-owned dead links, never user
# files.
find "$hf" -type d | while IFS= read -r d; do
  rel=${d#"$hf"}
  rel=${rel#/}
  destdir="$home/$rel"
  [ -d "$destdir" ] || continue
  for entry in "$destdir"/* "$destdir"/.*; do
    [ -L "$entry" ] || continue
    case "$(readlink "$entry")" in
    *-home-manager-files/*) [ -e "$entry" ] || rm -f "$entry" ;;
    esac
  done
done

exit 0
