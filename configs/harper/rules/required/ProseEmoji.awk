# Reports an emoji in markdown prose: a pictograph, flag, or skin tone
# in U+1F000 to U+1FAFF, a symbol whose default presentation is emoji
# (a check mark button, a sparkle, a star), any symbol followed by the
# U+FE0F emoji selector, a keycap, or a `:shortcode:`. Every prose line
# counts, since emoji turn up in link text, table cells, and summaries
# as often as in headings and list items. The U+2600 to U+27BF block
# stays out as a range, because a plain check mark in a table cell or a
# prompt glyph is text, not decoration.
#
# A range needs a UTF-8 locale. BEGIN_CHECK reads `utf8`, which the
# reader sets when gawk reads a two-byte character as one, and under
# the C locale spells the same block as its UTF-8 bytes, so the output
# matches in both.
#
# Output shape, one line per finding:
#   <file>:<line>:<col>: Style::ProseEmoji: <message>

@include "markdown.awk"

function BEGIN_CHECK(   list, n, i, bmp) {
  # Characters below U+1F000 whose default presentation is emoji.
  # A text-default symbol such as ✓, ★, ⚠, or ❯ passes on its own and
  # counts only with the U+FE0F variation selector after it.
  n = split("⌚ ⌛ ⏩ ⏪ ⏫ ⏬ ⏰ ⏳ ◽ ◾ ☔ ☕ ♈ ♉ ♊ ♋ ♌ ♍ ♎ ♏ ♐ ♑ ♒ ♓ ♿ ⚓ ⚡ ⚪ ⚫ ⚽ ⚾ ⛄ ⛅ ⛎ ⛔ ⛪ ⛲ ⛳ ⛵ ⛺ ⛽ ✅ ✊ ✋ ✨ ❌ ❎ ❓ ❔ ❕ ❗ ➕ ➖ ➗ ➰ ➿ ⬛ ⬜ ⭐ ⭕", list, " ")
  bmp = ""
  for (i = 1; i <= n; i++) {
    bmp = bmp "|" list[i]
  }
  # U+FE0F asks for emoji presentation, U+20E3 builds a keycap, and
  # U+1F000 to U+1FAFF holds the pictographs, flags, and skin tones.
  # A range needs a UTF-8 locale; under the C locale the same block is
  # spelled as its UTF-8 bytes.
  if (utf8) {
    smp = "[🀀-🫿]"
  } else {
    smp = "\xF0\x9F[\x80-\xAB][\x80-\xBF]"
  }
  emoji = "(" substr(bmp, 2) "|️|⃣|" smp ")"
  shortcode = "(^|[ \t(])(:[a-z0-9_+-]*[a-z][a-z0-9_+-]*:)([ \t.,;!?)]|$)"
}

function prose(text,   hit, col, start, end) {
  if (match(text, emoji)) {
    start = RSTART
    end = RSTART + RLENGTH
    # A selector or keycap mark names the symbol before it.
    if (substr(text, start, RLENGTH) ~ /^(️|⃣)$/) {
      start--
      while (!utf8 && start > 1 && substr(text, start, 1) ~ "^[\x80-\xBF]$") {
        start--
      }
    }
    hit = substr(text, start, end - start)
    col = column(text, start)
  } else if (match(text, shortcode)) {
    hit = substr(text, RSTART, RLENGTH)
    sub(/^[ \t(]/, "", hit)
    sub(/[ \t.,;!?)]$/, "", hit)
    col = column(text, RSTART + (substr(text, RSTART, 1) != ":"))
  } else {
    return
  }
  printf "%s:%d:%d: Style::ProseEmoji: Emoji %s decorates the text. Delete it, or say the point in words.\n", FILENAME, FNR, col, hit
}
