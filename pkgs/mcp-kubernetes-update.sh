#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/mcp-kubernetes.nix"
REPO="Azure/mcp-kubernetes"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "mcp-kubernetes: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "mcp-kubernetes is up-to-date"
  exit 0
fi

# Update hashes for each platform
declare -A platforms=(
  ["aarch64-darwin"]="darwin-arm64"
  ["x86_64-darwin"]="darwin-amd64"
  ["aarch64-linux"]="linux-arm64"
  ["x86_64-linux"]="linux-amd64"
)

for nix_system in "${!platforms[@]}"; do
  suffix="${platforms[$nix_system]}"
  url="https://github.com/$REPO/releases/download/v${latestVersion}/mcp-kubernetes-${suffix}"
  newHash=$(nix store prefetch-file --json "$url" | jq -r '.hash')
  currentHash=$(grep -A2 "\"$nix_system\"" "$PKG_FILE" | grep -Po '(?<=hash = ")[^"]+')
  sed -i "s|$currentHash|$newHash|" "$PKG_FILE"
  echo "  $nix_system: $currentHash -> $newHash"
done

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"
echo "mcp-kubernetes updated to $latestVersion"
