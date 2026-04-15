#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/mcp-opentofu.nix"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(curl -sL https://api.github.com/repos/opentofu/opentofu-mcp-server/releases/latest | jq -r '.tag_name' | sed 's/^v//')

echo "mcp-opentofu: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "mcp-opentofu is up-to-date"
  exit 0
fi

# Compute source hash
tarballUrl="https://github.com/opentofu/opentofu-mcp-server/archive/refs/tags/v${latestVersion}.tar.gz"
newSrcHash=$(nix hash to-sri --type sha256 "$(nix-prefetch-url --unpack "$tarballUrl" 2>/dev/null)")
echo "  src hash: $newSrcHash"

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"

# Update src hash (first hash in file)
currentSrcHash=$(grep -Po '(?<=hash = ")[^"]+' "$PKG_FILE" | head -1)
sed -i "0,/$currentSrcHash/s||$newSrcHash|" "$PKG_FILE"

# Invalidate pnpm deps hash so the next build reports the correct one
sed -i "s|hash = \"sha256-[A-Za-z0-9+/=]\+\";|hash = \"sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\";|2" "$PKG_FILE"

echo "mcp-opentofu version + src hash updated to $latestVersion."
echo "Now run 'nix build .#mcp-opentofu' to get the new pnpmDeps hash and paste it into $PKG_FILE."
