# Reports a run of 400 or more words of markdown prose, with no list,
# table, code block, or heading to break it. Prose that long often
# hides a list of steps, a set of parallel items, or a comparison that
# reads better as a table. The Optional rule of the same name
# reports a run of 250 to 399 words, so one run never draws both
# findings.
#
# lib/runs.awk finds the runs and counts their words. It also lists the
# lines that end a run. A one-item list, a one-row table, an HTML line,
# and a horizontal rule do not end one, so decoration never clears the
# finding. A heading ends a run, because it starts a new topic. Each
# finding names the line and column of the run's first word.
#
# It hit 28 lines in the measure.py docs corpus. In 8 of 20 sampled
# hits, the run hid steps, parallel items, or a comparison. The other
# 12 were interviews, explanations, or a foreword, which the message
# tells the writer to keep in prose. The word counts of the two groups
# overlapped, from 406 to 892 words, so a higher threshold does not
# separate them.
#
# Output format, one line per finding:
#   <file>:<line>:<col>: Readability::ProseWallOfText: <message>

@include "runs.awk"

function BEGIN_CHECK() {
  min_words = 400
}

function run(words) {
  if (words >= min_words) {
    printf "%s:%d:%d: Readability::ProseWallOfText: Wall of text. %d words with no list, table, or code block. Move steps or parallel items into a list or a table, and keep reasoning in prose. Add a heading only where the topic changes.\n", run_file, run_line, run_col, words
  }
}
