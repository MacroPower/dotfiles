# Decision tree: URL shape -> tool

WACZ-first. Browsertrix handles every web-page case. Escalate only
when the input is media, or when the capture requires interaction
browsertrix cannot script.

## Step 1: classify the URL

| URL shape | Tool | Notes |
| --- | --- | --- |
| Anything that renders as a web page | `browsertrix-crawler` | Static HTML, JS-rendered SPAs, infinite-scroll feeds, logged-in dashboards. The crawler runs a real Chromium, so JS execution and dynamic content are free. |
| Video host (`youtube.com`, `youtu.be`, `vimeo.com`, `twitch.tv`, `bilibili.com`, `*.m3u8`) | `yt-dlp` | WACZ is the wrong container for raw media -- use yt-dlp's native muxing. |
| Audio host or podcast feed (`*.mp3`, `*.opus`, `*.m4a`, `anchor.fm`, `soundcloud.com`, RSS audio enclosures) | `yt-dlp` | Same reasoning. |
| Raw `*.pdf` link | `browsertrix-crawler` | The crawler captures the PDF byte-for-byte inside the WARC. Replay shows the original PDF; download via `wacz extract` to get a standalone file. |
| Capture that requires multi-step interaction browsertrix cannot script | `/playwright-cli` handoff | See [playwright-handoff.md](playwright-handoff.md). |

When a URL fits more than one row (a YouTube watch page links to a
video but is itself a web page), pick the row matching the user's
intent. "Save this YouTube video" -> `yt-dlp`. "Snapshot this YouTube
page so I can read the comments offline" -> `browsertrix-crawler`.

## Step 2: single page vs site crawl

| Phrase | Command |
| --- | --- |
| "archive this page", "snapshot this URL", "save this article" | `btrix page <url>` |
| "mirror this site", "archive the docs", "crawl this section" | `btrix site <root-url> -- --pageLimit N` |
| "archive this thread and the next 20 posts" | `btrix site <root-url> -- --extraHops 1 --pageLimit 20` |

`btrix page` sets `--scopeType page` (single page only). `btrix site`
sets `--scopeType prefix` so the crawler stays under the seed path.
Everything after `--` is passed verbatim to the crawler.

`--depth` controls how many link-hops the crawler follows from each
seed. `--extraHops` controls hops the crawler is allowed to take
*outside* the prefix scope (useful for grabbing first-party images on
a CDN host). For smoke tests, `--pageLimit` is the clearest cap --
prefer it over fiddling with depth/hops.

## Step 3: logged-in vs anonymous

Anonymous: no extra flags.

Logged in: create a profile once with `btrix create-login-profile`
from inside `~/Documents/archives/<host>/`, then pass `--profile` on
every subsequent capture (`btrix page <url> --profile`). The wrapper
publishes the noVNC viewport on `6080` and the control page on
`9223`, and handles the `-it`/`-t` selection automatically. Full
recipe in [browsertrix-wacz.md](browsertrix-wacz.md).

## Host normalization rule

The `<host>` segment in `~/Documents/archives/<host>/...` is the URL host
preserved verbatim. Do not strip `www.` and do not alias-normalize.
For example, `x.com` and `twitter.com` resolve to the same service but
should archive under different host folders.

## WACZ flattening rule

After every crawl, flatten `collections/<slug>/<slug>.wacz` up to the
working directory and discard the sibling `archive/`, `pages/`, and
`logs/` directories. The `.wacz` is self-contained for replay; the
intermediates are dead weight. Exact commands and rationale:
[browsertrix-wacz.md](browsertrix-wacz.md).

## Common host heuristics

| Host pattern | Default action |
| --- | --- |
| `twitter.com`, `x.com` (thread or status URL) | `btrix page <url>` (defaults YAML already enables `autoscroll,siteSpecific`) |
| `youtube.com/watch`, `youtu.be/<id>` | yt-dlp (the user almost always wants the video) |
| `youtube.com/@channel`, `/c/`, `/playlist` | yt-dlp with `--playlist-end` |
| `vimeo.com/<id>` | yt-dlp |
| `github.com/<owner>/<repo>` (the repo page, not a clone) | `btrix page <url>`; consider `btrix site <url> -- --extraHops 1` to pull READMEs from subdirs |
| `github.com/.../issues/<n>`, `/pull/<n>` | `btrix page <url>` (autoscroll is on by default for long threads) |
| `docs.*`, `*.readthedocs.io`, `*.gitbook.io` | `btrix site <url> -- --pageLimit <N>` |
| Personal blog post URL | `btrix page <url>` |
| `*.pdf` | `btrix page <url>` (WACZ wraps the PDF byte-for-byte) |
| Logged-in dashboard | `btrix page <url> --profile`; run `btrix create-login-profile --url <login-url>` first |
