#!/usr/bin/env bash
# Updates pkgs/radar.nix and pkgs/radar-desktop.nix to the latest
# release of radar. pkgs/radar-update-test.sh tests this script.
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/radar.nix"
DESKTOP_PKG_FILE="$DIRNAME/radar-desktop.nix"
REPO="skyhook-io/radar"

# replace FILE OLD NEW replaces OLD with NEW on each line of FILE. sed
# reads OLD as a pattern, and it exits with status 0 when no line of
# FILE matches the pattern. The function fails in that case, so the
# script never reports an update that it did not write.
replace() {
  local file=$1 old=$2 new=$3
  if ! grep -q -- "$old" "$file"; then
    echo "radar-update: no line of ${file##*/} matches: $old" >&2
    return 1
  fi
  sed -i "s|$old|$new|" "$file"
}

# Get current and latest versions. Each package file holds its own
# version, and the script reads both. A run that stops between its two
# writes leaves radar.nix at the latest version and radar-desktop.nix at
# an earlier one. The next run then updates radar-desktop.nix.
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
currentDesktopVersion=$(grep -Po '(?<=version = ")[^"]+' "$DESKTOP_PKG_FILE")
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "radar: current=$currentVersion latest=$latestVersion"
echo "radar-desktop: current=$currentDesktopVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $currentDesktopVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "radar and radar-desktop are up-to-date"
  exit 0
fi

# Edit a copy of each package file. The script puts the copies in place
# only after every download and every edit succeeded. A run that fails
# before that step leaves both package files unchanged.
workDir=$(mktemp -d)
trap 'rm -rf "$workDir"' EXIT
newPkgFile="$workDir/radar.nix"
newDesktopPkgFile="$workDir/radar-desktop.nix"
cp "$PKG_FILE" "$newPkgFile"
cp "$DESKTOP_PKG_FILE" "$newDesktopPkgFile"

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
  currentHash=$(grep -A2 "\"$nix_system\"" "$newPkgFile" | grep -Po '(?<=hash = ")[^"]+')
  replace "$newPkgFile" "$currentHash" "$newHash"
  echo "  $nix_system: $currentHash -> $newHash"
done

# Update version
replace "$newPkgFile" "version = \"$currentVersion\"" "version = \"$latestVersion\""

# Update radar-desktop package
desktopUrl="https://github.com/$REPO/releases/download/v${latestVersion}/radar-desktop_v${latestVersion}_darwin_universal.zip"
newDesktopHash=$(nix store prefetch-file --json "$desktopUrl" | jq -r '.hash')
currentDesktopHash=$(grep -Po '(?<=hash = ")[^"]+' "$newDesktopPkgFile")
replace "$newDesktopPkgFile" "$currentDesktopHash" "$newDesktopHash"
replace "$newDesktopPkgFile" "version = \"$currentDesktopVersion\"" "version = \"$latestVersion\""
echo "  radar-desktop: $currentDesktopHash -> $newDesktopHash"

# Put both copies in place
cp "$newPkgFile" "$PKG_FILE"
cp "$newDesktopPkgFile" "$DESKTOP_PKG_FILE"
echo "radar and radar-desktop updated to $latestVersion"
