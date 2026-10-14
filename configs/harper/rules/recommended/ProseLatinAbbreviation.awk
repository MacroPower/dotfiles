# Reports a Latin abbreviation in markdown prose and in source comments:
# `e.g.`, `i.e.`, `cf.`, `viz.`, `et al.`, and `n.b.`. Many readers mix
# up `e.g.` (for example) and `i.e.` (that is), and the two readings
# disagree on whether the list after it is complete. The English
# phrase has one reading, so each finding names its replacement. Harper
# reads `e.g.` as one abbreviation token that no Weir pattern names, so
# the check is awk.
#
# The abbreviation needs a character before it that is not a letter, a
# digit, `.`, `/`, `_`, or `-`, so a path (`/etc/i.e./x`) or a name
# (`foo_e.g.`) passes. A letter, a digit, `/`, `_`, or `-` after it
# also makes it pass (`cf.go`, `cf.Name`). The reader blanks code spans
# and link destinations first, so a match inside one passes. `vs.`
# stays out, because it has one reading, and Go code often names a
# variable `vs`. `etc.` belongs to the open-list rule.
#
# It hit 279 lines in the measure.py corpus, and 20 of 20 sampled hits
# were Latin abbreviations.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseLatinAbbreviation: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  latin = "(^|[^A-Za-z0-9./_-])([Ee]\\.[ ]?[Gg]\\.?|[Ii]\\.[ ]?[Ee]\\.?|[Vv]iz\\.|[Cc]f\\.|[Ee]t al\\.?|[Nn]\\.[ ]?[Bb]\\.)([^A-Za-z0-9/_-]|$)"
  english["e.g"] = "Write `for example` or `such as`."
  english["i.e"] = "Write `that is`."
  english["viz"] = "Write `namely`."
  english["cf"] = "Write `see` or `compare`."
  english["etal"] = "Name the others, or open the list with `such as`."
  english["n.b"] = "Delete it and state the point."
}

function report(text, col, hit,   key) {
  key = tolower(hit)
  gsub(/ /, "", key)
  sub(/\.$/, "", key)
  printf "%s:%d:%d: Style::ProseLatinAbbreviation: Latin abbreviation `%s`. %s\n", FILENAME, FNR, column(text, col), hit, english[key]
}

function prose(text,   rest, off, hit, start, len) {
  rest = text
  off = 0
  while (match(rest, latin)) {
    start = RSTART
    len = RLENGTH
    hit = substr(rest, RSTART, RLENGTH)
    if (hit ~ /^[^A-Za-z]/) {
      hit = substr(hit, 2)
      start++
    }
    sub(/[^A-Za-z.]$/, "", hit)
    report(text, off + start, hit)
    # Resume at the last character of the match, so the character after
    # one abbreviation can open the next match.
    off += RSTART + len - 2
    rest = substr(rest, RSTART + len - 1)
    if (len <= 1) {
      break
    }
  }
}
