# Reports an obligation word written in capitals in markdown prose
# (`MUST`, `SHOULD`, `MAY`, `REQUIRED`, `RECOMMENDED`, `OPTIONAL`). The
# capitals come from standards documents and add no meaning, and the
# technical-writing skill keeps the words lowercase. Weir ignores case,
# so a Weir rule cannot tell `MUST` from `must`, and the check is awk.
#
# The obligation rule already reports `is REQUIRED to` and `is
# RECOMMENDED`, and the may-not rule reports `MAY NOT`, so this check
# skips those forms. A word alone in a table cell passes, because an
# options table labels a column with it. Words glued to an underscore
# or a digit are identifiers (`MAY_FAIL`) and pass too.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseCapsObligation: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  caps = "(^|[^A-Za-z0-9_])(MUST|SHOULD|MAY|REQUIRED|RECOMMENDED|OPTIONAL)([^A-Za-z0-9_]|$)"
  owned = "[Ii][Ss][ \t]+(REQUIRED|RECOMMENDED)|[Aa][Rr][Ee][ \t]+(REQUIRED|RECOMMENDED)|MAY[ \t]+NOT"
  cell = "\\|[ \t]*(MUST|SHOULD|MAY|REQUIRED|RECOMMENDED|OPTIONAL)[ \t]*(\\||$)"
}

function prose(text,   at, word) {
  text = blank(text, owned)
  text = blank(text, cell)
  if (match(text, caps)) {
    at = RSTART
    if (substr(text, at, 1) !~ /[A-Z]/) {
      at++
    }
    word = substr(text, at)
    sub(/[^A-Z].*/, "", word)
    printf "%s:%d:%d: Style::ProseCapsObligation: Uppercase %s. Write it in lowercase.\n", FILENAME, FNR, column(text, at), word
  }
}
