# Splits markdown prose into sentences for a check. A check file pulls
# this splitter in with `@include "sentences.awk"`, which also pulls in
# markdown.awk, and defines two functions:
#
#   BEGIN_CHECK()  runs once at startup, after `utf8` is set
#   sentence(n)    runs on each sentence, which holds n tokens
#
# A token is a run of characters other than whitespace, punctuation
# included. sentence() reads token i from `tok[i]`, its line from
# `tok_line[i]`, and its character column from `tok_col[i]`. The file
# that holds the sentence is in `sentence_file`. sentence_words(n)
# counts the words of the sentence for a length check.
# quoted_opening(n) finds a quotation that opens the sentence, for a
# check that reads the first word. split_quotation(n, quoted) finds the
# second part of such a quotation when an attribution splits it, and
# quotation_end(n, s) finds the token that closes a quotation.
#
# Sentences run across the lines of a paragraph and end at a token that
# ends in `.`, `?`, or `!`, after any closing quote, paren, bracket, or
# emphasis. A closing quote is straight or curly (`"`, `'`, `”`, or
# `’`). `e.g.`, `i.e.`, `vs.`, `cf.`, and a three-period ellipsis do
# not end a sentence. A quote after the mark can close a quotation
# inside a sentence, so a lowercase word after the quote continues the
# sentence (`prints "Lucky penny!" every time`). That word can sit on
# the next line of the paragraph. ends_sentence() and
# continues_sentence() in lib/markdown.awk hold these tests.
#
# Paragraphs end at a skipped line, which a blank line or a fenced
# block leaves. They also end at a bare line, which lib/markdown.awk
# marks in `bare_line`. A bare line holds only `>` markers, HTML
# comments, and `pre` elements. A bare `>` between two quoted
# paragraphs is one. A line that holds only a code span or a URL
# reaches prose() as spaces, but it continues the sentence of the line
# above it.
#
# List items, headings, footnote definitions (`[^1]: text`), table
# rows, horizontal rules, and lines that open with an HTML tag each
# start a new paragraph. So does a line that opens with more `>`
# markers than the paragraph that it follows, because it opens a
# deeper blockquote. The test for an HTML tag reads the line after
# lib/markdown.awk blanks it. A line that opens with an inline `code`,
# `kbd`, or `samp` element therefore continues the paragraph. A
# horizontal rule repeats one mark, `-`, `*`, or `_`, three or more
# times, such as `***` or `- - -`. Headings, table rows, horizontal
# rules, and HTML lines do not hold a sentence, and the marker that
# opens a list item is not a token.
#
# CommonMark lets a block interrupt a paragraph only at an indent of
# fewer than four columns. The library applies that limit to list
# items, headings, footnote definitions, table rows, horizontal rules,
# HTML lines, and bare lines. A line with a deeper indent is more text
# of the paragraph, and its words join the sentence
# (`The scheduler retries`, `    - and then`, `it waits.`). At any
# indent, a line of only `>` markers and whitespace still ends the
# paragraph, and so does a line with more `>` markers than the
# paragraph.
#
# A delimiter row with a deeper indent is more text too, so no table
# opens there. A line of `=` or `-` marks with a deeper indent does not
# underline the paragraph. An ordered list marker that opens a line with
# a deeper indent is a token. The word after it decides whether its
# period ends the sentence, as after a quote. So `    1. and then`
# continues the sentence, and `    2026. Then` ends it after `2026.`.
#
# GFM counts the indent from the left edge of the block that holds the
# paragraph. Outside a list and a blockquote, the edge is the start of
# the line. In a list item, it is the column where the text of the item
# starts. In a blockquote, it sits after the `>` markers and the space
# that follows them. In a footnote definition (`[^1]: text`), it sits
# four columns past the edge of the block that holds the definition. A
# tab moves to the next multiple of four columns.
#
# The library does not track that edge, so indent() counts from the
# columns of the first line of the paragraph. On a line with as many `>`
# markers as the paragraph, it counts from the column where the text of
# the paragraph starts. On a line with fewer markers, it counts from the
# column of the first marker that the line lacks in the first line of
# the paragraph (`  > The scheduler retries` and `      - and then` in a
# `-` item). When the edge after the markers of the line sits further
# right, it counts from that edge.
#
# Each of these columns shifts with the `>` markers of the line.
# indent() takes the column after the markers of the line and the one
# space after them. It also takes the same column after as many markers
# in the first line of the paragraph. Each column moves by the distance
# between the two. So the line in `> - The scheduler retries`,
# `   >     - and then` counts from the text of the item three columns
# further right, and the nested item ends the sentence.
#
# A paragraph that opens at a list marker can open items inside items
# on its first line (`- - text`, `1. - text`). item_column() in
# lib/markdown.awk returns the column where the text of each item
# starts. A later line with as many `>` markers as the paragraph counts
# from the text of the innermost item whose text it reaches. GFM reads a
# line left of the text of an item as outside the item. So a line left
# of the text of every item counts from the first list marker
# (`1.   The scheduler retries` above `    - and then`).
#
# GFM does not open a paragraph in a list item whose first line holds
# only its marker, a heading, a horizontal rule, or indented code. A
# deep line right after such a line is therefore not more text of a
# paragraph. The library reads that line as it reads a line indented
# fewer than four columns (`- # Setup` above `      - and then`).
#
# Where GFM opens a paragraph, its text never sits left of the edge of
# its block. The list markers and the `>` markers of its first line
# never sit left of the edge of the block that holds them either. On a
# later line, GFM keeps each edge at the same distance from the column
# after the `>` markers of that line. The library shifts its columns the
# same way. It can still count more columns than GFM when it opens a
# paragraph at a line that GFM reads as more text of the paragraph
# before. GFM then keeps the edge of the block of that paragraph.
#
# A line that opens with `<` or `|` can end a paragraph that way. So can
# a delimiter row and each row of the table below it. GFM does not open
# a table at a delimiter row that holds another number of cells than the
# row above it. A paragraph that opens at a line of text right after
# such a line counts from no further left than the text of the
# paragraph before. That column shifts with the `>` markers too. So the
# nested item stays out of the sentence in `- The pod holds the cache.`,
# `<b>Then</b> it waits.`, `The scheduler retries`, `    - and then`.
# In GFM, a line with a list marker or with more `>` markers than the
# paragraph before opens a block. Its own columns then set the count.
#
# The library does not carry that column past a line that
# lib/markdown.awk skips. GFM reads some of those lines as more text of
# the paragraph. A link reference definition is one, and so are a deep
# fence marker and a deep line that opens an HTML comment spanning
# lines. A nested list item after such a line then joins the sentence
# above the item (`- The pod holds the cache.`,
# `[guide]: https://example.com`, `The scheduler retries`,
# `    - and then`).
#
# The library can also count fewer columns than GFM. It then ends a
# paragraph whose text starts one to three columns past the edge, at a
# line that GFM reads as more text (`   The scheduler retries` above
# `    - and then`). A list marker one to three columns past the edge of
# the block around its item does the same to a line left of the text of
# the item (`- Alpha one`, `     1. The scheduler retries`,
# `       - and then`). A `>` marker one to three columns past the edge
# does the same to a line that lacks the marker
# (`>    > The scheduler retries` above `>     - and then`). A footnote
# definition indented fewer than four columns ends the open paragraph
# and opens its own, as in GFM. The library counts the text of that
# paragraph from four columns past the label. So a label one to three
# columns past the edge does the same
# (`   [^1]: The scheduler retries` above `        - and then`).
#
# A table in a list item also makes the library count fewer columns at
# the lines of text below it. GFM ends the item at a line left of its
# text and opens a paragraph there. The library counts from the text of
# the item (`- The pod holds the cache.`, `  a | b`, `  --- | ---`,
# `The scheduler retries`, `    - and then`).
#
# A line with a `>` marker left of the marker with the same index in
# the first line of the paragraph never counts as more text by its
# indent. Such a line ends the paragraph at any indent when it opens
# with a list marker, `#`, `<`, or `|`, holds a horizontal rule, or is
# bare. In a list item, GFM can end the item at such a marker and open
# a new blockquote (`  > The scheduler retries` in a `-` item, above
# `>       - and then`). Elsewhere, GFM can read the line as more text,
# because a marker can sit up to three columns past the edge of its
# block. A line of plain text with such a marker continues the
# paragraph, as it does in GFM outside a list item.
#
# The library reads a `>` at any indent as a blockquote marker.
# lib/markdown.awk skips each line of fenced code and of an HTML comment
# that spans lines, at any indent. A line such as `    > and then`,
# `    ~~~`, or `    <!-- a` therefore ends the paragraph, although GFM
# reads it as more text.
#
# A heading opens with one to six `#` marks. CommonMark reads a line
# with seven or more marks as text, and so does the library. Such a line
# right after a paragraph continues the paragraph, and its marks and
# words are tokens of its sentences (`####### It starts the worker.`).
#
# A setext heading is a paragraph with an underline, a line of only `=`
# or only `-` right under it. The underline turns the whole paragraph
# into a heading. So sentence() runs on the sentences of a paragraph
# only when the paragraph ends, and an underline drops them. The line
# after the underline starts a new paragraph. A `---` that passes the
# tests for an underline is one, and any other `---` is a horizontal
# rule.
#
# The tests for an underline and a horizontal rule read the line before
# lib/markdown.awk blanks its code spans and URLs, with only its HTML
# comments blanked. A line that holds only an emphasized code span, such
# as ``**`kubectl apply`**``, therefore stays text, although its blanked
# text holds only `*` marks.
#
# An underline must open with as many `>` markers as its paragraph.
# In a list item, it must also reach the column where the text of its
# paragraph starts. On the line of the marker, the text starts after
# the marker and the spaces after it. A later paragraph of the item
# starts at its indent. A line of `=` or `-` that indent() counts four
# or more columns into the paragraph is more text of the paragraph, as
# in GFM.
#
# Comment text (SHADOW=1) does not hold an underline. A line of `-` or
# `=` marks in a source comment frames a block of comment text and does
# not underline a heading. So a `---` line there is a horizontal rule,
# and a `===` line is a token.
#
# A table row is a line that opens with `|`, or a line of a pipe table
# whose rows lack leading pipes. Such a table opens at its delimiter
# row (`:--|:--`), which holds only pipes, colons, and hyphens. The
# line before the delimiter row is the header row, and its tokens drop
# out of the sentence that they joined. A header cell that ends in
# `.`, `?`, or `!` ends its sentence before the delimiter row arrives,
# so the words up to that cell still reach sentence(). When a closing
# quote follows the mark (`Says "Hi!"`), the token after the quote
# decides, as it does in prose, and a `|` ends the sentence. When the
# quote ends the row, the end stays pending, and the delimiter row
# drops the words of the row from the sentence.
#
# Each line after the delimiter row that holds a pipe is a body row,
# even one that opens with an HTML tag (`<kbd>|Enter`). A skipped or
# blank line, a list item, a heading, or a line without a pipe ends the
# table. So does a line that opens with more or fewer `>` markers than
# the delimiter row, such as a blockquote after the table.
#
# The library differs from GFM at two kinds of line after a table. GFM
# reads a plain line without a pipe as a body row of one cell. Such a
# line more often opens a paragraph, so the library splits it into
# sentences. GFM ends the table at a block-level HTML tag such as
# `<div>`. The library reads such a line as a body row when it holds a
# pipe.

@include "markdown.awk"

# Queues the pending tokens as a sentence of the paragraph.
function end_sentence(   i) {
  if (tok_n > 0) {
    sent_n++
    sent_len[sent_n] = tok_n
    sent_file[sent_n] = sentence_file
    for (i = 1; i <= tok_n; i++) {
      sent_tok[sent_n, i] = tok[i]
      sent_line[sent_n, i] = tok_line[i]
      sent_col[sent_n, i] = tok_col[i]
    }
  }
  tok_n = 0
  quote_end = 0
}

# Ends the paragraph and runs sentence() on each sentence that it holds.
function end_paragraph(   s, i) {
  end_sentence()
  for (s = 1; s <= sent_n; s++) {
    for (i = 1; i <= sent_len[s]; i++) {
      tok[i] = sent_tok[s, i]
      tok_line[i] = sent_line[s, i]
      tok_col[i] = sent_col[s, i]
    }
    sentence_file = sent_file[s]
    sentence(sent_len[s])
  }
  sent_n = 0
  para_line = -1
}

# Reports whether token t opens with a double quote, straight or curly,
# after any opening paren, bracket, or emphasis (`"Save`, `(“Save`).
# The pattern writes `“` as its UTF-8 bytes, as opens_quote() in
# lib/markdown.awk does.
function opens_double_quote(t) {
  return t ~ /^[(\[*_]*("|\342\200\234)/
}

# Returns the index of the token that closes the quotation that token s
# opens, or 0 when no token closes it. Token s opens with a quote, as
# opens_quote() in lib/markdown.awk tests. The closing token ends in a
# closing quote of the same family as the opening quote. A double quote
# opens with `"` or `“` and closes with `"` or `”`. A single quote opens
# with `'` or `‘` and closes with `'` or `’`. A quotation can therefore
# open with a straight quote and close with a curly one (`"Save
# changes?” appears`). A mark, a closing paren, a bracket, or emphasis
# can follow the quote (`"Save",`). The closing quote must come from the
# family of the opening one. An apostrophe at the end of a word (`"Copy
# the users' files"`) therefore does not close a quotation in double
# quotes.
#
# A later token that opens with a quote of the family opens an inner
# quotation (`“Run "make" if the tests pass,” the guide says`). The
# function skips the closing quote of each inner quotation, so the first
# quotation closes at `pass,”`.
#
# A single closing quote is the same mark as an apostrophe. In single
# quotes, a word that ends in `s` and the quote can therefore be a
# plural possessive (`'Copy the users' files'`) or the end of the
# quotation (`'Save logs' appears`). The function holds the first such
# token and skips each later one. Emphasis can follow the quote of such
# a token (`logs'**`). A plural possessive comes before its noun. A
# token with a mark, a closing paren, or a bracket after the quote
# (`fails',`, `logs'.`) is therefore not such a token. The function
# returns the first token that ends in the quote and that it neither
# holds nor skips, such as `data'`, `fills!'`, or `fails',`. It returns
# the held token when no such token follows. A later quotation after the
# held token counts as an inner quotation (`'Save logs' appears if the
# run prints 'Done!'`), so its closing quote does not end the search.
#
# The patterns write each curly quote as its UTF-8 bytes, as
# ends_sentence() in lib/markdown.awk does.
function quotation_end(n, s,   i, t, o, q, held, inner) {
  # o matches each opening quote of the family, and q each closing one.
  if (opens_double_quote(tok[s])) {
    o = "(\"|\342\200\234)"
    q = "(\"|\342\200\235)"
  } else {
    o = "('|\342\200\230)"
    q = "('|\342\200\231)"
  }
  # Drop the opening quote, so a token that holds only the opening quote
  # (`" Save" appears`) does not close the quotation.
  t = tok[s]
  sub(/^[(\[*_]*/, "", t)
  sub(/^(["']|\342\200\234|\342\200\230)/, "", t)
  held = 0
  inner = 0
  for (i = s; i <= n; i++) {
    if (i > s) {
      t = tok[i]
      # A token that opens another quotation of the same family opens an
      # inner quotation, and the next closing quote ends it. That quote
      # can end the same token (`“Run "make" if`).
      if (t ~ ("^[(\\[*_]*" o)) {
        inner++
      }
    }
    if (t !~ (q "[)*_\\],;:.?!]*$")) {
      continue
    }
    if (inner) {
      inner--
      continue
    }
    if (o ~ /'/ && t ~ ("[sS]" q "[*_]*$")) {
      if (!held) {
        held = i
      }
      continue
    }
    return i
  }
  return held
}

# Returns the index of the token that closes a quotation at the start of
# the sentence, which quotation_end() finds. When no token closes the
# quotation, the function returns the first token for which
# ends_sentence() returns 2. That token holds a period, a question mark,
# or an exclamation mark before a closing quote of the other family
# (`"Save changes?’ appears`). The function returns 0 when the sentence
# does not open with a quote, or when it finds neither token.
#
# The sentence continues past that token (`"Save" appears if the file
# changed`, `"Save changes?" appears if you close the editor`), so its
# first word sits inside the quotation. A check that reads the first
# word as the opener of the sentence applies it only to the words up to
# that token.
function quoted_opening(n,   i) {
  if (!opens_quote(tok[1])) {
    return 0
  }
  i = quotation_end(n, 1)
  if (i) {
    return i
  }
  # No closing quote of the family closes the quotation. A mark before a
  # quote of the other family still closes it (`"Save changes?’
  # appears`).
  for (i = 1; i <= n; i++) {
    if (ends_sentence(tok[i]) == 2) {
      return i
    }
  }
  return 0
}

# Returns the index of the token that opens the second part of a
# quotation that an attribution splits, such as `the guide says` in
# `"Copy nothing," the guide says, "unless the tests pass."`. The
# quotation opens the sentence, and its first part closes at token
# `quoted`, which quoted_opening() returns. The function returns 0 when
# the sentence does not hold such a second part. quotation_end() finds
# the token that closes the second part.
#
# The second part opens at the first later token that opens with a
# quote of the family of the first part. The words between the two
# parts form the attribution. The function returns that token only when
# these tests pass:
#
# - A comma sits among the marks at the end of the first part, before
#   or after its closing quote (`nothing,"`, `‘Never’,`).
# - The attribution holds one or more words, and the last one ends in a
#   comma (`says,`).
# - No other word of the attribution ends in a comma, a semicolon, a
#   colon, a period, a question mark, or an exclamation mark. Other
#   marks can follow that mark, such as the emphasis of `*guide,*`.
# - No word of the attribution is `and`, `but`, `or`, `nor`, `so`,
#   `yet`, `then`, `while`, `whereas`, `because`, `although`, `though`,
#   `if`, `unless`, `when`, `whenever`, `where`, `wherever`, `until`,
#   `since`, `after`, `before`, or `once`. Each of these words can join
#   a second clause, a subject and its verb, to the words before it
#   (`appears in the log when the tooltip says,`). The test reads a
#   word in emphasis as the word itself (`_and_`). A paren attached to
#   the word keeps it out of the list, so an aside still reads as part
#   of an attribution (`the guide (unless updated) says,`).
#
# These tests keep out most main clauses that go on after a quoted
# string and then quote another one (`"Do not disturb" hangs on the
# front door, "Knock unless busy" on the back door`, `"Don't panic,"
# appears in the log; the tooltip says, "Retry unless busy."`, `"Don't
# panic," appears in the log and the tooltip says, "Retry unless
# busy."`). Two quotations in a row (`‘Never’, ‘Retry unless busy’, and
# ‘Save’`) do not hold an attribution between them, so they do not form
# one quotation.
#
# The list leaves out `that`, `which`, `who`, and `as`, because each of
# them also opens a phrase inside an attribution (`the guide that ships
# with the tool says,`, `as the guide says,`). A main clause that one of
# them joins therefore reads as an attribution (`"Don't panic," appears
# above a button that says, "Retry unless busy."`). So does a phrase
# without a verb (`"Not now," in red, "Retry unless busy" in gray`).
function split_quotation(n, quoted,   i, k, w) {
  if (!quoted || tok[quoted] !~ /,[^A-Za-z0-9]*$/) {
    return 0
  }
  for (i = quoted + 1; i <= n; i++) {
    if (opens_quote(tok[i]) && opens_double_quote(tok[i]) == opens_double_quote(tok[1])) {
      break
    }
  }
  if (i > n || i == quoted + 1 || tok[i - 1] !~ /,$/) {
    return 0
  }
  for (k = quoted + 1; k < i; k++) {
    # Emphasis does not hide a word of the list (`_and_`).
    w = tolower(tok[k])
    gsub(/^[*_]+|[*_]+$/, "", w)
    if (w ~ /^(and|but|or|nor|so|yet|then|while|whereas|because|although|though|if|unless|when|whenever|where|wherever|until|since|after|before|once)$/) {
      return 0
    }
    if (k < i - 1 && tok[k] ~ /[,;:.?!][^A-Za-z0-9]*$/) {
      return 0
    }
  }
  return i
}

# Returns the number of `>` markers that open the line.
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
# count never starts left of the rightmost column in `para_edge` that
# the text of the line reaches. When the text reaches none, the count
# never starts left of `para_edge[0]`. On a line with `d` markers, fewer
# than the paragraph has, it never starts left of `para_mark[d]`. Each
# of these columns first moves by `shift`, the distance from
# `para_qbase[d]` to the column where the count starts.
function indent(   n, base, d, edge, c, j, shift) {
  match($0, /^([ \t]*>)*/)
  n = RLENGTH
  base = cols(n)
  if (n && substr($0, n + 1, 1) ~ /[ \t]/) {
    base++
  }
  match($0, /^([ \t]*>)*[ \t]*/)
  c = cols(RLENGTH)
  d = quote_depth()
  shift = base - para_qbase[d]
  if (d < para_quote) {
    edge = para_mark[d] + shift
  } else {
    edge = para_edge[0] + shift
    for (j = 1; j < para_edges; j++) {
      if (para_edge[j] + shift <= c) {
        edge = para_edge[j] + shift
      }
    }
  }
  if (edge > base) {
    base = edge
  }
  return c - base
}

# Returns 1 when no `>` marker of the line sits left of the marker with
# the same index in the first line of the paragraph. In a list item, GFM
# can end the item at a marker further left and open a new blockquote
# there (`  > The scheduler retries` in a `-` item, above
# `>       - and then`).
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

# Reports whether the line underlines the paragraph on the line before
# it, which makes the paragraph a setext heading. `line` holds the line
# as written, with its HTML comments blanked. An underline is a run of
# `=` or of `-` at the quote depth of the paragraph. In a list item, it
# reaches the column where the text of the paragraph starts. Comment
# text does not hold an underline.
function underline(line) {
  return !shadow_mode && FILENAME == split_file && FNR == para_line + 1 && line ~ /^[ \t]*(=+|-+)[ \t]*$/ && quote_depth() == para_quote && match(line, /[^ \t]/) > para_item
}

# Reports whether `line`, the line as written with its HTML comments
# blanked, is a horizontal rule.
function horizontal_rule(line) {
  return line ~ /^[ \t]*((-[ \t]*){3,}|(\*[ \t]*){3,}|(_[ \t]*){3,})$/
}

function prose(text,   rest, pos, t, col, line, e, deep, n, i, k, open, lazy, carry, s, lead, marker) {
  # `raw` holds the line before clean() in lib/markdown.awk blanks its
  # code spans and URLs.
  line = blank(raw, "<!--([^-]|-[^-])*-->")
  # `open` holds 1 when the line comes right after a line of the open
  # paragraph, and `lazy` when it comes right after a line that
  # `lazy_end` marks.
  open = FILENAME == split_file && FNR == para_line + 1
  lazy = lazy_end && FILENAME == split_file && FNR == split_line + 1
  lazy_end = 0
  # A line indented four or more columns cannot open a list item, a
  # heading, a footnote definition, a table row, an HTML block, or a
  # horizontal rule. Nor can it underline a setext heading. `deep`
  # therefore marks it as more text of the open paragraph
  # (`    - first`, `    ---`). A line with more `>` markers than the
  # paragraph still ends it, at any indent.
  # `deep` never marks a line with a marker left of the paragraph's
  # marker, because such a marker can open a new blockquote. It never
  # marks a line of only `>` markers and whitespace either, because that
  # line stays a blank line of the quote. Nor does it mark a line after a
  # list item where `para_open` holds 0, because GFM does not open a
  # paragraph in that item.
  deep = open && para_open && raw ~ /[^ \t]/ && quote_depth() <= para_quote && marks_hold() && indent() >= 4
  if (!deep && underline(line)) {
    # The paragraph is a heading, so its sentences drop.
    tok_n = 0
    quote_end = 0
    sent_n = 0
    para_line = -1
    split_line = FNR
    return
  }
  if (FILENAME != split_file || FNR != split_line + 1 || (bare_line && !deep) || (!deep && (text ~ /^[ \t]*([-*+]|[0-9]+[.)]|#{1,6})([ \t]|$)/ || text ~ /^[ \t]*\[\^[^] \t]+\]:/ || horizontal_rule(line))) || (FNR == para_line + 1 && quote_depth() > para_quote) || (in_table ? !index(text, "|") || quote_depth() != table_quote : !deep && text ~ /^[ \t]*</)) {
    end_paragraph()
    in_table = 0
  } else if (!deep && index(text, "|") && text ~ /^[ \t]*\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$/) {
    # A delimiter row makes the line before it the header row, so the
    # tokens of that line leave the sentence. A delimiter row that `deep`
    # marks is more text of the paragraph, so no table opens there.
    while (tok_n > 0 && tok_line[tok_n] == split_line) {
      tok_n--
    }
    in_table = 1
    table_quote = quote_depth()
  }
  if (in_table || (!deep && text ~ /^[ \t]*\|/)) {
    end_paragraph()
  }
  split_file = FILENAME
  split_line = FNR
  # A line that holds only a code span or a URL passes this test. It
  # joins the paragraph, so an underline can follow it. So does a line
  # that `deep` marks.
  if (in_table || (!deep && (bare_line || text ~ /^[ \t]*(#{1,6}([ \t]|$)|[|<])/ || horizontal_rule(line)))) {
    # GFM reads a line that opens with `<` or `|` as more text of the
    # open paragraph unless the line opens an HTML block or a table. It
    # also reads a delimiter row and the rows below it as more text when
    # the delimiter row holds another number of cells than the row above
    # it. The line after such a line can then be more text of that
    # paragraph too, in a block whose edge sits right of its own text.
    # `lazy_end` marks such a line, or a line of a table, when it ends the
    # open paragraph or follows another line that `lazy_end` marks. The
    # text of a bare line holds only spaces, so `lazy_end` never marks
    # one.
    lazy_end = (in_table || text ~ /^[ \t]*[|<]/) && (open || lazy)
    return
  }
  if (FNR != para_line + 1) {
    # The line opens a paragraph, so it sets the quote depth and the
    # list item column that underline() reads. lib/markdown.awk blanks a
    # code span or a URL at the start of the text. So the spaces before
    # the text come from `$0`, which holds the same columns.
    # A line with more `>` markers than the paragraph before opens a
    # blockquote in GFM. Any other line right after a line that
    # `lazy_end` marks can be more text of the paragraph before. With `d`
    # markers on this line, `carry` then holds how many columns the text
    # of that paragraph starts right of its `para_qbase[d]`, and -1
    # otherwise.
    carry = lazy && quote_depth() <= para_quote ? para_text - para_qbase[quote_depth()] : -1
    para_quote = quote_depth()
    para_item = 0
    # `para_text` holds the column where the text of the paragraph
    # starts, or the edge of the text of a footnote definition.
    # `para_mark[k]` holds the column of the `>` marker after the first
    # `k` markers of the line. `para_qbase[k]` holds the column
    # after the first `k` markers and the one space after them, and
    # `para_qbase[0]` holds 0. `para_edge[0]` to
    # `para_edge[para_edges - 1]` hold the columns that indent() can
    # count from, from left to right. A paragraph that does not open at a
    # list marker holds one such column, `para_text`.
    match($0, /^([ \t]*>)*[ \t]*/)
    n = RLENGTH
    para_text = cols(n)
    k = 0
    para_qbase[0] = 0
    for (i = 1; i <= n; i++) {
      if (substr($0, i, 1) == ">") {
        para_mark[k++] = cols(i - 1)
        para_qbase[k] = cols(i) + (substr($0, i + 1, 1) ~ /[ \t]/)
      }
    }
    if (text ~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/) {
      match(text, /^[ \t]*([-*+]|[0-9]+[.)])/)
      para_item = RLENGTH
      match(substr($0, para_item + 1), /^[ \t]*/)
      para_item += RLENGTH
      # GFM reads a line left of the text of an item as outside the
      # item. So it counts the indent of the line from the edge of the
      # block around the item. `para_edge[0]` holds the column of the
      # first list marker, which sits zero to three columns right of that
      # edge. Each later entry holds the column where the text of an item
      # on the line starts, from the outermost item in. item_column() in
      # lib/markdown.awk returns that column. A list marker at the text
      # of an item opens an item inside it (`- - text`, `1. - text`).
      # blank() writes one space for each character of the `>` markers,
      # and a tab among them takes up to four columns. So prose() expands
      # the tabs of the line before it blanks the markers, and the
      # columns of `s` match the characters of `s`.
      s = blank(expand_tabs($0), "^([ \t]*>)+")
      match(s, /^ */)
      para_edge[0] = RLENGTH
      para_edges = 1
      para_text = 0
      do {
        para_text += item_column(substr(s, para_text + 1))
        para_edge[para_edges++] = para_text
      } while (substr(s, para_text + 1) ~ /^([-*+]|[0-9]{1,9}[.)])( |$)/)
      # GFM does not open a paragraph in an item whose first line holds
      # only its marker, a heading, a horizontal rule, or indented code
      # (`- # Setup`, `- ***`). item_column() puts the column of indented
      # code one column past the marker, so four or more spaces follow
      # it. The loop reads the marks of a rule such as `* * *` as list
      # markers, and no text follows them.
      s = substr(s, para_text + 1)
      para_open = s !~ /^( *$|    |#{1,6}( |$))/ && !horizontal_rule(s)
      # A footnote definition in the item adds the edge of its text.
      if (s ~ /^\[\^[^] \t]+\]:/) {
        para_text += 4
        para_edge[para_edges++] = para_text
      }
    } else {
      if (in_list) {
        # A later paragraph of a list item.
        para_item = n
      }
      # GFM puts the edge of the text of a footnote definition
      # (`[^1]: text`) four columns past the edge of the block that holds
      # the definition. Its label opens zero to three columns past that
      # edge, so the column four past the label never sits left of it.
      if (text ~ /^[ \t]*\[\^[^] \t]+\]:/) {
        para_text += 4
      }
      # indent() never counts from left of the column `carry` columns
      # right of `para_qbase[para_quote]`. A list marker can open an item
      # in GFM, so the text of the item sets the edge, and the branch
      # above leaves `carry` out.
      if (carry >= 0 && para_qbase[para_quote] + carry > para_text) {
        para_text = para_qbase[para_quote] + carry
      }
      para_edge[0] = para_text
      para_edges = 1
      para_open = 1
    }
  }
  para_line = FNR
  rest = text
  pos = 0
  # `lead` holds 1 until the loop reads the first token of a line that
  # `deep` marks.
  lead = deep
  while (match(rest, /[^ \t]+/)) {
    t = substr(rest, RSTART, RLENGTH)
    col = pos + RSTART
    pos += RSTART + RLENGTH - 1
    rest = substr(rest, RSTART + RLENGTH)
    marker = lead && t ~ /^[0-9]+\.$/
    lead = 0
    # The token before ended in a mark and a closing quote. The sentence
    # ended there unless this token continues it. The sentence ends
    # before the list marker test, so a marker after the quote
    # (`"Done!" 2) Run`) drops out of the next sentence, as it does
    # after `Done!`.
    if (quote_end && !continues_sentence(t)) {
      end_sentence()
    }
    quote_end = 0
    if (tok_n == 0 && t ~ /^([-*+]|[0-9]+[.)])$/) {
      continue
    }
    tok_n++
    tok[tok_n] = t
    tok_line[tok_n] = FNR
    tok_col[tok_n] = column(col)
    if (tok_n == 1) {
      sentence_file = FILENAME
    }
    e = ends_sentence(t)
    if (e == 1 && marker) {
      # GFM reads an ordered list marker that opens a deep line as text.
      # The word after it decides whether its period ends the sentence,
      # as after a quote (`    1. and then`, `    2026. Then it stops.`).
      e = 2
    }
    if (e == 1) {
      end_sentence()
    } else if (e == 2) {
      quote_end = 1
    }
  }
}

# Returns the number of words in the sentence and sets `words_from` to
# the index of the first token that holds one. Each part of a hyphenated
# word counts (`read-only` is two words), and a token with no letter
# (`42`, `|`) counts as none.
function sentence_words(n,   i, k, m, parts, words) {
  words = 0
  words_from = 0
  for (i = 1; i <= n; i++) {
    m = split(tok[i], parts, "-")
    for (k = 1; k <= m; k++) {
      if (parts[k] ~ /[A-Za-z]/) {
        if (!words_from) {
          words_from = i
        }
        words++
      }
    }
  }
  return words
}

END {
  end_paragraph()
}
