# Delete

Removing files safely.

## gomi (safer rm)

```bash
gomi <file>  # send file to trash
gomi <dir>/  # send directory to trash
```

Trash directory is platform-native (managed config at `~/.config/gomi/config.yaml`):
- macOS: `~/.Trash` (Finder-visible; restore from Finder works).
- Linux: `~/.local/share/Trash` (XDG Trash spec).
- Containers: `.Trash-$uid` (local to mounted files).

Caveats:
- rm-compat flags (`-r`, `-f`, `-i`, etc.) parse but are no-ops -- gomi always recurses.
- To restore, walk the trash directory directly, don't use the gomi CLI.

## Recipes

### Find and remove duplicate files

See [dedupe.md](dedupe.md).

### Find and remove empty subdirectories

```bash
czkawka_cli empty-folders -d src/                    # report only
czkawka_cli empty-folders -d src/ -y                 # move to trash
fd -t d . src/ -x rmdir {} 2>/dev/null               # portable, refuses non-empty
```

The `fd` form is safe by `rmdir`'s refusal to remove non-empty dirs;
prefer `czkawka_cli` on huge trees where the per-dir fork cost adds up.
