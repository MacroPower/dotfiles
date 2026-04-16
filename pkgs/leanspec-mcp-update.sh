#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/leanspec-mcp.nix"
REPO="codervisor/lean-spec"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "leanspec-mcp: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "leanspec-mcp is up-to-date"
  exit 0
fi

# Compute source hash
tarballUrl="https://github.com/$REPO/archive/refs/tags/v${latestVersion}.tar.gz"
srcHash=$(nix store prefetch-file --unpack --json "$tarballUrl" | jq -r '.hash')
echo "  src hash: $srcHash"

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"

# Update source hash
currentSrcHash=$(grep -A4 'fetchFromGitHub' "$PKG_FILE" | grep -Po '(?<=hash = ")[^"]+')
sed -i "s|$currentSrcHash|$srcHash|" "$PKG_FILE"

# Invalidate cargo hash so the next build reports the correct one
currentCargoHash=$(grep -Po '(?<=cargoHash = ")[^"]+' "$PKG_FILE")
sed -i "s|$currentCargoHash|sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=|" "$PKG_FILE"

echo "leanspec-mcp version + src hash updated to $latestVersion."
echo "Now run a build to get the new cargoHash and paste it into $PKG_FILE."
