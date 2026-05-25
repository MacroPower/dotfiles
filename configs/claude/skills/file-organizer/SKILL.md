---
name: file-organizer
description: >-
  Organize, sort, rename, dedupe, or archive files on disk.
  Use when reshaping a directory tree, batch-renaming, deduplicating, sorting media
  by date or location, mounting or extracting disk images, or cleaning up a folder.
  Skip for git operations and code edits.
---

# file-organizer

Filesystem reorganization with the tools available on this machine. Reach
for these before hand-rolling shell loops or one-off scripts.

## Scale safety (read first)

These tools will happily walk a TB-scale tree or millions of files and
appear to hang for many minutes. Three rules:

1. **Probe before recursing.** Always run a depth-1 walk first to learn
   the shape of the tree:

   ```bash
   df -h <dir>                                    # filesystem capacity
   fd --max-depth 1 -t f . <dir> | wc -l          # top-level fanout
   dust -d 1 -n 5 <dir>                           # byte distribution
   ```

   Decide the strategy from these numbers. Never run a recursive command
   on a directory whose size you have not confirmed.

2. **Cap every recursive command.** Always pass the tool's depth, count,
   or size flag. Defaults by phase:

   - probe   = `--max-depth 1`
   - inspect = `--max-depth 2`, `-d 2`, `--level=2`
   - work    = depth 3+ only after a probe shows it is safe

   Never run `dust .`, `fclones group .`, `eza -lT .`, or
   `czkawka_cli image -d .` bare on an unknown tree.

3. **Bound anything still unbounded via the Bash tool itself.** In Claude
   Code, the Bash tool already enforces a timeout: 120000 ms (2 min)
   default, tunable up to 600000 ms (10 min) via the `timeout` parameter.
   Pass `timeout: 30000` (30 s) for probes, `timeout: 300000` (5 min) for
   known long jobs. For anything that may run longer than 10 min, set
   `run_in_background: true`; stdout is captured to a file the harness
   reports back, fetch it with the `Read` tool, and you'll be notified on
   completion. If a bounded run trips its limit, *stop and re-plan*; do
   not bump the limit reflexively. (Other harnesses without these
   parameters can fall back to the shell `timeout` utility, e.g.
   `timeout 30 <cmd>` / `timeout 5m <cmd>` -- see
   [large-fs.md](references/large-fs.md#bounding-long-commands) for
   exit-code semantics.)

**NUL-delimited pipelines at scale.** A million-file Downloads folder
will contain filenames with spaces and newlines. Use
`fd -0 ... | xargs -0 ...` (or `fd ... -X cmd`, which delimits
internally). At this scale, newline-delimited pipelines are a
correctness bug.

Detail and per-tool flag tables: [large-fs.md](references/large-fs.md).

## Naming conventions (read second)

Names and layouts produced by this skill must follow the rules below.
They override any tool's built-in defaults; if a tool's pattern doesn't
fit, post-process with `rnr regex` (or `fd ... -x mv` for cases regex
can't reach).

1. **Charset.** Prefer `[a-z0-9_-]`. Lowercase. Use `-` between words,
   `_` to separate logical fields (date from time, subject from
   version). Other characters are allowed but should be avoided.

2. **Periods are reserved for semantic compound extensions.**
   For example, ones a tool dispatches on.
   Good: `user.schema.json`, `index.d.ts`, `archive.tar.gz`.
   Bad: `my.thing.is.cool.json`, `report.draft.v2.pdf`.

3. **Layout is domain-driven, not type-driven.**
   Group by what the files are *about*,
   not what format they happen to be in.

4. **Dates and times.**

   - Date only:    `yyyy-MM-dd`            (`2026-04-28`)
   - Date + time:  `yyyy-MM-dd_HH-mm-ss`   (`2026-04-28_14-30-00`)

   24-hour clock. RFC 3339 shape with `:` replaced by `-` so the name
   stays path-safe. Timestamps hold wall-clock / civil time as
   recorded -- no zone marker. Any zone information needed downstream
   lives in sidecar metadata (EXIF, a sibling README), not the
   filename. Use date+time only when seconds-precision actually
   matters; otherwise stay at date-only.

Full rationale, edge cases, and normalization recipes:
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
- **Rewrite text inside files** ->
  `sd` (not `sed`).
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

- Inspect first with bounded forms: `eza -lT --level=2 src/`,
  `dust -d 2 src/`, or `fd --max-depth 2 ... | head` before any move
  or rename. See "Scale safety" above for why the caps are mandatory.
- Dry-run destructive ops, e.g. `fd -e jpeg -x echo mv {} {.}.jpg`.
  Verify the printed commands, then execute. Many tools (`rnr`, `fclones`)
  default to dry-run; always check.
- For media, try `photo-cli copy` before writing date / location sorting
  yourself.
