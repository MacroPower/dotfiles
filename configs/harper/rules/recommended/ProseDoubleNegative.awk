# A double negative puts two negative words in one clause, a subject
# and its verb such as `the tests pass`. The reader has to cancel the
# two to find the claim (`the hook does not run unless the caller holds
# the lock`). `unless` means `if not`, so it counts as the second
# negative. The positive form states the claim directly (`the hook runs
# only when the caller holds the lock`).
#
# The check reads each sentence of markdown prose. The negative words
# are `not`, `never`, `no`, `cannot`, `none`, `nothing`, `neither`,
# `nor`, `nobody`, and every word that ends in `n't`. `no matter` opens
# no negation, and the not-only rule reports `not only` and `not just`,
# so all three stay out. The check reports three forms:
#
# - `unless` with a negative word in the eight words before it. The
#   scan stops at `and`, `but`, `so`, `or`, and `yet`, which open a new
#   clause, and it skips the words inside parentheses, whose negation
#   stays inside them. A sentence that opens with `Do not`, `Don't`, or
#   `Never` negates every step in it, so any `unless` outside
#   parentheses counts, at any distance.
# - `without` or `except` with a negative word in the six words before
#   it (`cannot start without a certificate`). The scan stops at a word
#   that opens a clause (`and`, `because`, `which`). It also stops at a
#   word that ends in a comma, a colon, a semicolon, a period, or `)`.
# - `not` before a word with a negative prefix from a list (`not
#   unlike`, `not unsafe`). Here the positive form also makes the claim
#   stronger (`not unsafe` becomes `safe`), so the writer has to settle
#   which claim they meant. The built-in LitotesDirectPositive reports
#   `not` before `unusual`, `unreasonable`, `unlikely`, `unimportant`,
#   `infrequent`, `inaccurate`, `irrelevant`, `unclear`, `uncommon`,
#   `insignificant`, and `impossible`, so the list leaves those out. It
#   also leaves out `unknown`, which names a value (`means zero, not
#   unknown`).
#
# A `without` that names a property of the noun before it (`nodes
# without swap`) still fires, because a word list cannot tell it from
# `cannot start without a certificate`. lib/sentences.awk finds the
# sentence boundaries.
#
# It hit 50 lines in the measure.py corpus, and 20 of 20 sampled hits
# stacked two negatives.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseDoubleNegative: <message>

@include "sentences.awk"

function BEGIN_CHECK() {
  neg = "^(not|never|no|cannot|none|nothing|neither|nor|nobody)$"
  boundary = "^(unless|without|except|but|and|or|nor|yet|so|because|if|when|while|since|although|though|whereas|until|which|that)$"
  un = "^(unlike|unexpected|unnecessary|unsafe|invalid|incorrect|inconsistent|insecure|improbable|unaware|uncommonly|unreasonably|unusually|infrequently)$"
  msg = "Double negative. State the positive case, so `does not run unless the caller holds the lock` becomes `runs only when the caller holds the lock`."
}

# Returns token i in lowercase without the punctuation around it.
function word(i,   w) {
  w = tolower(tok[i])
  gsub(/^[^a-z']+|[^a-z']+$/, "", w)
  return w
}

# Reports whether token i is a negative word.
function negation(i,   w) {
  w = word(i)
  if (w == "no" && word(i + 1) == "matter") {
    return 0
  }
  if (w == "not" && (word(i + 1) == "only" || word(i + 1) == "just")) {
    return 0
  }
  return w ~ neg || w ~ /n't$/
}

function report(i) {
  printf "%s:%d:%d: Style::ProseDoubleNegative: %s\n", sentence_file, tok_line[i], tok_col[i], msg
}

function sentence(n,   i, j, w, d, t, opener, depth_at) {
  opener = (word(1) == "don't" || word(1) == "never" || (word(1) == "do" && word(2) == "not"))
  d = 0
  for (i = 1; i <= n; i++) {
    depth_at[i] = d
    t = tok[i]
    d += gsub(/\(/, "(", t) - gsub(/\)/, ")", t)
  }
  for (i = 1; i <= n; i++) {
    w = word(i)
    if (w == "unless") {
      if (opener && depth_at[i] == 0) {
        report(i)
        return
      }
      for (j = i - 1; j >= 1 && j >= i - 8; j--) {
        # Skip back over the words in parentheses, whose negation stays
        # inside them.
        if (tok[j] ~ /\)[,;:.]?$/ && tok[j] !~ /\(/) {
          while (j > 1 && tok[j] !~ /\(/) {
            j--
          }
          continue
        }
        if (negation(j)) {
          report(i)
          return
        }
        if (word(j) ~ /^(and|but|so|or|yet)$/) {
          break
        }
      }
    } else if (w == "without" || w == "except") {
      for (j = i - 1; j >= 1 && j >= i - 6; j--) {
        # A comma, colon, semicolon, or period after word j closes a
        # clause, and a negative word before it belongs to that clause.
        if (tok[j] ~ /[,;:.]["')*_\]]*$/ || tok[j] ~ /\)$/) {
          break
        }
        if (negation(j)) {
          report(i)
          return
        }
        if (word(j) ~ boundary) {
          break
        }
      }
    } else if (w == "not" && word(i + 1) ~ un && tok[i] !~ /[,;:.]$/) {
      report(i)
      return
    }
  }
}
