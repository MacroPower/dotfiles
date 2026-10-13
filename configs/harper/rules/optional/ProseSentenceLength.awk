# Reports a sentence of 26 to 40 words in markdown prose. The
# technical-writing skill splits a sentence past about 25 words, and
# Harper's LongSentences reports one past 40 at the Recommended tier,
# so this check covers the band between them as a suggestion.
#
# The count follows LongSentences, so one sentence never draws both
# findings. Each part of a hyphenated word counts (`read-only` is two
# words), and a token with no letter (`42`, `|`) counts as none. The
# reader blanks code spans, so a span counts as none either.
#
# Sentences run across the lines of a paragraph and end at a `.`, `?`,
# or `!` before whitespace or the line end, after any closing quote,
# paren, bracket, or emphasis. Paragraphs end at a skipped line, which
# a blank line or a fenced block leaves, and at a line the reader
# passes as blank, which a bare `>` between two quoted paragraphs
# becomes. List items, headings, table rows, and lines that open with
# an HTML tag each start a new one. Headings, table rows, and HTML
# lines count no words. Each finding names the line and column where
# the sentence starts.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Readability::ProseSentenceLength: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  min_words = 26
  max_words = 40
  last_line = 0
  words = 0
}

function flush() {
  if (words >= min_words && words <= max_words) {
    printf "%s:%d:%d: Readability::ProseSentenceLength: Sentence of %d words. Split it so each sentence carries one thought.\n", start_file, start_line, start_col, words
  }
  words = 0
}

function prose(text,   rest, pos, tok, col, parts, n, i) {
  if (FNR != last_line + 1 || text ~ /^[ \t]*$/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)]|#+)([ \t]|$)/ || text ~ /^[ \t]*[|<]/) {
    flush()
  }
  last_line = FNR
  if (text ~ /^[ \t]*(#+([ \t]|$)|[|<])/) {
    return
  }
  rest = text
  pos = 0
  while (match(rest, /[^ \t]+/)) {
    tok = substr(rest, RSTART, RLENGTH)
    col = pos + RSTART
    pos += RSTART + RLENGTH - 1
    rest = substr(rest, RSTART + RLENGTH)
    if (tok ~ /^([-*+]|[0-9]+[.)])$/ && words == 0) {
      continue
    }
    n = split(tok, parts, "-")
    for (i = 1; i <= n; i++) {
      if (parts[i] ~ /[A-Za-z]/) {
        if (words == 0) {
          start_file = FILENAME
          start_line = FNR
          start_col = column(text, col)
        }
        words++
      }
    }
    if (tok ~ /[.?!]["')*_\]]*$/) {
      flush()
    }
  }
}

END {
  flush()
}
