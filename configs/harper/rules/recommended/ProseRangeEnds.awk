# Reports a range that does not say whether it includes its ends in
# markdown prose (`a weight between 1 and 100`). The reader cannot tell
# whether 1 and 100 are themselves valid. The fix adds `inclusive` or
# `exclusive`, or names each end (`from 1 up to but not including
# 100`).
#
# The check reports two forms, where each number opens with a digit.
# The first is `between`, a number, `and`, and a number. It skips
# `difference between` and `distinction between`, which compare two
# values (`the distinction between 1 and 2`). The second is `range`
# with a number pair in the next four words, joined by `to`, `through`,
# or a hyphen (`the range 0-65535`, `values range from 0.0 to 1.0`).
# A sentence that holds `inclusive` or `exclusive` passes, and so does a
# range of dates (`between 2022-11 and 2024-05`). `from N to M` without
# `range` stays out, because it often describes a change (`the replicas
# drop from 3 to 1`). lib/sentences.awk finds the sentence boundaries.
#
# A range that an estimate word opens passes (`typically between 0.5
# and 1.5`), because an estimate has no exact ends. The estimate words
# are `typically`, `usually`, `roughly`, `approximately`, and
# `generally` in the three words before `between`. `about` and `around`
# stay out, because they also open phrases such as `learn about values
# between 1 and 100`.
#
# It hit 29 lines in the measure.py docs, and 27 of 29 hits left the
# ends of a limit open (`a weight between 1 and 100`, `the range
# 0-65535`). The other 2 were ratios between 0 and 1, where the ends
# matter less.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseRangeEnds: <message>

@include "sentences.awk"

function BEGIN_CHECK() {
  message = "Range ends. Say whether the range includes each end, such as `between 1 and 100, inclusive`."
}

# Returns token i in lowercase without the punctuation around it.
function word(i,   w) {
  w = tolower(tok[i])
  gsub(/^[^a-z0-9]+|[^a-z0-9]+$/, "", w)
  return w
}

# Reports whether token t is a number that is not a year-month date.
function number(t) {
  return t ~ /^[$]?[0-9]/ && t !~ /^[12][0-9][0-9][0-9]-[01][0-9]([^0-9]|$)/
}

# Reports whether one of the three words before token i marks the range
# as an estimate (`typically between 0.5 and 1.5`).
function estimate(i,   k) {
  for (k = i - 1; k >= 1 && k >= i - 3; k--) {
    if (word(k) ~ /^(typically|usually|roughly|approximately|generally)$/) {
      return 1
    }
  }
  return 0
}

function report(i) {
  printf "%s:%d:%d: Style::ProseRangeEnds: %s\n", sentence_file, tok_line[i], tok_col[i], message
}

function sentence(n,   i, j, all) {
  for (i = 1; i <= n; i++) {
    all = all " " tolower(tok[i])
  }
  if (all ~ /inclusive|exclusive/) {
    return
  }
  for (i = 1; i <= n; i++) {
    if (word(i) == "between" && i + 3 <= n && number(tok[i + 1]) && word(i + 2) == "and" && number(tok[i + 3]) && word(i - 1) !~ /^(difference|differences|distinction)$/ && !estimate(i)) {
      report(i)
      return
    }
    if (word(i) ~ /^ranges?$/) {
      for (j = i + 1; j <= i + 4 && j <= n; j++) {
        if (number(tok[j]) && tok[j] ~ /^[$]?[0-9][0-9.,]*[-–][0-9]/) {
          report(i)
          return
        }
        if (number(tok[j]) && j + 2 <= n && word(j + 1) ~ /^(to|through)$/ && number(tok[j + 2])) {
          report(i)
          return
        }
      }
    }
  }
}
