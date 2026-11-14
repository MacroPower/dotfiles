# Overview
#
# Reports a plural written as a word with `(s)` or `(es)` glued to it
# (`file(s)`, `match(es)`) in markdown prose and in source comments. The
# reader cannot tell whether the sentence means one thing or many. The
# fix writes the plural, or writes `one or more`. The rule sits at
# Required, because it matches one exact form and the fix is mechanical.
#
# Expression
#
# A Weir rule cannot name a literal paren, so the check is awk. The
# check includes lib/markdown.awk and reads one line at a time. The word
# before the paren needs two letters. lib/markdown.awk blanks code
# spans, so the check never reads the text inside one.
#
# False Positives
#
# Math notation (`f(s)`) passes, because `f` is one letter. A call such
# as `len(s)` passes inside a code span. A comment writes code without
# backticks, so `len(s)` and `cap(s)` pass on their own.
#
# Measurement
#
# The rule hit 51 lines in the measure.py corpus, and 19 of 20 sampled
# hits were plurals that the writer can spell out. The one misfire was
# `packet(s)` in a quoted passage of an RFC.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProsePluralParen: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  plural = "[A-Za-z][A-Za-z]+\\(e?s\\)"
}

function prose(text,   hit) {
  if (match(text, plural) && substr(text, RSTART, RLENGTH) !~ /^(len|cap)\(/) {
    hit = substr(text, RSTART, RLENGTH)
    printf "%s:%d:%d: Style::ProsePluralParen: Plural `%s` in parentheses. Write the plural, or write `one or more`.\n", FILENAME, FNR, column(text, RSTART), hit
  }
}
