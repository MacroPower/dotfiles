# Reports a list ending that leaves its contents to the reader in
# markdown prose (`etc.`, `and so on`, `or the like`, `among others`).
# The fix names every item, or says at the start that the list is
# partial (`such as`). Harper reads `etc.` as one abbreviation token
# that no Weir pattern names, so the check is awk, and it reaches
# markdown files and commit messages only.
#
# `etc` needs a character before it that is not a letter, a digit, `/`,
# or `.`, so a path (`/etc/nixos`) passes. `and more` and `similar`
# count only before clause punctuation, because mid-sentence `and more`
# compares (`safer and more convenient`). The reader blanks code spans
# first, so a match inside one passes.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseOpenList: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  open_list = "(^|[^a-z0-9/.])etc([^a-z]|$)|(^|[^a-z])(and so on|and so forth|and the like|or the like|among others|and whatnot)([^a-z]|$)|(^|[^a-z])(and more|and similar|or similar)[.;:,)]"
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
    printf "%s:%d:%d: Style::ProseOpenList: Open-ended list %s. Name every item, or open the list with such as.\n", FILENAME, FNR, column(text, at), hit
  }
}
