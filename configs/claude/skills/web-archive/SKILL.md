---
name: web-archive
description: >-
  Archive web pages, videos, and full sites for offline use.

  Trigger on "archive this page", "save this URL offline",
  "snapshot this article", "mirror this site", "download this video",
  "capture this thread", or any request to preserve web content for later.

  SKIP for unrelated uses of "archive", for example tar/zip archives,
  archived issues/PRs, mailing-list archives, and so on.
---

# Web Archive

WACZ-first offline capture for the open web. Web pages, whether static,
JS-rendered, logged-in, or full crawls, go through `btrix` and land as a single
`.wacz` (the format published by the Internet Archive, Library of Congress, and
the Webrecorder ecosystem). Video and audio go through `yt-dlp`. If `btrix`
cannot script the interaction, hand off to `/playwright-cli`.

## Decision matrix

| Input | Command | Output |
| --- | --- | --- |
| Web page (static, JS-rendered, logged-in) | `btrix page <url>` | `<slug>.wacz` |
| Web site / docs section (full crawl) | `btrix site <url> -- --pageLimit N` | `<slug>.wacz` |
| Video, audio, podcast feed | `yt-dlp <url>` | `<slug>.mkv` / `<slug>.opus` |
| Multi-step interactive capture browsertrix cannot script | `/playwright-cli` | PDF / HTML / PNG |

URL-shape heuristics, plus when to escalate from a single page to a
crawl, live in [references/decision-tree.md](references/decision-tree.md).

## Output convention

Default layout:

```
~/Documents/archives/<host>/<YYYY-MM-DD>-<slug>.<ext>
```

- `<host>`: the URL host preserved verbatim. Do not strip `www.` and
  do not alias-normalize (`www.example.com` stays `www.example.com`,
  `x.com` stays `x.com`, `youtu.be` stays `youtu.be`).
- `<slug>`: `slugify` applied to the page title or, when no title is
  available, the last URL path segment.
- `<ext>`: `.wacz` for web pages; `.mkv` / `.opus` for `yt-dlp`.
- Never overwrite an existing archive without an explicit
  "re-archive" intent from the user.
- Override: when the user explicitly names a directory ("save to this
  repo", "save here"), use that instead.

Logged-in profiles persist at `~/Documents/archives/<host>/profiles/profile.tar.gz`
and are reused across crawls.

## Tools

| Tool | Reference |
| --- | --- |
| `btrix` wrapper around `browsertrix-crawler` (WACZ for web pages, including site crawls and logged-in captures) | [references/browsertrix-wacz.md](references/browsertrix-wacz.md) |
| `yt-dlp` (video, audio, podcast feeds) | [references/yt-dlp.md](references/yt-dlp.md) |
| `/playwright-cli` (narrow fallback for interactions browsertrix cannot script) | [references/playwright-handoff.md](references/playwright-handoff.md) |

Each reference holds the canonical invocation, flag reference, and
per-host recipes. Do not restate them here.

## Handing off to playwright-cli

`btrix` handles single-page WACZ, full-site WACZ, and logged-in
captures. Hand off to `/playwright-cli` only when the interaction
itself is the thing `btrix` cannot script: multi-step navigation,
PDF rendering of an already-interactive page, single-element
screenshots, or per-element interaction before capture. Recipe:
[references/playwright-handoff.md](references/playwright-handoff.md).
