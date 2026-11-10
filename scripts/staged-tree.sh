#!/usr/bin/env bash
# Runs a command in a temporary copy of the staged tree.
#
# usage: staged-tree.sh <command> [<argument>...]
#
# A pre-commit hook that reads the working tree also sees the changes
# that the commit leaves unstaged. This script exports the index to a
# temporary directory and runs the command there, so the command reads
# the content that the commit records. In a hook, git sets
# GIT_INDEX_FILE, so `git commit -a` and `git commit <path>` export the
# index that they build.
set -euo pipefail

root=$(git rev-parse --show-toplevel)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

git -C "$root" checkout-index --all --prefix="$tmp/"

# A hook inherits GIT_DIR and GIT_INDEX_FILE. Unset them so that a git
# command in the copy cannot write to this repository.
# shellcheck disable=SC2046 # the variable names split on whitespace
unset $(git rev-parse --local-env-vars)

cd "$tmp"
"$@"
