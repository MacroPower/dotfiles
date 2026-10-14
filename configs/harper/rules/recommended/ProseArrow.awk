# Reports a spaced ASCII arrow (`->`, `=>`, `<-`) standing in for a
# verb in markdown prose. The Required check of the same name reports
# the Unicode arrows. This one sits a tier lower because a spaced ASCII
# arrow also appears in prose that quotes code without a code span.
#
# The message names three fixes: the verb that the arrow stands for, a
# colon after a label, and `>` for a menu path. It hit 5 lines in the
# measure.py corpus, and 1 of 5 stood in for a word (`v0.1.1 => v0.2.0
# is a minor release`). The other four were code in a `<pre>` block or
# in a Hugo shortcode, which the markdown reader does not skip.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseArrow: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  # A spaced arrow (`plan -> apply`) stands in for a verb. Glued arrows
  # (`x->y`, `x=>x+1`) are code written without a code span, which the
  # check leaves alone.
  ascii = "(^|[ \t])(->|=>|<-)([ \t]|$)"
}

function prose(text,   hit) {
  if (match(text, ascii)) {
    hit = substr(text, RSTART, RLENGTH)
    gsub(/[ \t]/, "", hit)
    printf "%s:%d:%d: Style::ProseArrow: Arrow `%s` stands in for a word. Write the verb, a colon after a label, or `>` for a menu path.\n", FILENAME, FNR, column(text, RSTART + (substr(text, RSTART, 1) ~ /[ \t]/)), hit
  }
}
