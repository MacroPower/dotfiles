# Overview
#
# Reports a paragraph of more than six sentences in markdown prose. The
# technical-writing skill keeps a paragraph to one topic and six
# sentences or fewer, and a longer one usually carries a second topic. A
# long paragraph makes the reader find where one topic ends and the next
# one starts. The fix splits the paragraph, so each paragraph carries
# one topic. The rule sits at Recommended, because a paragraph of more
# than six sentences can also keep to one topic.
#
# Expression
#
# The check includes lib/markdown.awk and reads one paragraph at a time.
# The paragraph boundaries follow ProseSentenceLength. A paragraph ends
# at a line that lib/markdown.awk skips or marks in `bare_line`, such as
# a bare `>` between two quoted paragraphs. A line that holds only a
# code span or a URL continues the paragraph. A paragraph also ends
# where a list item, heading, table row, or HTML line starts, so each
# list item counts as its own paragraph.
#
# A pipe table without a leading pipe reaches the check as text. A
# paragraph that holds a line with two pipes, or a delimiter row such as
# `:--|:--`, counts as a table and does not draw a finding. A table row
# that opens with a pipe ends the paragraph before it and opens a new
# one. Its two pipes mark the new paragraph as a table. The rows without
# a leading pipe after a delimiter row such as `|:--|:--|` join that
# paragraph and do not draw a finding. The guard also passes a long
# paragraph of prose that holds two pipes outside a code span.
#
# Sentences end at a `.`, `?`, or `!` before whitespace or the line end,
# after any closing quote, paren, bracket, or emphasis. A closing quote
# is straight or curly (`"`, `'`, `”`, or `’`). `e.g.`, `i.e.`, `vs.`,
# and `cf.` do not end a sentence, and neither does a three-period
# ellipsis (`an implicit "About..." in front`). A lowercase word after a
# closing quote continues the sentence, because the quote closed a
# quotation inside it (`prints "Lucky penny!" every time`).
# ends_sentence() and continues_sentence() in lib/markdown.awk hold
# these tests. Words after the last sentence end count as one more
# sentence.
#
# lib/markdown.awk blanks code spans, link destinations, and bare URLs
# to spaces. The period after a span, a link, or a URL then stands apart
# from the words. A `.`, `?`, or `!` that stands apart ends the sentence
# in the same way, and so does one after a number (`It defaults to 1.`).
# A sentence counts only when it holds a word, so a code span with a
# period after it is not a sentence of its own.
#
# A run-in label in bold that ends in a period or a colon names the
# paragraph's topic (`**Tutorial.** The learner's success`). Such a
# label counts as no sentence when it opens the paragraph. Each finding
# names the line and column where the paragraph starts.
#
# False Positives
#
# An abbreviation that ends in a period counts as a sentence end when
# it sits inside a sentence (`a TERM (aka. SIGTERM) signal`, `logs,
# metrics, etc. in one place`). A paragraph of six sentences that holds
# one such abbreviation therefore draws a finding.
#
# Measurement
#
# The rule hit 69 lines in the measure.py docs corpus, and 13 of 19
# sampled hits carried a second topic or a hidden list. Five misfires
# kept to one topic, and one counted the closing tag of a Hugo
# shortcode (`{{< /note >}}`) as a sentence.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Readability::ProseParagraphLength: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  max_sentences = 6
  last_line = 0
  sentences = 0
  open = 0
  table = 0
}

function flush() {
  if (open) {
    sentences++
  }
  if (sentences > max_sentences && !table) {
    printf "%s:%d:%d: Readability::ProseParagraphLength: Paragraph of %d sentences. Split it so each paragraph carries one topic.\n", start_file, start_line, start_col, sentences
  }
  sentences = 0
  open = 0
  table = 0
  quote_end = 0
}

function prose(text,   rest, pos, tok, col, e) {
  if (FNR != last_line + 1 || bare_line || text ~ /^[ \t]*([-*+]|[0-9]+[.)]|#+)([ \t]|$)/ || text ~ /^[ \t]*[|<]/) {
    flush()
  }
  last_line = FNR
  if (text ~ /\|.*\|/ || text ~ /^[ \t]*:?-+:?[ \t]*\|/) {
    table = 1
  }
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
    # The token before ended in a mark and a closing quote. The sentence
    # ended there unless this token continues it.
    if (quote_end && !continues_sentence(tok)) {
      sentences++
      open = 0
    }
    quote_end = 0
    if (tok ~ /[A-Za-z]/) {
      if (!open && sentences == 0) {
        start_file = FILENAME
        start_line = FNR
        start_col = column(col)
      }
      open = 1
    }
    e = open ? ends_sentence(tok) : 0
    if (e == 1) {
      sentences++
      open = 0
    } else if (e == 2) {
      quote_end = 1
    }
  }
}

END {
  flush()
}
