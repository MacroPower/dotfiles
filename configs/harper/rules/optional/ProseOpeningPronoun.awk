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
# opens a deeper blockquote (`> to restart the pod.`). That line opens a
# paragraph in the quote, and an underline or a rule under it ends that
# paragraph. The check does not report a paragraph that opens this way,
# although GFM reads it as a paragraph (`The pod holds the cache.`,
# `> It runs on the node.`).
#
# As in CommonMark, a heading, a table row, an HTML line, a list item, or
# a horizontal rule ends the paragraph only at an indent of fewer than
# four columns. The limit also applies to a line that holds only an HTML
# comment or a `pre` element, which lib/markdown.awk marks in
# `bare_line`. A line with a deeper indent cannot open a block there, so
# it is more text of the paragraph (`It may help`, `    - first`, `to
# restart the pod.`). At any indent, the check still reads a line with
# more `>` markers than the paragraph as a deeper blockquote, so the
# line ends the paragraph.
#
# The check reads a line as more text only when no `>` marker of the
# line sits left of the marker with the same index in the first line of
# the paragraph. In a list item, GFM can end the item at a marker
# further left and open a new blockquote (`  > It may help` in a `-`
# item, `>       - first`).
#
# GFM counts the indent from the left edge of the block that holds the
# paragraph. A tab moves to the next multiple of four columns. In a
# blockquote, the edge sits after the `>` markers and the space that
# follows them.
#
# The check does not track that edge. On a line with as many markers as
# the paragraph, it counts from the column where the text of the
# paragraph starts (`   It may help`, `       - first`). On a line with
# fewer markers, it counts from the column of the first marker that the
# line lacks in the first line of the paragraph (`  > It may help`,
# `      - first` in a `-` item). When the edge after the markers of the
# line sits further right, the check counts from that edge. The text of
# a paragraph and a `>` marker never sit left of the edge of their
# block. So the check never counts more columns than GFM does.
#
# GFM can still read a line with a deep indent as the header row of a
# table, when a delimiter row with as many cells sits right under it
# (`It may help`, `    | a | to |`, `| --- | --- |`). The paragraph then
# ends above the header row. The check reads a deep line the same way
# when the line ends the paragraph at a smaller indent, such as a line
# that opens with `|` or a list marker. The first sentence then drops
# the text of that line. A delimiter row indented four or more columns
# is more text of the paragraph, so no table opens there.
#
# lib/markdown.awk opens a list at a line such as `    - first`, even
# when that line continues a paragraph. It also passes the indented code
# of a list to the check. The check therefore skips indented code below
# a paragraph that holds a line indented four or more columns. Only
# blank lines and lines that lib/markdown.awk skips can sit between the
# paragraph and the code. A line there is code when its indent, counted
# as for the lines of the paragraph, reaches four columns (`It may
# help`, `    - first`, a blank line, `    make build`).
#
# GFM reads such a line as code too, because the check never counts
# more columns than GFM does. The code ends at the first line with a
# smaller indent, and that line opens a paragraph. lib/markdown.awk
# skips a link reference definition within three columns of the margin.
# A line right under such a definition is not code, because GFM reads it
# as more text of the paragraph that the definition opens.
#
# A paragraph outside a list can hold a line of rule marks (` It may
# help to restart the pod`, `     - first`, `     * * *`). Under that
# line, the check counts the indent of the code from the start of the
# line, as after a rule (a blank line, `    make build`). Only blank
# lines can sit between the two. Outside a list, GFM counts from there
# too.
#
# A heading opens with one to six `#` marks. CommonMark reads a line
# with seven or more marks as text, and so do the check and
# lib/markdown.awk. Such a line right after a paragraph continues the
# paragraph (`####### to restart the pod.`). A code span or a list item
# also runs on across the line. A paragraph after the line does not sit
# under a heading, so the check can report it.
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
# A horizontal rule repeats one mark, `-`, `*`, or `_`, three or more
# times, with or without spaces between the marks (`***`, `- - -`). A
# line of three or more `-` marks that fails the tests for an underline
# is a rule. A rule is not a list item, and it does not open a
# paragraph. The line after it therefore opens a paragraph, with or
# without a blank line above the rule (`The pod holds the cache.`,
# `***`, `It runs on the node.`). A rule does not count toward the
# first paragraph of the file either.
#
# Outside a list and a blockquote, the check reads a line indented four
# or more spaces after a rule as indented code, as GFM does (`***`,
# `    This is some code.`). Only blank lines can sit between the rule
# and the line. Outside a list, lib/markdown.awk skips indented code
# after a blank line or a thematic break. It does not read a break at
# rule marks that are indented code themselves, such as `    ***` right
# under a closing fence. Nor does it read one at rule marks with an
# HTML comment after them, which the check reads as a rule. The check
# therefore skips such a line itself, and each indented line after it.
# The first line that is not indented opens a paragraph.
#
# A rule with no indent cannot sit in a list item, so lib/markdown.awk
# ends the list there. It keeps the list open at an indented rule right
# under a paragraph of a list item. The check therefore counts a rule
# as part of a list only when a list is open above the rule and the
# rule is indented. A rule outside a list closes the list for the lines
# after it, until a list item opens one. lib/markdown.awk can keep the
# list open there, such as after a line of a paragraph that opens like
# a list item (`    - first`).
#
# Code under `- - -` or `  - - -` outside a list therefore passes, and
# so does code under a `***` that ends a list. Code under a later
# indented rule passes too (`* * *`, `The pod holds the cache.`,
# `  ***`, `    This is some code.`). A line of `-` marks under an
# indented paragraph after such a rule stays an underline.
#
# The check skips the lines after a heading, a table row, an HTML line,
# or a list item, up to the next blank line. It skips a rule there too.
# It therefore misses a paragraph right after such a rule, which GFM
# reads as a paragraph (`## Setup`, `***`, `It runs on the node.`).
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
# pronoun points at the heading's topic, which the reader can see. In a
# blockquote, a line of only `>` markers between the two counts as a
# blank line (`> ## Setup`, `>`, `> This is the setup step.`). The
# first paragraph of a file passes when front matter or any other line
# sits above it, because there the pronoun points at the page.
#
# GFM counts the indent from the left edge of the block. Outside a list
# and a blockquote, the edge is the start of the line. In a list item,
# it is the column where the text of the item starts. The text of a
# paragraph and a `>` marker can each start up to three columns past
# that edge, but the check counts from them. It therefore ends the
# paragraph at a line such as `    - first` under `   It may help`
# outside a list, or `       - first` under `     It may help` in a `-`
# item. GFM reads that line as more text of the paragraph, but the
# guards miss the words after it. When such a line holds the marks of a
# rule (`     ***` under `   It may help`), the line after it opens a
# paragraph of its own. That paragraph can draw a finding.
#
# lib/markdown.awk can pass indented code under such a paragraph to the
# check, such as after a line of `- first`, at which it opens a list.
# The check reads that code as a paragraph when it sits fewer than four
# columns past the text of the paragraph (`   It may help to restart the
# pod`, `       - first`, a blank line, `    It runs on the node.`).
#
# A `>` marker can sit up to three columns past the edge of its block.
# So a line can still continue the blockquote when its marker sits left
# of the marker with the same index in the first line of the paragraph.
# The check ends the paragraph at such a line when the line opens with a
# list marker or a block (`   > It may help` above `>     - first`, or
# `> > It may help` above `>>     - first`).
#
# GFM also reads a line of plain text as the header row of a table (`a |
# to` above `--- | ---`). The check reads that line and the delimiter
# row as text of the paragraph, so the guards read the words of the
# table.
#
# The check measures the indent of a delimiter row as for the lines of
# the paragraph. It can therefore read a row as the delimiter row of a
# table where GFM reads the row as more text of the paragraph. Examples
# are a row at the margin under a paragraph of a list item, and a row
# indented four columns under ` It may help`. The guards then miss the
# words of the header row.
#
# lib/markdown.awk closes a list at a heading or a table row indented
# fewer than four spaces, even one inside a list item. The check
# therefore cannot tell an indented paragraph outside a list from one
# in such an item, and it counts from the text of both. In such an item,
# a count from the start of the line reads a nested list item as more
# text of the paragraph (`   ## Setup`, `   It may help`, `    - first`
# in a `1.` item).
#
# Under a line of rule marks, the check counts the indent of code from
# the start of the line, as after a rule. In such an item, it can
# therefore skip a later paragraph of the item that sits fewer than
# four columns past the text of the item. An example is `   ## Setup`,
# a blank line, `   It may help to restart the pod`, `       - first`,
# `       - - -`, a blank line, and `    It runs on the node.` in a
# `1.` item. The check reads `       - first` there as text and not as
# a list item. So a rule under it, such as `     ***`, reads as a rule
# outside a list, and the check skips the paragraph under that rule too.
#
# In such an item, a line such as `       - first` also leaves the
# paragraph that holds it, and each later paragraph of the item, outside
# a list. The check then reads a line of `-` or `=` marks left of the
# text of the paragraph as an underline and drops the finding. GFM does
# not read that line as an underline. It reads `  ---` there as a rule
# that ends the item, and `  ===` as more text of the paragraph. An
# example is a `1.` item that holds `   ## Setup`, a blank line,
# `   The pod runs.`, a blank line, `   It may help`, `       - first`,
# and `  ---`. A table row in the item has the same effect.
#
# lib/markdown.awk passes indented code in a list item to the check.
# The check skips that code only below a paragraph that holds a line
# indented four or more columns. Below another paragraph of a list
# item, it can report the code (`- The pod holds the cache.`, a blank
# line, `      It runs on the node.`).
#
# The check reads a `>` at any indent as a blockquote marker.
# lib/markdown.awk skips each line of an HTML comment that spans lines,
# and each line of fenced code, at any indent. The check therefore ends
# the paragraph at a line such as `    > first`, `    <!-- a`, or
# `    ~~~`, although GFM reads that line as more text of the paragraph.
# A line such as `    1. first` continues the paragraph, but its `1.`
# ends the first sentence. The guards then miss the words after each of
# these lines.
#
# The check reads an indented line after a rule as code only outside a
# list and a blockquote. After an indented rule in a list, GFM can read
# the line as code, and the check can still report it there.
# lib/markdown.awk skips indented code in a blockquote, so the check
# does not see such a line after a rule there (`> ***`, `>     This is
# some code.`). The check also counts an indented rule left of the
# text of a list item as part of the list. GFM ends the list at that
# rule and reads an indented line under it as code. The check can report
# that line (`  The pod holds the cache.` in a list item, ` ***`,
# `    This is some code.`).
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
# paragraph can tell whether a heading sits directly above it. A line of
# only `>` markers is a blank line inside a blockquote, so it does not
# count. `def_line` names the last line that opens like a link
# reference definition (`[guide]: https://example.com`).
# lib/markdown.awk skips such a line only within three columns of the
# margin, and it passes every other one to prose().
{
  if (FNR == 1) {
    prev_nb = 0
    cur_nb = 0
    heading_line = 0
    def_line = 0
    rule_line = 0
    head_line = 0
  }
  if ($0 !~ /^[ \t>]*$/) {
    prev_nb = cur_nb
    cur_nb = FNR
  }
  if ($0 ~ /^([ \t]*>)*[ \t]*\[[^]^][^]]*\]:/) {
    def_line = FNR
  }
}

@include "markdown.awk"

function BEGIN_CHECK() {
  pronoun = "^(it|they|this|these|that|those)$"
  verb = "^(is|are|was|were|has|have|had|does|do|did|can|cannot|will|would|could|should|must|may|might|also|then|only|still|runs|returns|uses|needs|requires|includes|provides|helps|leads|results|applies|depends|reads|writes|holds|set|sets|adds|removes|creates|changes|tells|shows)$"
  expletive = "^(recommended|suggested|certainly|advised|encouraged|discouraged|important|necessary|possible|impossible|advisable|best|common|easy|hard|difficult|useful|helpful|safe|worth|likely|unlikely|not|a|an|good|better|unclear|clear|true|false|essential|required|often|usually|generally|sometimes|up|also)$"
  consequence = "^(means|meant|ensures|happens|prevents|causes|keeps|works|matters|leads|led|results|resulted|makes|allows|lets|gives)$"
  hrule = "^[ \t]*((-[ \t]*){3,}|(\\*[ \t]*){3,}|(_[ \t]*){3,})$"
  delimiter = "^[ \t]*\\|?[ \t]*:?-+:?[ \t]*(\\|[ \t]*:?-+:?[ \t]*)*\\|?[ \t]*$"
  last_file = ""
  last_line = 0
  gap = 1
  paras = 0
  pending = 0
  in_para = 0
  para_more = 0
  rule_above = 0
  rule_list = 0
  list_seen = 0
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

# Returns the column, counted from 0, after the first `n` characters of
# the line. A tab moves to the next multiple of four columns, as in
# CommonMark.
function cols(n,   i, col) {
  col = 0
  for (i = 1; i <= n; i++) {
    col += substr($0, i, 1) == "\t" ? 4 - col % 4 : 1
  }
  return col
}

# Returns the indent of the line in columns. The count starts after the
# `>` markers of the line and the one space after them. A tab after the
# markers gives one column to that space and the rest to the indent, as
# in CommonMark. On a line with as many markers as the paragraph, the
# count never starts left of `para_text`. On a line with `d` markers,
# fewer than the paragraph has, it never starts left of `para_mark[d]`.
function indent(   n, base, d, edge) {
  match($0, /^([ \t]*>)*/)
  n = RLENGTH
  base = cols(n)
  if (n && substr($0, n + 1, 1) ~ /[ \t]/) {
    base++
  }
  d = quote_depth()
  edge = d < para_quote ? para_mark[d] : para_text
  if (edge > base) {
    base = edge
  }
  match($0, /^([ \t]*>)*[ \t]*/)
  return cols(RLENGTH) - base
}

# Returns 1 when no `>` marker of the line sits left of the marker with
# the same index in the first line of the paragraph. In a list item, GFM
# can end the item at a marker further left and open a new blockquote
# there (`  > It may help` in a `-` item, `>       - first`).
function marks_hold(   n, i, k) {
  match($0, /^([ \t]*>)*/)
  n = RLENGTH
  k = 0
  for (i = 1; i <= n; i++) {
    if (substr($0, i, 1) == ">" && cols(i - 1) < para_mark[k++]) {
      return 0
    }
  }
  return 1
}

# Returns the number of cells in the table row s, as GFM counts them. A
# `|` after a backslash is text of its cell. A `|` that opens the row
# does not end a cell. The text after the last `|` counts as a cell only
# when it is not blank.
function cells(s,   n) {
  gsub(/\\\|/, "", s)
  sub(/^[ \t]*\|/, "", s)
  n = gsub(/\|/, "|", s)
  sub(/^.*\|/, "", s)
  return n + (s ~ /[^ \t]/)
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

# Opens a paragraph at the line. `in_para` holds until the paragraph
# ends, whether or not the paragraph draws a finding. An underline must
# sit at the quote depth of the paragraph. When `item` marks the
# paragraph as part of a list item, the underline must also reach the
# column where the paragraph's text starts, as underline() in
# lib/sentences.awk requires. `para_item` counts the characters before
# that column.
#
# For every paragraph, `para_text` holds the column where its text
# starts. `para_mark[k]` holds the column of the `>` marker after the
# first `k` markers of the line. The text of a paragraph never starts
# left of the block that holds it. A marker never sits left of the
# block that holds its quote either. So a count from these columns
# never exceeds the indent that GFM counts.
function open_para(item,   n, i, k) {
  in_para = 1
  para_more = 0
  para_quote = quote_depth()
  match($0, /^([ \t]*>)*[ \t]*/)
  n = RLENGTH
  para_item = item ? n : 0
  para_text = cols(n)
  k = 0
  for (i = 1; i <= n; i++) {
    if (substr($0, i, 1) == ">") {
      para_mark[k++] = cols(i - 1)
    }
  }
}

function prose(text,   start, rest, t1, t2, t3, w1, shown, line, after_rule, list_before, more, block) {
  start = (FILENAME != last_file || FNR != last_line + 1 || gap)
  # `rule_above` marks a horizontal rule, or indented code under one, on
  # the last line that reached prose(). `prev_nb` names the last line
  # above this one that is not blank, so only blank lines sit between
  # the two.
  after_rule = rule_above && FILENAME == last_file && prev_nb == last_line
  rule_above = 0
  # lib/markdown.awk changes `in_list` only on a line that reaches
  # prose(), so `list_before` holds the list state from before this
  # line.
  list_before = FILENAME == last_file ? list_seen : 0
  # The tests for an underline and a horizontal rule read the line
  # before lib/markdown.awk blanks code spans. An emphasized code span
  # (``**`make`**``) then does not read as a rule of four `*` marks.
  line = blank(raw, "<!--([^-]|-[^-])*-->")
  # A line indented four or more columns cannot open a heading, a table
  # row, an HTML block, a list item, or a rule. `more` therefore marks it
  # as more text of the open paragraph (`    - first`). A line with more
  # `>` markers than the paragraph still ends it, at any indent. So does
  # a line with a marker left of the paragraph's marker, which can open
  # a new blockquote.
  more = !start && in_para && quote_depth() <= para_quote && marks_hold() && indent() >= 4
  # `para_more` marks a paragraph that holds a line that `more` marks.
  if (more) {
    para_more = 1
  }
  # `rule_line` names a line that `more` marks and that holds the marks
  # of a horizontal rule, when no list is open above it (`    * * *`).
  if (more && line ~ hrule && !list_before) {
    rule_line = FNR
  }
  # `start` and `in_para` together mark a line below the paragraph, or
  # below code under it. Only blank lines and lines that
  # lib/markdown.awk skips sit between the two. lib/markdown.awk opens a
  # list at a line such as `    - first`, and it passes the code of a
  # list to the check. So the check skips that code itself.
  #
  # Below a paragraph that `para_more` marks, such a line with an
  # indent() of four or more columns is code. GFM counts at least as
  # many columns there. When `rule_line` names the last line above that
  # is not blank, a line indented four or more columns from its start is
  # code too, as after a rule. `rule_line` then names the code line, so
  # the code under it skips as well. Take a link reference definition
  # that lib/markdown.awk skips. The line right under it is more text of
  # the paragraph that the definition opens, so it is not code. A line
  # that only looks like a definition, such as a line of the code,
  # reaches prose() and sets `last_line`. The line under it can still be
  # code.
  #
  # The paragraph stays open across the code, and each later line of the
  # code skips too. The first line with a smaller indent opens a
  # paragraph.
  if (start && in_para && para_more && FILENAME == last_file && (def_line != FNR - 1 || last_line == FNR - 1) && (indent() >= 4 || (rule_line == prev_nb && $0 ~ /^(    |\t)/))) {
    if (rule_line == prev_nb) {
      rule_line = FNR
    }
    gap = 1
    last_line = FNR
    return
  }
  # `list_seen` follows `in_list`, but a rule outside a list, and the
  # code under such a rule, close the list for the lines after them.
  # lib/markdown.awk can keep `in_list` on there. Only a list item opens
  # the list again. `- - -` and `* * *` count as rules and not as list
  # items, so neither opens it. A line of the paragraph that looks like
  # a list item does not open the list either. A paragraph sits in a
  # list only when `list_seen` holds.
  list_seen = in_list && (list_before || (!more && text ~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/ && line !~ hrule))
  if (FILENAME != last_file) {
    paras = 0
  }
  last_file = FILENAME
  last_line = FNR
  # A bare line ends the paragraph. When `more` marks a line that holds
  # an HTML comment or a `pre` element, the line is more text of the
  # paragraph. A line of `>` markers and spaces stays a blank line of
  # the quote.
  if (bare_line && !(more && raw ~ /[^ \t]/)) {
    finish()
    in_para = 0
    gap = 1
    return
  }
  gap = 0
  # A setext underline makes the paragraph a heading. An empty list item
  # cannot interrupt a paragraph, so a lone `-` there is an underline.
  # This test therefore runs before the test for a list item. A line of
  # three or more `-` marks that fails this test is a horizontal rule.
  if (!start && in_para && quote_depth() == para_quote && line ~ /^[ \t]*(=+|-+)[ \t]*$/ && match(line, /[^ \t]/) > para_item) {
    pending = 0
    in_para = 0
    return
  }
  # A horizontal rule ends the paragraph before it and does not open
  # one, so the next line opens a paragraph. The test runs before the
  # test for a list item, because `- - -` is a rule and not a list item.
  # A rule among the lines after a heading, a table row, an HTML line,
  # or a list item falls through. The check skips it with those lines.
  if (line ~ hrule && !more && (start || in_para)) {
    finish()
    in_para = 0
    gap = 1
    rule_above = 1
    # A rule with no indent cannot sit in a list item, and
    # lib/markdown.awk ends the list there. It keeps `in_list` for an
    # indented rule right under a paragraph of a list item. `rule_list`
    # therefore marks a rule in a list only when a list is open above
    # the rule and the rule is indented. `line` has its `>` markers
    # blanked to spaces, so a rule in a quoted list counts as indented.
    # A rule outside a list closes the list for the lines after it.
    rule_list = list_before && line ~ /^[ \t]/
    list_seen = rule_list
    return
  }
  # No paragraph is open after a rule, so an indented line there opens
  # indented code, with or without blank lines between the two. Outside
  # a blockquote, lib/markdown.awk skips indented code only outside a
  # list, and only after a blank line or a thematic break that it reads.
  # It does not read a break at rule marks that sit in indented code or
  # that hold an HTML comment. It can also keep the list open where
  # `list_seen` is off. This test therefore skips the code after a rule.
  # The test skips no line after a rule in a list, where code must sit
  # four columns past the text of the item. It skips no line in a
  # blockquote either, because `$0` opens with the `>` markers. The code
  # ends at the first line that is not indented, and that line opens a
  # paragraph. A code line that looks like a list item (`    - foo`)
  # does not open a list.
  if (after_rule && !rule_list && $0 ~ /^(    |\t)/) {
    gap = 1
    rule_above = 1
    list_seen = 0
    return
  }
  # A delimiter row of a table turns the line above it into the header
  # row, when the two rows hold the same number of cells. The paragraph
  # then ends above the header row, so the first sentence drops the text
  # of that row. `head_line` names a line that `more` marks and that
  # ends the paragraph at a smaller indent. The first sentence holds that
  # line (`    | a | to |`, `    - first`). A delimiter row that `more`
  # marks is more text of the paragraph, and one that opens a list item
  # opens the list item.
  if (!start && head_line == FNR - 1 && !more && quote_depth() == para_quote && raw ~ delimiter && text !~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/ && cells(raw) == head_cells) {
    first = head_first
    finish()
    in_para = 0
    return
  }
  # A heading opens with one to six `#` marks. A line with more marks is
  # text, as in CommonMark, so it opens or continues a paragraph.
  if (!more && text ~ /^[ \t]*#{1,6}([ \t]|$)/) {
    heading_line = FNR
  }
  # Headings, table rows, HTML lines, and list items are not paragraphs,
  # so each one ends the paragraph before it. The lines that continue a
  # list item are not paragraphs either. A line that `more` marks looks
  # like one of them but continues the paragraph.
  block = text ~ /^[ \t]*(#{1,6}([ \t]|$)|[|<])/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/
  if (!more && block) {
    finish()
    in_para = 0
    return
  }
  if (!start) {
    if (!in_para) {
      return
    }
    if (quote_depth() > para_quote) {
      # A deeper blockquote ends the paragraph and opens one in the
      # quote. That paragraph does not draw a finding, but an underline
      # or a rule under it still ends it.
      finish()
      open_para(list_seen)
    } else if (pending && !first_done) {
      # Any other line continues the paragraph, so it can hold the rest
      # of the first sentence. A line here that opens like a block, a
      # bare line, or a rule has a deep indent. At a smaller indent, the
      # line ends the paragraph. It can still turn out to be the header
      # row of a table, so the check keeps the sentence from before that
      # line.
      if (block || bare_line || line ~ hrule) {
        head_line = FNR
        head_first = first
        head_cells = cells(raw)
      }
      first = first "\n"
      read_first(text)
    }
    return
  }
  # The line opens a paragraph, so the paragraph before it has ended.
  finish()
  open_para(list_seen)
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
  first = ""
  first_end = 0
  first_done = 0
  read_first(text)
}

END {
  finish()
}
