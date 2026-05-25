# Inspect

Read-only operations: find files, list trees, measure disk usage,
extract fields, pipeline plumbing.

## fd -- find files

Drop-in `find` replacement: smart-case, gitignore-aware, parallel.

```bash
fd --max-depth 3 -e jpg -e jpeg . src/    # by extension(s), depth-capped
fd -t f -t d 'pattern'                    # files and directories matching pattern
fd --changed-before 30d                   # files modified more than 30 days ago
fd --changed-within 1h                    # files modified in the last hour
fd -H -I 'pattern'                        # include hidden + ignored files
fd --size +100M                           # files larger than 100 MB
fd --max-results 100 -e log               # bounded walk -- halts walkers early
fd -0 'pattern' | xargs -0 mv -t dst/     # NUL-delimited pipe into xargs
fd -e jpeg -x echo mv {} {.}.jpg          # dry-run a per-file rename
fd -e jpeg -x mv {} {.}.jpg               # then drop `echo` to actually rename
fd -e log -X rm                           # batch a single command over all results
```

`-x` runs once per match (placeholders: `{}`, `{/}`, `{//}`, `{.}`, `{/.}`).
`-X` runs once with all results appended (use for batchable commands like
`rm`, `tar`, `wc -l`).

Always pass `--max-depth` when the tree size is unknown. Prefer
`--max-results N` over `| head` -- see
[large-fs.md](large-fs.md#streaming-and-sampling-patterns).

## eza -- list files

`ls` with icons, git status, and tree mode.

```bash
eza -l src/                          # long listing (non-recursive, safe)
eza -lT --level=2 src/               # tree view, two levels deep
eza -lT --level=4 src/               # deeper tree, only after a probe
eza -l --git src/                    # git status column (in a repo)
eza -l --sort=size --reverse src/    # largest first
eza -l --sort=modified src/          # newest at the bottom
eza -lD src/                         # directories only (capital D)
```

Use `eza -lT --level=N` over `tree`: same look, gitignore-aware.
`eza -lT` without `--level` walks every subdirectory -- never run it on
`~`, a NAS mount, or a media archive.

## dust -- disk usage

`dust` prints a non-interactive size tree.

```bash
dust -d 2 .                             # tree, two levels deep
dust -d 3 -n 50 .                       # depth 3, top 50 entries
dust -d 2 -z 1M .                       # only entries >= 1 MB
dust -n 50 -r -d 3 .                    # 50 entries, reversed (largest at top)
dust -T 4 -d 3 .                        # 4 threads
dust -d 3 -X .git -X node_modules src/  # exclude paths
```

`-z 1M` takes a size string, not a bare number -- it filters out the
small-file long tail.

`czkawka_cli big` complements with a flat top-N largest-files list
(see [dedupe.md](dedupe.md#czkawka_cli----similar--fuzzy-duplicates)).

## jq -- JSON processing

For sorting / grouping when metadata lives in JSON.

```bash
jq -r '.[].path' index.json                       # extract a field
jq -r '.[] | select(.size > 1000000) | .path' .   # filter then extract
jq -r '.[] | [.date, .path] | @tsv' index.json    # build TSV for awk/sort
jq -s 'group_by(.date) | map({date: .[0].date, count: length})' .
```

`-r` strips quotes, `-s` slurps stdin into one array.

## Pipeline helpers (moreutils)

```bash
sponge file        # absorb stdin then write to file
                   #  (use for `cmd file | ... | sponge file`)
chronic cmd        # suppress output unless cmd fails
ifne cmd           # run cmd only if stdin is non-empty
pee 'cmd1' 'cmd2'  # tee for command pipelines
```

`sponge` is the safe way to overwrite a file from a pipeline that reads
it -- shell redirection truncates the file before the pipeline starts.
[rename.md](rename.md) leans on `sponge` for in-place rewrites.

`ts` (also from `moreutils`) lives in [large-fs.md](large-fs.md) under
monitoring -- it's a long-job concern, not a per-domain one.

## Recipes

First-contact preflight + thresholds:
[large-fs.md](large-fs.md#preflight-estimate-scale).

### Find what's hogging disk

```bash
dust -d 3 ~/Downloads                            # tree summary, depth 3
dust -d 3 -n 50 -r ~/Downloads                   # 50 entries, largest first
dust -d 3 -n 50 ~/                               # whole-home triage
fd --max-depth 4 -t f --size +500M . ~/          # files larger than 500 MB
czkawka_cli big -d ~/ -n 50                      # 50 largest files (flat list)
czkawka_cli empty-folders -d ~/                  # find empty subdirs
```

Whole-home triage warrants the Bash tool's `timeout: 300000` parameter,
or `run_in_background: true` if the tree could exceed 10 min.

### Build an inventory CSV from a tree

```bash
fd --max-depth 4 -0 -t f . src/ | xargs -0 stat -c '%n,%s,%y' > inventory.csv
```

`-X` (single argv burst) blows ARG_MAX on large trees; the NUL pipe
into `xargs -0` chunks automatically. On a million-file tree this
produces a multi-GB CSV; preflight with `fd --max-depth 1 -t f . src/
| wc -l` and consider segmenting.

GNU `coreutils` is on PATH on this machine, so `stat -c` works on Darwin
and Linux alike.

For photos specifically, `photo-cli info -a -i src/ -o inventory.csv -e 2`
gives a richer CSV (date, coordinates, address). See
[photo-cli.md](photo-cli.md).

If the inventory is already JSON, pipe it through `jq`:

```bash
jq -r '.[] | [.date, .size, .path] | @tsv' index.json | sort > inventory.tsv
```

### Move only the K newest files

```bash
fd -0 -t f --max-results K -e jpg . | xargs -0 mv -t dst/
```

`--max-results` halts the walk early on huge trees. NUL-delimit so
filenames with spaces or newlines survive, and use `mv -t` so a single
`mv` handles all K files.
