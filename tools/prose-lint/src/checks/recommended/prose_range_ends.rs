//! Overview
//!
//! Reports a range that does not say whether it includes its ends in
//! markdown prose (`a weight between 1 and 100`). The reader cannot tell
//! whether 1 and 100 are themselves valid. The fix adds `inclusive` or
//! `exclusive`, or names each end (`from 1 up to but not including 100`).
//! The rule sits at Recommended, because the ends matter less for a ratio.
//!
//! Expression
//!
//! The check reads the sentences that `sentences` in
//! `src/checks/sentences.rs` finds in each paragraph block of the markdown
//! model, one sentence at a time. It reports two forms, where each number
//! opens with a digit. The first is `between`, a number, `and`, and a
//! number. The second is `range` with a number pair in the next four words,
//! joined by `to`, `through`, or a hyphen (`the range 0-65535`, `values
//! range from 0.0 to 1.0`). A sentence that holds `inclusive` or
//! `exclusive` passes.
//!
//! False Positives
//!
//! `difference between` and `distinction between` pass, because they
//! compare two values (`the distinction between 1 and 2`). A range of dates
//! passes too (`between 2022-11 and 2024-05`). `from N to M` without
//! `range` stays out, because it often describes a change (`the replicas
//! drop from 3 to 1`).
//!
//! A range that an estimate word opens passes (`typically between 0.5 and
//! 1.5`), because an estimate has no exact ends. The estimate words are
//! `typically`, `usually`, `roughly`, `approximately`, and `generally` in
//! the three words before `between`. `about` and `around` stay out, because
//! they also open phrases such as `learn about values between 1 and 100`.
//!
//! Measurement
//!
//! The rule hit 29 lines in the measure.py docs, and 27 of 29 hits left the
//! ends of a limit open (`a weight between 1 and 100`, `the range
//! 0-65535`). The other 2 were ratios between 0 and 1, where the ends
//! matter less.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::checks::sentences::{Token, sentences};
use crate::markdown::Markdown;

const MESSAGE: &str =
    "Range ends. Say whether the range includes each end, such as `between 1 and 100, inclusive`.";

static NUMBER: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^\$?[0-9]").expect("the pattern is valid"));
static YEAR_MONTH: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^[12][0-9]{3}-[01][0-9](?:[^0-9]|$)").expect("the pattern is valid")
});
static PAIR: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^\$?[0-9][0-9.,]*[-\x{2013}][0-9]").expect("the pattern is valid")
});

/// Returns token `i` of `tokens` in lowercase without the characters
/// other than letters and digits at its ends, or an empty string outside
/// the sentence.
fn word(tokens: &[Token], i: Option<usize>) -> String {
    i.and_then(|i| tokens.get(i)).map_or_else(String::new, |t| {
        t.text
            .to_lowercase()
            .trim_matches(|c: char| !c.is_ascii_lowercase() && !c.is_ascii_digit())
            .to_string()
    })
}

/// Returns whether the token `token` is a number that is not a
/// year-month date.
fn number(token: &str) -> bool {
    NUMBER.is_match(token) && !YEAR_MONTH.is_match(token)
}

/// Returns the index of the token that the finding of one sentence names,
/// if any.
fn sentence(tokens: &[Token]) -> Option<usize> {
    let all = tokens
        .iter()
        .map(|t| t.text.to_lowercase())
        .collect::<Vec<_>>()
        .join(" ");
    if all.contains("inclusive") || all.contains("exclusive") {
        return None;
    }
    let n = tokens.len();
    let w = |i: usize| word(tokens, Some(i));
    let text = |i: usize| tokens[i].text.as_str();
    for i in 0..n {
        if w(i) == "between"
            && i + 3 < n
            && number(text(i + 1))
            && w(i + 2) == "and"
            && number(text(i + 3))
            && !matches!(
                word(tokens, i.checked_sub(1)).as_str(),
                "difference" | "differences" | "distinction"
            )
            && !(i.saturating_sub(3)..i).any(|k| {
                matches!(
                    w(k).as_str(),
                    "typically" | "usually" | "roughly" | "approximately" | "generally"
                )
            })
        {
            return Some(i);
        }
        if matches!(w(i).as_str(), "range" | "ranges") {
            for j in i + 1..(i + 5).min(n) {
                if number(text(j)) && PAIR.is_match(text(j)) {
                    return Some(i);
                }
                if number(text(j))
                    && j + 2 < n
                    && matches!(w(j + 1).as_str(), "to" | "through")
                    && number(text(j + 2))
                {
                    return Some(i);
                }
            }
        }
    }
    None
}

/// Returns a finding for each sentence that holds a range without its
/// ends.
pub fn check(model: &Markdown) -> Vec<Hit> {
    sentences(model)
        .iter()
        .filter_map(|tokens| {
            sentence(tokens).map(|i| Hit {
                at: tokens[i].at,
                message: MESSAGE.to_string(),
            })
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    /// Returns the line and the column of each finding on `text`.
    fn found(text: &str) -> Vec<(usize, usize)> {
        let region = Region::markdown(text);
        check(&Markdown::new(&region))
            .into_iter()
            .map(|hit| {
                let at = region.position(hit.at);
                (at.line, at.col)
            })
            .collect()
    }

    #[test]
    fn sentences() {
        struct Case {
            name: &'static str,
            text: &'static str,
            /// The line and the column of each finding.
            want: Vec<(usize, usize)>,
        }
        let cases = [
            Case {
                name: "`between`",
                text: "Set a weight between 1 and 100.\n",
                want: vec![(1, 14)],
            },
            Case {
                name: "`range` and a hyphen",
                text: "Pick a port in the range 0-65535.\n",
                want: vec![(1, 20)],
            },
            Case {
                name: "`range` and `to`",
                text: "Values range from 0.0 to 1.0.\n",
                want: vec![(1, 8)],
            },
            Case {
                name: "inclusive",
                text: "Set a weight between 1 and 100, inclusive.\n",
                want: vec![],
            },
            Case {
                name: "dates and an estimate",
                text: "It ran between 2022-11 and 2024-05. It takes typically between 0.5 and 1.5 seconds.\n",
                want: vec![],
            },
        ];
        for case in cases {
            assert_eq!(found(case.text), case.want, "{}", case.name);
        }
    }
}
