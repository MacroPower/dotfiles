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
#
# Sentences run across the lines of a paragraph and end at a token that
# ends in `.`, `?`, or `!`, after any closing quote, paren, bracket, or
# emphasis. A closing quote is straight or curly (`"`, `'`, `”`, or
# `’`). `e.g.`, `i.e.`, `vs.`, `cf.`, and a three-period ellipsis do
# not end a sentence. ends_sentence() in lib/markdown.awk holds this
# test. Paragraphs end at a skipped line, which a blank line or a
# fenced block leaves. They also end at a bare line, which
# lib/markdown.awk marks in `bare_line`. A bare line holds only `>`
# markers, HTML comments, and `pre` elements. A bare `>` between two
# quoted paragraphs is one. A line that holds only a code span or a URL
# reaches prose() as spaces, but it continues the sentence of the line
# above it.
#
# List items, headings, table rows, horizontal rules, and lines that
# open with an HTML tag each start a new paragraph. So does a line that
# opens with more `>` markers than the paragraph that it follows,
# because it opens a deeper blockquote. The test for an HTML tag reads
# the line after lib/markdown.awk blanks it. A line that opens with an
# inline `code`, `kbd`, or `samp` element therefore continues the
# paragraph. A horizontal rule repeats one mark, `-`, `*`, or `_`,
# three or more times, such as `***` or `- - -`. Headings, table rows,
# horizontal rules, and HTML lines do not hold a sentence, and the
# marker that opens a list item is not a token.
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
# starts at its indent. GFM reads a line of `=` or `-` indented four or
# more spaces past the paragraph as more text of the paragraph. The
# library reads that line as an underline.
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
# so the words up to that cell still reach sentence(). Each later line
# that holds a pipe is a body row, even one that opens with an HTML
# tag (`<kbd>|Enter`). A skipped or blank line, a list item, a heading,
# or a line without a pipe ends the table. So does a line that opens
# with more or fewer `>` markers than the delimiter row, such as a
# blockquote after the table.
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

# Returns the number of `>` markers that open the line.
function quote_depth(   p) {
  match($0, /^([ \t]*>)*/)
  p = substr($0, 1, RLENGTH)
  return gsub(/>/, "", p)
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

function prose(text,   rest, pos, t, col, line) {
  # `raw` holds the line before clean() in lib/markdown.awk blanks its
  # code spans and URLs.
  line = blank(raw, "<!--([^-]|-[^-])*-->")
  if (underline(line)) {
    # The paragraph is a heading, so its sentences drop.
    tok_n = 0
    sent_n = 0
    para_line = -1
    split_line = FNR
    return
  }
  if (FILENAME != split_file || FNR != split_line + 1 || bare_line || text ~ /^[ \t]*([-*+]|[0-9]+[.)]|#+)([ \t]|$)/ || horizontal_rule(line) || (FNR == para_line + 1 && quote_depth() > para_quote) || (in_table ? !index(text, "|") || quote_depth() != table_quote : text ~ /^[ \t]*</)) {
    end_paragraph()
    in_table = 0
  } else if (index(text, "|") && text ~ /^[ \t]*\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$/) {
    # A delimiter row makes the line before it the header row, so the
    # tokens of that line leave the sentence.
    while (tok_n > 0 && tok_line[tok_n] == split_line) {
      tok_n--
    }
    in_table = 1
    table_quote = quote_depth()
  }
  if (in_table || text ~ /^[ \t]*\|/) {
    end_paragraph()
  }
  split_file = FILENAME
  split_line = FNR
  # A line that holds only a code span or a URL passes this test. It
  # joins the paragraph, so an underline can follow it.
  if (in_table || bare_line || text ~ /^[ \t]*(#+([ \t]|$)|[|<])/ || horizontal_rule(line)) {
    return
  }
  if (FNR != para_line + 1) {
    # The line opens a paragraph, so it sets the quote depth and the
    # list item column that underline() reads. lib/markdown.awk blanks a
    # code span or a URL at the start of the text. So the spaces before
    # the text come from `$0`, which holds the same columns.
    para_quote = quote_depth()
    para_item = 0
    if (text ~ /^[ \t]*([-*+]|[0-9]+[.)])([ \t]|$)/) {
      match(text, /^[ \t]*([-*+]|[0-9]+[.)])/)
      para_item = RLENGTH
      match(substr($0, para_item + 1), /^[ \t]*/)
      para_item += RLENGTH
    } else if (in_list) {
      # A later paragraph of a list item.
      match($0, /^([ \t]*>)*[ \t]*/)
      para_item = RLENGTH
    }
  }
  para_line = FNR
  rest = text
  pos = 0
  while (match(rest, /[^ \t]+/)) {
    t = substr(rest, RSTART, RLENGTH)
    col = pos + RSTART
    pos += RSTART + RLENGTH - 1
    rest = substr(rest, RSTART + RLENGTH)
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
    if (ends_sentence(t)) {
      end_sentence()
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
