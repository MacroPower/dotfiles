# yt-dlp (video / audio)

`yt-dlp` is the only correct tool for raw video/audio capture. WACZ
wraps full HTTP fidelity, which is useful for the page surrounding a
video but terrible at storing a long media file -- the WARC bloats
and replay tools mis-handle large `Range` requests.

The user's global config at `~/.config/yt-dlp/config` already sets
the archival defaults: max-resolution sort, mkv container (merge +
remux), embedded metadata / thumbnail / chapters / English subs,
SponsorBlock chapter marks, sane network retries, and output rooted
at `~/Documents/archives/<host>/<YYYY-MM-DD>-<title>.<ext>` with
`--restrict-filenames` applied. **Everything below is overrides for
when those defaults are wrong.** For the common case just supply the
URL.

## Canonical invocation

```bash
yt-dlp <url>
```

## Format overrides

The global config selects max resolution via `-S` sort. To cap or
pin, pass `-f` (which bypasses the sort entirely):

```bash
# Cap at 1080p
yt-dlp -f 'bv*[height<=1080]+ba/b' <url>

# Cap at 720p
yt-dlp -f 'bv*[height<=720]+ba/b' <url>

# Pick a specific format ID (find them with -F)
yt-dlp -F <url>
yt-dlp -f 137+140 <url>
```

`-f` accepts a `/`-separated chain of fallbacks. Each candidate is a
single format spec or a `bv*+ba` combination (best video, best audio).

## Audio extraction

```bash
yt-dlp -x --audio-format opus <url>
```

`-x` (alias `--extract-audio`) demuxes audio and converts to
`--audio-format` (`opus`, `m4a`, `mp3`, `flac`, `wav`, `vorbis`,
`aac`, `best`). `opus` is the smallest modern lossy choice; `m4a` is
the friendliest for Apple devices.

## Playlists, channels, podcast feeds

The default `-o` template doesn't carry playlist hierarchy. Override
the template and cap with `--playlist-end`:

```bash
# Playlist, cap at first 25
yt-dlp --playlist-end 25 \
  -o "$HOME/Documents/archives/%(webpage_url_domain)s/%(playlist_title)s/%(playlist_index)03d-%(title)s.%(ext)s" \
  <playlist-url>

# Channel, cap at most recent 10 uploads
yt-dlp --playlist-end 10 \
  -o "$HOME/Documents/archives/%(webpage_url_domain)s/%(uploader)s/%(upload_date>%Y-%m-%d)s-%(title)s.%(ext)s" \
  <channel-url>

# Podcast RSS feed (audio)
yt-dlp -x --audio-format opus \
  -o "$HOME/Documents/archives/%(webpage_url_domain)s/%(playlist_title)s/%(upload_date>%Y-%m-%d)s-%(title)s.%(ext)s" \
  <rss-url>
```

`--playlist-end N` is shorthand for `--playlist-items :N`;
`--playlist-items` also accepts arbitrary slices (`1,3,5-10,15`).

## SponsorBlock

The config marks chapters non-destructively. To actually cut segments
out of the file:

```bash
yt-dlp --sponsorblock-remove sponsor,selfpromo,interaction <url>
```

Categories: `sponsor`, `intro`, `outro`, `selfpromo`, `interaction`,
`preview`, `music_offtopic`, `filler`.

## Subtitle overrides

Config defaults write + embed English (manual + auto). Common overrides:

```bash
# All languages
yt-dlp --sub-langs all <url>

# Separate .vtt instead of embedded
yt-dlp --write-subs --no-embed-subs --sub-langs en <url>
```

## Cookies from browser

```bash
yt-dlp --cookies-from-browser firefox <url>
yt-dlp --cookies-from-browser 'chrome:Default' <url>
yt-dlp --cookies-from-browser safari <url>
```

Reads the live browser cookie jar without an export step. Supported
browsers: `brave`, `chrome`, `chromium`, `edge`, `firefox`, `opera`,
`safari`, `vivaldi`, `whale`. The optional `:<profile>` suffix selects
a specific profile directory.

Use this for any site with an age-gate, paywall, or member-only feed.

## Download archive

```bash
yt-dlp --download-archive "$HOME/Documents/archives/<host>/.archive.txt" <url>
```

`--download-archive` records every successful download in a flat text
file. On the next run, yt-dlp skips entries already archived.

## Per-host filename overrides

Tweets often lack a `title`, so fall back to `uploader` + `id`:

```bash
yt-dlp -o "$HOME/Documents/archives/%(webpage_url_domain)s/%(upload_date>%Y-%m-%d)s-%(uploader)s-%(id)s.%(ext)s" <url>
```

## Common gotchas

- The default `-o` uses `%(upload_date>%Y-%m-%d)s`. Videos with no
  upload date (rare on YouTube, common on Vimeo / Twitter) produce
  `NA-<title>.<ext>`. Override `-o` if you want a different fallback.
- Passing any `-f` bypasses the global config's `-S` sort -- if you
  want a cap *plus* the config's codec preference, you need a richer
  `-f` expression instead.
- Live streams need `--live-from-start` to capture from the beginning
  rather than from the moment yt-dlp connects.
- Geo-blocked content can be unblocked with `--geo-bypass` or
  `--geo-bypass-country <CC>`, but neither helps when the host
  fingerprints by IP rather than headers.
- `--ignore-config` disables loading the user config file. Rarely
  useful but exists if a one-off must run with vanilla defaults.
