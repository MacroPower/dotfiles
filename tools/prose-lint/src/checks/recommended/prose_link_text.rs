//! Overview
//!
//! Reports link text that does not name a destination in markdown prose
//! (`[here](url)`, `[this page][ref]`, `[more info](url)`, `[this
//! section](#x)`). A reader who scans the links sees `here` with no hint
//! of where it goes. The fix names the page or the section that the link
//! opens, as the technical-writing skill asks. The rule sits at
//! Recommended, because a listed word can also be the name of the
//! destination.
//!
//! Expression
//!
//! The check compares the whole link text with a list of phrases. The
//! model blanks the `](url)` of a link in the prose of a line. The check
//! therefore matches the source of the line, which keeps the same
//! columns.
//! It counts a match only when the prose still holds its `[`, so a link
//! inside a code span passes.
//!
//! False Positives
//!
//! An image (`![here](x.png)`) passes, because its alt text describes the
//! picture.
//!
//! A link whose text is the name of its destination still matches when
//! the name is on the list, such as `[this](url)` for the JavaScript
//! keyword `this`.
//!
//! Measurement
//!
//! The rule hit 9 lines in the measure.py corpus, and 9 of 9 sampled hits
//! did not name a destination.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::Markdown;

static VAGUE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"(?:^|[^!])(\[)(here|click here|this|this link|link|this page|this doc|this document|this article|this post|this section|this guide|read more|more|learn more|see more|more info|more information|more details)\][(\[]",
    )
    .expect("the pattern is valid")
});

/// Returns a finding for the first link on each line whose text does
/// not name its destination.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .lines
        .iter()
        .filter(|line| line.prose)
        .filter_map(|line| {
            let lower = line.source.to_ascii_lowercase();
            let c = VAGUE.captures(&lower)?;
            let open = c.get(1)?.start();
            let at = line.at(&line.source, open);
            if line.text.chars().nth(at - line.start) != Some('[') {
                return None;
            }
            let words = c.get(2)?.range();
            Some(Hit {
                at,
                message: format!(
                    "Link text `{}` names no destination. Name the page or section it opens.",
                    &line.source[words]
                ),
            })
        })
        .collect()
}
