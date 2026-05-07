#!/usr/bin/env bash
# Mirror https://docs.spacelift.io as local markdown.
#
# MkDocs Material serves raw markdown when you append `.md` to any URL
# (Content-Type: text/markdown), so we just walk /sitemap.xml and fetch
# each entry. Re-running overwrites; if a URL ever 404s on re-run, the
# previous file is kept (last-known-good wins).
set -euo pipefail
IFS=$'\n\t'

OUT_DIR="${1:-./spacelift-docs}"
BASE_URL="https://docs.spacelift.io"
SITEMAP_URL="$BASE_URL/sitemap.xml"
UA="spacelift-docs-mirror/1.0"

command -v curl >/dev/null 2>&1 || {
  echo "error: curl is required but not found in PATH" >&2
  exit 1
}

mkdir -p "$OUT_DIR"

echo "fetching sitemap: $SITEMAP_URL"
mapfile -t urls < <(
  curl -fsSL --max-time 30 -A "$UA" "$SITEMAP_URL" |
    grep -oE '<loc>[^<]+</loc>' |
    sed -E 's:</?loc>::g'
)

if [ "${#urls[@]}" -lt 50 ]; then
  echo "error: sitemap parse yielded ${#urls[@]} URLs (expected ~200+)." >&2
  echo "       site format may have changed (e.g. <sitemapindex>)." >&2
  exit 1
fi

echo "found ${#urls[@]} URLs; mirroring to $OUT_DIR"

failures=0
for url in "${urls[@]}"; do
  path="${url#"$BASE_URL"}"
  case "$path" in
  "" | "/")
    rel="index.md"
    md_url="$BASE_URL/index.md"
    ;;
  /*)
    rel="${path#/}.md"
    md_url="${url}.md"
    ;;
  *)
    rel="$path.md"
    md_url="${url}.md"
    ;;
  esac
  dest="$OUT_DIR/$rel"
  mkdir -p "$(dirname "$dest")"
  if ! effective=$(
    curl -fSL \
      --retry 3 --retry-delay 2 --retry-all-errors --retry-connrefused \
      --max-time 30 \
      -A "$UA" \
      -o "$dest" \
      -w '%{url_effective}' \
      "$md_url" 2>/dev/null
  ); then
    failures=$((failures + 1))
    echo "FAIL: $md_url" >&2
    continue
  fi

  # Path-only redirect check: ignore scheme/host/trailing-slash normalization.
  req_path="${md_url#*://*/}"
  eff_path="${effective#*://*/}"
  if [ "$req_path" != "$eff_path" ]; then
    echo "warn: $md_url -> $effective" >&2
  fi

  echo "  $rel"
done

printf '%s\n' "${urls[@]}" >"$OUT_DIR/.sitemap.txt"

echo "mirrored $((${#urls[@]} - failures))/${#urls[@]} pages, $failures failures"
