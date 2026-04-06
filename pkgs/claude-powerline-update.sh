#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/claude-powerline.nix"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(curl -s https://registry.npmjs.org/@owloops/claude-powerline/latest | jq -r '.version')

echo "claude-powerline: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "claude-powerline is up-to-date"
  exit 0
fi

# Compute tarball hash
tarballUrl="https://registry.npmjs.org/@owloops/claude-powerline/-/claude-powerline-${latestVersion}.tgz"
newHash=$(nix store prefetch-file --json "$tarballUrl" | jq -r '.hash')
echo "  hash: $newHash"

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"

# Update hash
currentHash=$(grep -Po '(?<=hash = ")[^"]+' "$PKG_FILE")
sed -i "s|$currentHash|$newHash|" "$PKG_FILE"

echo "claude-powerline updated to $latestVersion"
