//! Overview
//!
//! Reports a plural written as a word with `(s)` or `(es)` glued to it
//! (`file(s)`, `match(es)`) in markdown prose and in source comments. The
//! reader cannot tell whether the sentence means one thing or many. The
//! fix writes the plural, or writes `one or more`. The rule sits at
//! Required, because it matches one exact form and the fix is mechanical.
//!
//! Expression
//!
//! A Weir rule cannot name a literal paren, so the check is native. The
//! check reads the prose of each line of the markdown model. Code spans
//! are blank there, so the check never reads the text inside one. The
//! word before the paren needs two letters.
//!
//! False Positives
//!
//! Math notation (`f(s)`) passes, because `f` is one letter. A call such
//! as `len(s)` passes inside a code span. A comment writes code without
//! backticks, so `len(s)` and `cap(s)` pass on their own.
//!
//! Measurement
//!
//! The rule hit 51 lines in the measure.py corpus, and 19 of 20 sampled
//! hits were plurals that the writer can spell out. The one misfire was
//! `packet(s)` in a quoted passage of an RFC.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::Markdown;

static PLURAL: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[A-Za-z][A-Za-z]+\(e?s\)").expect("the pattern is valid"));

/// Returns a finding for the first plural in parentheses on each line,
/// unless that plural is `len(s)` or `cap(s)`.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .lines
        .iter()
        .filter_map(|line| {
            let hit = PLURAL.find(&line.text)?;
            if hit.as_str().starts_with("len(") || hit.as_str().starts_with("cap(") {
                return None;
            }
            Some(Hit {
                at: line.at(&line.text, hit.start()),
                message: format!(
                    "Plural `{}` in parentheses. Write the plural, or write `one or more`.",
                    hit.as_str()
                ),
            })
        })
        .collect()
}
