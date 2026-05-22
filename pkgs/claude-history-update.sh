#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
PKG_FILE="$DIRNAME/claude-history.nix"
REPO="raine/claude-history"

# Get current and latest versions
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
latestVersion=$(gh release view --repo "$REPO" --json tagName -q '.tagName | ltrimstr("v")')

echo "claude-history: current=$currentVersion latest=$latestVersion"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "claude-history is up-to-date"
  exit 0
fi

# Update source hash
newHash=$(nix run nixpkgs#nix-prefetch-github -- raine claude-history --rev "v${latestVersion}" --json 2>/dev/null | jq -r '.hash')
currentHash=$(grep -A5 'fetchFromGitHub' "$PKG_FILE" | grep -Po '(?<=hash = ")[^"]+')
sed -i "s|$currentHash|$newHash|" "$PKG_FILE"
echo "  src hash: $currentHash -> $newHash"

# Update version
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"

# Resolve cargoHash by building the package; the vendor stage reports the
# correct hash via a deliberate mismatch against lib.fakeHash.
echo "  resolving cargoHash via nix build..."
REPO_ROOT=$(git -C "$DIRNAME" rev-parse --show-toplevel)
PKG_FILE_ABS=$(realpath "$PKG_FILE")
sed -i -E 's|cargoHash = ("[^"]+"\|lib\.fakeHash);|cargoHash = lib.fakeHash;|' "$PKG_FILE"
buildOutput=$(nix build --impure --no-link --expr "
  let
    flake = builtins.getFlake \"$REPO_ROOT\";
    pkgs = import flake.inputs.nixpkgs { system = builtins.currentSystem; };
  in
    pkgs.callPackage $PKG_FILE_ABS { }
" 2>&1 || true)
newCargoHash=$(echo "$buildOutput" | sed -n 's|.*got: *\(sha256-[A-Za-z0-9+/=]*\).*|\1|p' | head -1)
if [[ -z $newCargoHash ]]; then
  echo "ERROR: failed to resolve cargoHash. nix build output:" >&2
  echo "$buildOutput" >&2
  exit 1
fi
sed -i "s|cargoHash = lib.fakeHash;|cargoHash = \"$newCargoHash\";|" "$PKG_FILE"
echo "  cargoHash: $newCargoHash"
echo "claude-history updated to $latestVersion"
