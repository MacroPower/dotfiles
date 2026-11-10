#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
README="$SCRIPT_DIR/../../../README.md"
BEGIN="<!-- BEGIN INVENTORY -->"
END="<!-- END INVENTORY -->"

# With --staged, evaluate the flake from a copy of the staged tree, so
# the inventory matches the commit and not the unstaged changes around
# it. The copy is not a git repository, so the flake reference is a path.
if [[ ${1:-} == --staged ]]; then
  eval_inventory=("$SCRIPT_DIR/../../../scripts/staged-tree.sh" nix eval --raw "path:.#inventory")
else
  eval_inventory=(nix eval --raw ".#inventory")
fi

inventory=$("${eval_inventory[@]}" 2>/dev/null | jq -r -f "$SCRIPT_DIR/inventory.jq")

# Replace everything between the markers. Uses ENVIRON to avoid awk's
# -v escape processing (which mangles backslashes in the content).
INVENTORY="$inventory" awk \
  -v begin="$BEGIN" -v end="$END" '
  $0 == begin { print; printf "%s\n", ENVIRON["INVENTORY"]; skip=1; next }
  $0 == end   { print; skip=0; next }
  !skip
' "$README" >"$README.tmp" && mv "$README.tmp" "$README"
