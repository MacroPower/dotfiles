# Gates a stdio MCP server on the presence of project markers in the current
# project scope. Exits 1 when no marker matches and a project context is
# detected, which Claude Code silently treats as server-unavailable.
#
# Args: <marker-glob> [marker-glob...] -- <real-command> [real-args...]
#
# Scope:
#   - in a git repo -> the repo toplevel (no depth cap on fd).
#   - else          -> $PWD with `--max-depth 3`, so a folder-of-repos launch
#                      takes the union across children without a $HOME launch
#                      crawling the whole filesystem.
#
# Decision:
#   - any marker matched (fd quiet exit 0) -> exec.
#   - no match AND inside a repo           -> exit 1 (server dropped).
#   - no match AND no .git reachable       -> exec (fail open).
#   - fd call times out or errors          -> exec (fail open).
#
# Failing open on timeout/error is deliberate: the gate must never be the
# reason Claude Code hangs or loses a tool unexpectedly.

# Per-fd timeout in seconds. Exit 1 from fd means "no match"; anything else
# (including 124 from timeout, or errors) is treated as fail-open.
guard_timeout=2

markers=()
while [ $# -gt 0 ]; do
  case "$1" in
  --)
    shift
    break
    ;;
  *)
    markers+=("$1")
    shift
    ;;
  esac
done

if scope="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  in_repo=1
  depth=()
else
  in_repo=0
  scope="$PWD"
  depth=(--max-depth 3)
fi

# `-q` is required to signal match/no-match via exit 0/1. Without it, fd always
# returns 0 regardless of hits. Repeated `-g` is a mode toggle, not an OR, so
# we invoke fd once per marker.
for m in "${markers[@]}"; do
  rc=0
  timeout "$guard_timeout" fd -q --hidden "${depth[@]}" -g "$m" "$scope" || rc=$?
  case "$rc" in
  0) exec "$@" ;;
  1) ;;
  *) exec "$@" ;;
  esac
done

if [ "$in_repo" -eq 1 ]; then
  exit 1
fi

# Outside a repo: drop only if a child git repo is reachable within the depth
# cap (folder-of-repos that matches none of the markers). `--no-ignore` is
# required because fd skips `.git` by default; `--hidden` because `.git` is a
# hidden directory.
rc=0
timeout "$guard_timeout" fd -q --hidden --no-ignore "${depth[@]}" --type d \
  --glob .git "$scope" || rc=$?
case "$rc" in
0) exit 1 ;;
*) exec "$@" ;;
esac
