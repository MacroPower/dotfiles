---
name: file-manager
description: >-
  Manage, organize, sort, rename, dedupe, or archive files on disk.
  Use when reshaping a directory tree, batch-renaming, deduplicating, sorting media
  by date or location, mounting or extracting disk images, or cleaning up a folder.
  Skip for git operations and code edits.
---

# file-manager

Filesystem management and reorganization with the tools on this machine.

## Scale safety (read first)

These tools will happily walk a TB-scale tree or millions of files and
appear to hang for many minutes. Four rules:

1. **Probe before recursing.** Run a depth-1 walk first to learn the
   shape of the tree:

   ```bash
   df -h <dir>                                    # filesystem capacity
   fd --max-depth 1 -t f . <dir> | wc -l          # top-level fanout
   dust -d 1 -n 10 <dir>                          # byte distribution
   ```

   Decide the strategy from these numbers. Never run a recursive command
   on a directory whose size you have not confirmed.

2. **Cap every recursive command** with the tool's depth, count, or size
   flag. Defaults by phase:

   - probe   = `--max-depth 1`
   - inspect = `--max-depth 2`, `-d 2`, `--level=2`
   - work    = depth 3+ only after a probe shows it is safe

   Never run `dust .`, `fclones group .`, `eza -lT .`, or
   `czkawka_cli image -d .` bare on an unknown tree.

3. **Bound anything still unbounded** with the Bash tool's `timeout`
   (default 120000 ms, max 600000 ms) or `run_in_background: true` for
   jobs that may exceed 10 min. If a bounded run trips its limit, *stop
   and re-plan*; do not bump the limit reflexively. See
   [large-fs.md](references/large-fs.md#bounding-long-commands) for
   per-job sizing and the non-Claude-Code `timeout(1)` fallback.

4. **NUL-delimited pipes at scale.** Million-file trees contain
   filenames with spaces and newlines. Use `fd -0 ... | xargs -0 ...`
   (or `fd ... -X cmd`, which delimits internally). Newline-delimited
   pipelines are a correctness bug at this scale.

Per-tool flag tables and segmentation patterns:
[large-fs.md](references/large-fs.md).

## Naming conventions (read second)

Output names and layouts must follow these rules. They override any
tool's built-in defaults; post-process with `rnr regex` (or `fd ... -x
mv`) when a tool's pattern doesn't fit.

1. **Charset:** `[a-z0-9_-]`. Lowercase. `-` between words, `_` between
   logical fields (date/time, subject/version).
2. **Periods only for semantic compound extensions** -- ones a tool
   dispatches on (`.tar.gz`, `.d.ts`, `.schema.json`). Version, draft,
   and locale markers belong inside the stem.
3. **Layout is domain-driven, not type-driven.** Group by what the
   files are *about*, not their format.
4. **Dates:** `yyyy-MM-dd` (`2026-04-28`); add `_HH-mm-ss` only when
   seconds matter. Wall-clock / civil time, no zone marker -- zone info
   lives in sidecar metadata.

Rationale, edge cases, normalization recipes:
[naming.md](references/naming.md).

## Quick Reference

- **Photos / videos with EXIF** ->
  `photo-cli copy`. Always try it before scripting EXIF parsing yourself.
  See [photo-cli.md](references/photo-cli.md).
- **Find files / inspect a tree** ->
  `fd` (not `find`),
  `eza -lT` (not `ls`).
  See [inspect.md](references/inspect.md).
- **What's eating disk space** ->
  `dust` (tree summary).
  See [inspect.md](references/inspect.md).
- **Find exact duplicates** ->
  `fclones`.
- **Find similar duplicates** (rotated photos, near-duplicate audio) ->
  `czkawka_cli`.
  See [dedupe.md](references/dedupe.md).
- **Bulk rename** ->
  `rnr regex` for pattern-based renames,
  or `fd ... -x mv {} {.}.<new>` for one-offs in a pipeline.
  See [rename.md](references/rename.md).
- **Bulk-rewrite text inside many non-source files** (notes, configs,
  CSV/TSV, generated data) ->
  `sd` (not `sed`). For single source files prefer the Edit tool --
  `sd` rewrites in place with no undo.
  See [rename.md](references/rename.md).
- **Pull fields from documents** ->
  `jq` (JSON -- see [inspect.md](references/inspect.md)),
  `yq` (YAML).
- **Tweak EXIF beyond `photo-cli`** (e.g. shift timestamps, rename single files by tag) ->
  `exiftool`.
  See [images.md](references/images.md).
- **Optimize images** ->
  `jpegoptim` (JPEG),
  `oxipng` (PNG).
  Verify with `jpeginfo -c` / `pngcheck`.
  See [images.md](references/images.md).
- **Convert / resize images** ->
  `magick` (ImageMagick).
  See [images.md](references/images.md).
- **Extract zip / rar / 7z / tar / tar.gz** ->
  `7zz`.
  See [archives.md](references/archives.md).
- **Mount or extract a disk image** (`.img`, VHD, VMDK, QCOW2) ->
  `7zz` or extraction,
  `apfs-fuse` / `ntfs-3g` / `mount.exfat-fuse` for FUSE mount.
  See [fuse.md](references/fuse.md).
- **Archive a sorted tree** ->
  `tar --zstd`.
  See [archives.md](references/archives.md).
- **Safer delete** ->
  `gomi` instead of `rm`.
  See [delete.md](references/delete.md).
- **Monitor progress** of a long op ->
  `pv` (pipe throughput),
  `progress` (peek at running cp/mv/dd/tar),
  `viddy` (watch the destination grow),
  `ts` (timestamp stderr).
  See [large-fs.md](references/large-fs.md#monitoring-progress).

## Workflow

The pattern for any non-trivial reorganization:

1. **Probe** with bounded walks: `eza -lT --level=2 src/`,
   `dust -d 2 src/`, or `fd --max-depth 2 ... | head`. Don't skip this
   even on small trees -- it's cheap insurance against running
   `dust .` on the wrong directory. Scale safety above explains why
   the caps are mandatory.
2. **Plan the layout** before any move. Domain-driven, not type-driven
   (see [naming.md](references/naming.md)). Sketch the target tree,
   map source files into it, *then* reach for `mv`.
3. **Dry-run destructive ops.** `rnr regex` and `fclones group` both
   default to a preview; `fd ... -x mv` does not, so prefix the action
   with `echo` (`fd ... -x echo mv ...`) and read the printed commands
   before re-running without `echo`.
4. **Apply** with the bounded forms (depth flag, `--max-results`,
   NUL-delimited pipes at scale).
5. **Verify** afterwards: post-move tree (`eza -lT --level=2 dst/`),
   file count parity (`fd -t f . src/ | wc -l` vs `fd -t f . dst/ | wc -l`),
   and -- for destructive ops -- confirm the trash holds what you
   expect before anything is emptied.

For media, try `photo-cli copy` before writing date / location sorting
yourself.
