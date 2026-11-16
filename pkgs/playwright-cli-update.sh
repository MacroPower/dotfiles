#!/usr/bin/env bash
# Updates pkgs/playwright-cli.nix and the skill files in
# configs/claude/skills/playwright-cli to the latest release of
# playwright-cli. pkgs/playwright-cli-update-test.sh tests this script.
#
# The skill directory holds the skill files of the release in the
# package file, and you may edit those files in place. The script takes
# each difference between the skill directory and the skill files of
# that release as a local edit. It makes each local edit again in the
# skill files of the latest release.
#
# A local edit conflicts with the latest release when that release
# changes a line of the edit or a line next to the edit. An edit of a
# file also conflicts when that release removes the file. The removal
# of a file from the skill directory conflicts when that release
# changes the file. A file that the skill directory adds conflicts when
# that release adds a file with other text at the same path. The script
# then prints each edit that conflicts, changes no file, and fails.
#
# Options:
#   -f, --force       Update a package file that holds the latest
#                     version.
#   --drop-conflicts  Update although a local edit conflicts. The script
#                     prints each edit that conflicts, and it keeps the
#                     text of the latest release in place of that edit.
set -euo pipefail

FORCE=false
DROP_CONFLICTS=false
for arg in "$@"; do
  case $arg in
  -f | --force) FORCE=true ;;
  --drop-conflicts) DROP_CONFLICTS=true ;;
  esac
done

DIRNAME=$(dirname "$0")
REPO_ROOT=$(git -C "$DIRNAME" rev-parse --show-toplevel)
PKG_FILE="$DIRNAME/playwright-cli.nix"
SKILL_DIR="$REPO_ROOT/configs/claude/skills/playwright-cli"

# 1. Get current and latest versions + the gitHead SHA of each.
currentVersion=$(grep -Po '(?<=version = ")[^"]+' "$PKG_FILE")
currentRev=$(grep -Po '(?<=rev = ")[^"]+' "$PKG_FILE")
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

# Edit a copy of the package file, and merge the skill files next to
# that copy. The script puts both in place only after every download,
# the merge, the build, and every edit succeeded. A run that fails
# before that step leaves the package file and the skill directory
# unchanged. realpath makes the path of the copy absolute, because the
# Nix expression of the build imports the copy by that path. The trap
# also removes the two copies that step 7 makes next to the package
# file and the skill directory.
workDir=$(realpath "$(mktemp -d)")
trap 'rm -rf "$workDir" "$SKILL_DIR.new" "$PKG_FILE.new"' EXIT
newPkgFile="$workDir/playwright-cli.nix"
cp "$PKG_FILE" "$newPkgFile"

# unpack_skill REV DIR unpacks the source archive of the commit REV and
# moves the skill files of that commit to the directory DIR.
unpack_skill() {
  local rev=$1 dir=$2
  mkdir "$dir.archive"
  curl -sL "https://github.com/microsoft/playwright-cli/archive/$rev.tar.gz" |
    tar -xz -C "$dir.archive"
  if [[ ! -f $(echo "$dir.archive"/playwright-cli-*)/skills/playwright-cli/SKILL.md ]]; then
    echo "ERROR: the archive of $rev holds no skills/playwright-cli/SKILL.md" >&2
    exit 1
  fi
  mv "$dir.archive"/playwright-cli-*/skills/playwright-cli "$dir"
}

# 2. Unpack the skill files of both releases. Each set comes from the
#    git tarball at the SHA of its release, so SKILL.md and references/
#    come from the commit of the package. A package file at the latest
#    version names one commit for both releases. The skill files come
#    before the hashes, because the build of step 6 is slow and a
#    conflict stops the run in step 3.
if [[ ! -f $SKILL_DIR/SKILL.md ]]; then
  echo "ERROR: $SKILL_DIR holds no SKILL.md, so the script cannot find the local edits. Restore the directory from git." >&2
  exit 1
fi
newSkillDir=$workDir/new-skill
unpack_skill "$latestRev" "$newSkillDir"
oldSkillDir=$newSkillDir
if [[ $currentRev != "$latestRev" ]]; then
  oldSkillDir=$workDir/old-skill
  unpack_skill "$currentRev" "$oldSkillDir"
fi

# 3. Merge the skill files into $mergedDir. Each path that one of the
#    three trees holds gets one pass of the loop, with these files:
#
#    - $old: the file of the release in the package file.
#    - $mine: the file of the skill directory.
#    - $new: the file of the latest release.
#    - $result: the file that the merged tree holds at the path. It
#      stays empty for a path that the merged tree does not hold.
#
#    The merged tree keeps the text of the latest release in place of
#    each local edit that conflicts.
mergedDir=$workDir/merged-skill
kept=$workDir/kept
conflicts=$workDir/conflicts
mkdir "$mergedDir"
: >"$kept"
: >"$conflicts"
while IFS= read -r file; do
  old=$oldSkillDir/$file
  mine=$SKILL_DIR/$file
  new=$newSkillDir/$file
  result=
  if [[ -f $mine && -f $new ]]; then
    if [[ ! -f $old ]]; then
      old=/dev/null
    fi
    if cmp -s "$mine" "$old"; then
      result=$new
    else
      # Both diff3 commands start from the text of the latest release.
      # `diff3 -3` makes only the local edits that do not conflict.
      # `diff3 -e` makes every local edit, and it puts the local text
      # in place of each change of the release that conflicts. So the
      # two outputs differ by exactly the edits that conflict.
      diff3 -m -3 "$new" "$old" "$mine" >"$workDir/applied"
      diff3 -m -e "$new" "$old" "$mine" >"$workDir/forced"
      result=$workDir/applied
      if ! cmp -s "$workDir/applied" "$workDir/forced"; then
        {
          if [[ $old == /dev/null ]]; then
            echo "$file: the release adds this file, and the skill directory holds it with other text:"
          else
            echo "$file: the release changes a line of these local edits, or a line next to them:"
          fi
          diff -u -L "$file after the update" -L "$file with these edits" \
            "$workDir/applied" "$workDir/forced" || [[ $? == 1 ]]
        } >>"$conflicts"
      fi
      if ! cmp -s "$workDir/applied" "$new"; then
        echo "kept the local edits of $file" >>"$kept"
      fi
    fi
  elif [[ -f $new ]]; then
    if [[ ! -f $old ]]; then
      result=$new
    elif cmp -s "$old" "$new"; then
      echo "kept the removal of $file" >>"$kept"
    else
      result=$new
      echo "$file: the release changes this file, and the skill directory does not hold it" >>"$conflicts"
    fi
  elif [[ -f $mine ]]; then
    if [[ ! -f $old ]]; then
      result=$mine
      echo "kept the local file $file" >>"$kept"
    elif ! cmp -s "$mine" "$old"; then
      {
        echo "$file: the release removes this file, which holds these local edits:"
        diff -u -L "$file of $currentVersion" -L "$file with these edits" \
          "$old" "$mine" || [[ $? == 1 ]]
      } >>"$conflicts"
    fi
  fi
  if [[ -n $result ]]; then
    mkdir -p "$(dirname "$mergedDir/$file")"
    cp "$result" "$mergedDir/$file"
  fi
done < <(
  for dir in "$oldSkillDir" "$SKILL_DIR" "$newSkillDir"; do
    (cd "$dir" && find . -type f)
  done | sed 's|^\./||' | LC_ALL=C sort -u
)
if [[ -s $conflicts && $DROP_CONFLICTS == false ]]; then
  {
    echo "ERROR: these local edits of the skill files conflict with playwright-cli $latestVersion:"
    cat "$conflicts"
    echo "The script changed no file. To update without these edits, run it again with --drop-conflicts. Then make the edits again by hand."
  } >&2
  exit 1
fi

# 4. Source hash via nix-prefetch-github at the gitHead.
newHash=$(nix run nixpkgs#nix-prefetch-github -- microsoft playwright-cli \
  --rev "$latestRev" --json 2>/dev/null | jq -r '.hash')
currentHash=$(grep -A5 'fetchFromGitHub' "$newPkgFile" | grep -Po '(?<=hash = ")[^"]+' || true)

# 5. Patch version, rev, src hash. The src-hash field is bare
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

# 6. Resolve npmDepsHash by building the copy of the package file. The
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

# 7. Put the skill files in place, then the package file. The script
#    first copies the merged tree and the package file next to their
#    destinations. A copy that fails leaves both destinations unchanged.
#    The script then renames each copy to its destination. A rename
#    moves a whole file or a whole directory, so the skill directory
#    never holds the files of two releases. Step 3 of the next run
#    compares that directory with the files of one release.
#
#    The package file comes last because the check in step 1 reads only
#    that file. A run that stops in this step leaves the package file
#    unchanged. The next run then repeats every update that did not need
#    --force.
rm -rf "$SKILL_DIR.new" "$SKILL_DIR.old"
cp -r "$mergedDir" "$SKILL_DIR.new"
cp "$newPkgFile" "$PKG_FILE.new"
mv "$SKILL_DIR" "$SKILL_DIR.old"
mv "$SKILL_DIR.new" "$SKILL_DIR"
rm -rf "$SKILL_DIR.old"
mv "$PKG_FILE.new" "$PKG_FILE"

echo "playwright-cli updated to $latestVersion"
sed 's/^/  /' "$kept"
if [[ -s $conflicts ]]; then
  {
    echo "WARNING: the update dropped these local edits of the skill files, which conflict with playwright-cli $latestVersion:"
    cat "$conflicts"
  } >&2
fi
