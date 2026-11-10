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

# On the Lima virtiofs mount, opening .git/index can fail for about a
# second while another process replaces the file. Git reads a missing
# index as an empty one, so the loop exports again when the copy is
# empty.
for attempt in 1 2 3 4 5; do
  git -C "$root" checkout-index --all --prefix="$tmp/"
  [[ -n $(ls -A "$tmp") ]] && break
  if ((attempt == 5)); then
    echo "staged-tree: git read an index with no entries; retry the commit" >&2
    exit 1
  fi
  sleep 1
done

# A hook inherits GIT_DIR and GIT_INDEX_FILE. Unset them so that a git
# command in the copy cannot write to this repository.
# shellcheck disable=SC2046 # the variable names split on whitespace
unset $(git rev-parse --local-env-vars)

cd "$tmp"
"$@"
