# Overview
#
# Reports `over`, `under`, `above`, or `below` before a number in prose
# (`over 40 entries`, `under five seconds`). Each word also names a
# place, so the reader has to pick between a place and a quantity. The
# fix writes `more than`, `fewer than`, or `less than`, and each states
# the quantity in one reading. The rule sits at Recommended, because a
# word such as `below` can still name a place before a number. The
# guards cover only the phrases that they list.
#
# Expression
#
# Harper does not tag a digit as a number, so no Weir pattern can name
# one, and the check is awk. The check includes lib/markdown.awk and
# reads markdown files, commit messages, and source comments.
# lib/markdown.awk blanks code spans first, so a match inside one
# passes.
#
# A number is a digit, a spelled number from two to ten, `half`,
# `twice`, `a dozen`, `a hundred`, `a thousand`, or `hundreds` and its
# kin.
#
# The check blanks `the above` and `the below`, the text from `split` to
# `over`, and each of the four words after `generic`, `falls`, `drops`,
# or `rises`. The guard for `drops` also passes `drops over 40 packets`,
# where `drops` means discards, a case rare enough to leave.
#
# False Positives
#
# `one` stays out, because it also names a member of a group (`below one
# of the panes`). `the above` and `the below` point at a passage (`the
# above two keys`), so the check skips them.
#
# The check also skips three phrases where the word has one reading.
# `generic over` names the type parameters of a generic type (`generic
# over two types`). After `split`, `over` names where the text breaks
# (`split the command over two lines`). After `falls`, `drops`, or
# `rises`, the verb already names the direction (`the memory falls below
# 1GiB`).
#
# Outside those phrases, a word such as `below` can still name a place
# before a number (`the pane sits below two other panes`). The rule
# accepts that misfire.
#
# Measurement
#
# The rule hit 26 lines in the measure.py corpus, and 20 of 20 sampled
# hits compared a quantity or a version number.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseOverUnder: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  amount = "(^|[^a-z])(over|under|above|below)[ \t]+([0-9]|(two|three|four|five|six|seven|eight|nine|ten|half|twice|dozens|hundreds|thousands|millions)([^a-z]|$)|a[ \t]+(dozen|hundred|thousand|million)([^a-z]|$))"
}

function prose(text,   lower, at, word) {
  lower = blank(tolower(text), "(^|[^a-z])the[ \t]+(above|below)")
  lower = blank(lower, "(^|[^a-z])(generic|falls?|drops?|rises?)[ \t]+(over|under|above|below)")
  lower = blank(lower, "(^|[^a-z])split[ \t][^.;:]*[ \t]over")
  if (match(lower, amount)) {
    at = RSTART
    if (substr(lower, at, 1) !~ /[a-z]/) {
      at++
    }
    word = substr(lower, at)
    sub(/[^a-z].*/, "", word)
    printf "%s:%d:%d: Style::ProseOverUnder: `%s` before a number. Write `more than`, `fewer than`, or `less than`.\n", FILENAME, FNR, column(at), word
  }
}
