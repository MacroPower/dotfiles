# Overview
#
# Reports a paragraph of markdown prose that hides a list. The
# technical-writing skill names two signs. Sentences that walk through
# steps with `first`, `then`, and `finally` hide a numbered list.
# Consecutive sentences that open the same way hide a bulleted list or
# a table. Such a paragraph makes the reader pick the steps or the
# items out of its sentences. The fix moves the steps into a numbered
# list, and the items into a bulleted list or a table. The rule sits at
# Recommended, because a plan of two steps and a list that continues
# from an earlier paragraph also match.
#
# Expression
#
# The check includes lib/markdown.awk and reads one paragraph at a
# time. The paragraph and sentence boundaries follow
# ProseParagraphLength.
#
# The first form is two sentences in one paragraph that open with
# different sequence words, where at least one of them counts (`first`,
# `second`, `next`, `finally`).
#
# The second form is three sentences in a row that open with the same
# two words (`The node controller`). lib/markdown.awk blanks code
# spans, so `The --force flag skips` and `The --dry-run flag prints`
# both open with `The flag`.
#
# Each finding names the line and column where the paragraph starts.
#
# False Positives
#
# `then` alone narrates (`Then the pod restarts`), so two `then`
# sentences pass. `now` stays out, because it marks a turn in an
# argument (`Now say you have ten pods`).
#
# Related Rules
#
# Openings with `a` or `an` (`A pod`) pass, because
# ProseIndefiniteOpener reports those sentences and asks for a plural
# subject instead of a list.
#
# Measurement
#
# The rule hit 20 lines in the measure.py docs corpus, and 15 of 20
# sampled hits hid steps, parallel points, or cases. Three misfires
# were plans of two steps (`First, we'll... Then, we'll...`). One
# `Finally` continued a list from earlier paragraphs, and one run
# matched only after lib/markdown.awk blanked its code spans.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Readability::ProseHiddenList: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  last_line = 0
  reset()
  seq = "^(first|firstly|second|secondly|third|thirdly|fourth|then|next|finally|lastly|afterward|afterwards)$"
  counts = "^(first|firstly|second|secondly|third|thirdly|fourth|next|finally|lastly)$"
}

function reset() {
  sentences = 0
  opening = 1
  fresh = 1
  nseq = 0
  ordinal = 0
  distinct = ""
  run = 1
  best = 1
  prev_key = ""
  cur_words = 0
  cur_key = ""
  table = 0
}

function flush(   parts) {
  if (table) {
    reset()
    return
  }
  if (nseq >= 2 && ordinal && split(distinct, parts, " ") >= 2) {
    printf "%s:%d:%d: Readability::ProseHiddenList: Hidden list. Sentences that open with `first`, `then`, or `finally` walk through steps. Move the steps into a numbered list.\n", start_file, start_line, start_col
  } else if (best >= 3) {
    printf "%s:%d:%d: Readability::ProseHiddenList: Hidden list. %d sentences in a row open the same way. Move the items into a bulleted list, or into a table when they share attributes.\n", start_file, start_line, start_col, best
  }
  reset()
}

# Ends the current sentence and compares its opening to the last one.
function end_sentence() {
  # ProseIndefiniteOpener reports the sentences that open with `a` or
  # `an`, and its fix (a plural subject) differs from a list.
  if (cur_key ~ /^(a|an) /) {
    cur_key = ""
  }
  if (cur_key != "" && cur_key == prev_key) {
    run++
    if (run > best) {
      best = run
    }
  } else {
    run = 1
  }
  prev_key = cur_key
  cur_key = ""
  cur_words = 0
  opening = 1
}

function prose(text,   rest, pos, tok, col, word) {
  if (FILENAME != last_file || FNR != last_line + 1 || text ~ /^[ \t]*$/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)]|#+)([ \t]|$)/ || text ~ /^[ \t]*[|<]/) {
    flush()
  }
  last_file = FILENAME
  last_line = FNR
  # A pipe table without a leading pipe reaches prose() as text.
  if (text ~ /\|.*\|/ || text ~ /^[ \t]*:?-+:?[ \t]*\|/) {
    table = 1
  }
  if (text ~ /^[ \t]*(#+([ \t]|$)|[|<:])/) {
    return
  }
  if (fresh) {
    text = blank(text, "^[ \t]*(([-*+]|[0-9]+[.)])[ \t]+)?(\\*\\*[^*]+[.:]\\*\\*|__[^_]+[.:]__)")
  }
  rest = text
  pos = 0
  while (match(rest, /[^ \t]+/)) {
    tok = substr(rest, RSTART, RLENGTH)
    col = pos + RSTART
    pos += RSTART + RLENGTH - 1
    rest = substr(rest, RSTART + RLENGTH)
    if (fresh && tok ~ /^([-*+]|[0-9]+[.)])$/) {
      continue
    }
    if (tok !~ /[A-Za-z0-9]/) {
      continue
    }
    if (fresh) {
      start_file = FILENAME
      start_line = FNR
      start_col = column(text, col)
      fresh = 0
    }
    word = tolower(tok)
    gsub(/[^a-z0-9]/, "", word)
    if (opening && word ~ seq) {
      nseq++
      if (word ~ counts) {
        ordinal = 1
      }
      if (index(" " distinct " ", " " word " ") == 0) {
        distinct = distinct " " word
      }
    }
    opening = 0
    if (cur_words < 2 && word != "") {
      cur_key = cur_words ? cur_key " " word : word
      cur_words++
    }
    if (tok ~ /[.?!]["')*_\]]*$/ && tok !~ /\.\.\.["')*_\]]*$/ && tolower(tok) !~ /^[("'*_]*(e\.g|i\.e|vs|cf)\.$/) {
      if (cur_words < 2) {
        cur_key = ""
      }
      end_sentence()
    }
  }
}

END {
  flush()
}
