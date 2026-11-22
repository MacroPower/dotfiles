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
# `bare_line`, a heading, an HTML block, a list item, or a horizontal
# rule ends the paragraph. So does a line that opens with more `>`
# markers than the first line of the paragraph, because it opens a
# deeper blockquote (`> to restart the pod.`). That line opens a
# paragraph in the quote, and the check reads that paragraph as any
# other, so it can draw a finding (`The pod holds the cache.`, `> It
# runs on the node.`).
#
# A blockquote also ends a table or a list item, and it can sit on the
# line right under a heading. A line with more `>` markers than such a
# block right above it therefore opens a paragraph in the quote too
# (`| a | b |`, `| - | - |`, `> It runs on the node.`). deeper_quote()
# tests the line, under a paragraph or such a block. Each `>` marker of
# the line must sit fewer than four columns past the edge that the
# marker before it leaves. Further right, GFM reads the line as more
# text or as indented code (`> The pod holds the cache.`, `    > > It
# runs on the node.`). After a marker, the edge sits after the marker
# and the space after it. The first marker counts from the text of the
# innermost list item that the line reaches, or from the margin. A line
# of the item's paragraph left of its text keeps the item open, but its
# first marker counts from the edge left of the item. So GFM reads a
# `>` four columns past the margin there as more text of the paragraph
# (`10.  The pod holds the cache and`, `    > It runs on the node.`).
#
# A list item can open a blockquote after its list marker (`- > The pod
# holds the cache.`). The block then holds the markers of that quote
# too, and skip_depth() counts them for a line that sits in the item.
# A line with no more markers than that continues the quote (`  > It
# runs on the node.`). A marker left of the text of the item ends the
# item and opens a new blockquote, which opens a paragraph (`> It runs
# on the node.`).
#
# A list item can also hold an HTML block, after its list marker (`-
# <details>`) or on a later line (`- Install the agent.`, `  <div>`).
# The block holds each later line that stays in the item, so a quoted
# line there is text of the block (`  > It runs on the node.`). A line
# left of the text of the item ends the item and the block, and it
# opens a block of its own (`It runs on the node.`). A block on a later
# line ends this way only in an item that `item_depth` counts, so not in
# a blockquote.
#
# The check does not track a list item in a blockquote. It therefore
# misses a blockquote four or more columns past the edge that it uses
# but within three columns of the text of such an item (`> - The pod
# holds the cache.`, `>      > It runs on the node.`).
#
# GFM opens an HTML block of types 1 to 6 at a line that opens with one
# of the tags that cmark-gfm, the parser that GitHub uses, lists for
# those types. Such a block ends the paragraph (`<div>`, `</td>`,
# `<pre`, `<!--`, `<?`). The check reads the tag before
# lib/markdown.awk blanks a comment or a `pre` element, so
# `<!-- note --> to restart the pod.` ends the paragraph too. A line
# that opens with any other tag is more text of the paragraph (`It may
# help`, `<span>x</span>`, `to restart the pod.`), because an HTML block
# of type 7 cannot interrupt a paragraph. A line that opens with `|` is
# more text of the paragraph too, unless a delimiter row turns it into
# the header row of a table.
#
# A line with fewer `>` markers than the paragraph sits outside the
# blockquote that holds the paragraph. A line left of the text of a
# list item sits outside the item. GFM reads such a line as more text
# of the paragraph only when the line does not open a block. A line
# that holds only one complete tag opens an HTML block of type 7 there,
# and the block ends the blockquote or the list item (`> It may help`,
# `<br>`, `> to restart the pod.`). The check therefore ends the
# paragraph at such a line.
#
# In a list item, a `>` marker left of the text of the item opens a new
# blockquote outside the item, and the paragraph ends there. The check
# ends the paragraph at a line that opens with `|` or `<` after a `>`
# marker left of the marker in the first line of the paragraph (`  >
# It may help`, `> <span>x</span> to restart the pod.` in a `-` item).
# A line of plain text after such a marker still continues the
# paragraph, so the guards read its words (`  > It may help`, `> to
# restart the pod.` in a `-` item).
#
# As in CommonMark, a heading, an HTML block, a list item, or a
# horizontal rule ends the paragraph only at an indent of fewer than
# four columns. The limit also applies to a line that holds only an HTML
# comment or a `pre` element, which lib/markdown.awk marks in
# `bare_line`. A line with a deeper indent cannot open a block there, so
# it is more text of the paragraph (`It may help`, `    - first`, `to
# restart the pod.`). At any indent, the check still ends the paragraph
# at a line with more `>` markers than the paragraph. The line opens a
# paragraph in the quote. When deeper_quote() rejects the line, that
# paragraph does not draw a finding.
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
# GFM reads a line of the paragraph as the header row of a table when a
# delimiter row with as many cells sits right under it (`It may help`,
# `| a | to |`, `| --- | --- |`). The paragraph then ends above the
# header row, and the first sentence drops the text of that row. The
# check reads each line after the first line of the paragraph this way,
# at any indent (`a | to`, `to restart` above `:--`, `    - first`). A
# delimiter row indented four or more columns is more text of the
# paragraph, so no table opens there. In a list item outside a
# blockquote, a delimiter row must also reach the column where the text
# of the item starts. A row left of that column is more text of the
# paragraph (`  It may help`, `  a | to restart`, `| --- | --- |` in a
# `-` item).
#
# The tests for an underline, a delimiter row, and a line that opens
# with `|` or `<` read the list item that holds the paragraph. They
# place a paragraph in a list item when the paragraph reaches the text
# of an open item. Outside a blockquote, the check reads the open items
# that lib/markdown.awk counts. As in GFM, lib/markdown.awk keeps an
# item open at a line of its paragraph at the margin (`- The pod holds
# the cache and`, `serves the reads.`).
#
# A paragraph indented fewer columns than the text of the item above it
# therefore sits outside the list, as in GFM. A delimiter row or an
# underline at the margin ends that paragraph. A line there that holds
# only one tag continues it (`1. The pod holds the cache.`, a blank
# line, `  It may help`, `  | a | to |`, `| - | - |`).
#
# lib/markdown.awk passes indented code in a list item to the check,
# unless a heading or a table row sits above the code in the item. The
# check therefore skips indented code below a paragraph that holds a
# line indented four or more columns. Only blank lines and lines that
# lib/markdown.awk skips can sit between the paragraph and the code. A
# line there is code when its indent, counted as for the lines of the
# paragraph, reaches four columns (`  It may help`, `      - first`, a
# blank line, `      make build` in a `-` item).
#
# GFM reads such a line as code too, because the check never counts
# more columns than GFM does. The code ends at the first line with a
# smaller indent, and that line opens a paragraph. lib/markdown.awk
# skips a link reference definition within three columns of the margin.
# A line right under such a definition is not code, because GFM reads it
# as more text of the paragraph that the definition opens.
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
# tests. A line of `=` or `-` that `more` marks is more text of the
# paragraph and not an underline (`It may help`, `    ---`). The check
# counts that indent from the text of the paragraph. So it still reads
# a line as an underline when GFM counts four columns from the edge of
# the block and the check counts fewer (`   It runs on the node.`,
# `     ---`). It then misses the paragraph, which GFM keeps.
#
# False Positives names the guard for a paragraph under a heading. The
# guard records the `>` markers of the heading. For a heading in a list
# item, it also records the markers before the list marker and the text
# column of the item. A paragraph passes only when holds() finds those
# markers and that column on its first line. The check
# knows the column of an item that `item_depth` counts, or of an item
# whose heading follows its list marker. It therefore passes a
# paragraph after a list in a blockquote that ends in a heading (`> -
# The pod holds the cache.`, `>`, `>   ## Setup`, `>`, `> It runs on
# the node.`).
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
# ends the list there. It keeps the list open at a rule that reaches
# the text of the item. The check therefore counts a rule as part of a
# list only when a list is open above the rule and the rule is
# indented. A rule outside a list closes the list for the lines after
# it, until a list item opens one. lib/markdown.awk reads rule marks
# with an HTML comment after them as a list item, so it can keep the
# list open there (`* * * <!-- note -->`).
#
# Code under `- - -` or `  - - -` outside a list therefore passes, and
# so does code under a `***` that ends a list. Code under a later
# indented rule passes too (`* * *`, `The pod holds the cache.`,
# `  ***`, `    This is some code.`). A line of `-` marks under an
# indented paragraph after such a rule stays an underline.
#
# The check skips the lines after a heading, an HTML block, or a list
# item, up to the next blank line. It also skips the lines after a line
# that opens with `|` or `<` at the start of a paragraph. Under a
# paragraph, it skips the rows of a table. It skips a rule among those
# lines too, so a line of `---` under a table does not read as an
# underline. It therefore misses a paragraph right after such a rule,
# which GFM reads as a paragraph (`## Setup`, `***`, `It runs on the
# node.`).
#
# Among those lines, a line with more `>` markers than the block that
# the check skips opens a paragraph, as deeper_quote() decides. Its text
# must also sit fewer than four columns past the edge after its
# markers. Further right, GFM reads the text as indented code in the
# quote (`- > The pod holds the cache.`, `>     It runs on the node.`).
#
# No paragraph opens in an HTML block, `>` lines included. A block of
# type 6 or 7 runs to the next blank line. A block of types 1 to 5 runs
# to the first line that holds its end token, such as `</pre>`, `-->`,
# or `?>`. That can be the line that opens the block
# (`<script>x</script>`). The check also misses a paragraph right under
# such a line, because it skips the lines up to the blank line (`<pre>`,
# `x`, `</pre>`, `It runs on the node.`).
#
# A line of only `>` markers is text of the HTML block too, unless it is
# a blank line of the blockquote that holds the block (`<div>`, `>`, `>
# It runs on the node.`). Such a blank line ends the block, as a blank
# line outside a blockquote does (`> <div>`, `>`, `> >`, `> > It runs
# on the node.`). The block also holds a line right under a line that
# lib/markdown.awk skips, such as quoted indented code (`<div>`,
# `>     x`, `> It runs on the node.`).
#
# In a blockquote, a line with fewer `>` markers than the HTML block
# ends the quote and the block (`> <div>`, `---`, `> It runs on the
# node.`). So does such a line that lib/markdown.awk skips (`> <div>`,
# `[a]: x`, `> It runs on the node.`). So does such a line right under
# a block of types 1 to 5 that has ended (`> <script>x</script>`, `It
# runs on the node.`). The line opens a paragraph, unless GFM reads it
# as indented code (`> <div>`, `It runs on the node.`).
#
# A line that holds only one tag opens an HTML block among those lines,
# unless GFM reads the tag as more text of a paragraph that the check
# skips. `skip_text` marks such a paragraph, which opens with `|` or `<`
# (`<span>x</span> y`, `<br>`) or holds the text of a list item (`- The
# pod holds the cache.`, `  <br>`). The tag must stay in the block that
# holds the paragraph, as skip_holds() decides. A tag left of the text
# of the item, or with fewer `>` markers, opens an HTML block (`- The
# pod holds the cache.`, `<br>`). So does a tag under a delimiter row
# that turns the paragraph into a table, or under a rule or an
# underline (`| a | b |`, `| - | - |`, `<br>`).
#
# skip_underline() tests an underline of such a paragraph as for a
# paragraph that the check reads. The underline turns the paragraph
# into a heading, which the guard for a paragraph under a heading reads.
# A lone `-` there is an underline too (`| a |`, `-`). A line of `=`
# that fails the test is more text of the paragraph.
#
# A tag four or more columns past the edge of its block is indented
# code in GFM, and indented code never opens an HTML block.
# lib/markdown.awk passes such a line to the check right under a
# heading, a table, or a fence. With no paragraph open, the line does
# not open an HTML block for the check (`## Setup`, `    <div>`). The
# check counts from the text of the innermost item that `item_depth`
# counts, or else from the margin.
#
# In a blockquote, and in a list while `list_seen` holds, such a tag
# still opens an HTML block for the check. So does a tag right under a
# link reference definition, which GFM reads as more text of the
# paragraph that the definition opens (`[a]: x`, `<br>`). The HTML
# block then holds the lines up to the next blank line. So the check
# misses a paragraph under a line that lib/markdown.awk skips or that
# holds only a comment (`[a]: x`, `<br>`, a fence, `> It runs on the
# node.`). Other examples are `> | a |`, `> | - |`, `>     <div>`, `>
# <!-- c -->`, `> It runs on the node.` and `- Install the agent.`, a
# blank line, `      <div>`, `  <!-- c -->`, `  It runs on the node.`.
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
# pronoun points at the heading's topic, which the reader can see. A
# setext heading counts too, and its underline is the line above the
# paragraph (`Setup`, `-----`, a blank line, `It runs on the node.`).
# So does a heading in a list item, also one right after the list marker
# (`- ## Setup`). So does a setext heading of a paragraph whose lines the
# check skips, such as one that opens with inline HTML (`<b>Setup</b>
# steps`, `=====`).
#
# In a blockquote, a line of only `>` markers between the heading and
# the paragraph counts as a blank line (`> ## Setup`, `>`, `> This is
# the setup step.`). A blockquote under the heading sits in its section,
# so a paragraph in a deeper quote passes too (`## Setup`, `> It runs on
# the node.`).
#
# The paragraph must sit in the block that holds the heading. A
# paragraph with fewer `>` markers than the heading follows the
# blockquote that holds the heading, so it can draw a finding (`> ##
# Setup`, a blank line, `It runs on the node.`). A line of only
# whitespace ends that blockquote too, so a paragraph in a new quote
# under it can draw a finding (`> ## Setup`, a blank line, `> It runs on
# the node.`). Under a heading outside a blockquote, a line of only `>`
# markers opens an empty quote. A paragraph that follows that quote can
# draw a finding (`## Setup`, `>`, a blank line, `> It runs on the
# node.`). A paragraph left of the text of the list item that holds the
# heading follows the list (`- ## Setup`, a blank line, `It runs on the
# node.`).
#
# lib/markdown.awk passes indented code right under a line of only `>`
# markers, and the check reads that code as a paragraph. Under a
# heading, the line of `>` opens an empty quote, so the guard does not
# pass the code, and the check reports it (`## Setup`, `>`, `    It
# runs on the node.`).
#
# lib/markdown.awk also passes indented code right under a line that
# holds only an HTML comment, and right under a closing fence. The
# check reads that code as a paragraph too, so it can report the code
# (`<!-- c -->`, `    It runs on the node.`).
#
# The first paragraph of a file passes when front matter or any other
# line sits above it, because there the pronoun points at the page.
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
# lib/markdown.awk can pass indented code in a list item under such a
# paragraph to the check. The check reads that code as a paragraph when
# it sits fewer than four columns past the text of the paragraph
# (`     It may help to restart the pod`, `         - first`, a blank
# line, `      It runs on the node.` in a `-` item).
#
# A `>` marker can sit up to three columns past the edge of its block.
# So a line can still continue the blockquote when its marker sits left
# of the marker with the same index in the first line of the paragraph.
# The check ends the paragraph at such a line when the line opens with a
# list marker or a block (`   > It may help` above `>     - first`, or
# `> > It may help` above `>>     - first`). In a list item, it also
# ends the paragraph when such a line opens with `|` or `<` (`   > It
# may help` above `  > <br>` or `  > | a | to |` in a `-` item).
#
# GFM also reads the first line of a paragraph as the header row of a
# table when a delimiter row sits right under it (`It runs | on the
# node.` above `--- | ---`). No paragraph opens there. The check still
# reads the line as the first line of a paragraph, so it can report the
# header row.
#
# cmark-gfm does not open a table in the rest of a paragraph after a
# delimiter row whose cell count differs from the line above it. The
# check still reads a later line of the paragraph as the header row of
# a table (`It may help`, `| a | b |`, `| --- |`, `| to |`, `| --- |`).
# The guards then miss the words of that line.
#
# The check measures the indent of a delimiter row as for the lines of
# the paragraph. It can therefore read a row as the delimiter row of a
# table where GFM reads the row as more text of the paragraph, such as
# a row indented four columns under ` It may help`. The guards then miss
# the words of the header row.
#
# In a list item, the text of a paragraph can start up to three columns
# past the text of the item. GFM reads an underline or a line that
# holds only one tag as part of the item when the line reaches the text
# of the item. The check needs the line to reach the text of the
# paragraph.
#
# Left of that column, an underline does not turn the paragraph into a
# heading, so the check reports the paragraph (`   It may help.`, `  ---`
# in a `-` item). A line there that holds only one tag ends the
# paragraph, so the guards miss the words after it (`   It may help`,
# `  <br>`, `   to restart the pod.`).
#
# lib/markdown.awk does not count the items of a list in a blockquote.
# There the test for a delimiter row needs the row to reach the text of
# the paragraph. Left of that column, it reads a row at the text of the
# item as more text of the paragraph. The guards then read the words of
# the table (`> - The pod holds the cache.`, `>`, `>    It may help`,
# `>    a | to restart`, `>   | --- | --- |`).
#
# lib/markdown.awk closes an open code span at a line that opens with
# `|` or `<`. GFM reads such a line as more text of the paragraph, so
# the span can close on it (``It may help with `x``, ``| y` to restart
# the pod.``). lib/markdown.awk reads the backtick that closes the span
# as one that opens a span, and it blanks the words after it. The
# guards then miss those words.
#
# lib/markdown.awk skips indented code in a list item only after a
# heading or a table row in the item. It passes other indented code in
# a list item to the check. The check skips that code only below a
# paragraph that holds a line indented four or more columns. Below
# another paragraph of a list item, it can report the code (`- The pod
# holds the cache.`, a blank line, `      It runs on the node.`).
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
# some code.`).
#
# A rule in a blockquote counts as indented for that test, because the
# check blanks its `>` markers. lib/markdown.awk keeps a list in a
# blockquote open at a rule one to three columns past the `>`. So it
# passes the quoted code under that rule to the check. GFM ends the
# item at such a rule left of its text and reads the line under it as
# code. The check can report that line (`> - The pod holds the cache.`,
# `>`, `>   The pod holds the cache.`, `>  ***`, `>     This is some
# code.`).
#
# Among the lines that the check skips after a block, a line with fewer
# `>` markers than the block sets the depth that a later blockquote
# must pass. GFM can read such a line as more text of a paragraph in
# the block, such as a line that looks like a list item but sits four
# columns in. The check then reports a quoted line that GFM reads as
# more text of that paragraph too (`> - The pod holds the cache.`,
# `    - first`, `  > It runs on the node.`).
#
# Expression names a blockquote in a quoted list item that the check
# misses. A later line of that blockquote with its marker within three
# columns of the edge after `> ` opens a paragraph for the check. GFM
# reads that line as more text of the paragraph in the missed quote
# (`> - The pod holds the cache.`, `>      > The pod holds the cache.`,
# `>   > It runs on the node.`).
#
# GFM does not end an HTML block of types 1 to 5 at a blank line.
# lib/markdown.awk passes the lines after a blank line in such a block
# to the check, and the check ends the block at the blank line. It
# therefore reports a paragraph that GFM reads as text of the block
# (`<pre>`, a blank line, `It runs on the node.`, `</pre>`).
#
# GFM reads a link reference definition right under a line of paragraph
# text as more text of the paragraph, also on a line with fewer `>`
# markers. lib/markdown.awk skips the definition, so the check opens a
# paragraph at the line under it. That line can draw a finding (`> > The
# pod holds the cache.`, `[a]: x`, `> > It runs on the node.`). A later
# line with the markers of the first paragraph then opens a deeper
# blockquote for the check. GFM reads that line as more text of the
# first paragraph (`> > The pod holds the cache.`, `> [a]: x`, `> The
# pod holds the cache.`, `> > It runs on the node.`).
#
# Under a definition that opens a blockquote, the check reads a lazy
# line of text as a paragraph outside the quote. A quoted line under it
# then opens a deeper blockquote for the check, but GFM reads that line
# as more text of the paragraph (`- > The pod holds the cache.`, `> [a]:
# x`, `The pod holds the cache.`, `> It runs on the node.`). A line that
# holds only one tag continues that paragraph for the check. GFM opens
# an HTML block at the tag instead, and a quoted line under the tag is
# text of the block (`> [a]: x`, `<span>x</span> y`, `<br>`, `> > It
# runs on the node.`).
#
# A quoted line left of the text of a list item ends the item in GFM,
# and it opens a new blockquote. The check reads such a line as more
# text of a quoted paragraph in the item when the line holds as many
# `>` markers. It then reads an underline under the line as a rule, so
# it reports a paragraph in a deeper quote under the rule. GFM places
# that paragraph right under a heading (`- The pod holds the cache.`,
# `  > The pod holds the cache.`, `> Setup`, `> ---`, `> > It runs on
# the node.`).
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
# count. `last_blank` names the last line of only whitespace, which
# lib/markdown.awk never passes to prose().
#
# `heading_gap` holds the number of `>` markers of the last line after a
# heading that is blank or holds only `>` markers. heading_at() starts
# it at the markers of the heading. A line with fewer markers than the
# line before it ends a blockquote, and `heading_broken` records that.
# Take `## Setup` and `>`. A line of only whitespace under them ends the
# empty quote that `>` opens. A paragraph in a quote after that line
# therefore does not sit right under the heading (`## Setup`, `>`, a
# blank line, `> It runs on the node.`). A line with a `>` marker left
# of the text of the list item that holds the heading ends the item, so
# `heading_broken` records that too (`- > ## Setup`, `>`, `  > It runs
# on the node.`).
#
# `def_line` names the last
# line that opens like a link reference definition (`[guide]:
# https://example.com`). lib/markdown.awk skips such a line only within
# three columns of the margin, and it passes every other one to
# prose().
#
# The block also ends the HTML block that `skip_html` marks at a line
# with fewer `>` markers than the block, as prose() explains. After a
# list item, a line left of the text of the item ends the block too.
# lib/markdown.awk skips some such lines (`> <div>`, `[a]: x`), so
# prose() does not see them. `html_end` names that line.
#
# A block of types 1 to 5 ends at the first line that holds the pattern
# in `skip_end`, which html_end_of() in lib/markdown.awk returns. The
# block above `@include` tests each line, also a line that
# lib/markdown.awk skips. `close_line` names that line, which still
# sits in the block, and the block ends for the line after it. A line
# with fewer `>` markers right under it ends the blockquote that held
# the block, so the test for such a line runs first.
{
  if (FNR == 1) {
    prev_nb = 0
    cur_nb = 0
    heading_line = 0
    heading_outer = 0
    heading_reach = 0
    heading_gap = 0
    heading_broken = 0
    last_blank = 0
    def_line = 0
    head_line = 0
    html_end = 0
    close_line = 0
  }
  if ($0 !~ /^[ \t>]*$/) {
    prev_nb = cur_nb
    cur_nb = FNR
  }
  if ($0 ~ /^[ \t]*$/) {
    last_blank = FNR
  }
  if ($0 ~ /^[ \t>]*$/) {
    heading_broken = heading_broken || quote_depth() < heading_gap || (quote_depth() > heading_outer && !holds(heading_outer, heading_reach))
    heading_gap = quote_depth()
  }
  if (skip_html && (quote_depth() < skip_quote || (skip_item && $0 ~ /[^ \t]/ && !skip_holds()))) {
    skip_html = 0
    skip_quote = quote_depth()
    skip_inner = 0
    html_end = FNR
  }
  if (close_line && close_line < FNR) {
    skip_html = 0
    close_line = 0
  }
  if (skip_html && skip_end != "" && tolower(blank($0, "^([ \t]*>)+")) ~ skip_end) {
    close_line = FNR
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
  list_depth = 0
  skip_quote = 0
  skip_inner = 0
  skip_col = 0
  skip_html = 0
  skip_end = ""
  skip_item = 0
  skip_reach = 0
  skip_text = 0
  skip_cells = 0
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

# Returns the column after the `>` markers of the line and the one space
# after them. A tab after the markers gives one column to that space and
# the rest to the indent, as in CommonMark.
function quote_edge(   n, base) {
  match($0, /^([ \t]*>)*/)
  n = RLENGTH
  base = cols(n)
  if (n && substr($0, n + 1, 1) ~ /[ \t]/) {
    base++
  }
  return base
}

# Returns the column where the text of the line starts.
function text_col() {
  match($0, /^([ \t]*>)*[ \t]*/)
  return cols(RLENGTH)
}

# Returns the indent of the line in columns. The count starts at
# quote_edge(). On a line with as many markers as the paragraph, the
# count never starts left of `para_text`. On a line with `d` markers,
# fewer than the paragraph has, it never starts left of `para_mark[d]`.
function indent(   base, d, edge) {
  base = quote_edge()
  d = quote_depth()
  edge = d < para_quote ? para_mark[d] : para_text
  if (edge > base) {
    base = edge
  }
  return text_col() - base
}

# Returns the number of columns between quote_edge() and the text of the
# line, without the limits that indent() applies.
function inset() {
  return text_col() - quote_edge()
}

# Records a heading on the line for the guard in prose(). `q` counts the
# `>` markers of the heading. For a heading in a list item, `outer`
# counts the markers before the list marker. `reach` then holds the
# distance from the edge after those markers to the text of the item.
# Outside a list, `outer` is `q` and `reach` is 0.
function heading_at(q, outer, reach) {
  heading_line = FNR
  heading_outer = outer
  heading_reach = reach
  heading_gap = q
  heading_broken = 0
}

# Returns the text column of the innermost list item that `item_depth`
# counts, or 0. The check does not track a list item in a blockquote,
# so it returns 0 for a quoted line.
function item_reach() {
  return quote_depth() ? 0 : item_depth ? item_cols[item_depth] : 0
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

# Returns 1 when the line opens a blockquote inside the block above it,
# which sits after `q` markers. The line must hold more than `q`
# markers, and GFM must read each of them as a marker. So each marker
# must sit fewer than four columns past the edge that the marker before
# it leaves, after that marker and the space after it. Further right,
# GFM reads the rest of the line as more text or as indented code. The
# first marker counts from the text of the innermost list item that the
# line reaches, which `item_match` in lib/markdown.awk counts, or from
# the margin. A line of the item's paragraph left of its text keeps the
# item open, but its first marker counts from the edge left of the
# item, as in GFM (`10.  The pod holds the cache and`, `    > It runs on
# the node.`).
function deeper_quote(q,   n, i, k, edge) {
  match($0, /^([ \t]*>)*/)
  n = RLENGTH
  edge = item_match ? item_cols[item_match] : 0
  k = 0
  for (i = 1; i <= n; i++) {
    if (substr($0, i, 1) != ">") {
      continue
    }
    if (cols(i - 1) - edge >= 4) {
      return 0
    }
    k++
    edge = cols(i) + (substr($0, i + 1, 1) ~ /[ \t]/)
  }
  return k > q
}

# Returns the number of `>` markers after the list marker that opens the
# line, as the list-item branch of lib/markdown.awk counts them. Each
# one opens a blockquote in the item (`- > text`, `- - > text`, `> - >
# text`). The function sets `item_col` to the column of the first of
# those markers, with tabs expanded, or to 0 when the line holds none.
# It sets `item_line` to 1 when the line opens with a list marker.
# `item_rest` then holds the text after the list markers and the `>`
# markers of the item, and `item_text` holds the column where that text
# starts.
function item_quotes(   s, n, c) {
  s = blank(expand_tabs($0), "^([ \t]*>)+")
  n = 0
  item_col = 0
  item_line = 0
  item_rest = ""
  item_text = 0
  if (!match(s, /^[ \t]*([-*+]|[0-9]{1,9}[.)])([ \t]+|$)/)) {
    return 0
  }
  item_line = 1
  c = RLENGTH
  s = substr(s, RLENGTH + 1)
  while (match(s, /^ {0,3}(>[ \t]?|([-*+]|[0-9]{1,9}[.)])([ \t]+|$))/)) {
    if (index(substr(s, 1, RLENGTH), ">")) {
      if (!n) {
        item_col = c + index(s, ">") - 1
      }
      n++
    }
    c += RLENGTH
    s = substr(s, RLENGTH + 1)
  }
  item_rest = s
  item_text = c + (match(s, /[^ \t]/) ? RSTART - 1 : 0)
  return n
}

# Returns the number of `>` markers of the block that the check skips,
# as the line sees them. `skip_quote` counts the markers that open the
# block. A list item adds the `skip_inner` markers after its list marker
# (`- > The pod holds the cache.`). The line stays in the quotes of the
# item when its marker after the first `skip_quote` markers sits at
# least `skip_col` columns past the edge that the marker before it
# leaves. `skip_col` holds that distance for the first marker after the
# list marker, on the line of the item (`  > It runs on the node.`). A
# marker further left sits left of the text of the item. It ends the
# item and the quotes in it, and it opens a new blockquote (`> It runs
# on the node.`).
function skip_depth(   n, i, k, edge) {
  if (!skip_inner) {
    return skip_quote
  }
  match($0, /^([ \t]*>)*/)
  n = RLENGTH
  k = 0
  edge = 0
  for (i = 1; i <= n; i++) {
    if (substr($0, i, 1) != ">") {
      continue
    }
    if (++k > skip_quote) {
      return skip_quote + (cols(i - 1) - edge >= skip_col ? skip_inner : 0)
    }
    edge = cols(i) + (substr($0, i + 1, 1) ~ /[ \t]/)
  }
  return skip_quote
}

# Returns 1 when the line holds at least `q` `>` markers and reaches
# `reach` columns past the edge after the first `q` of them. The
# function measures the first character after those markers and the
# whitespace after them, which can be a `>` marker too.
function holds(q, reach,   n, i, k, edge) {
  match($0, /^([ \t]*>)*/)
  n = RLENGTH
  k = 0
  edge = 0
  for (i = 1; i <= n && k < q; i++) {
    if (substr($0, i, 1) == ">") {
      k++
      edge = cols(i) + (substr($0, i + 1, 1) ~ /[ \t]/)
    }
  }
  if (k < q) {
    return 0
  }
  match(substr($0, i), /^[ \t]*/)
  return cols(i - 1 + RLENGTH) - edge >= reach
}

# Returns 1 when the line stays in the innermost block that holds the
# block the check skips. After a list item that opens a blockquote after
# its list marker, the line must hold the markers of that quote, as
# skip_depth() reads them (`- > <div>`, `  > > It runs on the node.`).
# Otherwise holds() tests the first `skip_quote` markers and
# `skip_reach`. After a list item, `skip_reach` holds the distance to
# the text of the item (`- <div>`, `  > It runs on the node.`).
function skip_holds() {
  if (skip_inner) {
    return quote_depth() >= skip_quote + skip_inner && skip_depth() > skip_quote
  }
  return holds(skip_quote, skip_reach)
}

# Returns 1 when `line` is a setext underline of the paragraph that
# `skip_text` marks (`<b>Setup</b> steps`, `=====`). The line holds only
# `=` or only `-` marks and as many `>` markers as the paragraph, and
# skip_holds() keeps it in the block of the paragraph. Its marks must
# sit fewer than four columns past `skip_reach`, or past the edge after
# the quotes of a list item that `skip_inner` counts. Further right, GFM
# reads the line as more text of the paragraph (`<b>Setup</b> steps`,
# `    ---`).
function skip_underline(line) {
  return skip_text && line ~ /^[ \t]*(=+|-+)[ \t]*$/ && quote_depth() == skip_quote + skip_inner && skip_holds() && inset() - (skip_inner ? 0 : skip_reach) < 4
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
# sit at the quote depth of the paragraph. When `para_list` marks the
# paragraph as part of a list item, the underline must also reach the
# column where the paragraph's text starts, as underline() in
# lib/sentences.awk requires. `para_item` counts the characters before
# that column, and `para_inset` holds the inset() of the line.
#
# A delimiter row must reach the column where the text of the item
# starts, which `para_reach` holds. Outside a blockquote, that is the
# text column of the innermost item that `item_depth` counts. In a
# blockquote, `para_reach` holds `para_text`.
#
# `item_depth` in lib/markdown.awk counts the open list items outside a
# blockquote. It drops each item that the line, or its first `>`
# marker, sits left of, unless the line is more text of the paragraph
# of the item. It keeps an item that holds a heading or a table row. So
# `para_list` holds when `item_depth` counts an item (`- The pod holds
# the cache.`, `  | a |`, a blank line, `  It may help`). A paragraph
# indented fewer columns than the text of the item above it sits
# outside the list (`1. The pod holds the cache.`, a blank line, `  It
# may help`).
#
# lib/markdown.awk does not count the items of a list in a blockquote.
# In such a list, `para_list` follows `item`, which follows `in_list`
# from lib/markdown.awk.
#
# For every paragraph, `para_text` holds the column where its text
# starts. `para_mark[k]` holds the column of the `>` marker after the
# first `k` markers of the line. The text of a paragraph never starts
# left of the block that holds it. A marker never sits left of the
# block that holds its quote either. So a count from these columns
# never exceeds the indent that GFM counts.
function open_para(item,   n, i, k) {
  in_para = 1
  skip_text = 0
  para_more = 0
  para_quote = quote_depth()
  match($0, /^([ \t]*>)*[ \t]*/)
  n = RLENGTH
  para_inset = inset()
  para_text = cols(n)
  para_list = item_depth > 0 || (item && para_quote)
  para_item = para_list ? n : 0
  para_reach = para_quote ? para_text : item_cols[item_depth]
  k = 0
  for (i = 1; i <= n; i++) {
    if (substr($0, i, 1) == ">") {
      para_mark[k++] = cols(i - 1)
    }
  }
}

function prose(text,   start, quote_open, rest, t1, t2, t3, w1, shown, line, after_rule, list_before, more, html, block, inline) {
  start = (FILENAME != last_file || FNR != last_line + 1 || gap)
  # A blockquote ends a paragraph, a table, or a list item, and it can
  # sit right under a heading. A line that opens one also opens a
  # paragraph in the quote (`The pod holds the cache.`, `> It runs on
  # the node.`). `quote_open` marks such a line right under a paragraph,
  # or right under a block whose lines the check skips. `start` holds
  # for that line too, so the line takes the tests of the first line of
  # a paragraph. inset() must be less than four. Further right, GFM
  # reads the text as indented code in the quote (`>     It runs on the
  # node.`).
  #
  # `skip_quote` holds the number of `>` markers of the block that the
  # check skips. `skip_html` marks an HTML block, which holds its lines
  # up to the next blank line, `>` lines included, or up to its end
  # token. An HTML block does not take a line with fewer `>` markers as
  # more of its text. Such a line ends the blockquote that holds the
  # block, and the block with it (`> <div>`, `---`). A later blockquote
  # counts from the markers of that line. The block above `@include`
  # makes this test and sets `html_end`, so a line that lib/markdown.awk
  # skips ends the HTML block too (`> <div>`, `[a]: x`, `> It runs on
  # the node.`).
  #
  # The line at `html_end` opens a block of its own, so `start` holds
  # for it (`> <div>`, `It runs on the node.`). So does a line left of
  # the text of a list item that opens an HTML block (`- <details>`, `It
  # runs on the node.`). GFM reads the line as indented code when a `>`
  # marker of the line sits four or more columns past the edge before
  # it. The same holds when its text sits that far past the edge, and
  # `start` then stays off (`> > <div>`, `    > x`). The first marker
  # and the text count from the text of the innermost list item that the
  # line reaches, as in deeper_quote(). So on a line left of the text of
  # the item, they count from the margin or from the text of an outer
  # item.
  #
  # After a list item, skip_depth() also counts the `>` markers after
  # the list marker, which `skip_inner` holds. So `  > It runs on the
  # node.` under `- > The pod holds the cache.` continues that quote,
  # but `> It runs on the node.` opens a new one.
  #
  # lib/markdown.awk skips some lines of an HTML block, such as quoted
  # indented code (`<div>`, `>     x`). The line after one still sits in
  # the block when no blank line sits between them, so `start` turns
  # off for it.
  if (FILENAME != last_file || last_blank > last_line) {
    skip_html = 0
  }
  if (html_end == FNR && deeper_quote(-1) && text_col() - (quote_depth() ? quote_edge() : (item_match ? item_cols[item_match] : 0)) < 4) {
    start = 1
  }
  if (start && skip_html && !in_para && !gap) {
    start = 0
  }
  quote_open = !start && inset() < 4 && (in_para ? deeper_quote(para_quote) : !skip_html && deeper_quote(skip_depth()))
  start = start || quote_open
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
  # A line indented four or more columns cannot open a heading, an HTML
  # block, a list item, or a rule. `more` therefore marks it as more
  # text of the open paragraph (`    - first`). A line with more
  # `>` markers than the paragraph still ends it, at any indent. So does
  # a line with a marker left of the paragraph's marker, which can open
  # a new blockquote.
  more = !start && in_para && quote_depth() <= para_quote && marks_hold() && indent() >= 4
  # `para_more` marks a paragraph that holds a line that `more` marks.
  if (more) {
    para_more = 1
  }
  # `start` and `in_para` together mark a line below the paragraph, or
  # below code under it. Only blank lines and lines that
  # lib/markdown.awk skips sit between the two. lib/markdown.awk passes
  # the indented code of a list item to the check, so the check skips
  # that code itself.
  #
  # Below a paragraph that `para_more` marks, such a line with an
  # indent() of four or more columns is code. GFM counts at least as
  # many columns there. Take a link reference definition that
  # lib/markdown.awk skips. The line right under it is more text of
  # the paragraph that the definition opens, so it is not code. A line
  # that only looks like a definition, such as a line of the code,
  # reaches prose() and sets `last_line`. The line under it can still be
  # code.
  #
  # The paragraph stays open across the code, and each later line of the
  # code skips too. The first line with a smaller indent opens a
  # paragraph.
  if (start && in_para && para_more && FILENAME == last_file && (def_line != FNR - 1 || last_line == FNR - 1) && indent() >= 4) {
    gap = 1
    last_line = FNR
    return
  }
  # A blockquote that ends a list item also ends the list above it. So
  # does the line at `html_end`, which ends an HTML block of the item and
  # the item with it. `list_depth` holds the `item_depth` of the last
  # line that reached prose(), so `item_depth` drops from that count to 0
  # at such a line. `list_before` then turns off.
  #
  # The test for a rule reads `list_before`. The check blanks an HTML
  # comment before that test, so it reads rule marks with a comment after
  # them as a rule, where lib/markdown.awk reads text. That rule sits
  # outside the list, so the check skips the indented line under it,
  # which GFM reads as more text of the quote (`- The pod holds the
  # cache.`, `> *** <!-- c -->`, `    It runs on the node.`).
  #
  # The blockquote can open a list of its own (`- The pod holds the
  # cache.`, `> - The pod runs.`). lib/markdown.awk keeps `in_list` on
  # for that list, and its list item turns `list_seen` on below. A
  # paragraph of that list then sits in a list. So a `---` under it left
  # of the text of the item is a rule and not an underline, as in GFM
  # (`>`, `>   It runs on the node.`, `> ---`).
  if (list_depth && !item_depth && (quote_depth() || html_end == FNR)) {
    list_before = 0
  }
  list_depth = item_depth
  # `list_seen` follows `in_list`, but a rule outside a list, and the
  # code under such a rule, close the list for the lines after them.
  # lib/markdown.awk can keep `in_list` on there. Only a list item opens
  # the list again. `- - -` and `* * *` count as rules and not as list
  # items, so neither opens it. A line of the paragraph that looks like
  # a list item does not open the list either. open_para() reads
  # `list_seen` as `item`.
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
  #
  # In an HTML block, a bare line is text of the block, unless it is a
  # blank line of the blockquote that holds the block. So `>` under
  # `<div>` does not end the block, but `>` under `> <div>` does. After
  # a list item, the block sits in the quotes that `skip_inner` counts
  # (`  >` under `- > <div>`). Every other bare line therefore clears
  # `skip_html`. A later line with more
  # `>` markers then opens a blockquote (`> <div>`, `>`, `> >`, `> > It
  # runs on the node.`). A later line with fewer markers continues a
  # paragraph in the quote and does not end an HTML block (`> <div>`,
  # `>`, `> The pod holds the cache.`, `It runs on the node.`).
  if (bare_line && !in_para && skip_html && (quote_depth() > skip_quote + (skip_item ? skip_inner : 0) || raw ~ /[^ \t]/)) {
    gap = 0
    return
  }
  if (bare_line && !(more && raw ~ /[^ \t]/)) {
    finish()
    in_para = 0
    gap = 1
    skip_html = 0
    return
  }
  gap = 0
  # A setext underline makes the paragraph a heading. An empty list item
  # cannot interrupt a paragraph, so a lone `-` there is an underline.
  # This test therefore runs before the test for a list item. A line of
  # three or more `-` marks that fails this test is a horizontal rule. A
  # line that `more` marks is more text of the paragraph, so it is not an
  # underline (`    ---`).
  if (!start && !more && in_para && quote_depth() == para_quote && line ~ /^[ \t]*(=+|-+)[ \t]*$/ && match(line, /[^ \t]/) > para_item) {
    pending = 0
    in_para = 0
    heading_at(para_quote, para_quote, item_reach())
    skip_quote = para_quote
    skip_inner = 0
    skip_html = 0
    return
  }
  # A horizontal rule ends the paragraph before it and does not open
  # one, so the next line opens a paragraph. The test runs before the
  # test for a list item, because `- - -` is a rule and not a list item.
  # A rule among the lines that the check skips after a heading, an HTML
  # block, a table, or a list item falls through. The check skips it
  # with those lines.
  if (line ~ hrule && !more && (start || in_para)) {
    finish()
    in_para = 0
    gap = 1
    rule_above = 1
    # A rule with no indent cannot sit in a list item, and
    # lib/markdown.awk ends the list there. It keeps `in_list` for a rule
    # that reaches the text of the item. `rule_list`
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
  # a blockquote and a list, lib/markdown.awk skips indented code only
  # after a blank line or a thematic break that it reads. In a list
  # item, it skips the code only after a heading or a table row in the
  # item. It does not read a break at rule marks that sit in indented
  # code or that hold an HTML comment. It can also keep the list open
  # where `list_seen` is off. This test therefore skips the code after a
  # rule.
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
  # of that row. `head_line` names the latest line that continued the
  # paragraph, and `head_first` holds the first sentence from before
  # that line. A delimiter row that `more` marks is more text of
  # the paragraph, and one that opens a list item opens the list item.
  # In a list item, a row left of `para_reach`, the text column of the
  # item, is more text of the paragraph too.
  if (!start && head_line == FNR - 1 && !more && quote_depth() == para_quote && (!para_list || text_col() >= para_reach) && raw ~ delimiter && text !~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/ && cells(raw) == head_cells) {
    first = head_first
    finish()
    in_para = 0
    skip_quote = para_quote
    skip_inner = 0
    skip_html = 0
    return
  }
  # A setext underline also turns a paragraph that `skip_text` marks
  # into a heading (`<b>Setup</b> steps`, `=====`). A lone `-` there is
  # an underline and not an empty list item.
  if (!start && !in_para && skip_underline(line)) {
    skip_text = 0
    heading_at(quote_depth(), skip_quote, skip_inner ? skip_col : skip_reach)
    return
  }
  # A heading opens with one to six `#` marks. A line with more marks is
  # text, as in CommonMark, so it opens or continues a paragraph.
  if (!more && text ~ /^[ \t]*#{1,6}([ \t]|$)/) {
    heading_at(quote_depth(), quote_depth(), item_reach())
  }
  # Headings, HTML blocks, and list items are not paragraphs, so each one
  # ends the paragraph before it. The lines that continue a list item
  # are not paragraphs either. `block` marks those lines and every other
  # line that opens with `|` or `<`. html_block() in lib/markdown.awk
  # reads `raw`, because lib/markdown.awk blanks an HTML comment or a
  # `pre` element that closes on its line (`<!-- note --> to restart
  # the pod.`). `html_lone` in lib/markdown.awk matches a line that holds
  # only one tag.
  #
  # A line with fewer `>` markers than the paragraph, or a line left of
  # the text of a list item, does not reach the paragraph in GFM. When
  # such a line holds only one tag, it opens an HTML block of type 7.
  # That block ends the blockquote or the list item, and the paragraph
  # with it (`> It may help`, `<br>`). Such a line also opens an HTML
  # block where no paragraph is open. That holds at the start of a
  # block, and under a block whose lines the check skips, such as a
  # table (`| a |`, `| - |`, `<br>`). `skip_text` marks a paragraph
  # that the check skips. Under it, a line that skip_holds() keeps in the
  # block of the paragraph is more text of the paragraph
  # (`<span>x</span> y`, `<br>`).
  #
  # In a list item, the check compares the inset() of the line with that
  # of the first line of the paragraph. It therefore measures from the
  # text of the paragraph and not from the text of the item, as the
  # tests for an underline and a delimiter row do. The inset() starts
  # after the `>` markers, so a quoted line with no space after its
  # last marker still reaches the text (`  ></span>` under `  > It may
  # help`).
  #
  # `inline` marks a line that opens with `|` or `<` and does not open
  # an HTML block. In a paragraph, such a line is more text of the
  # paragraph (`| a | b |`, `<span>x</span>`). At the start of a
  # paragraph, the check skips the line. There it opens a table, an HTML
  # block, or a paragraph that does not open with a pronoun. A line that
  # `more` marks looks like a block but continues the paragraph.
  #
  # In a list item, a `>` marker left of the marker in the first line of
  # the paragraph can sit left of the text of the item. There it opens a
  # new blockquote outside the item, and the paragraph ends. `inline`
  # therefore leaves out such a line, so a line that opens with `|` or
  # `<` after the marker ends the paragraph (`  > It may help`, `>
  # <span>x</span> to restart the pod.`).
  html = html_block(raw) || (tolower(raw) ~ html_lone && (start || (in_para ? quote_depth() <= para_quote && (quote_depth() < para_quote || (para_list && inset() < para_inset)) : !(skip_text && skip_holds()))))
  block = html || text ~ /^[ \t]*(#{1,6}([ \t]|$)|[|<])/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/
  inline = !html && text ~ /^[ \t]*[|<]/ && (!para_list || marks_hold())
  # The check skips the lines after such a block up to the next blank
  # line, unless `quote_open` marks one. Inside an HTML block, each
  # later block line keeps `skip_html` and `skip_quote`, because GFM
  # reads the line as text of the HTML block (`> <div>`, `> > ## Setup`).
  #
  # A list item reads the text after its list marker and the `>` markers
  # after it. That text can open an HTML block in the item (`- <div>`),
  # and `skip_item` marks the block of a list item. So does an HTML
  # block on a later line of an item that `item_depth` counts (`- The
  # pod holds the cache.`, `  <div>`). Plain text after the list marker
  # is the first line of a paragraph (`- The pod holds the cache.`), and
  # a heading there sits in the item (`- ## Setup`). `skip_reach` holds
  # the distance from the edge after the first `skip_quote` markers to
  # the text of the item. Any other block holds
  # a paragraph only when the line opens with `|` or `<` at the start of
  # a paragraph. There `skip_reach` holds the distance to the text of the
  # list item that holds the paragraph. The check does not track a list
  # item in a blockquote. There it uses the inset() of the paragraph, as
  # the test for a tag in an open paragraph does. `skip_cells` holds
  # the number of cells of the last line, for a delimiter row under it.
  #
  # GFM reads a tag four or more columns past the text of the item that
  # `item_depth` counts, or past the margin, as indented code when no
  # paragraph is open (`## Setup`, `    <div>`). Such a line therefore
  # does not set `skip_html`. In a blockquote, and in a list while
  # `list_seen` holds, the line still opens an HTML block.
  if (!more && block && (start || !inline)) {
    finish()
    if (start || in_para || !skip_html) {
      skip_quote = quote_depth()
      skip_inner = item_quotes()
      skip_col = item_col - quote_edge()
      skip_item = item_line
      if (item_line) {
        rest = item_rest
        skip_html = html_block(rest) || tolower(rest) ~ html_lone
        skip_text = !skip_html && rest ~ /[^ \t]/ && rest !~ /^[ \t]*(#{1,6}([ \t]|$)|```|~~~)/ && rest !~ hrule
        skip_reach = item_text - quote_edge()
        if (rest ~ /^[ \t]*#{1,6}([ \t]|$)/) {
          heading_at(quote_depth() + skip_inner, skip_quote, skip_inner ? skip_col : skip_reach)
        }
      } else {
        rest = raw
        skip_html = html && ((in_para && !start) || quote_depth() || list_seen || text_col() - item_reach() < 4)
        skip_text = start && inline
        skip_reach = quote_depth() ? (list_seen ? inset() : 0) : item_reach()
        skip_item = html && item_reach() && text_col() >= skip_reach
      }
      skip_end = skip_html ? html_end_of(rest) : ""
      close_line = skip_end != "" && tolower(rest) ~ skip_end ? FNR : 0
      skip_cells = cells(rest)
    }
    in_para = 0
    return
  }
  if (!start) {
    if (!in_para) {
      # A rule, an underline, or a delimiter row with as many cells as the
      # line above it ends the paragraph that `skip_text` marks.
      if (skip_text && (line ~ hrule || (raw ~ delimiter && cells(raw) == skip_cells))) {
        skip_text = 0
      }
      skip_cells = cells(raw)
      return
    }
    if (quote_depth() > para_quote) {
      # A line with more `>` markers than the paragraph that
      # `quote_open` does not mark holds a marker four or more columns
      # past the edge that the marker before it leaves. GFM reads the
      # line as more text of the paragraph or as indented code. The
      # check still ends the paragraph there and opens one in the quote.
      # That paragraph does not draw a finding, but an underline or a
      # rule under it still ends it.
      finish()
      open_para(list_seen)
    } else {
      # Any other line continues the paragraph, so it can hold the rest
      # of the first sentence. The line can still turn out to be the
      # header row of a table, so the check keeps the sentence from
      # before the line. It does so whether or not the paragraph holds a
      # finding. A delimiter row under the line then ends the paragraph,
      # and the rows of the table never reach the test for an underline.
      head_line = FNR
      head_first = first
      head_cells = cells(raw)
      if (pending && !first_done) {
        first = first "\n"
        read_first(text)
      }
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
  # The paragraph must sit in the blockquote of the heading, or in a
  # deeper one. A paragraph with fewer `>` markers follows the
  # blockquote that holds the heading (`> ## Setup`, a blank line, `It
  # runs on the node.`). Each line between the two must hold at least as
  # many markers as the line before it, so that no blockquote ends. A
  # line of only whitespace therefore ends the quote of the heading, but
  # a line of only `>` markers does not (`> ## Setup`, a blank line, `>
  # It runs on the node.`). The paragraph must hold the markers of the
  # last line before it, which `heading_gap` counts (`## Setup`, `>`,
  # `It runs on the node.`).
  #
  # The paragraph must also stay in the list item that holds the
  # heading. `heading_outer` counts the markers before the list marker,
  # and `heading_reach` holds the distance from the edge after them to
  # the text of the item (`- ## Setup`, `  > It runs on the node.`).
  if (heading_line && heading_line == prev_nb && !heading_broken && para_quote >= heading_gap && holds(heading_outer, heading_reach)) {
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
