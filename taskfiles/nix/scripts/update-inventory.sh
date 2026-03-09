#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
README="$SCRIPT_DIR/../../../README.md"
BEGIN="<!-- BEGIN INVENTORY -->"
END="<!-- END INVENTORY -->"

inventory=$(nix eval --raw .#inventory 2>/dev/null | jq -r -f "$SCRIPT_DIR/inventory.jq")

# Replace everything between the markers. Uses ENVIRON to avoid awk's
# -v escape processing (which mangles backslashes in the content).
INVENTORY="$inventory" awk \
  -v begin="$BEGIN" -v end="$END" '
  $0 == begin { print; printf "%s\n", ENVIRON["INVENTORY"]; skip=1; next }
  $0 == end   { print; skip=0; next }
  !skip
' "$README" >"$README.tmp" && mv "$README.tmp" "$README"
