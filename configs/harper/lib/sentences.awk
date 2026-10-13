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
# that holds the sentence is in `sentence_file`.
#
# Sentences run across the lines of a paragraph and end at a token that
# ends in `.`, `?`, or `!`, after any closing quote, paren, bracket, or
# emphasis. `e.g.`, `i.e.`, `vs.`, `cf.`, and a three-period ellipsis
# do not end a sentence. Paragraphs end at a skipped line, which a
# blank line or a fenced block leaves, and at a line the reader passes
# as blank, which a bare `>` between two quoted paragraphs becomes.
# List items, headings, table rows, and lines that open with an HTML
# tag each start a new paragraph. Headings, table rows, and HTML lines
# hold no sentences, and the marker that opens a list item is not a
# token.

@include "markdown.awk"

function end_sentence() {
  if (tok_n > 0) {
    sentence(tok_n)
  }
  tok_n = 0
}

function prose(text,   rest, pos, t, col) {
  if (FILENAME != split_file || FNR != split_line + 1 || text ~ /^[ \t]*$/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)]|#+)([ \t]|$)/ || text ~ /^[ \t]*[|<]/) {
    end_sentence()
  }
  split_file = FILENAME
  split_line = FNR
  if (text ~ /^[ \t]*(#+([ \t]|$)|[|<])/) {
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
    tok_col[tok_n] = column(text, col)
    if (tok_n == 1) {
      sentence_file = FILENAME
    }
    if (t ~ /[.?!]["')*_\]]*$/ && t !~ /\.\.\.["')*_\]]*$/ && tolower(t) !~ /^[("'*_]*(e\.g|i\.e|vs|cf)\.$/) {
      end_sentence()
    }
  }
}

END {
  end_sentence()
}
