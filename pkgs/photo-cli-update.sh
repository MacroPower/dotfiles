#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/photo-cli.nix"
REPO="photo-cli/photo-cli"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "photo-cli: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "photo-cli is up-to-date"
  exit 0
fi

# Prefetch new nupkg hash
url="https://www.nuget.org/api/v2/package/photo-cli/${latestVersion}"
newHash=$(nix store prefetch-file --json "$url" | jq -r '.hash')
currentHash=$(grep -Po '(?<=nugetHash = ")[^"]+' "$PKG_FILE")
sed -i "s|$currentHash|$newHash|" "$PKG_FILE"
echo "  hash: $currentHash -> $newHash"

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"
echo "photo-cli updated to $latestVersion"
