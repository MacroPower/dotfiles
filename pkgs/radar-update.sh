#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/radar.nix"
REPO="skyhook-io/radar"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "radar: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "radar is up-to-date"
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
  url="https://github.com/$REPO/releases/download/v${latestVersion}/radar_v${latestVersion}_${suffix}.tar.gz"
  newHash=$(nix store prefetch-file --json "$url" | jq -r '.hash')
  currentHash=$(grep -A2 "\"$nix_system\"" "$PKG_FILE" | grep -Po '(?<=hash = ")[^"]+')
  sed -i "s|$currentHash|$newHash|" "$PKG_FILE"
  echo "  $nix_system: $currentHash -> $newHash"
done

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"
echo "radar updated to $latestVersion"

# Update radar-desktop package
DESKTOP_PKG_FILE="$DIRNAME/radar-desktop.nix"
desktopUrl="https://github.com/$REPO/releases/download/v${latestVersion}/radar-desktop_v${latestVersion}_darwin_universal.zip"
newDesktopHash=$(nix store prefetch-file --json "$desktopUrl" | jq -r '.hash')
currentDesktopHash=$(grep -Po '(?<=hash = ")[^"]+' "$DESKTOP_PKG_FILE")
sed -i "s|$currentDesktopHash|$newDesktopHash|" "$DESKTOP_PKG_FILE"
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$DESKTOP_PKG_FILE"
echo "radar-desktop: $currentDesktopHash -> $newDesktopHash"
