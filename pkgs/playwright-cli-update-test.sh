#!/usr/bin/env bash
# Tests playwright-cli-update.sh on a copy of playwright-cli.nix and on
# a skill directory that the test creates. The test reads the two files
# from the directory of this script. It does not reach the network,
# because functions stand in for `git`, `curl`, and `nix`.
#
# The `update-script` test of pkgs/playwright-cli.nix runs this script.
# To run it by hand, run `bash pkgs/playwright-cli-update-test.sh`.
set -euo pipefail

SRC=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"

# The directory that stands in for the repository. The stand-in for
# `git` prints it as the root of the repository.
export REPO=$work/repo

# The path of the skill directory from the root of the repository.
SKILL=configs/claude/skills/playwright-cli

# The commit of the release in the package file.
OLD_REV=$(grep -Po '(?<=rev = ")[^"]+' "$SRC/playwright-cli.nix")

# The version and the commit that the stand-in for `curl` prints as the
# latest release. No release of playwright-cli carries this version.
export LATEST=0.0.0-test
export REV=0000000000000000000000000000000000000000

# The hash that the stand-in for `nix` prints for the source of the
# release, and the hash that its build prints for the npm dependencies.
export SRC_HASH=sha256-TestSrc000000000000000000000000000000000000=
export DEPS_HASH=sha256-TestDeps00000000000000000000000000000000000=

# The output of the stand-in for `nix build`. Nix prints these lines for
# a package file whose hash of the npm dependencies is `lib.fakeHash`.
export BUILD_OUTPUT="error: hash mismatch in fixed-output derivation '/nix/store/00000000000000000000000000000000-playwright-cli-$LATEST-npm-deps.drv':
         specified: sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
            got:    $DEPS_HASH"

# The file that holds each archive URL that the stand-in for `curl`
# received.
export FETCHED=$work/fetched

# The file that holds the arguments of each call of the stand-in for
# `nix`.
export NIX_CALLS=$work/nix-calls

# The copy that the stand-in for `nix build` makes of the package file
# in pkgs/. It shows what that file held during the build.
export SNAPSHOT=$work/snapshot.nix

# skill_md DESCRIPTION INSTALL [COMMAND] prints a SKILL.md with the
# description DESCRIPTION. INSTALL is the line of its last section. With
# COMMAND, its list of commands ends in that command.
skill_md() {
  printf '%s\n' '---' 'name: playwright-cli' "description: $1" '---' '' \
    '# Commands' '' 'open' 'close' ${3:+"$3"} '' '# Installation' '' "$2"
}

# The source archive of the release in the package file, which the
# stand-in for `curl` prints for every commit other than REV. As in an
# archive of GitHub, one directory that carries the commit in its name
# holds the files. The skill files are in skills/playwright-cli of that
# directory.
export OLD_ARCHIVE=$work/old.tar.gz
OLD_SKILL=$work/old-release/playwright-cli-$OLD_REV/skills/playwright-cli
mkdir -p "$OLD_SKILL/references"
skill_md 'the description of the release' 'install the package' >"$OLD_SKILL/SKILL.md"
printf '%s\n' 'one' 'two' 'three' >"$OLD_SKILL/references/edited.md"
echo 'changed in a later release' >"$OLD_SKILL/references/changed.md"
echo "removed in $LATEST" >"$OLD_SKILL/references/removed.md"
echo 'not in the skill directory' >"$OLD_SKILL/references/unwanted.md"
echo '{}' >"old-release/playwright-cli-$OLD_REV/package.json"
tar -cz -C old-release -f "$OLD_ARCHIVE" "playwright-cli-$OLD_REV"

# The source archive of the latest release, which the stand-in for
# `curl` prints for the commit REV. The release adds a command to
# SKILL.md. It changes references/changed.md, adds references/added.md,
# and removes references/removed.md.
export ARCHIVE=$work/archive.tar.gz
NEW_SKILL=$work/new-release/playwright-cli-$REV/skills/playwright-cli
mkdir -p "$NEW_SKILL/references"
skill_md 'the description of the release' 'install the package' 'snapshot' >"$NEW_SKILL/SKILL.md"
cp "$OLD_SKILL/references/edited.md" "$OLD_SKILL/references/unwanted.md" "$NEW_SKILL/references/"
echo "changed in $LATEST" >"$NEW_SKILL/references/changed.md"
echo "added in $LATEST" >"$NEW_SKILL/references/added.md"
echo '{}' >"new-release/playwright-cli-$REV/package.json"
tar -cz -C new-release -f "$ARCHIVE" "playwright-cli-$REV"

# A source archive of the latest release that also changes the
# description in SKILL.md.
cp -r new-release conflict-release
skill_md "the description of $LATEST" 'install the package' 'snapshot' \
  >"conflict-release/playwright-cli-$REV/skills/playwright-cli/SKILL.md"
tar -cz -C conflict-release -f conflict.tar.gz "playwright-cli-$REV"

# A source archive that holds no skill files.
mkdir -p "bare/playwright-cli-$REV"
echo '{}' >"bare/playwright-cli-$REV/package.json"
tar -cz -C bare -f bare.tar.gz "playwright-cli-$REV"

# The skill directory before the update. It holds the skill files of
# the release in the package file with these local edits:
#
# - SKILL.md has another description and another line in its last
#   section.
# - references/edited.md has another second line.
# - references/local.md is a file that no release holds.
# - references/unwanted.md is not in the directory.
mkdir -p skill/references
skill_md 'the local description' 'run the package with npx' >skill/SKILL.md
printf '%s\n' 'one' 'a local line' 'three' >skill/references/edited.md
cp "$OLD_SKILL/references/changed.md" "$OLD_SKILL/references/removed.md" skill/references/
echo 'a local file' >skill/references/local.md

# The skill directory after the update. It holds the skill files of the
# latest release with the same local edits.
mkdir -p merged/references
skill_md 'the local description' 'run the package with npx' 'snapshot' >merged/SKILL.md
cp skill/references/edited.md skill/references/local.md merged/references/
cp "$NEW_SKILL/references/changed.md" "$NEW_SKILL/references/added.md" merged/references/

# git stands in for `git -C DIR rev-parse --show-toplevel`. It prints
# the directory in REPO.
git() {
  echo "$REPO"
}

# curl stands in for the downloads of the script. For the URL of the npm
# registry, it prints the latest release. It takes every other URL as
# the URL of a source archive, and it appends that URL to the file at
# FETCHED. It fails when the URL names the commit in FAIL_ARCHIVE. It
# prints the file at ARCHIVE for the commit REV, and the file at
# OLD_ARCHIVE for every other commit.
curl() {
  local url=${!#}
  if [[ $url == *registry.npmjs.org* ]]; then
    echo "{\"version\": \"$LATEST\", \"gitHead\": \"$REV\"}"
    return
  fi
  echo "$url" >>"$FETCHED"
  if [[ -n ${FAIL_ARCHIVE:-} && $url == */$FAIL_ARCHIVE.tar.gz ]]; then
    echo "curl: (6) Could not resolve host: github.com" >&2
    return 6
  fi
  if [[ $url == */$REV.tar.gz ]]; then
    cat "$ARCHIVE"
  else
    cat "$OLD_ARCHIVE"
  fi
}

# nix stands in for the two nix commands of the script. It appends its
# arguments to the file at NIX_CALLS.
#
# For `nix run nixpkgs#nix-prefetch-github`, it prints the hash in
# SRC_HASH. It fails when FAIL_PREFETCH is set.
#
# Every other command is `nix build`, and its last argument is an
# expression that names a package file. The function copies the package
# file in pkgs/ to the file at SNAPSHOT. It prints the text in
# BUILD_OUTPUT when the package file of the expression holds the latest
# version and `lib.fakeHash` for the npm dependencies. It prints an
# error without a hash for every other package file. The function
# fails after both, as `nix build` fails on a hash that does not match.
nix() {
  echo "$*" >>"$NIX_CALLS"
  case "$*" in
  *nix-prefetch-github*)
    if [[ -n ${FAIL_PREFETCH:-} ]]; then
      echo "error: unable to download the source" >&2
      return 1
    fi
    echo "{\"hash\": \"$SRC_HASH\"}"
    ;;
  *)
    local file
    file=$(grep -o '/[^" ]*/playwright-cli\.nix' <<<"${!#}" | head -1)
    cp "$REPO/pkgs/playwright-cli.nix" "$SNAPSHOT"
    if ! grep -q "version = \"$LATEST\"" "$file" || ! grep -q 'npmDepsHash = lib.fakeHash;' "$file"; then
      echo "error: the package file '$file' does not hold the latest version and lib.fakeHash" >&2
      return 1
    fi
    echo "$BUILD_OUTPUT" >&2
    return 1
    ;;
  esac
}

# cp stands in for the cp program. It fails when FAIL_CP is set and the
# destination ends in the text in FAIL_CP. It runs the cp program for
# every other destination.
cp() {
  if [[ -n ${FAIL_CP:-} && ${!#} == *"$FAIL_CP" ]]; then
    echo "cp: cannot create '${!#}': No space left on device" >&2
    return 1
  fi
  command cp "$@"
}

# mv stands in for the mv program. It fails when FAIL_MV is set and the
# destination ends in the text in FAIL_MV. It runs the mv program for
# every other destination.
mv() {
  if [[ -n ${FAIL_MV:-} && ${!#} == *"$FAIL_MV" ]]; then
    echo "mv: cannot move to '${!#}': Permission denied" >&2
    return 1
  fi
  command mv "$@"
}

# The script runs under bash, and bash runs an exported function before
# a program of the same name.
export -f git curl nix cp mv

# fail MESSAGE prints the message and the output of the last run of the
# script, and it ends the test.
fail() {
  echo "playwright-cli-update-test: $1" >&2
  if [[ -f out ]]; then
    sed 's/^/  /' out >&2
  fi
  exit 1
}

# fresh puts a copy of the script and of the package file in pkgs/ of
# the directory in REPO. It puts a copy of `skill` in the skill
# directory.
fresh() {
  rm -rf repo
  mkdir -p repo/pkgs "repo/$SKILL"
  cp "$SRC/playwright-cli-update.sh" "$SRC/playwright-cli.nix" repo/pkgs/
  chmod u+w repo/pkgs/*
  cp -r skill/. "repo/$SKILL/"
}

# update runs the script as the pkg:update task runs it, with the
# arguments of the function. The output of the script goes to the file
# `out`.
update() {
  : >"$FETCHED"
  : >"$NIX_CALLS"
  rm -f "$SNAPSHOT"
  (cd repo && bash pkgs/playwright-cli-update.sh "$@") >out 2>&1
}

# updated succeeds when the directory in REPO holds the files that the
# first update left there, and no other file.
updated() {
  diff -r repo updated >/dev/null
}

# merged succeeds when the skill directory holds the skill files of the
# latest release with the local edits of `skill`, and no other file.
merged() {
  diff -r "repo/$SKILL" merged >/dev/null
}

# field NAME prints the text in double quotes after `NAME = ` in the
# package file.
field() {
  grep -Po "(?<=$1 = \")[^\"]+" repo/pkgs/playwright-cli.nix
}

# rest FILE prints each line of FILE that holds no version, no commit,
# and no hash.
rest() {
  grep -v -e 'version = "' -e 'rev = "' -e 'hash = "' -e 'npmDepsHash = ' "$1"
}

# refuses FAULT runs the script on the files in the directory in REPO.
# The script must fail, and it must neither change a file of that
# directory nor leave a file in it. FAULT names the fault that stops the
# script.
refuses() {
  rm -rf before
  cp -r repo before
  if update; then
    fail "the script succeeded although $1"
  fi
  diff -r repo before >/dev/null ||
    fail "the script changed a file or left a file although $1"
}

# rejects FAULT NAME VALUE runs the script on fresh files with the
# variable NAME set to VALUE in its environment. The script must fail
# and leave the package file and the skill directory unchanged. FAULT
# names the fault that the variable causes.
rejects() {
  fresh
  (export "$2=$3" && refuses "$1")
}

# conflicts FAULT TEXT runs the script on the files in the directory in
# REPO, which hold a local edit that conflicts with the latest release.
# FAULT names the conflict, and TEXT is a part of the lines that the
# script prints for the edit. Without --drop-conflicts, the script must
# fail and change no file. It must print TEXT and name the option, and
# it must run no nix command. With --drop-conflicts, it must print TEXT
# and update the package file.
conflicts() {
  refuses "$1"
  grep -qF -- "$2" out ||
    fail "the script did not print the local edit although $1"
  grep -qF -- '--drop-conflicts' out ||
    fail "the script did not name --drop-conflicts although $1"
  [[ ! -s $NIX_CALLS ]] ||
    fail "the script ran a nix command before it reported that $1"
  update --drop-conflicts ||
    fail "the script failed with --drop-conflicts although only $1"
  grep -qF -- "$2" out ||
    fail "the script did not print the local edit that it dropped because $1"
  [[ $(field version) == "$LATEST" ]] ||
    fail "with --drop-conflicts, the script did not update the package file although only $1"
}

# Faults before the script puts a file in place.
rejects 'the download of the source hash failed' FAIL_PREFETCH 1
rejects 'the build printed no hash' BUILD_OUTPUT 'error: builder failed'
rejects 'the download of the source archive of the latest release failed' FAIL_ARCHIVE "$REV"
rejects 'the download of the source archive of the release in the package file failed' FAIL_ARCHIVE "$OLD_REV"
rejects 'the source archive of the latest release holds no skill files' ARCHIVE "$work/bare.tar.gz"
rejects 'the source archive of the release in the package file holds no skill files' OLD_ARCHIVE "$work/bare.tar.gz"
fresh
rm "repo/$SKILL/SKILL.md"
refuses 'the skill directory holds no SKILL.md'

# A release whose downloads and build succeed. The package file must
# hold the version, the commit, and both hashes of the release. The
# skill directory must hold the skill files of the release with the
# local edits, and no other file. The script must name the files whose
# local edits it kept. The script must not write the package file in
# pkgs/ before the build ends, so the file at SNAPSHOT must equal the
# first package file.
fresh
update || fail 'the script failed on a release whose downloads and build succeed'
[[ $(field version) == "$LATEST" && $(field rev) == "$REV" ]] ||
  fail 'the script did not write the latest version and its commit to the package file'
[[ $(field hash) == "$SRC_HASH" && $(field npmDepsHash) == "$DEPS_HASH" ]] ||
  fail 'the script did not write the hash of the source and of the npm dependencies to the package file'
[[ $(rest repo/pkgs/playwright-cli.nix) == "$(rest "$SRC/playwright-cli.nix")" ]] ||
  fail 'the script changed a line of the package file that holds no version, no commit, and no hash'
merged ||
  fail 'the skill directory does not hold the skill files of the latest release with the local edits and no other file'
for line in 'kept the local edits of SKILL.md' 'kept the local edits of references/edited.md' \
  'kept the local file references/local.md' 'kept the removal of references/unwanted.md'; do
  grep -qFx "  $line" out || fail "the script did not print that it $line"
done
if grep -q 'WARNING' out; then
  fail 'the script printed a warning although every local edit applies'
fi
cmp -s "$SNAPSHOT" "$SRC/playwright-cli.nix" ||
  fail 'the script wrote the package file in pkgs/ before the build ended'
grep -q "/$REV.tar.gz$" "$FETCHED" ||
  fail 'the script did not download the source archive of the commit of the latest release'
grep -q "/$OLD_REV.tar.gz$" "$FETCHED" ||
  fail 'the script did not download the source archive of the commit in the package file'
[[ -z $(find repo -name '*.new' -o -name '*.old') ]] ||
  fail 'the script left a copy next to the package file or the skill directory'
cp -r repo updated

# The package file at the latest version. The script must report it as
# up to date, run no download and no build, and change no file.
update || fail 'the script failed on a package file at the latest version'
grep -q 'playwright-cli is up-to-date' out ||
  fail 'the script did not report a package file at the latest version as up to date'
[[ ! -s $FETCHED && ! -s $NIX_CALLS ]] ||
  fail 'the script ran a download or a build for a package file at the latest version'
updated || fail 'the script changed a file for a package file at the latest version'

# The same files with --force. The script must download and build
# again. Each replacement then puts back the text that the package file
# already holds, and the skill directory keeps its local edits.
update --force || fail 'the script failed with --force on a package file at the latest version'
[[ -s $FETCHED && -s $NIX_CALLS ]] ||
  fail 'the script ran no download or no build with --force'
updated || fail 'the script changed a file with --force although the release is the same'

# The package file at the latest version with `lib.fakeHash` for the npm
# dependencies. That package does not build. Without --force, the script
# must not report it as up to date, and it must write the hash.
sed -i 's|npmDepsHash = "[^"]*";|npmDepsHash = lib.fakeHash;|' repo/pkgs/playwright-cli.nix
update || fail 'the script failed on a package file that holds lib.fakeHash'
if grep -q 'playwright-cli is up-to-date' out; then
  fail 'the script reported a package file that holds lib.fakeHash as up to date'
fi
updated || fail 'the script did not write the hash to a package file that holds lib.fakeHash'

# A run that stops while it copies the merged skill files or the new
# package file next to its destination. The script must change no file.
# Without --force, the next run must then finish the update.
for copy in "$SKILL.new" pkgs/playwright-cli.nix.new; do
  fresh
  FAIL_CP=$copy refuses "the copy to $copy failed"
  update || fail "the script failed in the run after a failed copy to $copy"
  updated || fail "the run after a failed copy to $copy did not finish the update"
done

# A run that stops after it put the skill files in place. The package
# file must still hold its first version, because the next run reads
# that version. The skill directory then holds the skill files of the
# latest release with the local edits. Without --force, the next run
# must finish the update and keep the local edits.
fresh
if FAIL_MV=pkgs/playwright-cli.nix update; then
  fail 'the script succeeded although it did not put the package file in place'
fi
cmp -s repo/pkgs/playwright-cli.nix "$SRC/playwright-cli.nix" ||
  fail 'the script changed the package file although it did not put the package file in place'
merged ||
  fail 'the script did not put the skill files in place before the package file'
[[ -z $(find repo -name '*.new' -o -name '*.old') ]] ||
  fail 'the script left a copy next to the package file or the skill directory'
update || fail 'the script failed in the run after it put only the skill files in place'
updated || fail 'the run after the script put only the skill files in place did not finish the update'

# A local edit of a line that the latest release changes too. With
# --drop-conflicts, SKILL.md must hold the description of the release
# and the local edit of its last section, which does not conflict. The
# script must name SKILL.md as a file whose local edits it kept.
fresh
ARCHIVE=$work/conflict.tar.gz conflicts \
  'the release changes a line of a local edit' '+description: the local description'
skill_md "the description of $LATEST" 'run the package with npx' 'snapshot' |
  cmp -s - "repo/$SKILL/SKILL.md" ||
  fail 'with --drop-conflicts, SKILL.md does not hold the text of the release and the local edit that applies'
grep -qFx '  kept the local edits of SKILL.md' out ||
  fail 'with --drop-conflicts, the script did not print that it kept the local edit of SKILL.md that applies'
diff -r "repo/$SKILL/references" merged/references >/dev/null ||
  fail 'with --drop-conflicts, the script dropped a local edit of a file without a conflict'

# A local edit of a file that the latest release removes. With
# --drop-conflicts, the skill directory must not hold the file.
fresh
echo 'a local note' >>"repo/$SKILL/references/removed.md"
conflicts 'the release removes a file that holds a local edit' '+a local note'
merged || fail 'with --drop-conflicts, the script kept a file that the release removes'

# A file that the skill directory does not hold and that the latest
# release changes. With --drop-conflicts, the skill directory must hold
# the file of the release.
fresh
rm "repo/$SKILL/references/changed.md"
conflicts 'the release changes a file that the skill directory does not hold' 'references/changed.md:'
merged || fail 'with --drop-conflicts, the skill directory does not hold a file that the release changes'

# A file that the latest release adds and that the skill directory holds
# with other text. With --drop-conflicts, the skill directory must hold
# the file of the release.
fresh
echo 'a local reference' >"repo/$SKILL/references/added.md"
conflicts 'the release adds a file that the skill directory holds with other text' '+a local reference'
merged || fail 'with --drop-conflicts, the skill directory does not hold the file that the release adds'

echo "playwright-cli-update-test: pass"
