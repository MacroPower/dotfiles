# browsertrix-crawler (WACZ)

The primary tool for every web-page capture. WACZ is a ZIP envelope
around WARC plus a page index and CDX metadata; it is the format used
by the Internet Archive, the Library of Congress, and the Webrecorder
ecosystem. Replay works in any WACZ-compatible viewer (replayweb.page,
wabac.js, pywb).

Drive the crawler through the `btrix` wrapper. The wrapper owns the
`docker run` for `webrecorder/browsertrix-crawler`, mounts `$PWD` as
`/crawls/`, and injects a Nix-rendered defaults YAML
(`/etc/btrix/defaults.yaml`) positionally before user args so
per-call flags still override. Defaults enabled out of the box:
`--generateWACZ`, `--workers 4`, `--behaviors autoscroll,siteSpecific`.

Source: `home/btrix.nix` (`dotfiles.btrix.enable`). Override the image
tag, archives root, defaults attrset, or `extraDockerArgs` per host.

## Canonical single-page capture

```bash
btrix page <url>
```

Lands at `~/Documents/archives/<host>/<YYYY-MM-DD>-<slug>.wacz` and
prints the resolved path on stdout. Slug comes from the page
`<title>`; override with `--slug <arg>` (still slugified and
date-prefixed):

```bash
btrix page https://example.com/foo --slug "custom title"
# -> ~/Documents/archives/example.com/<YYYY-MM-DD>-custom-title.wacz
```

## Site crawl

```bash
btrix site <root-url> -- --pageLimit 100
```

`btrix site` sets `--scopeType prefix` so the crawler stays under the
seed path. Always cap site crawls with `--pageLimit`.

Scoping flags (pass after `--`):

| Flag | Effect |
| --- | --- |
| `--scopeType host` | Stay on the same host, regardless of path. Overrides the helper's `prefix`. |
| `--scopeType domain` | Stay on the same registrable domain (allows subdomains). |
| `--scopeType any` | Follow every link. Almost always wrong. |
| `--depth N` | Per-seed crawl depth (link hops from the seed). |
| `--extraHops N` | Extra hops permitted *beyond* the prefix scope (useful for first-party images on a CDN host). |
| `--pageLimit N` | Hard cap on total pages. The clearest dial for smoke tests. |
| `--include <regex>` | Whitelist URL pattern (repeatable). |
| `--exclude <regex>` | Blacklist URL pattern (repeatable). |

For ad-hoc smoke crawls, pin `--pageLimit` rather than fiddling with
depth/hops -- it bounds the run time predictably.

## Behaviors

`autoscroll` and `siteSpecific` are on by default. Override per call
with `-- --behaviors <list>`:

| Behavior | Effect |
| --- | --- |
| `autoscroll` | Scrolls to the bottom, triggering lazy-loaded content. Use for infinite-scroll feeds (Twitter, Mastodon, long Reddit threads). |
| `autoplay` | Auto-plays media so it loads into the WARC. |
| `autofetch` | Fires loaders for `srcset`, `<link rel=preload>`, and other speculative assets. |
| `siteSpecific` | Enables host-specific behaviors (e.g. for Twitter / Instagram patterns). |

Example for a Twitter thread with autoplay added on top of the defaults:

```bash
btrix page https://x.com/<user>/status/<id> -- --behaviors autoscroll,siteSpecific,autoplay
```

## Profile creation (logged-in captures)

```bash
mkdir -p ~/Documents/archives/<host> && cd ~/Documents/archives/<host>
btrix create-login-profile --url <login-url>
```

The wrapper publishes both ports the profile UI needs:

- `6080` -- noVNC frame serving the browser viewport.
- `9223` -- control page with the "Create Profile" button.

It picks `-it` when stdin is a TTY and falls back to `-t` otherwise
(non-interactive shells would crash on `-it` under `set -euo pipefail`).

Open `http://localhost:9223/` in a host browser. The embedded noVNC
frame (served from `6080`) shows the live Chromium. Log in through
the frame, dismiss any cookie banners, navigate to a known
authenticated page if necessary, then click "Create Profile" on the
control page. The host receives `./profiles/profile.tar.gz`.

## Profile reuse

```bash
cd ~/Documents/archives/<host>
btrix page <url> --profile
```

`--profile` reuses `./profiles/profile.tar.gz` from the current host
directory; `btrix` errors if it is missing.

## Validation and inspection

`btrix page` and `btrix site` run `wacz validate` after every crawl
and propagate its exit code. Use the standalone `wacz` CLI to
revalidate or unpack:

```bash
wacz validate <file>.wacz       # checks structure, hashes, and signatures
wacz extract <file>.wacz        # unpack into the current dir for spelunking
wacz --help                     # full command list
```

## Escape hatch: `btrix crawl`

`btrix crawl` is the raw passthrough -- it applies the defaults but
does NOT derive a slug, change directory, flatten, or validate. Use
it when the helper convention doesn't fit (custom collection name,
no flatten, no `--scopeType` opinion):

```bash
btrix crawl --url https://example.com --collection custom-name --customFlag value
```

## Common gotchas

- The helpers always set `--collection "$SLUG"`. Passing
  `-- --collection other-name` makes the flatten step fail. Use
  `btrix crawl` when you need a custom collection name.
- `-- --scopeType any` on `btrix site` will recursively crawl the
  whole web. The helper already sets `prefix`; override only when you
  mean it.
- Skipping `-- --pageLimit` on a large docs site -- crawls run for
  hours. Always cap site crawls.
- Profile creation in a non-interactive shell -- the wrapper falls
  back to `-t` so the script does not crash, but you still need a
  host browser at `http://localhost:9223/` to click "Create Profile".
