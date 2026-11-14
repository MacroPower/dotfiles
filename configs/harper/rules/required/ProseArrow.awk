# Overview
#
# Reports a Unicode arrow or its HTML entity that stands in for a verb
# in markdown prose (`client → proxy`, `Settings → Integrations`). The
# reader must guess the verb that the arrow replaces. The message names
# three fixes: the verb that the arrow stands for (`the client calls the
# proxy`), a colon after a label (`Yes: Hugo builds the site`), and `>`
# for a menu path. The rule sits at Required, because it matches exact
# characters, and every sampled hit stood in for a word.
#
# Expression
#
# The check includes lib/markdown.awk and reads one prose line at a
# time. `arrow` matches each arrow character and each HTML entity.
# `keyname` matches an arrow that names a key, and the check blanks
# every such arrow. It then reports the first arrow that remains on the
# line.
#
# False Positives
#
# An arrow that names a key does not stand in for a verb, so it passes.
# `keyname` takes an arrow as a key name after a modifier glyph or a
# `Ctrl+` style prefix (`⌘→`, `Ctrl+→`) and before the word `key` (`the
# → key`). It also takes an arrow that sits alone in a table cell. The
# up, down, and northeast arrows stay out, because docs use them as a
# link icon or a UI glyph.
#
# Related Rules
#
# The Recommended ProseArrow reports the spaced ASCII arrows (`->`,
# `=>`, `<-`). This tier reports only the Unicode arrows and their HTML
# entities, so the two tiers never report the same arrow.
#
# Measurement
#
# The rule hit 47 lines in the measure.py corpus, and 20 of 20 sampled
# hits stood in for a word. Eight followed a label (`Yes →`), six mapped
# one value to another (`< → &lt;`), and six drew a flow (`Pod → Node`).
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
    printf "%s:%d:%d: Style::ProseArrow: Arrow `%s` stands in for a word. Write the verb, a colon after a label, or `>` for a menu path.\n", FILENAME, FNR, column(text, RSTART), hit
  }
}
