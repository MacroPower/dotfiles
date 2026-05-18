# Handing off to /playwright-cli

Narrow fallback. Browsertrix profile creation already covers
logged-in captures, and the crawler handles JS-rendered pages
natively. Escalate to `/playwright-cli` only when browsertrix cannot
script the capture itself.

## When to hand off

- **Multi-step navigation.** The user must click through several
  pages (a wizard, a paginated search result) before the final
  capture, and that path cannot be expressed as a list of seed URLs.
- **Per-element interaction before capture.** Expanding a specific
  accordion, hovering to reveal a tooltip, switching a theme toggle.
- **Single-element screenshot.** WACZ does not produce loose PNGs.
- **Browser-context capture.** Anything requiring CDP introspection
  (network throttling, geolocation override, devicePixelRatio) before
  the snapshot.

## When NOT to hand off

- The page renders JS on its own. Browsertrix runs a real Chromium;
  it does not need playwright to "make JS work".
- The page requires login. Run `btrix create-login-profile` once and
  then capture with `btrix page <url> --profile` (see
  [browsertrix-wacz.md](browsertrix-wacz.md)).
- The page has infinite scroll. Browsertrix has a built-in
  `autoscroll` behavior.
- The user wants a WACZ "just to be safe". The WACZ format is
  faithful by construction. If browsertrix can reach the URL, the
  capture is already complete.

## Recipe

1. Load the `/playwright-cli` skill -- its `SKILL.md` is the canonical
   command surface for everything below.
2. Drive the page through the required interactions.
3. Capture with the appropriate playwright-cli command, writing into
   `~/Documents/archives/<host>/<YYYY-MM-DD>-<slug>.<ext>`:
   - PDF render -> `pdf --filename ...pdf`
   - HTML snapshot -> `--raw eval "document.documentElement.outerHTML"`
     redirected to `...html`
   - Element screenshot -> `screenshot <ref> --filename ...png`
4. Close the browser session when finished.

## Why this stays narrow

Every interaction-free capture is better served by WACZ -- a single
self-contained replay file, validated by `wacz validate`, hostable on
replayweb.page. Reserve `/playwright-cli` for the cases where the
*interaction itself* is the thing browsertrix cannot reproduce.
