# Overview
#
# Reports link text that names no destination in markdown prose
# (`[here](url)`, `[this page][ref]`, `[more info](url)`, `[this
# section](#x)`). A reader who scans the links sees `here` with no hint
# of where it goes. The fix names the page or the section that the link
# opens, as the technical-writing skill asks. The rule sits at
# Recommended, because a listed word can also be the name of the
# destination.
#
# Expression
#
# The check includes lib/markdown.awk and compares the whole link text
# with a list of phrases. lib/markdown.awk blanks a link's `](url)`
# before prose() sees the line, so the check matches the raw line in
# `$0`, which keeps the same columns. It counts a match only when the
# blanked line still holds its `[`, so a link inside a code span passes.
#
# False Positives
#
# An image (`![here](x.png)`) passes, because its alt text describes the
# picture.
#
# A link whose text is the name of its destination still matches when
# the name is on the list, such as `[this](url)` for the JavaScript
# keyword `this`.
#
# Measurement
#
# The rule hit 9 lines in the measure.py corpus, and 9 of 9 sampled hits
# named no destination.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseLinkText: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  vague = "(^|[^!])\\[(here|click here|this|this link|link|this page|this doc|this document|this article|this post|this section|this guide|read more|more|learn more|see more|more info|more information|more details)\\](\\(|\\[)"
}

function prose(text,   lower, at, hit) {
  lower = tolower($0)
  if (match(lower, vague)) {
    at = RSTART
    if (substr(lower, at, 1) != "[") {
      at++
    }
    if (substr(text, at, 1) != "[") {
      return
    }
    hit = substr($0, at + 1)
    sub(/\].*/, "", hit)
    printf "%s:%d:%d: Style::ProseLinkText: Link text `%s` names no destination. Name the page or section it opens.\n", FILENAME, FNR, column(text, at), hit
  }
}
