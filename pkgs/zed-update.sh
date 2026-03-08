#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/zed.nix"
REPO="zed-industries/zed"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "zed: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "zed is up-to-date"
  exit 0
fi

# Update srcs hashes
declare -A src_platforms=(
  ["aarch64-darwin"]="Zed-aarch64.dmg"
  ["aarch64-linux"]="zed-linux-aarch64.tar.gz"
  ["x86_64-linux"]="zed-linux-x86_64.tar.gz"
)

for nix_system in "${!src_platforms[@]}"; do
  asset="${src_platforms[$nix_system]}"
  url="https://github.com/$REPO/releases/download/v${latestVersion}/${asset}"
  newHash=$(nix store prefetch-file --json "$url" | jq -r '.hash')
  # Match hash on the line after the system name within the srcs block
  currentHash=$(sed -n "/^  srcs/,/^  };/{ /\"$nix_system\"/,/hash/{ s/.*hash = \"\([^\"]*\)\".*/\1/p; }; }" "$PKG_FILE")
  sed -i "s|$currentHash|$newHash|" "$PKG_FILE"
  echo "  srcs.$nix_system: $currentHash -> $newHash"
done

# Update remoteServerSrcs hashes
declare -A remote_platforms=(
  ["aarch64-linux"]="zed-remote-server-linux-aarch64.gz"
  ["x86_64-linux"]="zed-remote-server-linux-x86_64.gz"
)

for nix_system in "${!remote_platforms[@]}"; do
  asset="${remote_platforms[$nix_system]}"
  url="https://github.com/$REPO/releases/download/v${latestVersion}/${asset}"
  newHash=$(nix store prefetch-file --json "$url" | jq -r '.hash')
  currentHash=$(sed -n "/^  remoteServerSrcs/,/^  };/{ /\"$nix_system\"/,/hash/{ s/.*hash = \"\([^\"]*\)\".*/\1/p; }; }" "$PKG_FILE")
  sed -i "s|$currentHash|$newHash|" "$PKG_FILE"
  echo "  remoteServerSrcs.$nix_system: $currentHash -> $newHash"
done

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"
echo "zed updated to $latestVersion"
