# photo-cli reference

EXIF-driven photo organizer. Non-destructive: `copy` writes to `--output`,
the source is untouched. Project: <https://github.com/photo-cli/photo-cli>,
docs: <https://photocli.com>.

## Subcommands

| Verb | What it does |
| --- | --- |
| `copy` | Copy photos into a new folder hierarchy named by EXIF date/address. |
| `info` | Dump EXIF (date, coordinates, reverse-geocoded address) to a CSV report. |
| `archive` | Index photos into a SQLite database for incremental archiving. |
| `address` | Print the reverse-geocoded address for a single photo. |
| `settings` | List / set persistent settings (API keys, supported extensions, etc.). |

Run `photo-cli help <verb>` for the full flag set on each.

Default supported extensions: `jpg`, `jpeg`, `heic`, `png`. `.mov` companion
files for iPhone Live Photos travel alongside their photo. Extend via
`photo-cli settings -k SupportedExtensions -v jpg,...` and
`photo-cli settings -k CompanionExtensions -v mov,...`.

## Key flags (`copy`)

| Long | Short | Purpose |
| --- | --- | --- |
| `--input` | `-i` | Source folder (default: cwd). Never modified. |
| `--output` | `-o` | Destination folder. |
| `--process-type` | `-f` | Folder strategy: `SubFoldersPreserveFolderHierarchy` (2), `FlattenAllSubFolders`, etc. |
| `--naming-style` | `-s` | File naming pattern: `DateTimeWithSecondsAddress` (8), `DayAddress`, etc. |
| `--folder-append` | `-a` | Decorate folder names: `DayRange` (4), `FirstYearMonthDay`, `FirstYearMonth`, `FirstYear`, `MatchingMinimumAddress`. |
| `--folder-append-location` | `-p` | `Prefix` (1) or `Suffix` for the folder decoration. |
| `--number-style` | `-n` | Suffix style for duplicate timestamps: `PaddingZeroCharacter` (2), etc. |
| `--reverse-geocode` | `-e` | Provider: `OpenStreetMapFoundation` (2), `BigDataCloud` (1), `GoogleMaps` (3), `LocationIq` (5). |
| `--openstreetmap-properties` | `-r` | OSM admin levels to use, space-separated. e.g. `country city town suburb`. |
| `--no-coordinate` | `-c` | Action for photos with no GPS: `Continue` (0), `PreventProcess` (1), `DontCopyToOutput` (2), `InSubFolder` (3). |
| `--no-taken-date` | `-t` | Action for photos with no date: `Continue` (0), `PreventProcess` (1), `DontCopyToOutput` (2), `InSubFolder` (3), `AppendToEndOrderByFileName` (4). |
| `--verify` | `-v` | Verify each copy with SHA1; writes `sha1.lst`. |

`info` shares `-i`, `-o`, `-e`, `-r`, `-c`, `-t`. **Beware:** `-a` means
`--all-folders` for `info` (a boolean) but `--folder-append` for `copy`
(an enum); never paste `-a 4` into an `info` invocation.

## Reverse-geocode providers

| Provider | API key | Rate limit | Use when |
| --- | --- | --- | --- |
| `OpenStreetMapFoundation` (2) | none | 1 req/sec | Default. No setup, fine for personal libraries. |
| `BigDataCloud` (1) | yes | 50k/month | Larger libraries with a free key. |
| `GoogleMaps` (3) | yes | paid | Best precision, paid. |
| `LocationIq` (5) | yes | 5k/day | Free tier, good street-level detail. |

API keys for the paid providers go in `PHOTO_CLI_BIG_DATA_CLOUD_API_KEY`,
`PHOTO_CLI_GOOGLE_MAPS_API_KEY`, or `PHOTO_CLI_LOCATIONIQ_API_KEY` (or via
`photo-cli settings -k <KeyName> -v <key>`). OpenStreetMap needs no key.

## Idiomatic invocations

Inspect EXIF for everything under a directory before doing anything:

```bash
photo-cli info \
  --all-folders \
  --reverse-geocode OpenStreetMapFoundation \
  --openstreetmap-properties country city town suburb \
  --input ~/Downloads/photos \
  --output ~/Downloads/photos-report.csv
```

Organize into `<date-range>-<original-folder>/<datetime-address>.jpg`,
preserving the source tree (the README's headline example):

```bash
photo-cli copy \
  --input ~/Downloads/photos \
  --output ~/Pictures/sorted \
  --process-type SubFoldersPreserveFolderHierarchy \
  --naming-style DateTimeWithSecondsAddress \
  --folder-append DayRange \
  --folder-append-location Prefix \
  --number-style PaddingZeroCharacter \
  --reverse-geocode OpenStreetMapFoundation \
  --openstreetmap-properties country city town suburb \
  --no-coordinate InSubFolder \
  --no-taken-date InSubFolder \
  --verify
```

Same command, short aliases:

```bash
photo-cli copy -i ~/Downloads/photos -o ~/Pictures/sorted \
  -f 2 -s 8 -a 4 -p 1 -n 2 \
  -e 2 -r country city town suburb \
  -c 3 -t 3 -v
```

Flatten everything into `<country>/<city>/<town>/<day-address>.jpg`:

```bash
photo-cli copy \
  --input ~/Downloads/photos \
  --output ~/Pictures/by-place \
  --process-type FlattenAllSubFolders \
  --group-by AddressHierarchy \
  --naming-style DayAddress \
  --reverse-geocode OpenStreetMapFoundation \
  --openstreetmap-properties country city town suburb \
  --number-style OnlySequentialNumbers \
  --no-taken-date AppendToEndOrderByFileName \
  --no-coordinate InSubFolder
```

## Workflow tips

- Run `info` first to verify the EXIF actually contains what you expect.
- `copy` is non-destructive. Inspect the output tree, then delete the source.
- Use `--verify` for important libraries (extra SHA1 pass, writes `sha1.lst`).
- For a large library, use `archive` + a SQLite index instead of repeating
  `copy`. `archive` is incremental.
- Pipe the `info` CSV into `jq`, `awk`, or a spreadsheet to cross-check
  what got geocoded vs. what didn't.
- `no-address` and `no-address-and-no-photo-taken-date` subfolders catch
  EXIF-less files. Triage those by hand.
- The wrapped `photo-cli` already emits the policy-aligned
  dashed timestamp (`yyyy-MM-dd_HH-mm-ss`) -- the home-manager
  module sets the four `Date*Format*` env vars at wrap time, so
  `--naming-style DateTimeWithSecondsAddress` produces
  `2026-04-28_14-30-00_<address>.jpg` directly. The only remaining
  post-processing is to lowercase the reverse-geocoded address
  slug -- see [naming.md](naming.md) for the lowercase recipe.
