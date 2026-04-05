#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/mcp-argocd.nix"
REPO="argoproj-labs/mcp-for-argocd"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE" | head -1)
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "mcp-argocd: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "mcp-argocd is up-to-date"
  exit 0
fi

# Compute source hash
tarballUrl="https://github.com/$REPO/archive/refs/tags/v${latestVersion}.tar.gz"
srcHash=$(nix store prefetch-file --unpack --json "$tarballUrl" | jq -r '.hash')
echo "  src hash: $srcHash"

# Compute npm deps hash
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
curl -sL "$tarballUrl" | tar -xz -C "$tmpdir" --strip-components=1
npmDepsHash=$(prefetch-npm-deps "$tmpdir/package-lock.json" 2>/dev/null)
echo "  npmDepsHash: $npmDepsHash"

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"

# Update source hash
currentSrcHash=$(grep -A4 'fetchFromGitHub' "$PKG_FILE" | grep -Po '(?<=hash = ")[^"]+')
sed -i "s|$currentSrcHash|$srcHash|" "$PKG_FILE"

# Update npm deps hash
currentNpmHash=$(grep -Po '(?<=npmDepsHash = ")[^"]+' "$PKG_FILE")
sed -i "s|$currentNpmHash|$npmDepsHash|" "$PKG_FILE"

echo "mcp-argocd updated to $latestVersion"
