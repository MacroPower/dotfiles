# Reports a sentence of more than 32 words in markdown prose and in
# source comments. The technical-writing skill splits a sentence past
# about 25 words, and 3.8% of the sentences in the measure.py docs run
# past 32. The Optional rule of the same name reports a sentence of 26
# to 32 words that joins two clauses, so one sentence never draws both
# findings. A clause is a subject and its verb, such as `the tests
# pass`.
#
# lib/sentences.awk finds the sentence boundaries and counts the words.
# Each part of a hyphenated word counts (`read-only` is two words), and
# a token with no letter (`42`, `|`) counts as none. The reader blanks
# code spans, so a span counts as none either. Each finding names the
# line and column where the sentence starts.
#
# The message names where to split, from the first of these steps that
# fits the sentence:
#
# 1. A comma, then `and`, `but`, `so`, or `yet`, then a word that opens
#    a clause, such as `the`, `it`, or `they`. The message names that
#    split point. It says to drop an `and`, and to open the next
#    sentence with a `but`, `so`, or `yet`. A comma among the four
#    tokens before the comma marks a serial list (`futures, streams, and
#    the syntax`), and `, so that` states a purpose, so neither counts. The step skips a
#    sentence that opens with a condition or a cause (`If`, `When`,
#    `Because`), also after `However,` or `For example,`. Its first
#    comma usually closes the condition, not a thought.
# 2. A comma, then `which`, `who`, or `where`. The message asks for a
#    sentence that opens with the noun that the word stands for. A
#    sentence that opens with `This` instead draws the broad-reference
#    rule.
# 3. Neither. The message asks for a split between two thoughts, in the
#    words of the technical-writing skill.
#
# It hit 1,890 lines in the measure.py corpus, and 18 of 20 sampled hits
# read better as two sentences. The first step named a split point on
# 140 lines, and 15 of 20 sampled points were the right place to split.
# Its misses were serial lists with long items, a `so` that states a
# purpose, and splitter errors at Hugo shortcodes and curly quotes. The
# second step named a clause on 157 lines, and 19 of 20 sampled clauses
# read well as a sentence of their own.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Readability::ProseSentenceLength: <message>

@include "sentences.awk"

function BEGIN_CHECK() {
  min_words = 33
  limit = 32
  subjects = "^(the|this|these|those|it|they|we|you|i|he|she|there)$"
  subordinators = "^(if|when|whenever|unless|although|though|because|since|while|whereas|once)$"
}

# Returns token i in lowercase without the punctuation around it.
function word(i,   w) {
  w = tolower(tok[i])
  gsub(/^[^a-z']+|[^a-z']+$/, "", w)
  return w
}

# Reports whether the sentence opens with a condition or a cause, after
# at most one lead-in such as `However,` or `For example,`.
function opens_subordinate(   f) {
  f = words_from
  if (word(f) ~ subordinators) {
    return 1
  }
  if (tok[f] ~ /,$/ && word(f + 1) ~ subordinators) {
    return 1
  }
  return word(f) == "for" && word(f + 1) ~ /^(example|instance)$/ && word(f + 2) ~ subordinators
}

# Reports whether a comma sits among the four tokens before token i,
# the sign of a serial list (`futures, streams, and the syntax`).
function serial_list(i,   j) {
  for (j = i - 1; j >= i - 4 && j >= 1; j--) {
    if (tok[j] ~ /,$/) {
      return 1
    }
  }
  return 0
}

function advice(n,   i, c) {
  if (!opens_subordinate()) {
    for (i = 2; i < n - 1; i++) {
      if (tok[i] ~ /,$/ && word(i + 1) ~ /^(and|but|so|yet)$/ && word(i + 2) ~ subjects && !serial_list(i) && !(word(i + 1) == "so" && word(i + 2) == "that")) {
        c = word(i + 1)
        if (c == "and") {
          return sprintf("End the first sentence before `and %s`, and drop the `and`.", word(i + 2))
        }
        return sprintf("End the first sentence before `%s %s`, and open the next one with `%s`.", c, word(i + 2), toupper(substr(c, 1, 1)) substr(c, 2))
      }
    }
  }
  for (i = 2; i < n; i++) {
    if (tok[i] ~ /,$/ && word(i + 1) ~ /^(which|who|where)$/) {
      return sprintf("Give the `, %s` clause a sentence of its own, and open it with the noun that `%s` stands for.", word(i + 1), word(i + 1))
    }
  }
  return "Split it between two thoughts, and carry the relation into the next sentence with `so`, `but`, or `then`."
}

function sentence(n,   words) {
  words = sentence_words(n)
  if (words >= min_words) {
    printf "%s:%d:%d: Readability::ProseSentenceLength: Sentence of %d words, past the %d-word limit. %s\n", sentence_file, tok_line[words_from], tok_col[words_from], words, limit, advice(n)
  }
}
