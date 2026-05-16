#!/usr/bin/env bash
set -euo pipefail

FORCE=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  esac
done

DIRNAME=$(dirname "$0")
REPO_ROOT=$(git -C "$DIRNAME" rev-parse --show-toplevel)
PKG_FILE="$DIRNAME/playwright-cli.nix"
SKILL_DIR="$REPO_ROOT/configs/claude/skills/playwright-cli"

# 1. Get current and latest versions + the gitHead SHA for the latest.
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
registry=$(curl -s https://registry.npmjs.org/@playwright/cli/latest)
latestVersion=$(jq -r '.version' <<<"$registry")
latestRev=$(jq -r '.gitHead' <<<"$registry")

echo "playwright-cli: current=$currentVersion latest=$latestVersion ($latestRev)"
if [[ $currentVersion == "$latestVersion" && $FORCE == false ]]; then
  echo "playwright-cli is up-to-date"
  exit 0
fi

# 2. Source hash via nix-prefetch-github at the gitHead.
newHash=$(nix run nixpkgs#nix-prefetch-github -- microsoft playwright-cli \
  --rev "$latestRev" --json 2>/dev/null | jq -r '.hash')
currentHash=$(grep -A5 'fetchFromGitHub' "$PKG_FILE" | grep -Po '(?<=hash = ")[^"]+' || true)
currentRev=$(grep -Po '(?<=rev = ")[^"]+' "$PKG_FILE")

# 3. Patch version, rev, src hash; reset npmDepsHash to lib.fakeHash so
#    the next build reports the correct one (same pattern as
#    pkgs/git-surgeon-update.sh). Guard the src-hash sed because the
#    field is bare `lib.fakeHash` (unquoted) until the first build
#    succeeds, and an empty $currentHash would make sed a no-op or
#    error.
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$PKG_FILE"
sed -i "s|rev = \"$currentRev\"|rev = \"$latestRev\"|" "$PKG_FILE"
if [[ -n $currentHash ]]; then
  sed -i "s|$currentHash|$newHash|" "$PKG_FILE"
else
  sed -i "s|hash = lib.fakeHash|hash = \"$newHash\"|" "$PKG_FILE"
fi

currentDepsHash=$(grep -Po '(?<=npmDepsHash = ")[^"]+' "$PKG_FILE" || true)
if [[ -n $currentDepsHash ]]; then
  sed -i "s|npmDepsHash = \"$currentDepsHash\"|npmDepsHash = lib.fakeHash|" "$PKG_FILE"
fi
# Self-heal a stray quoted "lib.fakeHash" regardless of prior state.
sed -i 's|npmDepsHash = "lib.fakeHash"|npmDepsHash = lib.fakeHash|' "$PKG_FILE"

# 4. Re-vendor skill files in lockstep -- drop the old tree, copy from
#    the git tarball at the new SHA so SKILL.md and references/ never
#    drift.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -sL "https://github.com/microsoft/playwright-cli/archive/${latestRev}.tar.gz" |
  tar -xz -C "$tmp"
src=$(echo "$tmp"/playwright-cli-*)
rm -rf "$SKILL_DIR"
mkdir -p "$SKILL_DIR"
cp -r "$src/skills/playwright-cli/." "$SKILL_DIR/"

echo "playwright-cli updated to $latestVersion"
echo "NOTE: Run 'nix build .#playwright-cli' to get the correct npmDepsHash, then update $PKG_FILE"
