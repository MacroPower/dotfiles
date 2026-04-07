#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/claude-history.nix"
REPO="raine/claude-history"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "claude-history: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "claude-history is up-to-date"
  exit 0
fi

# Update source hash
newHash=$(nix run nixpkgs#nix-prefetch-github -- raine claude-history --rev "v${latestVersion}" --json 2>/dev/null | jq -r '.hash')
currentHash=$(grep -A5 'fetchFromGitHub' "$PKG_FILE" | grep -Po '(?<=hash = ")[^"]+')
sed -i "s|$currentHash|$newHash|" "$PKG_FILE"
echo "  src hash: $currentHash -> $newHash"

# Reset cargoHash so the next build reveals the correct one
currentCargoHash=$(grep -Po '(?<=cargoHash = ")[^"]+' "$PKG_FILE")
sed -i "s|$currentCargoHash|lib.fakeHash|" "$PKG_FILE"
# Replace quoted fakeHash with the unquoted expression
sed -i 's|cargoHash = "lib.fakeHash"|cargoHash = lib.fakeHash|' "$PKG_FILE"

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"
echo "claude-history updated to $latestVersion"
echo "NOTE: Run 'nix build .#claude-history' to get the correct cargoHash, then update $PKG_FILE"
