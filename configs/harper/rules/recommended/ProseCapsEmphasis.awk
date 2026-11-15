# Overview
#
# Reports a word written in capitals for emphasis in markdown prose and
# in source comments (`The hook ALWAYS runs`, `Do NOT commit the
# token`). The capitals stress the word and give the reader no reason.
# The technical-writing skill writes emphasis into the sentence, not the
# typeface, and keeps the obligation words lowercase. The fix states the
# consequence that the capitals stood in for (`Do not commit the token,
# because the push publishes it`). The rule sits at Recommended, because
# a comment that copies the text of a standard without naming the
# standard also matches.
#
# Expression
#
# The check includes lib/markdown.awk and reads one prose line at a
# time. Weir ignores case, so a Weir rule cannot tell `NOT` from `not`,
# and the check is awk.
#
# `words` holds the obligation words that standards documents
# capitalize: `MUST`, `SHOULD`, `MAY`, `REQUIRED`, `RECOMMENDED`, and
# `OPTIONAL`. It also holds the words that writers capitalize for
# emphasis: `NEVER`, `ALWAYS`, `NOT`, `ONLY`, `NONE`, `IMPORTANT`,
# `CANNOT`, and `DON'T`.
#
# The check first blanks quoted text, the forms that another rule
# reports, a word alone in a table cell, and the fixed phrases. It then
# looks for a word from `words`. An open quote carries over to the next
# line of the paragraph, so a quote that wraps across the lines of a
# comment passes whole. The check holds each finding until the paragraph
# ends, so a citation anywhere in the paragraph clears the finding.
#
# configs/harper/comment-checks.txt lists the check, so prose-lint also
# runs it on the comments of a source file. A `!<kind>` after a name in
# the list skips one comment kind, such as `!go` for Go comments, and
# the list puts none after this check. prose-lint skips the check on
# every file under `configs/claude/` and `.claude/`, so a word in
# capitals in the comment of a script in a skill passes
# (`# Do NOT retry`).
#
# False Positives
#
# `CRITICAL` stays out, because it also names a severity or a log level
# (`HIGH or CRITICAL`). A word alone in a table cell passes, because an
# options table labels a column or a value with it. Words glued to an
# underscore, a hyphen, or a digit are identifiers (`MAY_FAIL`,
# `X-NOT-CACHED`) and pass too. Some fixed phrases keep their capitals
# for a reason of their own: a GitHub alert (`> [!IMPORTANT]`), the
# marker on generated code (`DO NOT EDIT`), and the SQL constraint `NOT
# NULL`.
#
# A word inside double quotes passes, because the quote reproduces a
# standard's own words (`"A sender MUST NOT generate"`). A paragraph
# that cites a standard (`RFC 7232`, `Section 5.4.1`, or `§`) passes
# too, because code comments copy a standard's text without quote marks.
#
# prose-lint skips this check on prompt files for an agent (skills,
# agent definitions, `CLAUDE.md`, and `AGENTS.md`), which write capitals
# for emphasis on purpose.
#
# Related Rules
#
# The Recommended ProseObligation reports `is REQUIRED to` and `is
# RECOMMENDED`, and ProseMayNot reports `MAY NOT`, so this check skips
# those forms. ProseCallout matches a callout label only with a leading
# capital, so only this check reports an all-caps label (`IMPORTANT:`).
#
# Weir ignores case, so every other Weir rule that reports a word from
# `words` in lowercase reports it in capitals too. No guard separates
# such a rule from this check. For example, ProseNegativeContraction
# reports `DON'T`, and ProseMisplacedOnly reports `ONLY` before a verb.
# ProseMayPossibility reports `MAY` before a verb, so `The server MAY
# return a cached copy` draws a finding from that rule and from this
# check.
#
# The Recommended ProseHedge reports `SHOULD` after `it`, `this`, or
# `that` and before a verb such as `work` (`This SHOULD work`). The
# Optional ProseHedge reports `SHOULD` after a noun and before a verb
# such as `pass` (`The test SHOULD pass`).
#
# Measurement
#
# The rule hit 17 lines in the measure.py corpus, and 8 of 17 sampled
# hits capitalized the writer's own emphasis or obligation. Six of the
# misfires copy a standard's text in a Go comment that never names the
# standard. Three sit in an `AGENTS.md` file that prose-lint skips.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseCapsEmphasis: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  last_line = 0
  in_quote = 0
  held = 0
  cites = 0
  citation = "RFC[ \t]*[0-9]+|[Ss]ection[ \t]+[0-9]+(\\.[0-9]+)+|\302\247"
  words = "MUST|SHOULD|MAY|REQUIRED|RECOMMENDED|OPTIONAL|NEVER|ALWAYS|NOT|ONLY|NONE|IMPORTANT|CANNOT|DON('|\342\200\231)T"
  caps = "(^|[^A-Za-z0-9_-])(" words ")([^A-Za-z0-9_-]|$)"
  owned = "[Ii][Ss][ \t]+(REQUIRED|RECOMMENDED)|[Aa][Rr][Ee][ \t]+(REQUIRED|RECOMMENDED)|MAY[ \t]+NOT"
  cell = "\\|[ \t]*(" words ")[ \t]*(\\||$)"
  fixed = "\\[![A-Z]+\\]|DO[ \t]+NOT[ \t]+EDIT|NOT[ \t]+NULL"
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
  text = blank(text, fixed)
  if (match(text, caps)) {
    at = RSTART
    if (substr(text, at, 1) !~ /[A-Z]/) {
      at++
    }
    word = substr(text, at)
    match(word, "^(" words ")")
    word = substr(word, 1, RLENGTH)
    finding[++held] = sprintf("%s:%d:%d: Style::ProseCapsEmphasis: Uppercase emphasis `%s`. Write it in lowercase, and state the consequence when the reader needs a reason.", FILENAME, FNR, column(text, at), word)
  }
}

END {
  flush()
}
