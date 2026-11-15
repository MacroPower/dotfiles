# Overview
#
# Reports a spaced ASCII arrow (`->`, `=>`, `<-`) that stands in for a
# verb in markdown prose (`plan -> apply`). The reader must guess the
# verb that the arrow replaces. The message names three fixes: the verb
# that the arrow stands for (`the client calls the proxy`), a colon
# after a label (`Yes: Hugo builds the site`), and `>` for a menu path.
# The rule sits at Recommended, because a spaced ASCII arrow also
# appears in prose that quotes code without a code span.
#
# Expression
#
# The check includes lib/markdown.awk and reads one prose line at a
# time. `ascii` matches an arrow that has a space, a tab, or an edge of
# the line on each side. The check reports the first such arrow on a
# line.
#
# False Positives
#
# A glued arrow (`x->y`, `x=>x+1`) is code written without a code span,
# so the check leaves it alone.
#
# Related Rules
#
# The Required ProseArrow reports the Unicode arrows and their HTML
# entities. This tier reports only the spaced ASCII arrows, so the two
# tiers never report the same arrow.
#
# Measurement
#
# The rule hit 5 lines in the measure.py corpus, and 1 of 5 stood in for
# a word (`v0.1.1 => v0.2.0 is a minor release`). The other four were
# code in a `<pre>` block or in a Hugo shortcode, and lib/markdown.awk
# skips neither.
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
    printf "%s:%d:%d: Style::ProseArrow: Arrow `%s` stands in for a word. Write the verb, a colon after a label, or `>` for a menu path.\n", FILENAME, FNR, column(RSTART + (substr(text, RSTART, 1) ~ /[ \t]/)), hit
  }
}
