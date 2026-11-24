//! Overview
//!
//! A wall of text is a long run of markdown prose with no list, table,
//! code block, or heading to break it. This rule reports a run of 250 to
//! 399 words. A run that long often hides a list of steps, a set of
//! parallel items, or a comparison that reads better as a table. The
//! reader must pick those out of the paragraphs. The fix moves steps or
//! parallel items into a list or a table, and keeps reasoning in prose.
//! The rule sits at Optional, because a run of that length is as often an
//! interview, a story, or an argument, which belongs in prose.
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
//! The rule hit 126 lines in the measure.py docs corpus. In 10 of 20
//! sampled hits, the run hid steps, parallel items, or a comparison. The
//! other 10 were interviews, stories, or arguments, which belong in prose,
//! so the finding stays a suggestion.

use crate::checks::Hit;
use crate::checks::recommended::prose_wall_of_text::message;
use crate::checks::runs::runs;
use crate::markdown::Markdown;

/// The fewest words of a run that the check reports.
const MIN_WORDS: usize = 250;

/// The most words of a run that the check reports. The Recommended
/// ProseWallOfText reports a longer one.
const MAX_WORDS: usize = 399;

/// Returns a finding for each run of 250 to 399 words.
pub fn check(model: &Markdown) -> Vec<Hit> {
    runs(model)
        .into_iter()
        .filter(|run| (MIN_WORDS..=MAX_WORDS).contains(&run.words))
        .map(|run| Hit {
            at: run.at,
            message: message(run.words),
        })
        .collect()
}
