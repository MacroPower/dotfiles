# Reports a plural written as a word with `(s)` or `(es)` glued to it
# (`file(s)`, `match(es)`) in markdown prose. The reader cannot tell
# whether the sentence means one thing or many. A Weir rule cannot name
# a literal paren, so the check is awk. The word before the paren needs
# two letters, so math notation (`f(s)`) passes, and the reader blanks
# code spans, so a call such as `len(s)` passes inside one. It hit 40
# lines in the kubernetes and nix.dev docs, every one a plural.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProsePluralParen: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  plural = "[A-Za-z][A-Za-z]+\\(e?s\\)"
}

function prose(text,   hit) {
  if (match(text, plural)) {
    hit = substr(text, RSTART, RLENGTH)
    printf "%s:%d:%d: Style::ProsePluralParen: Plural %s in parentheses. Write the plural, or write one or more.\n", FILENAME, FNR, column(text, RSTART), hit
  }
}
