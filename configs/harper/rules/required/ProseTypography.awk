# Reports a curly quote (U+2018 to U+201F), the ellipsis character
# (U+2026), and the dash variants U+2010 to U+2012, U+2015, and the
# minus sign U+2212. The em and en dash belong to ProseDash. A Weir rule
# cannot do this check: Harper normalizes punctuation before Weir sees
# it, so a pattern for a curly quote also matches a plain ASCII quote.
#
# prose-lint runs the check on a markdown file as prose and on every
# other file it lints with RAW=1 set, which checks each line whole. Each
# finding names the character and its code point, because a curly quote
# and a straight one look alike in a terminal. In RAW mode the message
# also covers a string literal, where the character might be deliberate
# and its escape keeps the string the same.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Punctuation::ProseTypography: <message>

@include "markdown.awk"

function BEGIN_CHECK(   c) {
  # Each character as its UTF-8 bytes in octal, mapped to its code
  # point. A bracket expression of bytes fails under a UTF-8 locale.
  # An alternation of the byte strings matches the same text under any
  # locale, and so does a lookup by the matched text.
  code["\342\200\220"] = "2010"
  code["\342\200\221"] = "2011"
  code["\342\200\222"] = "2012"
  code["\342\200\225"] = "2015"
  code["\342\200\230"] = "2018"
  code["\342\200\231"] = "2019"
  code["\342\200\232"] = "201A"
  code["\342\200\233"] = "201B"
  code["\342\200\234"] = "201C"
  code["\342\200\235"] = "201D"
  code["\342\200\236"] = "201E"
  code["\342\200\237"] = "201F"
  code["\342\200\246"] = "2026"
  code["\342\210\222"] = "2212"
  bad = ""
  for (c in code) {
    bad = bad (bad == "" ? "" : "|") c
  }
}

function prose(text,   hit) {
  if (match(text, bad)) {
    hit = substr(text, RSTART, RLENGTH)
    if (raw_mode) {
      printf "%s:%d:%d: Punctuation::ProseTypography: Typographic character `%s` (`U+%s`). In a string literal, write its escape. In a comment, use a straight quote, three periods, or a hyphen.\n", FILENAME, FNR, column(text, RSTART), hit, code[hit]
    } else {
      printf "%s:%d:%d: Punctuation::ProseTypography: Typographic character `%s` (`U+%s`). Use a straight quote, three periods, or a hyphen.\n", FILENAME, FNR, column(text, RSTART), hit, code[hit]
    }
  }
}
