# Reports a long parenthetical in the middle of a sentence in markdown
# prose. A parenthetical is an aside set off in parentheses (`the pool
# (which the scheduler sizes once at startup from the node count) holds
# the workers`). The reader has to hold the sentence open across the
# aside, so a long one reads better as a sentence of its own.
#
# The check counts the words between the parentheses after the reader
# blanks code spans. It reports an aside of eight or more words when a
# lowercase word follows it, a sign that the sentence goes on. An aside
# at the end of a sentence passes. Each part of a hyphenated word
# counts, the way the sentence-length check counts it.
# lib/sentences.awk finds the sentence boundaries. Each finding names
# the line and column of the opening paren.
#
# It hit 163 lines in the measure.py corpus, and 19 of 20 sampled hits
# held the sentence open across an aside of a full clause or a list.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Readability::ProseLongParenthetical: <message>

@include "sentences.awk"

function BEGIN_CHECK() {
  min_words = 8
}

function sentence(n,   i, j, at, t, depth, words, parts, m, k) {
  for (i = 1; i <= n; i++) {
    at = index(tok[i], "(")
    if (!at) {
      continue
    }
    depth = 0
    words = 0
    for (j = i; j <= n; j++) {
      t = (j == i) ? substr(tok[j], at) : tok[j]
      m = split(t, parts, "-")
      for (k = 1; k <= m; k++) {
        if (parts[k] ~ /[A-Za-z]/) {
          words++
        }
      }
      depth += gsub(/\(/, "(", t) - gsub(/\)/, ")", t)
      if (depth <= 0) {
        break
      }
    }
    if (j < n && words >= min_words && tok[j + 1] ~ /^[^A-Za-z]*[a-z]/) {
      printf "%s:%d:%d: Readability::ProseLongParenthetical: Long parenthetical of %d words. Move the aside to a sentence of its own.\n", sentence_file, tok_line[i], tok_col[i] + at - 1, words
    }
    i = j
  }
}
