# Naming policy

Names and layouts produced by the `file-manager` skill follow these
rules. They override any tool's built-in defaults; if a tool's pattern
doesn't fit, run the tool first, then normalize with `rnr regex` (or
`fd ... -x mv` for cases regex can't reach).

## Charset

Prefer `[a-z0-9_-]`. Lowercase. Use `-` between words, `_` to separate
logical fields (date from time, subject from version). Other characters
are allowed but should be avoided.

## Periods are reserved for semantic compound extensions

A *semantic extension* is one a tool dispatches on -- the kernel,
filesystem, parser, or build system reads it. `.json`, `.tar`, `.zst`,
`.d.ts`, `.schema.json` qualify. Locale tags (`.en`), version markers
(`.v2`), draft markers (`.draft`), and date stamps do **not** -- they
belong inside the stem with `-` or `_`.

When in doubt: would a parser/loader change behavior based on this
suffix? If yes, keep the dot. If no, fold it into `-` or `_`.

## Layout is domain-driven, not media-type-driven

Group by what the files are *about*, not what format they happen to be
in. Media-type folders (`images/`, `videos/`, `pdfs/`) are acceptable
only as a temporary triage step on the way to a domain layout.

Good:

```
.
├── pets/
│   ├── 1.png
│   └── 2.mov
├── invoices/
│   └── 2026-q1.pdf
├── scans/
│   └── passport.jpg
├── talks/
│   └── 2025-strangeloop.mp4
└── receipts/
    └── 2026-04-amazon.pdf
```

Bad:

```
.
├── images/
│   ├── 1.png
│   └── passport.jpg
├── videos/
│   ├── 2.mov
│   └── 2025-strangeloop.mp4
├── pdfs/
│   └── 2026-q1.pdf
└── documents/
    └── amazon-receipt.pdf
```

When the domain is unclear at organize-time, dump into a single
`inbox/` and re-shape later. Don't lean on media folders as a default.

## Dates and times

| Form | Pattern | Example |
| --- | --- | --- |
| Date only | `yyyy-MM-dd` | `2026-04-28` |
| Date + time | `yyyy-MM-dd_HH-mm-ss` | `2026-04-28_14-30-00` |

24-hour clock. RFC 3339 shape with `:` replaced by `-` so the name
stays path-safe. Names carry wall-clock / civil time as recorded --
no zone marker. Any zone information needed downstream lives in
sidecar metadata (EXIF, a sibling README, a manifest), not the
filename.

The full timestamp regex is:

```
^[0-9]{4}-[0-9]{2}-[0-9]{2}(_[0-9]{2}-[0-9]{2}-[0-9]{2})?
```

Pick date-only unless seconds-precision actually matters.

## Normalization recipes

### Lowercase basenames

`rnr regex` ships a built-in case transform (`-t lower`). Prefer it
over a `tr` pipeline -- it works regardless of bash version, which
matters because macOS hosts in this repo ship bash 3.2 (no `${var,,}`).

```bash
rnr regex -t lower '(.*)' '$1' src/*       # dry-run preview
rnr regex -f -t lower '(.*)' '$1' src/*    # apply
```

`rnr` operates on basenames, so directory components are untouched.

### Fold dotted-separator stems into dashes

The hard case. `rnr regex` is single-pass and Rust regex has no
lookahead, so a one-shot "all dots except the last" replacement isn't
possible inside `rnr`. Drop into `fd` + bash and split the basename
explicitly:

```bash
# Dry-run: print what would happen
fd -t f -e json . src/ -x bash -c '
  d=$(dirname "$1"); b=$(basename "$1")
  ext=".${b##*.}"
  stem="${b%.*}"
  new="${stem//./-}${ext}"
  [ "$b" != "$new" ] && echo mv "$1" "$d/$new"
' _ {}

# Apply: drop the echo
fd -t f -e json . src/ -x bash -c '
  d=$(dirname "$1"); b=$(basename "$1")
  ext=".${b##*.}"
  stem="${b%.*}"
  new="${stem//./-}${ext}"
  [ "$b" != "$new" ] && mv "$1" "$d/$new"
' _ {}
```

For multi-extension files (`archive.tar.zst`), the pattern above only
preserves the final segment. If you need to preserve `.tar.zst` as a
unit, scope the walk to that suffix and guard against accidentally
matching plain `.zst` files:

```bash
fd -t f -e zst . src/ -x bash -c '
  b=${1##*/}
  case "$b" in
    *.tar.zst) ;;
    *) exit 0 ;;
  esac
  rest=${b%.tar.zst}
  new="${rest//./-}.tar.zst"
  [ "$b" != "$new" ] && mv "$1" "$(dirname "$1")/$new"
' _ {}
```

### Portable lowercase fallback

If `rnr` isn't on PATH:

```bash
fd -t f . src/ -x bash -c '
  d=$(dirname "$1"); b=$(basename "$1")
  lower=$(printf %s "$b" | tr "[:upper:]" "[:lower:]")
  [ "$b" != "$lower" ] && mv "$1" "$d/$lower"
' _ {}
```

### photo-cli round-trip

The wrapped `photo-cli` with `--naming-style DateTimeWithSecondsAddress`
emits `2026-04-28_14-30-00_<address>.jpg` -- timestamp is
policy-compliant. Lowercase the address slug:

```bash
rnr regex -f -t lower '(.*)' '$1' sorted/*
```

Stock `photo-cli` emits dotted timestamps
(`2026.04.28_14.30.00_<address>.jpg`); run
[Fold dotted-separator stems into dashes](#fold-dotted-separator-stems-into-dashes)
first, then the lowercase pass.

## When to deviate

Rare. Flag the deviation explicitly:

- If a third-party tool *requires* a specific name,
  keep the upstream name intact.
- An upstream archive ships filenames you cannot control without
  losing provenance -- preserve the original under `vendor/` or
  `upstream/` and put the renamed copy alongside.
- The file is a checksum/manifest pointing at other files (`sha1.lst`),
  match whatever names it references.
