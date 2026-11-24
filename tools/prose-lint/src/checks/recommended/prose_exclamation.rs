//! Overview
//!
//! Reports an exclamation mark that ends a sentence in markdown prose
//! (`That's it!`, `The tests pass now!`). The mark adds emphasis and no
//! fact, so the reader learns nothing from it. The fix ends the sentence
//! with a period. The rule sits at Recommended, because quoted program
//! output outside a code span also matches.
//!
//! Expression
//!
//! The check reads the prose of each line of the markdown model. Weir
//! reads `!` as negation, so no Weir rule can name the mark. The mark
//! counts after a letter, a digit, a closing bracket, a quote, or
//! emphasis, and before whitespace, the same closers, or the line end.
//!
//! False Positives
//!
//! The pattern leaves out `!=`, an image's `![`, an HTML comment's
//! `<!--`, a CSS `!important`, and a conventional-commit breaking marker
//! (`feat!:`). The model blanks code spans, so a mark inside one passes.
//! Program output that the prose quotes outside a code span still fires
//! (`prints "Done!" to the log`).
//!
//! Measurement
//!
//! The rule hit 396 lines in the measure.py docs corpus, and 18 of 20
//! sampled hits ended a sentence with the mark. The two misfires were
//! configuration values (`files = ['! docs/*']`) inside a Hugo
//! `code-toggle` shortcode, which the awk check that this check
//! replaced read as prose.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::Markdown;

static BANG: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r#"[A-Za-z0-9)"'*_\]](!+)(?:[ \t)"'*_\]]|$)"#).expect("the pattern is valid")
});

/// Returns a finding for the first exclamation mark on each line that
/// ends a sentence.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .lines
        .iter()
        .filter_map(|line| {
            let bang = BANG.captures(&line.text)?.get(1)?;
            Some(Hit {
                at: line.at(&line.text, bang.start()),
                message: "Exclamation mark. End the sentence with a period.".to_string(),
            })
        })
        .collect()
}
