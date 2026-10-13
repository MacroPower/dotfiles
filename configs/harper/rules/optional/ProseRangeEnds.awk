# Reports a range that does not say whether it includes its ends in
# markdown prose (`a weight between 1 and 100`). The reader cannot tell
# whether 1 and 100 are themselves valid. The fix adds `inclusive` or
# `exclusive`, or names each end (`from 1 up to but not including
# 100`).
#
# The check reports `between`, a number, `and`, and a number, where each
# number opens with a digit. A sentence that holds `inclusive` or
# `exclusive` passes, and so does a range of dates (`between 2022-11 and
# 2024-05`). `from N to M` stays out, because it often describes a
# change (`the replicas drop from 3 to 1`). lib/sentences.awk finds the
# sentence boundaries.
#
# It hit 22 lines in the measure.py corpus, and 22 of 22 sampled hits
# left the ends of the range open.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Style::ProseRangeEnds: <message>

@include "sentences.awk"

function BEGIN_CHECK() {
}

# Returns token i in lowercase without the punctuation around it.
function word(i,   w) {
  w = tolower(tok[i])
  gsub(/^[^a-z0-9]+|[^a-z0-9]+$/, "", w)
  return w
}

function sentence(n,   i, all) {
  for (i = 1; i <= n; i++) {
    all = all " " tolower(tok[i])
  }
  if (all ~ /inclusive|exclusive/) {
    return
  }
  for (i = 1; i + 3 <= n; i++) {
    if (word(i) == "between" && tok[i + 1] ~ /^[$]?[0-9]/ && tok[i + 1] !~ /^[0-9]+-[0-9]/ && word(i + 2) == "and" && tok[i + 3] ~ /^[$]?[0-9]/) {
      printf "%s:%d:%d: Style::ProseRangeEnds: Range ends. Say whether the range includes each end.\n", sentence_file, tok_line[i], tok_col[i]
      return
    }
  }
}
