# Reports an exclamation mark that ends a sentence in markdown prose
# (`That's it!`, `The tests pass now!`). Weir reads `!` as negation, so
# no Weir rule can name the mark. It counts after a letter, a digit, a
# closing bracket, a quote, or emphasis, and before whitespace, the
# same closers, or the line end. That leaves `!=`, an image's `![`, an
# HTML comment's `<!--`, a CSS `!important`, and a conventional-commit
# breaking marker (`feat!:`). The reader blanks code spans first, so a
# mark inside one passes.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseExclamation: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  bang = "([A-Za-z0-9)\"'*_]|\\])!+([ \t)\"'*_]|\\]|$)"
}

function prose(text) {
  if (match(text, bang)) {
    printf "%s:%d:%d: Style::ProseExclamation: Exclamation mark. End the sentence with a period.\n", FILENAME, FNR, column(text, RSTART + 1)
  }
}
