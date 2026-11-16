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

# The source archive of the release, which the stand-in for `curl`
# prints. As in an archive of GitHub, one directory that carries the
# commit in its name holds the files. The skill files are in
# skills/playwright-cli of that directory.
export ARCHIVE=$work/archive.tar.gz
NEW_SKILL=$work/upstream/playwright-cli-$REV/skills/playwright-cli
mkdir -p "$NEW_SKILL/references"
echo "skill of $LATEST" >"$NEW_SKILL/SKILL.md"
echo "added in $LATEST" >"$NEW_SKILL/references/added.md"
echo '{}' >"upstream/playwright-cli-$REV/package.json"
tar -cz -C upstream -f "$ARCHIVE" "playwright-cli-$REV"

# A source archive that holds no skill files.
mkdir -p "bare/playwright-cli-$REV"
echo '{}' >"bare/playwright-cli-$REV/package.json"
tar -cz -C bare -f bare.tar.gz "playwright-cli-$REV"

# The skill files of the release in the package file. The latest release
# does not hold references/removed.md.
mkdir -p old-skill/references
echo 'skill of the release in the package file' >old-skill/SKILL.md
echo "removed in $LATEST" >old-skill/references/removed.md

# git stands in for `git -C DIR rev-parse --show-toplevel`. It prints
# the directory in REPO.
git() {
  echo "$REPO"
}

# curl stands in for the two downloads of the script. For the URL of the
# npm registry, it prints the latest release. It takes every other URL
# as the URL of the source archive, and it appends that URL to the file
# at FETCHED. It then fails when FAIL_ARCHIVE is set, and it prints the
# file at ARCHIVE otherwise.
curl() {
  local url=${!#}
  if [[ $url == *registry.npmjs.org* ]]; then
    echo "{\"version\": \"$LATEST\", \"gitHead\": \"$REV\"}"
    return
  fi
  echo "$url" >>"$FETCHED"
  if [[ -n ${FAIL_ARCHIVE:-} ]]; then
    echo "curl: (6) Could not resolve host: github.com" >&2
    return 6
  fi
  cat "$ARCHIVE"
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

# The script runs under bash, and bash runs an exported function before
# a program of the same name.
export -f git curl nix cp

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
# the directory in REPO. It puts a copy of old-skill in the skill
# directory.
fresh() {
  rm -rf repo
  mkdir -p repo/pkgs "repo/$SKILL"
  cp "$SRC/playwright-cli-update.sh" "$SRC/playwright-cli.nix" repo/pkgs/
  chmod u+w repo/pkgs/*
  cp -r old-skill/. "repo/$SKILL/"
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

# unchanged succeeds when the package file and the skill directory hold
# what `fresh` put there.
unchanged() {
  cmp -s repo/pkgs/playwright-cli.nix "$SRC/playwright-cli.nix" &&
    diff -r "repo/$SKILL" old-skill >/dev/null
}

# updated succeeds when the package file equals updated/playwright-cli.nix
# and the skill directory holds the skill files of the latest release.
updated() {
  cmp -s repo/pkgs/playwright-cli.nix updated/playwright-cli.nix &&
    diff -r "repo/$SKILL" "$NEW_SKILL" >/dev/null
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

# rejects FAULT NAME VALUE runs the script on fresh files with the
# variable NAME set to VALUE in its environment. The script must fail
# and leave the package file and the skill directory unchanged. FAULT
# names the fault that the variable causes.
rejects() {
  fresh
  if (export "$2=$3" && update); then
    fail "the script succeeded although $1"
  fi
  unchanged ||
    fail "the script changed the package file or the skill directory although $1"
}

# Faults before the script puts a file in place. The script downloads
# the source archive after the build.
rejects 'the download of the source hash failed' FAIL_PREFETCH 1
rejects 'the build printed no hash' BUILD_OUTPUT 'error: builder failed'
rejects 'the download of the source archive failed' FAIL_ARCHIVE 1
rejects 'the source archive holds no skill files' ARCHIVE "$work/bare.tar.gz"

# A release whose downloads and build succeed. The package file must
# hold the version, the commit, and both hashes of the release. The
# skill directory must hold the skill files of the release and no other
# file. The script must not write the package file in pkgs/ before the
# build ends, so the file at SNAPSHOT must equal the first package file.
fresh
update || fail 'the script failed on a release whose downloads and build succeed'
[[ $(field version) == "$LATEST" && $(field rev) == "$REV" ]] ||
  fail 'the script did not write the latest version and its commit to the package file'
[[ $(field hash) == "$SRC_HASH" && $(field npmDepsHash) == "$DEPS_HASH" ]] ||
  fail 'the script did not write the hash of the source and of the npm dependencies to the package file'
[[ $(rest repo/pkgs/playwright-cli.nix) == "$(rest "$SRC/playwright-cli.nix")" ]] ||
  fail 'the script changed a line of the package file that holds no version, no commit, and no hash'
diff -r "repo/$SKILL" "$NEW_SKILL" >/dev/null ||
  fail 'the skill directory does not hold the skill files of the latest release and no other file'
cmp -s "$SNAPSHOT" "$SRC/playwright-cli.nix" ||
  fail 'the script wrote the package file in pkgs/ before the build ended'
grep -q "/$REV.tar.gz$" "$FETCHED" ||
  fail 'the script did not download the source archive of the commit of the latest release'
mkdir updated
cp repo/pkgs/playwright-cli.nix updated/

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
# already holds.
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

# A run that stops while it copies the skill files into the skill
# directory. The package file must still hold its first version, because
# the next run reads that version. Without --force, that run must then
# finish the update.
fresh
if FAIL_CP=$SKILL/ update; then
  fail 'the script succeeded although the copy of the skill files failed'
fi
cmp -s repo/pkgs/playwright-cli.nix "$SRC/playwright-cli.nix" ||
  fail 'the script wrote the package file although the copy of the skill files failed'
update || fail 'the script failed in the run after a failed copy of the skill files'
updated || fail 'the run after a failed copy of the skill files did not finish the update'

echo "playwright-cli-update-test: pass"
