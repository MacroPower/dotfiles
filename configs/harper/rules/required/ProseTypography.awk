# Reports a curly quote (U+2018 to U+201F), the ellipsis character
# (U+2026), and the dash variants U+2010 to U+2012, U+2015, and the
# minus sign U+2212. The em and en dash belong to ProseDash. A Weir rule
# cannot do this check: Harper normalizes punctuation before Weir sees
# it, so a pattern for a curly quote also matches a plain ASCII quote.
#
# prose-lint runs the check on a markdown file as prose and on every
# other file it lints with RAW=1 set, which checks each line whole.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Punctuation::ProseTypography: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  # Each character as its UTF-8 bytes in octal, joined by `|`. A
  # bracket expression of bytes fails under a UTF-8 locale, but this
  # alternation matches the same text under any locale.
  bad = "\342\200\220|\342\200\221|\342\200\222|\342\200\225|\342\200\230|\342\200\231|\342\200\232|\342\200\233|\342\200\234|\342\200\235|\342\200\236|\342\200\237|\342\200\246|\342\210\222"
}

function prose(text) {
  if (match(text, bad)) {
    printf "%s:%d:%d: Punctuation::ProseTypography: Typographic character. Use a straight quote, three periods, or a hyphen.\n", FILENAME, FNR, column(text, RSTART)
  }
}
