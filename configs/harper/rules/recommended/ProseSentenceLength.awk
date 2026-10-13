# Reports a sentence of more than 32 words in markdown prose and in
# source comments. The technical-writing skill splits a sentence past
# about 25 words, and 3.8% of the sentences in the measure.py docs run
# past 32. The Optional rule of the same name reports 26 to 32 words,
# so one sentence never draws both findings.
#
# lib/sentences.awk finds the sentence boundaries and counts the words.
# Each part of a hyphenated word counts (`read-only` is two words), and
# a token with no letter (`42`, `|`) counts as none. The reader blanks
# code spans, so a span counts as none either. Each finding names the
# line and column where the sentence starts.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Readability::ProseSentenceLength: <message>

@include "sentences.awk"

function BEGIN_CHECK() {
  min_words = 33
}

function sentence(n,   words) {
  words = sentence_words(n)
  if (words >= min_words) {
    printf "%s:%d:%d: Readability::ProseSentenceLength: Sentence of %d words. Split it so each sentence carries one thought.\n", sentence_file, tok_line[words_from], tok_col[words_from], words
  }
}
