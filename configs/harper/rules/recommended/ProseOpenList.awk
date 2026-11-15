# Overview
#
# Reports a list ending that leaves its contents to the reader (`etc.`,
# `and so on`, `or the like`, `among others`). The reader cannot tell
# which items the writer left out. The fix names every item, or says at
# the start that the list is partial (`such as`). The rule sits at
# Recommended, because a numbered pattern that has no last item ends the
# same way.
#
# Expression
#
# Harper reads `etc.` as one abbreviation token that no Weir pattern
# names, so the check is awk. The check includes lib/markdown.awk and
# reads markdown, commit messages, and source comments. lib/markdown.awk
# blanks code spans first, so a match inside one passes.
#
# `etc` needs a character before it that is not a letter, a digit, `/`,
# or `.`. `including but not limited to` counts anywhere, because it
# opens a list that it never closes. `and more`, `and many more`, `and
# much more`, `and others`, `and similar`, and `or similar` count only
# before a period, a semicolon, a colon, a comma, or a closing paren.
#
# False Positives
#
# A path passes (`/etc/nixos`), because of the character that `etc`
# needs before it. In the middle of a sentence, `and more` compares
# (`safer and more convenient`) and `and others` can name a group (`for
# you and others to read`), so both pass there.
#
# Measurement
#
# The rule hit 175 lines in the measure.py corpus, and 19 of 20 sampled
# hits left a list open. The misfire continued a numbered pattern that
# has no last item (`expr.0, expr.1, and so on`).
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseOpenList: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  open_list = "(^|[^a-z0-9/.])etc([^a-z]|$)|(^|[^a-z])(and so on|and so forth|and the like|or the like|among others|and whatnot|including but not limited to)([^a-z]|$)|(^|[^a-z])(and more|and similar|or similar|and others|and many more|and much more)[.;:,)]"
}

function prose(text,   lower, at, hit) {
  lower = tolower(text)
  if (match(lower, open_list)) {
    at = RSTART
    hit = substr(lower, RSTART, RLENGTH)
    if (hit !~ /^[a-z]/) {
      at++
      hit = substr(hit, 2)
    }
    sub(/[^a-z]+$/, "", hit)
    printf "%s:%d:%d: Style::ProseOpenList: Open-ended list `%s`. Name every item, or open the list with `such as`.\n", FILENAME, FNR, column(at), hit
  }
}
