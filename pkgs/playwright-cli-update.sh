#!/usr/bin/env bash
# Updates pkgs/playwright-cli.nix and the skill files in
# configs/claude/skills/playwright-cli to the latest release of
# playwright-cli. pkgs/playwright-cli-update-test.sh tests this script.
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

# A package file that holds `lib.fakeHash` does not build, so it is not
# up to date at any version. The script resolves its hashes again.
fakeHash=false
if grep -q '= lib\.fakeHash;' "$PKG_FILE"; then
  fakeHash=true
fi

echo "playwright-cli: current=$currentVersion latest=$latestVersion ($latestRev)"
if [[ $currentVersion == "$latestVersion" && $fakeHash == false && $FORCE == false ]]; then
  echo "playwright-cli is up-to-date"
  exit 0
fi
if [[ $fakeHash == true ]]; then
  echo "  ${PKG_FILE##*/} holds lib.fakeHash"
fi

# Edit a copy of the package file, and unpack the skill files of the
# release next to that copy. The script puts both in place only after
# every download, the build, and every edit succeeded. A run that fails
# before that step leaves the package file and the skill directory
# unchanged. realpath makes the path of the copy absolute, because the
# Nix expression of the build imports the copy by that path.
workDir=$(realpath "$(mktemp -d)")
trap 'rm -rf "$workDir"' EXIT
newPkgFile="$workDir/playwright-cli.nix"
cp "$PKG_FILE" "$newPkgFile"

# 2. Source hash via nix-prefetch-github at the gitHead.
newHash=$(nix run nixpkgs#nix-prefetch-github -- microsoft playwright-cli \
  --rev "$latestRev" --json 2>/dev/null | jq -r '.hash')
currentHash=$(grep -A5 'fetchFromGitHub' "$newPkgFile" | grep -Po '(?<=hash = ")[^"]+' || true)
currentRev=$(grep -Po '(?<=rev = ")[^"]+' "$newPkgFile")

# 3. Patch version, rev, src hash. The src-hash field is bare
#    `lib.fakeHash` (unquoted) until the first build succeeds.
#    $currentHash is then empty, and sed fails on an empty pattern. So
#    the script replaces `lib.fakeHash` in that case.
sed -i "s|version = \"$currentVersion\"|version = \"$latestVersion\"|" "$newPkgFile"
sed -i "s|rev = \"$currentRev\"|rev = \"$latestRev\"|" "$newPkgFile"
if [[ -n $currentHash ]]; then
  sed -i "s|$currentHash|$newHash|" "$newPkgFile"
else
  sed -i "s|hash = lib.fakeHash|hash = \"$newHash\"|" "$newPkgFile"
fi

# 4. Resolve npmDepsHash by building the copy of the package file. The
#    deps-fetch stage reports the correct hash via a deliberate mismatch
#    against lib.fakeHash.
echo "  resolving npmDepsHash via nix build..."
sed -i -E 's|npmDepsHash = ("[^"]+"\|lib\.fakeHash);|npmDepsHash = lib.fakeHash;|' "$newPkgFile"
buildOutput=$(nix build --impure --no-link --expr "
  let
    flake = builtins.getFlake \"$REPO_ROOT\";
    pkgs = import flake.inputs.nixpkgs { system = builtins.currentSystem; };
  in
    pkgs.callPackage \"$newPkgFile\" { }
" 2>&1 || true)
newDepsHash=$(echo "$buildOutput" | sed -n 's|.*got: *\(sha256-[A-Za-z0-9+/=]*\).*|\1|p' | head -1)
if [[ -z $newDepsHash ]]; then
  echo "ERROR: failed to resolve npmDepsHash. nix build output:" >&2
  echo "$buildOutput" >&2
  exit 1
fi
sed -i "s|npmDepsHash = lib.fakeHash;|npmDepsHash = \"$newDepsHash\";|" "$newPkgFile"
echo "  npmDepsHash: $newDepsHash"

# 5. Unpack the skill files from the git tarball at the new SHA, so
#    that SKILL.md and references/ come from the commit of the package.
mkdir "$workDir/archive"
curl -sL "https://github.com/microsoft/playwright-cli/archive/${latestRev}.tar.gz" |
  tar -xz -C "$workDir/archive"
newSkillDir=$(echo "$workDir"/archive/playwright-cli-*)/skills/playwright-cli
if [[ ! -f $newSkillDir/SKILL.md ]]; then
  echo "ERROR: the archive of $latestRev holds no skills/playwright-cli/SKILL.md" >&2
  exit 1
fi

# 6. Put the skill files in place, then the package file. Drop the old
#    skill tree first, so that no file of the old release stays in it.
#    The package file comes last because the check in step 1 reads only
#    that file. A run that stops in this step leaves the package file
#    unchanged. The next run then repeats every update that did not need
#    --force.
rm -rf "$SKILL_DIR"
mkdir -p "$SKILL_DIR"
cp -r "$newSkillDir/." "$SKILL_DIR/"
cp "$newPkgFile" "$PKG_FILE"

echo "playwright-cli updated to $latestVersion"
