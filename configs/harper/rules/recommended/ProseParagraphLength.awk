# Reports a paragraph of more than six sentences in markdown prose. The
# technical-writing skill keeps a paragraph to one topic and six
# sentences or fewer, and a longer one usually carries a second topic.
#
# The paragraph boundaries follow ProseSentenceLength. A paragraph ends
# at a skipped line or at a line the reader passes as blank. It also
# ends where a list item, heading, table row, or HTML line starts, so
# each list item counts as its own paragraph. Sentences end at a `.`,
# `?`, or `!` before whitespace or the line end, after any closing
# quote, paren, bracket, or emphasis. `e.g.`, `i.e.`, `vs.`, and `cf.`
# do not end a sentence, and neither does a three-period ellipsis
# (`an implicit "About..." in front`). Words after the last sentence
# end count as one more sentence. A run-in label in bold that ends in a
# period or a colon names the paragraph's topic (`**Tutorial.** The
# learner's success`). Such a label counts as no sentence when it opens
# the paragraph. Each finding names the line and column where the
# paragraph starts.
#
# It hit 48 lines in the measure.py docs corpus, and 15 of 20 sampled
# hits carried a second topic or a hidden list. Four misses kept to one
# topic, and one counted the `etc.` inside a sentence as a sentence end.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Readability::ProseParagraphLength: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  max_sentences = 6
  last_line = 0
  sentences = 0
  open = 0
}

function flush() {
  if (open) {
    sentences++
  }
  if (sentences > max_sentences) {
    printf "%s:%d:%d: Readability::ProseParagraphLength: Paragraph of %d sentences. Split it so each paragraph carries one topic.\n", start_file, start_line, start_col, sentences
  }
  sentences = 0
  open = 0
}

function prose(text,   rest, pos, tok, col) {
  if (FNR != last_line + 1 || text ~ /^[ \t]*$/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)]|#+)([ \t]|$)/ || text ~ /^[ \t]*[|<]/) {
    flush()
  }
  last_line = FNR
  if (text ~ /^[ \t]*(#+([ \t]|$)|[|<])/) {
    return
  }
  if (!open && sentences == 0) {
    text = blank(text, "^[ \t]*(([-*+]|[0-9]+[.)])[ \t]+)?(\\*\\*[^*]+[.:]\\*\\*|__[^_]+[.:]__)")
  }
  rest = text
  pos = 0
  while (match(rest, /[^ \t]+/)) {
    tok = substr(rest, RSTART, RLENGTH)
    col = pos + RSTART
    pos += RSTART + RLENGTH - 1
    rest = substr(rest, RSTART + RLENGTH)
    if (tok ~ /^([-*+]|[0-9]+[.)])$/ && !open && sentences == 0) {
      continue
    }
    if (tok !~ /[A-Za-z]/) {
      continue
    }
    if (!open && sentences == 0) {
      start_file = FILENAME
      start_line = FNR
      start_col = column(text, col)
    }
    open = 1
    if (tok ~ /[.?!]["')*_\]]*$/ && tok !~ /\.\.\.["')*_\]]*$/ && tolower(tok) !~ /^[("'*_]*(e\.g|i\.e|vs|cf)\.$/) {
      sentences++
      open = 0
    }
  }
}

END {
  flush()
}
