#!/usr/bin/env bash
set -euo pipefail

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/chief.nix"
REPO="minicodemonkey/chief"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "chief: current=$currentVersion latest=$latestVersion"
if [[ "$currentVersion" == "$latestVersion" ]]; then
  echo "chief is up-to-date"
  exit 0
fi

# Update hashes for each platform
declare -A platforms=(
  ["aarch64-darwin"]="darwin_arm64"
  ["x86_64-darwin"]="darwin_amd64"
  ["aarch64-linux"]="linux_arm64"
  ["x86_64-linux"]="linux_amd64"
)

for nix_system in "${!platforms[@]}"; do
  suffix="${platforms[$nix_system]}"
  url="https://github.com/$REPO/releases/download/v${latestVersion}/chief_${latestVersion}_${suffix}.tar.gz"
  newHash=$(nix store prefetch-file --json --unpack "$url" | jq -r '.hash')
  currentHash=$(grep -A1 "\"$nix_system\"" "$PKG_FILE" | grep -Po '(?<=hash = ")[^"]+')
  sed -i "s|$currentHash|$newHash|" "$PKG_FILE"
  echo "  $nix_system: $currentHash -> $newHash"
done

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"
echo "chief updated to $latestVersion"
