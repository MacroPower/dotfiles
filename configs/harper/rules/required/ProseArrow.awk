# Reports a Unicode arrow or its HTML entity standing in for a verb in
# markdown prose (`client → proxy`, `Settings → Integrations`). The
# Recommended check of the same name reports the spaced ASCII arrows.
# An arrow that names a key passes: after a modifier glyph or a `Ctrl+`
# style prefix, before the word `key`, or alone in a table cell. The
# up, down, and northeast arrows stay out, because docs use them as a
# link icon or a UI glyph.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseArrow: <message>

@include "markdown.awk"

function BEGIN_CHECK(   list, n, i) {
  n = split("→ ← ↔ ⇒ ⇐ ⇔ ⟶ ⟵ ⟷ ⟹ ⟸ ⟺ ↦ ⟼ ➔ ➜ ➝ ➞ ➡ ⬅ &rarr; &larr; &harr; &rArr; &lArr; &hArr;", list, " ")
  arrow = ""
  for (i = 1; i <= n; i++) {
    arrow = arrow "|" list[i]
  }
  arrow = "(" substr(arrow, 2) ")"
  # An arrow that names a key: after a modifier glyph or a `Ctrl+`
  # style prefix, before the word `key`, or alone in a table cell.
  keyname = "(⌘|⌃|⌥|⇧|[A-Za-z]+[+-])+" arrow "|" arrow "[ \t]*(arrow[ \t]+)?keys?([^A-Za-z]|$)|\\|[ \t]*" arrow "[ \t]*(\\||$)"
}

function prose(text,   hit) {
  text = blank(text, keyname)
  if (match(text, arrow)) {
    hit = substr(text, RSTART, RLENGTH)
    printf "%s:%d:%d: Style::ProseArrow: Arrow %s stands in for a verb. Write the verb, or use > for a menu path.\n", FILENAME, FNR, column(text, RSTART), hit
  }
}
