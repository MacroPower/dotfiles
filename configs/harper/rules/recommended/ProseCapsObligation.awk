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
# A word inside double quotes passes, because the quote reproduces a
# standard's own words (`"A sender MUST NOT generate"`). An open quote
# carries over to the next line of the paragraph, so a quote that wraps
# across the lines of a comment passes whole. A paragraph that cites a
# standard (`RFC 7232`, `Section 5.4.1`, or `§`) passes too, because
# code comments copy a standard's text without quote marks. The check
# holds each finding until the paragraph ends to read the citation. The
# two skips cut the hits in the measure.py corpus from 91 to 8, and 6 of
# the 8 copy a standard's text in a Go comment that never names it.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseCapsObligation: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  last_line = 0
  in_quote = 0
  held = 0
  cites = 0
  citation = "RFC[ \t]*[0-9]+|[Ss]ection[ \t]+[0-9]+(\\.[0-9]+)+|\302\247"
  caps = "(^|[^A-Za-z0-9_])(MUST|SHOULD|MAY|REQUIRED|RECOMMENDED|OPTIONAL)([^A-Za-z0-9_]|$)"
  owned = "[Ii][Ss][ \t]+(REQUIRED|RECOMMENDED)|[Aa][Rr][Ee][ \t]+(REQUIRED|RECOMMENDED)|MAY[ \t]+NOT"
  cell = "\\|[ \t]*(MUST|SHOULD|MAY|REQUIRED|RECOMMENDED|OPTIONAL)[ \t]*(\\||$)"
}

# Blanks the text inside double quotes, straight or curly, and keeps
# the quote open across the lines of a paragraph.
function unquote(s,   out, q) {
  out = ""
  while (match(s, /"|\342\200\234|\342\200\235/)) {
    q = substr(s, 1, RSTART - 1)
    out = out (in_quote ? spaces(length(q)) : q) substr(s, RSTART, RLENGTH)
    s = substr(s, RSTART + RLENGTH)
    in_quote = !in_quote
  }
  return out (in_quote ? spaces(length(s)) : s)
}

# Prints the findings held for the paragraph unless it cites a
# standard.
function flush(   i) {
  if (!cites) {
    for (i = 1; i <= held; i++) {
      print finding[i]
    }
  }
  held = 0
  cites = 0
}

function prose(text,   at, word) {
  if (FILENAME != last_file || FNR != last_line + 1) {
    flush()
    in_quote = 0
  }
  last_file = FILENAME
  last_line = FNR
  if (text ~ citation) {
    cites = 1
  }
  text = unquote(text)
  text = blank(text, owned)
  text = blank(text, cell)
  if (match(text, caps)) {
    at = RSTART
    if (substr(text, at, 1) !~ /[A-Z]/) {
      at++
    }
    word = substr(text, at)
    sub(/[^A-Z].*/, "", word)
    finding[++held] = sprintf("%s:%d:%d: Style::ProseCapsObligation: Uppercase obligation `%s`. Write it in lowercase for a person, or state what the system does in the present tense.", FILENAME, FNR, column(text, at), word)
  }
}

END {
  flush()
}
