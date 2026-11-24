//! Overview
//!
//! A wall of text is a long run of markdown prose with no list, table,
//! code block, or heading to break it. This rule reports a run of 400 or
//! more words. A run that long often hides a list of steps, a set of
//! parallel items, or a comparison that reads better as a table. The
//! reader must pick those out of the paragraphs. The fix moves steps or
//! parallel items into a list or a table, and keeps reasoning in prose.
//! The rule sits at Recommended, because a run of that length is more
//! often an interview, an explanation, or a foreword, which belongs in
//! prose.
//!
//! Expression
//!
//! The check reads the runs that `runs` in `src/checks/runs.rs` finds in
//! the blocks of the markdown model and counts their words. The module doc
//! of `src/checks/runs.rs` also lists the blocks that end a run. A
//! one-item list, a one-row table, an HTML block, and a thematic break do
//! not end one, so decoration never clears the finding. A heading ends a
//! run, because it starts a new topic. Each finding names the line and
//! column of the run's first word.
//!
//! Related Rules
//!
//! The Recommended ProseWallOfText reports a run of 400 or more words. The
//! Optional ProseWallOfText reports a run of 250 to 399 words, so one run
//! never draws both findings.
//!
//! Measurement
//!
//! The rule hit 28 lines in the measure.py docs corpus. In 8 of 20 sampled
//! hits, the run hid steps, parallel items, or a comparison. The other 12
//! were interviews, explanations, or a foreword, which the message tells
//! the writer to keep in prose. The word counts of the two groups
//! overlapped, from 406 to 892 words, so a higher threshold does not
//! separate them.

use crate::checks::Hit;
use crate::checks::runs::runs;
use crate::markdown::Markdown;

/// The fewest words of a run that the check reports.
const MIN_WORDS: usize = 400;

/// Returns the message of a finding on a run of `words` words. The
/// Optional ProseWallOfText prints the same message.
pub fn message(words: usize) -> String {
    format!(
        "Wall of text. {words} words with no list, table, or code block. Move steps or parallel items into a list or a table, and keep reasoning in prose. Add a heading only where the topic changes."
    )
}

/// Returns a finding for each run of 400 or more words.
pub fn check(model: &Markdown) -> Vec<Hit> {
    runs(model)
        .into_iter()
        .filter(|run| run.words >= MIN_WORDS)
        .map(|run| Hit {
            at: run.at,
            message: message(run.words),
        })
        .collect()
}
