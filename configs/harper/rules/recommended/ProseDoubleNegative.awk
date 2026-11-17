# Overview
#
# A double negative puts two negative words in one clause, a subject and
# its verb, such as `the tests pass`. The reader must cancel the two to
# find the claim (`the hook does not run unless the caller holds the
# lock`). `unless` means `if not`, so it counts as the second negative.
# The fix writes the positive form, which states the claim directly
# (`the hook runs only when the caller holds the lock`). The rule sits
# at Recommended, because a `without` that describes the noun before it
# also matches.
#
# Expression
#
# The check includes lib/sentences.awk, which finds the sentence
# boundaries, and reads each sentence of markdown prose. The negative
# words are `not`, `never`, `no`, `cannot`, `none`, `nothing`,
# `neither`, `nor`, `nobody`, and every word that ends in `n't`. The
# check reads a curly apostrophe inside a word as a straight one, so
# `can’t` counts the same as `can't`. prose-lint straightens the
# apostrophe in the copy that Harper reads, but this check reads the
# original markdown file or commit message. The check reports three
# forms:
#
# - `unless` with a negative word in the eight words before it. The scan
#   stops at `and`, `but`, `so`, `or`, and `yet`, which open a new
#   clause, and it skips the words inside parentheses, whose negation
#   stays inside them. It also stops at a word that ends in a period, a
#   question mark, or an exclamation mark and a closing quote (`says
#   “Never!” unless the lock is held`). The quotation holds a clause of
#   its own. The scan also stops at the closing quote of a quotation
#   that opens the sentence, such as the text of a sign (`"Do not
#   disturb" stays on the door unless the room is free`). The negation
#   of that quotation stays inside it. A sentence that opens with `Do
#   not`, `Don't`, or `Never` negates every step in it, so any `unless`
#   outside parentheses counts, at any distance. The opener does not
#   count when it sits inside a quotation that opens the sentence and
#   closes before the `unless` (`"Don't panic!" appears in the log
#   unless the run fails`). quoted_opening() in lib/sentences.awk finds
#   that quotation. In single quotes, the closing quote is the same mark
#   as an apostrophe. quoted_opening() can therefore end a quotation
#   early at a word inside it that ends in an apostrophe, and an
#   `unless` after that word passes. A word with a dropped letter ends
#   the quotation (`‘Never stop goin’ unless the build fails’`). So does
#   a plural possessive with a punctuation mark after it (`‘Never copy
#   the users’, the admins’, and the guests’ files unless the disk
#   fills’`). A plural possessive with no mark after it ends the
#   quotation when the quotation closes at another word that ends in `s`
#   and the quote (`‘Never run the users’ jobs unless the build passes’
#   is the rule`). The opener also does not count for an `unless` in the
#   second part of a quotation that an attribution splits (`"Never," the
#   guide says, "deploy unless the tests pass."`). The scan from that
#   `unless` stops at the first closing quote, so a negative word in the
#   first part does not count either (`"Copy nothing," the guide says,
#   "unless the tests pass."`).
# - `without` or `except` with a negative word in the six words before
#   it (`cannot start without a certificate`). The scan stops at a word
#   that opens a clause (`and`, `because`, `which`). It also stops at a
#   word that ends in a comma, a colon, a semicolon, a period, a
#   question mark, an exclamation mark, or `)`. A straight or a curly
#   closing quote can follow the mark (`said “not now,” then ran
#   without a lock`). A question mark or an exclamation mark stays
#   inside a sentence only before a closing quote (`said “Not now!”
#   then ran without a lock`).
# - `not` before a word with a negative prefix from a list (`not
#   unlike`, `not unsafe`). Here the positive form also makes the claim
#   stronger (`not unsafe` becomes `safe`), so the writer must settle
#   which claim they meant. The branch skips a `not` that ends in a
#   comma, a colon, a semicolon, a period, a question mark, or an
#   exclamation mark. The mark closes the clause of `not`. A straight
#   or a curly closing quote can follow the mark (`answers “not,”
#   unlike the old guide`).
#
# False Positives
#
# `no matter` is not a negation, so it stays out. The prefix list leaves
# out `unknown`, which names a value (`means zero, not unknown`).
#
# A `without` that names a property of the noun before it (`nodes
# without swap`) still fires, because a word list cannot tell it from
# `cannot start without a certificate`.
#
# Related Rules
#
# ProseNotOnly reports `not only` and `not just` and their contracted
# forms (`doesn't just`), so neither `not` nor such a contraction counts
# as a negative word before `only` or `just`. The `short` pattern holds
# the contractions of the `short` list in ProseNotOnly. Harper reads
# `doesn’t just` with a straight apostrophe, so ProseNotOnly reports it,
# and the `short` pattern matches it after word() straightens the
# apostrophe. `can't` stays out of both, because ProseNotOnly passes
# `can't just`, so `can't just run make without the docs` still counts.
#
# The built-in LitotesDirectPositive reports `not` before `unusual`,
# `unreasonable`, `unlikely`, `unimportant`, `infrequent`, `inaccurate`,
# `irrelevant`, `unclear`, `uncommon`, `insignificant`, and
# `impossible`, so the prefix list leaves those out.
#
# Measurement
#
# The rule hit 55 lines in the measure.py corpus, and 20 of 20 sampled
# hits stacked two negatives.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseDoubleNegative: <message>

@include "sentences.awk"

function BEGIN_CHECK() {
  neg = "^(not|never|no|cannot|none|nothing|neither|nor|nobody)$"
  boundary = "^(unless|without|except|but|and|or|nor|yet|so|because|if|when|while|since|although|though|whereas|until|which|that)$"
  short = "^(don't|doesn't|didn't|isn't|aren't|wasn't|weren't|couldn't|won't|wouldn't|shouldn't|mustn't|hasn't|haven't|hadn't|needn't)$"
  un = "^(unlike|unexpected|unnecessary|unsafe|invalid|incorrect|inconsistent|insecure|improbable|unaware|uncommonly|unreasonably|unusually|infrequently)$"
  msg = "Double negative. State the positive case, so `does not run unless the caller holds the lock` becomes `runs only when the caller holds the lock`."
}

# Returns token i in lowercase without the punctuation around it. A
# curly apostrophe inside the word then becomes a straight one, so
# `can’t` reads as `can't`. The strip runs first, because it keeps a
# straight `'`. In the other order, the closing quote of `‘not’`
# becomes a straight `'` that the strip keeps, and the word reads as
# `not'`. The pattern names the curly apostrophe by its UTF-8 bytes, as
# closes_clause() does.
function word(i,   w) {
  w = tolower(tok[i])
  gsub(/^[^a-z']+|[^a-z']+$/, "", w)
  gsub(/\342\200\231/, "'", w)
  return w
}

# Reports whether token i is a negative word.
function negation(i,   w) {
  w = word(i)
  if (w == "no" && word(i + 1) == "matter") {
    return 0
  }
  if ((w == "not" || w ~ short) && (word(i + 1) == "only" || word(i + 1) == "just")) {
    return 0
  }
  return w ~ neg || w ~ /n't$/
}

# Reports whether token i ends in a comma, a colon, a semicolon, a
# period, a question mark, or an exclamation mark, which closes a
# clause. A straight or a curly closing quote can follow the mark.
# lib/sentences.awk ends the sentence at a question mark or an
# exclamation mark without a closing quote. The scans therefore meet
# those two marks only before a quote that closes a quotation inside the
# sentence (`said "Not now!" then ran`). The pattern names each curly
# quote by its UTF-8 bytes, as ends_sentence() in lib/markdown.awk does.
function closes_clause(i) {
  return tok[i] ~ /[,;:.?!](["')*_\]]|\342\200\235|\342\200\231)*$/
}

function report(i) {
  printf "%s:%d:%d: Style::ProseDoubleNegative: %s\n", sentence_file, tok_line[i], tok_col[i], msg
}

function sentence(n,   i, j, w, d, t, opener, quoted, depth_at) {
  opener = (word(1) == "don't" || word(1) == "never" || (word(1) == "do" && word(2) == "not"))
  # An opener inside a quotation that opens the sentence negates only
  # the steps inside the quotation (`"Don't panic!" appears unless`).
  quoted = quoted_opening(n)
  d = 0
  for (i = 1; i <= n; i++) {
    depth_at[i] = d
    t = tok[i]
    d += gsub(/\(/, "(", t) - gsub(/\)/, ")", t)
  }
  for (i = 1; i <= n; i++) {
    w = word(i)
    if (w == "unless") {
      if (opener && depth_at[i] == 0 && (!quoted || i < quoted)) {
        report(i)
        return
      }
      for (j = i - 1; j >= 1 && j >= i - 8; j--) {
        # A quotation that closes with a mark holds a clause of its own,
        # so its negation stays inside the quotation (`says "Never!"
        # unless`). So does the negation of a quotation that opens the
        # sentence (`"Do not disturb" stays on the door unless`).
        if (ends_sentence(tok[j]) == 2 || j == quoted) {
          break
        }
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
        # A negative word before the end of a clause belongs to that
        # clause, so the scan stops at a word j that closes one.
        if (closes_clause(j) || tok[j] ~ /\)$/) {
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
    } else if (w == "not" && word(i + 1) ~ un && !closes_clause(i)) {
      report(i)
      return
    }
  }
}
