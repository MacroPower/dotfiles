# Overview
#
# A wall of text is a long run of markdown prose with no list, table,
# code block, or heading to break it. This rule reports a run of 400 or
# more words. A run that long often hides a list of steps, a set of
# parallel items, or a comparison that reads better as a table. The
# reader must pick those out of the paragraphs. The fix moves steps or
# parallel items into a list or a table, and keeps reasoning in prose.
# The rule sits at Recommended, because a run of that length is more
# often an interview, an explanation, or a foreword, which belongs in
# prose.
#
# Expression
#
# The check includes lib/runs.awk, which finds the runs and counts their
# words. lib/runs.awk also lists the lines that end a run. A one-item
# list, a one-row table, an HTML line, and a horizontal rule do not end
# one, so decoration never clears the finding. A heading ends a run,
# because it starts a new topic. Each finding names the line and column
# of the run's first word.
#
# Related Rules
#
# The Recommended ProseWallOfText reports a run of 400 or more words.
# The Optional ProseWallOfText reports a run of 250 to 399 words, so one
# run never draws both findings.
#
# Measurement
#
# The rule hit 28 lines in the measure.py docs corpus. In 8 of 20
# sampled hits, the run hid steps, parallel items, or a comparison. The
# other 12 were interviews, explanations, or a foreword, which the
# message tells the writer to keep in prose. The word counts of the two
# groups overlapped, from 406 to 892 words, so a higher threshold does
# not separate them.
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
