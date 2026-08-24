#!/usr/bin/env python3
"""Neutralize Claude Code's sandbox bare-repo protection for top-level
`config` / `hooks` directories.

When the sandbox builds its filesystem deny list it walks the cwd, the git
root, the directories between them, and every --add-dir root, and for each
one it unconditionally write-denies the entries named `config` and `hooks`
if they exist (the `H=["hooks","config"]` list in the bundled deny-list
builder). The intent is bare-repo defense: a bare git dir keeps `config`
and `hooks/` at its root, so git would read config and run hooks straight
out of the working directory. But this loop never checks for the bare-repo
indicators (`HEAD` / `objects` / `refs`) that gate the *rest* of that
protection -- so every ordinary repo with a top-level `config/` folder gets
it mounted read-only under the sandbox.

Blank the list to an empty, byte-length-preserving JS array. The genuine
bare-repo protections are untouched: the git-dir handler still fires only
when the indicators are present, the sandbox still blocks *creating* those
indicator files, and `.git/config` / `.git/hooks` are denied by separate
rules. All that is dropped is a layer that misfires on every normal repo.

Operates on the raw bun-compiled binary in place (same byte length, so the
wrapBuddy offsets and the mach-O layout stay valid); run it before the
binary is wrapped and, on Darwin, before the ad-hoc re-signing hook.
"""

import sys

# The minified array literal itself, without its assignment target. Anthropic
# builds one binary per platform and the minifier picks a different short
# variable name each time (`H=[...]` on linux-arm64, `U=[...]` on
# darwin-arm64), so keying off the assignment would only patch one platform.
# The array literal is identical across builds and occurs exactly once.
# Replacing it with an equal-length whitespace-padded empty array keeps the
# surrounding `<var>=[...]` assignment valid. If an upstream refactor changes
# the spelling, the occurrence count below trips and the build fails loudly
# rather than silently reverting to the read-only behavior.
PATTERN = b'["hooks","config"]'
REPLACEMENT = b"[" + b" " * 16 + b"]"  # empty array, identical length

assert len(REPLACEMENT) == len(PATTERN), "replacement must preserve byte length"


def main(path: str) -> None:
    with open(path, "rb") as f:
        data = f.read()

    count = data.count(PATTERN)
    if count != 1:
        sys.exit(
            f"claude-code sandbox patch: expected exactly 1 occurrence of "
            f"{PATTERN!r}, found {count}. The upstream bundle changed -- "
            f"re-verify the deny-list builder before shipping this patch."
        )

    with open(path, "wb") as f:
        f.write(data.replace(PATTERN, REPLACEMENT))


if __name__ == "__main__":
    main(sys.argv[1])
