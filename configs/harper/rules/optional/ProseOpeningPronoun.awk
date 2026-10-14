# Reports a paragraph of markdown prose that opens with a pronoun
# (`This is similar to recursive attribute sets`). A pronoun is a word
# that stands in for a noun named earlier, such as `it`, `they`, or
# `this`. The noun sits in the paragraph before. Readers who jump to
# the paragraph from a link or a search read back to find it. A
# paragraph that someone moves, or a paragraph added between the two,
# changes what the pronoun names. Naming the noun lets the paragraph
# stand on its own (`A submodule is similar to a recursive attribute
# set`).
#
# The check reads the first words of each paragraph. It reports `It`,
# `They`, `This`, `These`, `That`, or `Those` before a verb or a helping
# verb from a list (`is`, `can`, `requires`). `This cache` names its
# noun, so a noun after the pronoun passes. A plural pronoun takes a
# verb without an `-s`, so a word that ends in `s` after `These`,
# `Those`, or `They` counts as a noun (`These changes simplify`).
#
# Some openings use `it` as a placeholder subject that points at no
# earlier noun (`It is possible to`, `It is vital that`). `It` before
# `is` or `was` and an adjective such as `recommended` or `possible`
# passes. So does `It` when its first sentence holds `to` or `that`
# (`It may help to restart the pod`). ProseExpletive, ProseImportance,
# and ProseObligation report those openings. ProseBroadReference
# reports `This means`, `This leads`, and the other verbs of
# consequence, with or without `also` or `then` between them, and
# `This is because`, `why`, or `how`. Those openings pass here too.
#
# A paragraph whose first line ends in a colon passes, because its
# pronoun points forward at the block below (`These are the tiers:`).
# A paragraph directly under a heading passes, because there the
# pronoun points at the heading's topic, which the reader can see. The
# first paragraph of a file passes when front matter or any other line
# sits above it, because there the pronoun points at the page.
#
# The check reads markdown only. Go doc comments open later paragraphs
# with `It` by convention, and there the first paragraph names the
# function.
#
# It hit 107 lines in the measure.py docs corpus, and 18 of 20 sampled
# hits pointed back across the paragraph break. The two misses pointed
# at the section that held them.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseOpeningPronoun: <message>

# Tracks the last two non-blank lines, code and comments included, so a
# paragraph can tell whether a heading sits directly above it.
{
  if (FNR == 1) {
    prev_nb = 0
    cur_nb = 0
    heading_line = 0
  }
  if ($0 !~ /^[ \t]*$/) {
    prev_nb = cur_nb
    cur_nb = FNR
  }
}

@include "markdown.awk"

function BEGIN_CHECK() {
  pronoun = "^(it|they|this|these|that|those)$"
  verb = "^(is|are|was|were|has|have|had|does|do|did|can|cannot|will|would|could|should|must|may|might|also|then|only|still|runs|returns|uses|needs|requires|includes|provides|helps|leads|results|applies|depends|reads|writes|holds|set|sets|adds|removes|creates|changes|tells|shows)$"
  expletive = "^(recommended|suggested|certainly|advised|encouraged|discouraged|important|necessary|possible|impossible|advisable|best|common|easy|hard|difficult|useful|helpful|safe|worth|likely|unlikely|not|a|an|good|better|unclear|clear|true|false|essential|required|often|usually|generally|sometimes|up|also)$"
  consequence = "^(means|meant|ensures|happens|prevents|causes|keeps|works|matters|leads|led|results|resulted|makes|allows|lets|gives)$"
  last_file = ""
  last_line = 0
  gap = 1
  paras = 0
}

# Returns token t in lowercase without the punctuation around it.
function word(t,   w) {
  w = tolower(t)
  gsub(/^[^a-z']+|[^a-z']+$/, "", w)
  return w
}

function prose(text,   start, rest, t1, t2, t3, w1, first, shown) {
  start = (FILENAME != last_file || FNR != last_line + 1 || gap)
  if (FILENAME != last_file) {
    paras = 0
  }
  last_file = FILENAME
  last_line = FNR
  if (text ~ /^[ \t]*$/) {
    gap = 1
    return
  }
  gap = 0
  if (text ~ /^[ \t]*#+([ \t]|$)/) {
    heading_line = FNR
  }
  # Headings, table rows, HTML lines, and list items are not paragraphs,
  # and neither are the lines that continue a list item.
  if (text ~ /^[ \t]*(#+([ \t]|$)|[|<])/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/) {
    return
  }
  if (!start) {
    return
  }
  paras++
  # The first paragraph under a title or front matter points at the page.
  if (paras == 1 && FNR > 1) {
    return
  }
  # A heading directly above names the topic that the pronoun points at.
  if (heading_line && heading_line == prev_nb) {
    return
  }
  rest = text
  if (!match(rest, /[^ \t]+/)) {
    return
  }
  t1 = substr(rest, RSTART, RLENGTH)
  rest = substr(rest, RSTART + RLENGTH)
  t2 = match(rest, /[^ \t]+/) ? substr(rest, RSTART, RLENGTH) : ""
  rest = substr(rest, RSTART + RLENGTH)
  t3 = match(rest, /[^ \t]+/) ? substr(rest, RSTART, RLENGTH) : ""
  w1 = word(t1)
  if (t1 !~ /^[A-Z]/ || t1 ~ /[,.:;]$/ || w1 !~ pronoun || t2 !~ /^[a-z]/ || word(t2) !~ verb) {
    return
  }
  if (w1 == "it" && word(t2) ~ /^(is|was)$/ && word(t3) ~ expletive) {
    return
  }
  # A plural pronoun takes no `-s` verb, so `These changes` names a noun.
  if (w1 ~ /^(these|those|they)$/ && word(t2) ~ /s$/) {
    return
  }
  # ProseBroadReference reports these openings.
  if (w1 == "this" && word(t2) ~ /^(is|was)$/ && word(t3) ~ /^(because|why|how)$/) {
    return
  }
  if (w1 == "this" && (word(t2) ~ consequence || (word(t2) ~ /^(also|then)$/ && word(t3) ~ consequence))) {
    return
  }
  # An `it` with `to` or `that` later in its sentence stands in for the
  # clause after it (`It may help to play`).
  first = text
  sub(/[.?!]([ \t].*)?$/, "", first)
  if (w1 == "it" && first ~ /[ \t](to|that)[ \t]/) {
    return
  }
  if (text ~ /:[ \t]*$/ && text !~ /[.?!][ \t]/) {
    return
  }
  shown = t1
  gsub(/[^A-Za-z]/, "", shown)
  printf "%s:%d:%d: Style::ProseOpeningPronoun: Opening pronoun. The paragraph opens with `%s`, so the reader must look back for its noun. Name the noun.\n", FILENAME, FNR, column(text, index(text, t1)), shown
}
