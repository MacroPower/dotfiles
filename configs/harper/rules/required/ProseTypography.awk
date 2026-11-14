# Overview
#
# Reports a curly quote (U+2018 to U+201F), the ellipsis character
# (U+2026), and the dash variants U+2010 to U+2012, U+2015, and the
# minus sign U+2212. A curly quote and a straight one look alike in a
# terminal, so the reader cannot see which one a line holds. A search
# for the straight one also misses the curly one. The fix writes a
# straight quote, three periods, or a hyphen. The rule sits at Required,
# because it matches exact characters and the fix is mechanical.
#
# Expression
#
# The check includes lib/markdown.awk. `code` maps each character to its
# code point, and the check reports the first of those characters on a
# line. A Weir rule cannot do this check. Harper normalizes punctuation
# before Weir sees it, so a pattern for a curly quote also matches a
# plain ASCII quote.
#
# prose-lint runs the check on a markdown file as prose. On every other
# file that it lints, prose-lint sets RAW=1, and the check then reads
# each line whole. Each finding names the character and its code point,
# because a curly quote and a straight one look alike in a terminal.
# With RAW=1, the message also covers a string literal, where the
# character might be deliberate and its escape keeps the string the
# same.
#
# Related Rules
#
# ProseDash reports the em dash and the en dash, so this check leaves
# them out.
#
# Measurement
#
# The rule hit 3627 lines in the measure.py corpus, and 20 of 20 sampled
# hits were typographic characters in prose, 19 curly apostrophes and
# one curly double quote. The Rust book holds 3410 of the 3627 lines.
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
