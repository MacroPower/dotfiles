# Reports a long parenthetical in the middle of a sentence in markdown
# prose. A parenthetical is an aside that interrupts a sentence. It sits
# in parentheses (`the pool (which the scheduler sizes once at startup
# from the node count) holds the workers`), or in commas after the
# subject (`The hook, which runs after every Write and Edit of a
# markdown file, tags each finding`). The reader has to hold the
# sentence open across the aside. A long aside reads better as a
# sentence of its own after this one. An aside between a subject and its
# verb costs the most, because the reader holds the subject until the
# verb comes.
#
# The check reports an aside of 10 or more words. It counts the words
# after the reader blanks code spans. Each part of a hyphenated word
# counts, the way the sentence-length check counts it. A Hugo shortcode
# (`{{< param "version" >}}`) counts as no words, so a link destination
# that holds one does not read as a long aside.
# lib/sentences.awk finds the sentence boundaries. Each finding names
# the line and column where the aside opens.
#
# A parenthesized aside counts when a lowercase word follows it, a sign
# that the sentence goes on. An aside at the end of a sentence passes.
#
# A comma aside needs a short subject at the start of the sentence, with
# its comma within five words. The subject opens with a determiner, a
# word that introduces a noun such as `The`, `Each`, or `Any`. It can
# also be a code span or a capitalized name, which needs `which` or
# `who` after the comma. The aside ends at the next comma, and a
# lowercase word after it must carry on the sentence. A joining word
# (`and`, `because`, `until`), a pronoun such as `it`, a determiner, or
# an `-ing` word there starts a list item or a new clause instead. The
# check skips such an aside. A clause is a subject and its verb, such as
# `the tests pass`. The check also skips an opener with a verb before
# the first comma (`The results were alarming,`) and a time or manner
# phrase (`This time,`).
#
# It hit 90 lines in the measure.py corpus, and 20 of 20 sampled hits
# held the sentence open across an aside of a clause or a list. Seven
# hits were comma asides. One of them was a stray comma between a
# subject and its verb (`This application written in Go, simplifies`).
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Readability::ProseLongParenthetical: <message>

@include "sentences.awk"

function BEGIN_CHECK() {
  min_words = 10
  dets = "^(the|a|an|this|that|these|those|each|every|its|our|your|their|any|all|some|most|no)$"
  verbs = "^(is|are|was|were|be|been|should|can|will|would|could|must|may|might|has|have|had|do|does|did|include|includes)$"
  phrases = "^(time|way|year|week|day|point|case|end|least|most)$"
  stops = "^(and|or|but|the|a|an|this|that|it|they|we|you|which|who|so|then|for|in|on|at|to|of|with|as|if|when|because|until|before|after|unless|although|though|since|while|once|whereas)$"
}

# Returns token i in lowercase without the punctuation around it.
function word(i,   w) {
  w = tolower(tok[i])
  gsub(/^[^a-z']+|[^a-z']+$/, "", w)
  return w
}

# Returns the number of words in token t.
function words_in(t,   m, parts, k, c) {
  c = 0
  m = split(t, parts, "-")
  for (k = 1; k <= m; k++) {
    if (parts[k] ~ /[A-Za-z]/) {
      c++
    }
  }
  return c
}

# Fills ct[] with each token minus any Hugo shortcode, which can run
# across several tokens (`{{<`, `param`, `"version"`, `>}}`).
function strip_shortcodes(n,   i, t, out, open) {
  open = 0
  for (i = 1; i <= n; i++) {
    t = tok[i]
    out = ""
    while (t != "") {
      if (open) {
        if (match(t, /[>%]\}\}/)) {
          t = substr(t, RSTART + RLENGTH)
          open = 0
        } else {
          t = ""
        }
      } else if (match(t, /\{\{[<%]/)) {
        out = out substr(t, 1, RSTART - 1)
        t = substr(t, RSTART + RLENGTH)
        open = 1
      } else {
        out = out t
        t = ""
      }
    }
    ct[i] = out
  }
}

function report(i, col, words) {
  if (words >= min_words) {
    printf "%s:%d:%d: Readability::ProseLongParenthetical: Long parenthetical of %d words. Move the aside to its own sentence after this one, and name its subject.\n", sentence_file, tok_line[i], col, words
  }
}

function sentence(n) {
  strip_shortcodes(n)
  parens(n)
  commas(n)
}

function parens(n,   i, j, at, t, depth, words) {
  for (i = 1; i <= n; i++) {
    at = index(ct[i], "(")
    if (!at) {
      continue
    }
    depth = 0
    words = 0
    for (j = i; j <= n; j++) {
      t = (j == i) ? substr(ct[j], at) : ct[j]
      words += words_in(t)
      depth += gsub(/\(/, "(", t) - gsub(/\)/, ")", t)
      if (depth <= 0) {
        break
      }
    }
    if (j < n && tok[j + 1] ~ /^[^A-Za-z]*[a-z]/) {
      report(i, tok_col[i] + index(tok[i], "(") - 1, words)
    }
    i = j
  }
}

function commas(n,   i, first, open, j, words) {
  first = 0
  for (i = 1; i <= n; i++) {
    if (tok[i] ~ /[A-Za-z]/) {
      first = i
      break
    }
  }
  if (!first) {
    return
  }
  open = 0
  if (tok[1] ~ /^[^A-Za-z]*,$/ || (first == 1 && tok[1] ~ /^[^A-Za-z]*[A-Z][A-Za-z0-9]*,$/)) {
    # A code span, which the reader blanks to a bare comma, or a name.
    if (word(2) ~ /^(which|who|whose)$/) {
      open = 1
    }
  } else if (tok[first] ~ /^[^A-Za-z]*[A-Z]/ && word(first) ~ dets && word(first + 1) !~ /^(more|less|fewer|longer)$/) {
    for (i = first + 1; i <= first + 4 && i <= n; i++) {
      if (tok[i] ~ /[.;:()]/ || word(i) ~ verbs) {
        return
      }
      if (tok[i] ~ /,$/) {
        open = i
        break
      }
    }
    if (!open || word(open) ~ phrases) {
      return
    }
  }
  if (!open || word(open + 1) ~ /^(and|or)$/) {
    return
  }
  words = 0
  for (j = open + 1; j <= n; j++) {
    if (tok[j] ~ /[;:.]$/ && j < n) {
      return
    }
    words += words_in(ct[j])
    if (tok[j] ~ /,$/) {
      break
    }
  }
  if (j >= n || tok[j + 1] !~ /^[^A-Za-z]*[a-z]/ || word(j + 1) ~ stops || word(j + 1) ~ /ing$/) {
    return
  }
  report(open, tok_col[open], words)
}
