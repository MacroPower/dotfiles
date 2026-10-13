# Reports a sentence that opens with `a` or `an` and makes a claim
# about a whole class in markdown prose (`A reader opens one rule file at
# a time`). Grammar calls `a` and `an` the indefinite article, because
# each introduces a noun without pointing at a particular one, and calls
# `the` the definite article. The singular `a reader` reads two ways, as
# one particular reader or as any reader, so the sentence is harder to
# read than its plural (`Readers open one rule file at a time`). The
# prose skill writes a claim about a whole class with a plural subject
# or with `every`, and keeps `a` and `an` for one instance that the
# sentence introduces.
#
# The check reports two forms. The first is an opening `a` or `an` with
# a present-tense verb in the next four words (`A task is`, `A failed
# task can retry`). Awk has no part-of-speech tagger, so a lowercase
# word that ends in `s` counts as the verb, and so do `is`, `has`,
# `can`, and the other helping verbs in `helpers`. The scan stops at a
# comma and at a preposition or conjunction such as `of`, `with`, or
# `and`, so `A list of tasks` passes. Past-tense verbs do not end in
# `s`, so `A user reported the bug` passes, and so does a quantity after
# the article (`A few seconds later`).
#
# A sentence that introduces one instance takes the same form (`A cache
# sits in front of the store`), and a later sentence names it with `the`
# (`The cache holds the last hundred reads`). The check holds each
# finding until the paragraph ends and drops it when the text after it
# names the noun with `the`.
#
# The second form is the second of two sentences in a row that open with
# `a` or `an` in one paragraph (`A task is a unit of work. A task runs
# on one worker.`), which catches openers the verb test misses. Each
# run reports once however long it grows, and its later sentences skip
# the verb test.
#
# The paragraph and sentence boundaries follow ProseParagraphLength,
# run-in labels and ellipses included. Sentences that open with a code
# span open with the first word after it, because the reader blanks
# the span. Each finding names the line and column of the sentence's
# first word.
#
# Across about 24,500 sentences of the kubernetes, nix.dev, Rust book,
# and Hugo docs, the verb test fired 1.6 times per 100 sentences and the
# run test 0.1 times. In 38 of 40 sampled verb-test hits, the sentence
# made a claim about a whole class. The two misses introduced one thing
# that the paragraph never named again (`A JSON schema is available for
# YAML language servers`).
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseIndefiniteOpener: <message>

@include "markdown.awk"

function BEGIN_CHECK() {
  last_line = 0
  last_file = ""
  held = 0
  para = ""
  quantity = "^(few|couple|lot|number|bit|little|series|set|list|pair|total|variety|range|handful|dozen|hundred|thousand|majority|minority|while|year|day|week|month|minute|second|moment|time|single)$"
  stops = "^(of|for|with|in|on|at|to|from|by|that|which|who|whose|where|when|if|and|or|but|as|than|like|about|into|over|under|between|per|without|within|was|were|had|did|could|would|are)$"
  helpers = "^(is|has|does|can|cannot|must|should|may|might|will|needs)$"
  not_verbs = "^(always|sometimes|perhaps|afterwards|towards|besides|unless|thus|whereas|across|plus|its|yes|was|news)$"
  reset()
}

function reset() {
  flush()
  run = 0
  opening = 1
  fresh = 1
  scan = 0
}

# Prints each held finding unless the text after it in the paragraph
# names its noun with `the`, which marks one instance the sentence
# introduced.
function flush(   i) {
  for (i = 1; i <= held; i++) {
    if (tolower(substr(para, held_at[i])) !~ ("(^|[^a-z])the[ \t]+" held_noun[i] "([^a-z]|$)")) {
      printf "%s:%d:%d: Style::ProseIndefiniteOpener: Indefinite opener. A or an opens a sentence before a present-tense verb. Write a claim about a whole class with a plural subject or with every.\n", held_file[i], held_line[i], held_col[i]
    }
  }
  held = 0
  para = ""
}

function verb(tok, word) {
  if (word ~ helpers) {
    return 1
  }
  if (tok !~ /^[^A-Za-z]*[a-z]/ || word !~ /^[a-z]+s$/ || word ~ not_verbs) {
    return 0
  }
  return word !~ /(ss|us|is|ics|ous|ness)$/
}

function prose(text,   rest, pos, tok, col, word, base) {
  if (FILENAME != last_file || FNR != last_line + 1 || text ~ /^[ \t]*$/ || text ~ /^[ \t]*([-*+]|[0-9]+[.)]|#+)([ \t]|$)/ || text ~ /^[ \t]*[|<]/) {
    reset()
  }
  last_file = FILENAME
  last_line = FNR
  if (text ~ /^[ \t]*(#+([ \t]|$)|[|<])/) {
    return
  }
  if (fresh) {
    text = blank(text, "^[ \t]*(([-*+]|[0-9]+[.)])[ \t]+)?(\\*\\*[^*]+[.:]\\*\\*|__[^_]+[.:]__)")
  }
  base = length(para) + 1
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
    word = tolower(tok)
    gsub(/^[^a-z]+|[^a-z]+$/, "", word)
    if (opening) {
      opening = 0
      if (word == "a" || word == "an") {
        run++
        if (run == 2) {
          printf "%s:%d:%d: Style::ProseIndefiniteOpener: Indefinite opener. A or an opens two sentences in a row. Write a claim about a whole class with a plural subject or with every.\n", FILENAME, FNR, column(text, col)
        } else if (run == 1) {
          scan = 1
          scan_line = FNR
          scan_col = column(text, col)
        }
      } else {
        run = 0
      }
    } else if (scan) {
      if (scan == 1 && word ~ quantity) {
        scan = 0
      } else if (scan >= 2 && verb(tok, word)) {
        held++
        held_file[held] = FILENAME
        held_line[held] = scan_line
        held_col[held] = scan_col
        held_noun[held] = noun
        held_at[held] = base + pos
        scan = 0
      } else if (scan >= 2 && word ~ stops || scan >= 4) {
        scan = 0
      } else {
        noun = word
        sub(/'.*$/, "", noun)
        scan++
      }
      if (scan && tok ~ /[,;:()]["')*_\]]*$/) {
        scan = 0
      }
    }
    if (tok ~ /[.?!]["')*_\]]*$/ && tok !~ /\.\.\.["')*_\]]*$/ && tolower(tok) !~ /^[("'*_]*(e\.g|i\.e|vs|cf)\.$/) {
      opening = 1
      scan = 0
    }
  }
  para = para " " text
}

END {
  reset()
}
