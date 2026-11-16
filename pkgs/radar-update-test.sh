#!/usr/bin/env bash
# Tests radar-update.sh on copies of radar.nix and radar-desktop.nix.
# The test reads the three files from the directory of this script. It
# does not reach the network, because functions stand in for `gh` and
# `nix`.
#
# The `update-script` test of pkgs/radar.nix runs this script. To run
# it by hand, run `bash pkgs/radar-update-test.sh`.
set -euo pipefail

SRC=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"

# The version that the stand-in for `gh` prints as the latest release.
# No release of radar carries this version.
export LATEST=0.0.0-test

# The file that holds each URL that the stand-in for `nix` received.
export FETCHED=$work/fetched

# gh stands in for `gh release view`. It prints the version in LATEST.
gh() {
  echo "$LATEST"
}

# nix stands in for `nix store prefetch-file --json URL`. It appends the
# URL to the file at FETCHED. It fails for a URL that holds the text in
# FAIL_FETCH. For every other URL, it prints a hash that ends in the
# file name of the URL.
nix() {
  local url=${!#}
  echo "$url" >>"$FETCHED"
  if [[ -n ${FAIL_FETCH:-} && $url == *"$FAIL_FETCH"* ]]; then
    echo "error: unable to download '$url': HTTP error 404" >&2
    return 1
  fi
  echo "{\"hash\": \"sha256-${url##*/}\"}"
}

# The script runs under bash, and bash runs an exported function before
# a program of the same name.
export -f gh nix

# fail MESSAGE prints the message and the output of the last run of the
# script, and it ends the test.
fail() {
  echo "radar-update-test: $1" >&2
  if [[ -f out ]]; then
    sed 's/^/  /' out >&2
  fi
  exit 1
}

# fresh puts a copy of the script and of each package file in pkgs/.
fresh() {
  rm -rf pkgs
  mkdir pkgs
  cp "$SRC/radar-update.sh" "$SRC/radar.nix" "$SRC/radar-desktop.nix" pkgs/
  chmod u+w pkgs/*
}

# update runs the script as the pkg:update task runs it, with the
# arguments of the function. The output of the script goes to the file
# `out`.
update() {
  : >"$FETCHED"
  bash pkgs/radar-update.sh "$@" >out 2>&1
}

# same DIR succeeds when each package file in pkgs/ equals the file of
# the same name in DIR.
same() {
  cmp -s pkgs/radar.nix "$1/radar.nix" && cmp -s pkgs/radar-desktop.nix "$1/radar-desktop.nix"
}

# version FILE prints the version that FILE holds.
version() {
  grep -Po '(?<=version = ")[^"]+' "$1"
}

# hash_of FILE NAME prints the hash of the archive whose URL ends in
# NAME. Each package file holds the hash of an archive on the line after
# the URL of that archive.
hash_of() {
  grep -A1 -F "$2\";" "$1" | grep -Po '(?<=hash = ")[^"]+'
}

# rest FILE prints each line of FILE that holds no version and no hash.
rest() {
  grep -v -e 'version = "' -e 'hash = "' "$1"
}

# A release that lacks an archive. The download of that archive fails,
# and the script must leave both package files unchanged. The script
# downloads the archive of radar-desktop after the archives of radar.
for archive in radar-desktop_ _linux_amd64; do
  fresh
  if FAIL_FETCH=$archive update; then
    fail "the script succeeded although the download of the $archive archive failed"
  fi
  same "$SRC" ||
    fail "the script changed a package file although the download of the $archive archive failed"
done

# A release that holds every archive. Each package file must hold the
# latest version, and the hash of each archive after the URL of that
# archive.
fresh
update || fail 'the script failed on a release that holds every archive'
[[ $(version pkgs/radar.nix) == "$LATEST" && $(version pkgs/radar-desktop.nix) == "$LATEST" ]] ||
  fail 'the script did not write the latest version to both package files'
for name in _darwin_arm64.tar.gz _darwin_amd64.tar.gz _linux_arm64.tar.gz _linux_amd64.tar.gz; do
  [[ $(hash_of pkgs/radar.nix "$name") == "sha256-radar_v$LATEST$name" ]] ||
    fail "radar.nix does not hold the hash of the $name archive after the URL of that archive"
done
[[ $(hash_of pkgs/radar-desktop.nix _darwin_universal.zip) == "sha256-radar-desktop_v${LATEST}_darwin_universal.zip" ]] ||
  fail 'radar-desktop.nix does not hold the hash of its archive'
for file in radar.nix radar-desktop.nix; do
  [[ $(rest "pkgs/$file") == "$(rest "$SRC/$file")" ]] ||
    fail "the script changed a line of $file that holds no version and no hash"
done
mkdir updated
cp pkgs/radar.nix pkgs/radar-desktop.nix updated/

# Both package files at the latest version. The script must download no
# archive and change no file.
update || fail 'the script failed on package files at the latest version'
[[ ! -s $FETCHED ]] ||
  fail 'the script downloaded an archive for package files at the latest version'
same updated || fail 'the script changed a package file at the latest version'

# The same files with --force. The script must download the archives
# again. Each replacement then puts back the text that the file already
# holds, and the script must not count that replacement as a failure.
update --force || fail 'the script failed with --force on package files at the latest version'
[[ -s $FETCHED ]] ||
  fail 'the script downloaded no archive with --force'
same updated || fail 'the script changed a package file with --force although the release is the same'

# radar.nix at the latest version and radar-desktop.nix at the version
# before it. A run that stopped between its two writes leaves the files
# in this state. Without --force, the script must bring
# radar-desktop.nix to the latest version.
cp "$SRC/radar-desktop.nix" pkgs/radar-desktop.nix
update || fail 'the script failed when only radar-desktop.nix was behind the latest version'
same updated ||
  fail 'the script did not bring radar-desktop.nix to the latest version when only that file was behind'

# A version in radar-desktop.nix that sed does not match. sed reads the
# version `0[0]` as a pattern that matches only the text `00`, so the
# command that replaces the version matches no line. The script must
# fail, name the file, and leave both package files unchanged.
fresh
sed -i 's|version = "[^"]*"|version = "0[0]"|' pkgs/radar-desktop.nix
mkdir before
cp pkgs/radar.nix pkgs/radar-desktop.nix before/
if update; then
  fail 'the script succeeded although a replacement in radar-desktop.nix matched no line'
fi
grep -q 'no line of radar-desktop.nix matches' out ||
  fail 'the script did not name the file in which a replacement matched no line'
same before ||
  fail 'the script changed a package file although a replacement in radar-desktop.nix matched no line'

echo "radar-update-test: pass"
