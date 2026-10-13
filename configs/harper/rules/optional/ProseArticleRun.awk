# Reports the second of two sentences in a row that open with `a` or
# `an` in one markdown paragraph (`A task is a unit of work. A task runs
# on one worker.`). The singular `a task` reads two ways, as one
# particular task or as any task, and a run of such openers reads like
# a glossary. The prose skill writes a claim about a whole class with a
# plural subject (`Tasks run on one worker`) or with `every`, and keeps
# `a` and `an` for one instance that the sentence introduces. A run
# reports once however long it grows.
#
# The paragraph and sentence boundaries follow ProseParagraphLength,
# run-in labels and ellipses included. Sentences that open with a code
# span open with the first word after it, because the reader blanks
# the span. Each finding names the line and column of the second
# sentence's first word.
#
# Human-written docs rarely trip the check. Across about 34,000
# sentences of the kubernetes, nix.dev, Rust book, and Hugo docs, it
# fired 0.0 to 0.2 times per 100 sentences, and about 3 times per 100
# in the prose skill and these rule comments.
#
# Output shape, one line per finding:
#   <file>:<line>:<col>: Style::ProseArticleRun: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  last_line = 0
  reset()
}

function reset() {
  run = 0
  opening = 1
  fresh = 1
}

function prose(text,   rest, pos, tok, col, word) {
  if (FNR != last_line + 1 || text ~ /^[ \t]*$/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)]|#+)([ \t]|$)/ || text ~ /^[ \t]*[|<]/) {
    reset()
  }
  last_line = FNR
  if (text ~ /^[ \t]*(#+([ \t]|$)|[|<])/) {
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
    if (tok !~ /[A-Za-z]/) {
      continue
    }
    fresh = 0
    if (opening) {
      opening = 0
      word = tolower(tok)
      gsub(/^[^a-z]+|[^a-z]+$/, "", word)
      if (word == "a" || word == "an") {
        run++
        if (run == 2) {
          printf "%s:%d:%d: Style::ProseArticleRun: Second sentence in a row that opens with a or an. Write a claim about a whole class with a plural subject or with every.\n", FILENAME, FNR, column(text, col)
        }
      } else {
        run = 0
      }
    }
    if (tok ~ /[.?!]["')*_\]]*$/ && tok !~ /\.\.\.["')*_\]]*$/ && tolower(tok) !~ /^[("'*_]*(e\.g|i\.e|vs|cf)\.$/) {
      opening = 1
    }
  }
}
