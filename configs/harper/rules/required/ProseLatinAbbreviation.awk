# Reports a Latin abbreviation in markdown prose (`e.g.`, `i.e.`,
# `etc.`, `cf.`, `viz.`, `N.B.`, `et al.`). A reader who does not know
# the abbreviation stops on it, and a reader who does often swaps
# `e.g.` and `i.e.`. Weir reads the periods inside `e.g.` as separate
# tokens and cannot name these, so the check is awk, and it reaches
# markdown files and commit messages only. `e.g,` and `i.e,` count
# too, since a writer often drops the last period before a comma.
#
# A match needs a left boundary, a character before it that is not a
# letter, a digit, `/`, or `.`, so a path (`/etc.`) and a word that
# ends in one of the stems (`Graphviz.`) pass. `vs.` stays out,
# because comparison headings use it (`Podman vs. Docker`). The reader
# blanks code spans first, so a match inside one passes.
#
# Output shape, one line per finding:
#   <file>:<line>:<col>: Style::ProseLatinAbbreviation: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  latin = "(^|[^a-z0-9/.])(e\\.g[.,]|i\\.e[.,]|etc\\.|cf\\.|viz\\.|n\\.b\\.|et al\\.)"
}

function prose(text,   rest, base, at) {
  rest = tolower(text)
  base = 0
  while (match(rest, latin)) {
    at = RSTART
    if (substr(rest, at, 1) !~ /[a-z]/) {
      at++
    }
    printf "%s:%d:%d: Style::ProseLatinAbbreviation: Latin abbreviation. Write for example or that is, or say at the start that the list is partial.\n", FILENAME, FNR, column(text, base + at)
    base += RSTART + RLENGTH - 1
    rest = substr(rest, RSTART + RLENGTH)
  }
}
