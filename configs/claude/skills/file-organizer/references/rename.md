# Rename and bulk text rewrite

Filename rewrites with `rnr regex` and `fd ... -x mv`. File-content
rewrites with `sd`.

## rnr -- regex rename

Regex over filenames with subcommands: `rnr regex`, `rnr from-file`,
`rnr to-ascii`. Defaults to dry-run; `-f`/`--force` to apply.

```bash
rnr regex 'IMG_(\d+)' 'photo-$1' src/*.jpg     # dry-run preview
rnr regex -f 'IMG_(\d+)' 'photo-$1' src/*.jpg  # apply
rnr regex -r 'foo' 'bar' src/                  # recurse into src/
rnr regex -D 'IMG_(\d+)' 'photo-$1' src/       # also rename directories
rnr regex -b 'foo' 'bar' src/                  # backup files before rename
rnr regex -t lower '(.*)' '$1' src/*           # case transform (lower/upper)
```

`rnr regex` always prints a dry-run plan first unless `-f` is set.
`-t <case>` runs the substitution then folds the result -- useful for
normalizing basenames to lowercase regardless of the bash version on
the host (macOS bash 3.2 has no `${var,,}`).

For ad-hoc one-offs, `fd -x mv {} {.}.<new>` (see
[fd in inspect.md](inspect.md#fd----find-files)) is simpler than
reaching for a rename tool.

## sd -- find and replace text

`sed` replacement with PCRE-style regex. Operates on *file contents*, not
file names. For bulk renames, pair `fd` with `mv` or use `rnr regex`.

Scope: bulk rewrites across many non-source files -- notes, configs,
CSV/TSV, generated data. For a single source file, prefer Claude
Code's Edit tool: `sd` rewrites in place with no undo and no diff
preview after the fact.

```bash
sd 'foo' 'bar' notes.md              # replace inside a single file
sd '\bTODO\b' 'DONE' src/*.md        # multiple files (shell expands)
fd --max-depth 4 -e md -x sd 'foo' 'bar'  # rewrite every .md under cwd
sd -p 'foo' 'bar' notes.md           # preview only, do not write
echo 'line' | sd 'l(.)' '$1'         # also reads stdin
```

Capture groups use `$1`, `$2` (not `\1`). For pipelines that read and
overwrite the same file, pair with `sponge` (see
[inspect.md](inspect.md#pipeline-helpers-moreutils)).

## Recipes

Pipeline rename (single extension):

```bash
fd --max-depth 4 -e jpeg . src/ -x echo mv {} {.}.jpg   # dry-run
fd --max-depth 4 -e jpeg . src/ -x mv {} {.}.jpg        # apply
```

For policy-compliance recipes (lowercase basenames, strip generic
prefixes, fold dotted-separator stems), see
[naming.md](naming.md#normalization-recipes).

### Bulk text replacement across a tree

Verify the target subtree is the right scope before running -- `sd`
rewrites in place, no preview after the fact.

```bash
fd --max-depth 4 -e md -x echo sd 'OldName' 'NewName' {}    # dry-run
fd --max-depth 4 -e md -x sd 'OldName' 'NewName' {}         # apply
```

`sd` rewrites file *contents*, not names. For filename rewrites, use
`rnr regex` or the `fd ... -x mv {} {.}.<new>` pattern.

### Flatten a nested tree into one folder

```bash
fd --max-depth 6 -t f . nested/ -x mv {} flat/   # collisions on basename!
fd --max-depth 6 -t f . nested/ -x bash -c '
  rel="${1#nested/}"; mv "$1" "flat/${rel//\//_}"
' _ {}
```

`{/}` is the basename, `{//}` is the *full parent path* (e.g.
`nested/a/b`, not just `b`). The bash form replaces `/` with `_` so each
file gets a unique flat name.
