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
# emphasis. `e.g.`, `i.e.`, `vs.`, `cf.`, and a three-period ellipsis
# do not end a sentence. Paragraphs end at a skipped line, which a
# blank line or a fenced block leaves. They also end at a line the
# reader passes as blank, which a bare `>` between two quoted paragraphs
# becomes.
# List items, headings, table rows, and lines that open with an HTML
# tag each start a new paragraph. Headings, table rows, and HTML lines
# hold no sentences, and the marker that opens a list item is not a
# token.
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

function end_sentence() {
  if (tok_n > 0) {
    sentence(tok_n)
  }
  tok_n = 0
}

# Returns the number of `>` markers that open the line.
function quote_depth(   p) {
  match($0, /^([ \t]*>)*/)
  p = substr($0, 1, RLENGTH)
  return gsub(/>/, "", p)
}

function prose(text,   rest, pos, t, col) {
  if (FILENAME != split_file || FNR != split_line + 1 || text ~ /^[ \t]*$/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)]|#+)([ \t]|$)/ || (in_table ? !index(text, "|") || quote_depth() != table_quote : text ~ /^[ \t]*</)) {
    end_sentence()
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
    end_sentence()
  }
  split_file = FILENAME
  split_line = FNR
  if (in_table || text ~ /^[ \t]*(#+([ \t]|$)|[|<])/) {
    return
  }
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
    if (t ~ /[.?!]["')*_\]]*$/ && t !~ /\.\.\.["')*_\]]*$/ && tolower(t) !~ /^[("'*_]*(e\.g|i\.e|vs|cf)\.$/) {
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
  end_sentence()
}
