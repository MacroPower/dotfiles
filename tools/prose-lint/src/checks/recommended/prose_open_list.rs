//! Overview
//!
//! Reports a list ending that leaves its contents to the reader (`etc.`,
//! `and so on`, `or the like`, `among others`). The reader cannot tell
//! which items the writer left out. The fix names every item, or says at
//! the start that the list is partial (`such as`). The rule sits at
//! Recommended, because a numbered pattern that has no last item ends the
//! same way.
//!
//! Expression
//!
//! Harper reads `etc.` as one abbreviation token that no Weir pattern
//! names, so the check is native. The check reads the prose of each line
//! of the markdown model, in which code spans are blank, so a match
//! inside one passes.
//!
//! `etc` needs a character before it that is not a letter, a digit,
//! `/`, or `.`, and a character after it that is not a letter or `/`.
//! `including but not limited to` counts anywhere, because it opens a
//! list that it never closes. `and more`, `and many more`, `and much
//! more`, `and others`, `and similar`, and `or similar` count only
//! before a period, a semicolon, a colon, a comma, or a closing paren.
//!
//! False Positives
//!
//! A path passes (`/etc/nixos`), because of the character that `etc`
//! needs before it. A time zone name passes (`Etc/GMT+11`), because of
//! the character that `etc` needs after it. In the middle of a
//! sentence, `and more` compares (`safer and more convenient`) and `and
//! others` can name a group (`for you and others to read`), so both
//! pass there.
//!
//! Measurement
//!
//! The rule hit 177 lines in the measure.py corpus, and 18 of 20 sampled
//! hits left a list open. Both misfires continued a numbered pattern that
//! has no last item (`1 inside a top-level object or array, 2 inside a
//! nested object or array, and so on`).

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::Markdown;

static OPEN_LIST: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?:^|[^a-z0-9/.])(etc)(?:[^a-z/]|$)|(?:^|[^a-z])(and so on|and so forth|and the like|or the like|among others|and whatnot|including but not limited to)(?:[^a-z]|$)|(?:^|[^a-z])(and more|and similar|or similar|and others|and many more|and much more)[.;:,)]",
    )
    .expect("the pattern is valid")
});

/// Returns a finding for the first open-ended list on each line.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .lines
        .iter()
        .filter_map(|line| {
            let lower = line.text.to_ascii_lowercase();
            let c = OPEN_LIST.captures(&lower)?;
            let hit = c.iter().skip(1).flatten().next()?;
            Some(Hit {
                at: line.at(&lower, hit.start()),
                message: format!(
                    "Open-ended list `{}`. Name every item, or open the list with `such as`.",
                    hit.as_str()
                ),
            })
        })
        .collect()
}
