# Reports link text that names no destination in markdown prose
# (`[here](url)`, `[this page][ref]`). A reader who scans the links
# sees `here` with no hint of where it goes, and the technical-writing
# skill asks for link text that names the destination.
#
# The reader blanks a link's `](url)` before prose() sees the line, so
# the check matches the raw line in `$0`, which keeps the same columns.
# It counts a match only when the blanked line still holds its `[`, so
# a link inside a code span passes. An image (`![here](x.png)`) passes,
# since its alt text describes the picture.
#
# Output shape, one line per finding:
#   <file>:<line>:<col>: Style::ProseLinkText: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  vague = "(^|[^!])\\[(here|click here|this|this link|link|this page|this doc|this document|this article|this post|read more|more|learn more)\\](\\(|\\[)"
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
    printf "%s:%d:%d: Style::ProseLinkText: Link text \"%s\" names no destination. Name the page or section it opens.\n", FILENAME, FNR, column(text, at), hit
  }
}
