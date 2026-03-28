#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/rtk-bin.nix"
REPO="rtk-ai/rtk"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "rtk-bin: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "rtk-bin is up-to-date"
  exit 0
fi

# Update hashes for each platform
declare -A platforms=(
  ["aarch64-darwin"]="aarch64-apple-darwin"
  ["x86_64-darwin"]="x86_64-apple-darwin"
  ["aarch64-linux"]="aarch64-unknown-linux-gnu"
  ["x86_64-linux"]="x86_64-unknown-linux-musl"
)

for nix_system in "${!platforms[@]}"; do
  suffix="${platforms[$nix_system]}"
  url="https://github.com/$REPO/releases/download/v${latestVersion}/rtk-${suffix}.tar.gz"
  newHash=$(nix store prefetch-file --json "$url" | jq -r '.hash')
  currentHash=$(grep -A2 "\"$nix_system\"" "$PKG_FILE" | grep -Po '(?<=hash = ")[^"]+')
  sed -i "s|$currentHash|$newHash|" "$PKG_FILE"
  echo "  $nix_system: $currentHash -> $newHash"
done

# Update hook script hash
hookUrl="https://raw.githubusercontent.com/$REPO/v${latestVersion}/hooks/rtk-rewrite.sh"
newHookHash=$(nix store prefetch-file --json "$hookUrl" | jq -r '.hash')
currentHookHash=$(grep -A2 "hookScript" "$PKG_FILE" | grep -Po '(?<=hash = ")[^"]+')
sed -i "s|$currentHookHash|$newHookHash|" "$PKG_FILE"
echo "  hook: $currentHookHash -> $newHookHash"

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"
echo "rtk-bin updated to $latestVersion"
