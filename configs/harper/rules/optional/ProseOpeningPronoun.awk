# Overview
#
# Reports a paragraph of markdown prose that opens with a pronoun (`This
# is similar to recursive attribute sets`). A pronoun is a word that
# stands in for a noun named earlier, such as `it`, `they`, or `this`.
# The noun sits in the paragraph before. Readers who jump to the
# paragraph from a link or a search read back to find it. A paragraph
# that someone moves, or a paragraph added between the two, changes what
# the pronoun names.
#
# The fix names the noun, so the paragraph stands on its own (`A
# submodule is similar to a recursive attribute set`). The rule sits at
# Optional, because an opening pronoun also points at the section that
# holds the paragraph, where the reader can see the topic.
#
# Expression
#
# The check includes lib/markdown.awk and reads the first words of each
# paragraph. It reports `It`, `They`, `This`, `These`, `That`, or
# `Those` before a verb or a helping verb from a list (`is`, `can`,
# `requires`).
#
# The guards for `to`, `that`, and a colon read the first sentence of
# the paragraph, which can run onto the lines below its first line
# (`It may help` above `to restart the pod.`). The check therefore
# holds the finding until the paragraph ends, and then reports it at
# the first line. The end of the paragraph also ends the sentence. A
# blank line, a line that lib/markdown.awk skips or marks in
# `bare_line`, a heading, a table row, an HTML line, a list item, or a
# horizontal rule ends the paragraph. So does a line that opens with
# more `>` markers than the first line of the paragraph, because it
# opens a deeper blockquote (`> to restart the pod.`).
#
# A setext underline is a line of only `=` or only `-` marks right
# after a paragraph. It turns the paragraph into a heading, so the check
# drops the finding. A lone `-` there is an underline and not an empty
# list item, because an empty list item cannot interrupt a paragraph.
#
# The underline must open with as many `>` markers as the paragraph. In
# a list item, it must also reach the column where the text of the
# paragraph starts. underline() in lib/sentences.awk applies the same
# tests. Like underline(), the check reads a line of `=` or `-` indented
# four or more spaces past the text of the paragraph as an underline.
# The check therefore drops that paragraph, although GFM reads the line
# as more text of the paragraph.
#
# ends_sentence() in lib/markdown.awk finds the end of the first
# sentence, so `e.g.`, `i.e.`, `vs.`, `cf.`, and a three-period
# ellipsis do not end it (`It may help, e.g. to restart the pod`). A
# quoted `.`, `?`, or `!` before a lowercase word does not end it
# either, and that word can open the next line (`It writes "Done!" to
# the log`). continues_sentence() in lib/markdown.awk holds that test.
#
# A first sentence that ends in a three-period ellipsis, quoted or not,
# joins the next sentence. A `to` or `that` in that sentence, or a colon
# at the end of a line, then passes the paragraph (`It can hang...
# Then it fails to start`).
#
# The guards miss some paragraphs whose pronoun names an earlier noun.
# A `to` or `that` in another role passes `It` (`It adjusts the
# replicas to match the load`), and a first sentence that ends in a
# colon can still point back (`This is equivalent to these commands:`).
#
# False Positives
#
# `This cache` names its noun, so a noun after the pronoun passes. A
# plural pronoun takes a verb without an `-s`, so a word that ends in
# `s` after `These`, `Those`, or `They` counts as a noun (`These changes
# simplify`).
#
# A paragraph passes when a line ends in a colon before its first
# sentence ends (`These are the tiers:`). Its pronoun then points
# forward at the block below. lib/markdown.awk blanks a code span or a
# URL, so a colon before one at the end of the line also passes. There
# the pronoun points at the span (``This is the default: `false` ``).
#
# A paragraph directly under a heading passes, because there the
# pronoun points at the heading's topic, which the reader can see. The
# first paragraph of a file passes when front matter or any other line
# sits above it, because there the pronoun points at the page.
#
# The check reads markdown only. Go doc comments open later paragraphs
# with `It` by convention, and there the first paragraph names the
# function.
#
# Related Rules
#
# Some openings use `it` as a placeholder subject that points at no
# earlier noun (`It is possible to`, `It is vital that`). `It` before
# `is` or `was` and an adjective such as `recommended` or `possible`
# passes. So does `It` when its first sentence holds `to` or `that` (`It
# may help to restart the pod`). ProseExpletive, ProseImportance, and
# both tiers of ProseObligation report those openings.
#
# ProseBroadReference reports `This means`, `This leads`, and the other
# verbs of consequence, with or without `also` or `then` between them,
# and `This is because`, `why`, or `how`. Those openings pass here too.
#
# Measurement
#
# The rule hit 105 lines in the measure.py docs corpus, and 19 of 20
# sampled hits pointed back across the paragraph break. The misfire was
# a line of sample output that a Hugo page shows (`This is some code.`),
# where `This` points at the line itself.
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
  pending = 0
}

# Returns token t in lowercase without the punctuation around it.
function word(t,   w) {
  w = tolower(t)
  gsub(/^[^a-z']+|[^a-z']+$/, "", w)
  return w
}

# Adds the words of one line of the paragraph to `first`, the first
# sentence of the paragraph, through the token that ends the sentence.
# ends_sentence() in lib/markdown.awk reads each token, so `e.g.` and
# `reading...` do not end the sentence. After a token for which it
# returns 2, the sentence continues when continues_sentence() in
# lib/markdown.awk accepts the next token, which can open the next
# line. `first_end` keeps the result for the last token across lines,
# and `first_done` records that the sentence has ended. A line on which
# the sentence does not end joins `first` whole. It then keeps the
# spaces that lib/markdown.awk leaves for a code span or a URL at its
# end (``It sets the port to `8080` ``).
function read_first(text,   rest, pos, t) {
  rest = text
  pos = 0
  while (!first_done && match(rest, /[^ \t]+/)) {
    t = substr(rest, RSTART, RLENGTH)
    if (first_end == 2 && !continues_sentence(t)) {
      first_done = 1
    } else {
      pos += RSTART + RLENGTH - 1
      rest = substr(rest, RSTART + RLENGTH)
      first_end = ends_sentence(t)
      first_done = first_end == 1
    }
  }
  first = first (first_done ? substr(text, 1, pos) : text)
}

# Returns the number of `>` markers that open the line, as
# quote_depth() in lib/sentences.awk does.
function quote_depth(   p) {
  match($0, /^([ \t]*>)*/)
  p = substr($0, 1, RLENGTH)
  return gsub(/>/, "", p)
}

# Prints the finding that the paragraph holds back until it ends, unless
# its first sentence passes a guard. A newline in `first` marks the end
# of each line of the paragraph.
function finish() {
  if (!pending) {
    return
  }
  pending = 0
  # An `it` with `to` or `that` later in its sentence is a placeholder
  # subject that points at no earlier noun (`It may help to play`).
  if (pending_word == "it" && first ~ /[ \t\n](to|that)[ \t\n]/) {
    return
  }
  # A first sentence that runs to a colon at the end of a line points
  # at the block below. A sentence that ends earlier ends in a mark, so
  # it does not end in a colon.
  if (first ~ /:[ \t]*(\n|$)/) {
    return
  }
  printf "%s:%d:%d: Style::ProseOpeningPronoun: Opening pronoun. The paragraph opens with `%s`, so the reader must look back for its noun. Name the noun.\n", pending_file, pending_line, pending_col, pending_shown
}

function prose(text,   start, rest, t1, t2, t3, w1, shown, line) {
  start = (FILENAME != last_file || FNR != last_line + 1 || gap)
  if (FILENAME != last_file) {
    paras = 0
  }
  last_file = FILENAME
  last_line = FNR
  # A bare line ends the paragraph.
  if (bare_line) {
    finish()
    gap = 1
    return
  }
  gap = 0
  if (!start && pending) {
    # The tests for an underline and a horizontal rule read the line
    # before lib/markdown.awk blanks code spans. An emphasized code span
    # (``**`make`**``) then does not read as a rule of four `*` marks.
    line = blank(raw, "<!--([^-]|-[^-])*-->")
    # A setext underline makes the paragraph a heading. An empty list
    # item cannot interrupt a paragraph, so a lone `-` there is an
    # underline. This test therefore runs before the test for a list
    # item.
    if (quote_depth() == pending_quote && line ~ /^[ \t]*(=+|-+)[ \t]*$/ && match(line, /[^ \t]/) > pending_item) {
      pending = 0
      return
    }
  }
  if (text ~ /^[ \t]*#+([ \t]|$)/) {
    heading_line = FNR
  }
  # Headings, table rows, HTML lines, and list items are not paragraphs,
  # so each one ends the paragraph before it. The lines that continue a
  # list item are not paragraphs either.
  if (text ~ /^[ \t]*(#+([ \t]|$)|[|<])/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/) {
    finish()
    return
  }
  if (!start) {
    if (!pending) {
      return
    }
    if (quote_depth() > pending_quote || line ~ /^[ \t]*((-[ \t]*){3,}|(\*[ \t]*){3,}|(_[ \t]*){3,})$/) {
      # A deeper blockquote or a horizontal rule ends the paragraph.
      finish()
    } else if (!first_done) {
      # Any other line continues the paragraph, so it can hold the rest
      # of the first sentence.
      first = first "\n"
      read_first(text)
    }
    return
  }
  # The line opens a paragraph, so the paragraph before it has ended.
  finish()
  paras++
  # The first paragraph under a title or front matter points at the
  # page.
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
  # A plural pronoun does not take an `-s` verb, so `These changes`
  # names a noun.
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
  # The guards read the first sentence, which can run onto the lines
  # after this one, and an underline can turn the paragraph into a
  # heading. So the finding waits until the paragraph ends.
  shown = t1
  gsub(/[^A-Za-z]/, "", shown)
  pending = 1
  pending_file = FILENAME
  pending_line = FNR
  pending_col = column(index(text, t1))
  pending_shown = shown
  pending_word = w1
  # An underline must sit at the quote depth of the paragraph. In a
  # list item, it must also reach the column where the paragraph's text
  # starts, as underline() in lib/sentences.awk requires.
  pending_quote = quote_depth()
  pending_item = 0
  if (in_list) {
    match($0, /^([ \t]*>)*[ \t]*/)
    pending_item = RLENGTH
  }
  first = ""
  first_end = 0
  first_done = 0
  read_first(text)
}

END {
  finish()
}
