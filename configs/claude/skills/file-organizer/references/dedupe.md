# Dedupe

`fclones` for exact byte-identical matches. `czkawka_cli` for similar /
fuzzy (rotated photos, slightly different audio, re-encodes).

On huge libraries, see [large-fs.md](large-fs.md) for the size-floor
and segment-per-subfolder patterns -- hashing every 4 KB file in a
million-file tree is wasted work and often OOMs.

## fclones -- exact-match dedupe

Finds identical files by hash. Output is a text format that pipes into
the action subcommands.

```bash
fclones group -s 1M src/                 # list duplicate groups, >= 1 MB
fclones group -s 1M src/ --output dups.txt  # save the report
fclones group -s 100M src/               # only files larger than 100 MB
fclones group -s 1M --rf-over 2 src/     # over-replicated (3+ copies)
fclones group -s 1M --depth 2 src/       # cap recursion depth

# Pipe a report into an action:
fclones group -s 1M src/ | fclones remove      # delete duplicates (asks first)
fclones group -s 1M src/ | fclones link        # replace dupes with hardlinks
fclones group -s 1M src/ | fclones link --soft # ...or symlinks
fclones group -s 1M src/ | fclones move dups/  # move dupes into a holding dir
```

Lead with a size floor (`-s 1M`). Hashing every 4 KB file in a
million-file tree is wasted work; the long tail of small files rarely
matters for dedupe. `-s` accepts size suffixes (`1M`, `100M`, `2G`).

Use `fclones move` over `remove` for anything you don't want to
permanently lose.

`fclones group` prints a progress bar + ETA on stderr (suppress with
`--no-progress`).

## czkawka_cli -- similar / fuzzy duplicates

Rotated photos, slightly different audio, re-encodes. Slower than
`fclones` for exact dupes.

On TB-scale photo libraries, segment by top-level subfolder before
running -- `image`/`dup` have no `--depth` flag, so the only way to
bound them is via the input directory or `-R` (top-level only).
The in-memory hash table scales with file count; a million-file run
will OOM on a typical workstation.

```bash
czkawka_cli image -d src/ -m 1048576           # similar images, files >= 1 MiB
czkawka_cli image -d src/ -m 1048576 -s 10     # looser matching (max-diff 10)
czkawka_cli image -d src/ -c 16 -s 15 -m 1048576  # bigger hash, looser match
czkawka_cli image -d src/ -R                   # top-level only (no recursion)
czkawka_cli dup -d src/ -m 1048576             # exact-hash duplicates, >= 1 MiB
czkawka_cli music -d src/                      # similar audio by tag
czkawka_cli empty-folders -d src/              # find empty subdirectories
czkawka_cli big -d src/ -n 50                  # 50 largest files
czkawka_cli video -d src/ -m 10485760          # similar videos, >= 10 MiB
```

`-s` on `image` is `--max-difference` (0-40, default 5). Lower = stricter.

**Two `czkawka_cli` traps to know:**

- **`-R` means `--not-recursive`.** Inverted from the common convention
  (`grep -R`, `cp -R`, `ls -R` all *enable* recursion). The default is
  recursive; pass `-R` to scope to the top level only.
- **`-m` / `-i` take raw bytes**, no `1M` suffix. `fclones -s 1M`
  works; `czkawka_cli -m 1M` does not -- write `-m 1048576` instead.

`czkawka_cli` prints per-stage progress on stderr.

## Recipes

For photo libraries, `photo-cli copy --verify` detects byte-level
duplicates and emits `sha1.lst`. See [photo-cli.md](photo-cli.md).

On 1M+ file libraries, segment per top-level subfolder rather than
running over the whole root --
[large-fs.md](large-fs.md#segmenting-a-tb-tree) has the loop pattern
for both `fclones` and `czkawka_cli`.

### Hash-and-sort cross-check

Independent confirmation of an `fclones` run -- shells out to `sha1sum`
and groups by hash:

```bash
fd --max-depth 4 -0 -t f . src/ | xargs -0 sha1sum | sort > hashes.txt
```

Pipe that into `awk` / `uniq` to find groups, or diff against the
`fclones group` output.
