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
fd -t d -X rmdir 2>/dev/null             # rmdir refuses non-empty dirs, so this is safe
```
