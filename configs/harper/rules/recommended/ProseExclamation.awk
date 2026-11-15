# Overview
#
# Reports an exclamation mark that ends a sentence in markdown prose
# (`That's it!`, `The tests pass now!`). The mark adds emphasis and no
# fact, so the reader learns nothing from it. The fix ends the sentence
# with a period. The rule sits at Recommended, because quoted program
# output outside a code span also matches.
#
# Expression
#
# The check includes lib/markdown.awk and reads each prose line. Weir
# reads `!` as negation, so no Weir rule can name the mark. The mark
# counts after a letter, a digit, a closing bracket, a quote, or
# emphasis, and before whitespace, the same closers, or the line end.
#
# False Positives
#
# The pattern leaves out `!=`, an image's `![`, an HTML comment's
# `<!--`, a CSS `!important`, and a conventional-commit breaking marker
# (`feat!:`). lib/markdown.awk blanks code spans first, so a mark inside
# one passes. Program output that the prose quotes outside a code span
# still fires (`prints "Done!" to the log`).
#
# Measurement
#
# The rule hit 395 lines in the measure.py docs corpus, and 18 of 20
# sampled hits ended a sentence with the mark. The two misfires were
# configuration values (`files = ['! docs/*']`) inside a Hugo
# `code-toggle` shortcode, which lib/markdown.awk reads as prose.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseExclamation: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  bang = "([A-Za-z0-9)\"'*_]|\\])!+([ \t)\"'*_]|\\]|$)"
}

function prose(text) {
  if (match(text, bang)) {
    printf "%s:%d:%d: Style::ProseExclamation: Exclamation mark. End the sentence with a period.\n", FILENAME, FNR, column(RSTART + 1)
  }
}
