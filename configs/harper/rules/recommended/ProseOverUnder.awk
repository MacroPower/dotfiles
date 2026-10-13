# Reports `over`, `under`, `above`, or `below` before a number in
# markdown prose (`over 40 entries`, `under five seconds`). Each word
# also names a place, so `more than`, `fewer than`, or `less than`
# states the quantity in one reading. Harper does not tag a digit as a
# number, so no Weir pattern can name one, and the check is awk. It
# reaches markdown files and commit messages only.
#
# A number is a digit, a spelled number from two to ten, `half`,
# `twice`, `a dozen`, `a hundred`, `a thousand`, or `hundreds` and its
# kin. `one` stays out, because it also names a member of a group
# (`below one of the panes`). `the above` and `the below` point at a
# passage (`the above two keys`), so the check skips them. The reader
# blanks code spans first, so a match inside one passes.
#
# It hit 16 lines in the kubernetes and nix.dev docs, and every hit but
# `the above two keys` stated a quantity.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseOverUnder: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  amount = "(^|[^a-z])(over|under|above|below)[ \t]+([0-9]|(two|three|four|five|six|seven|eight|nine|ten|half|twice|dozens|hundreds|thousands|millions)([^a-z]|$)|a[ \t]+(dozen|hundred|thousand|million)([^a-z]|$))"
}

function prose(text,   lower, at, word) {
  lower = blank(tolower(text), "(^|[^a-z])the[ \t]+(above|below)")
  if (match(lower, amount)) {
    at = RSTART
    if (substr(lower, at, 1) !~ /[a-z]/) {
      at++
    }
    word = substr(lower, at)
    sub(/[^a-z].*/, "", word)
    printf "%s:%d:%d: Style::ProseOverUnder: %s before a number. Write more than, fewer than, or less than.\n", FILENAME, FNR, column(text, at), toupper(substr(word, 1, 1)) substr(word, 2)
  }
}
