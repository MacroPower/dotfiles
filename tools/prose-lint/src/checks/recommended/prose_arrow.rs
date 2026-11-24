//! Overview
//!
//! Reports a spaced ASCII arrow (`->`, `=>`, `<-`) that stands in for a
//! verb in markdown prose (`plan -> apply`). The reader must guess the
//! verb that the arrow replaces. The message names three fixes: the verb
//! that the arrow stands for (`the client calls the proxy`), a colon
//! after a label (`Yes: Hugo builds the site`), and `>` for a menu path.
//! The rule sits at Recommended, because a spaced ASCII arrow also
//! appears in prose that quotes code without a code span.
//!
//! Expression
//!
//! The check reads the prose of each line of the markdown model, in
//! which code, HTML, and link destinations are blank. `ASCII` matches an
//! arrow that has a space, a tab, or an edge of the line on each side.
//! The check reports the first such arrow on a line.
//!
//! False Positives
//!
//! A glued arrow (`x->y`, `x=>x+1`) is code written without a code span,
//! so the check leaves it alone.
//!
//! Related Rules
//!
//! The Required ProseArrow reports the Unicode arrows and their HTML
//! entities. This tier reports only the spaced ASCII arrows, so the two
//! tiers never report the same arrow.
//!
//! Measurement
//!
//! The rule hit 5 lines in the measure.py corpus, and 1 of 5 stood in for
//! a word (`v0.1.1 => v0.2.0 is a minor release`). The other four were
//! code in a `<pre>` block or in a Hugo shortcode. The measurement read
//! the corpus through lib/markdown.awk, which skips neither.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::Markdown;

/// A spaced arrow (`plan -> apply`), which stands in for a verb.
static ASCII: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?:^|[ \t])(->|=>|<-)(?:[ \t]|$)").expect("the pattern is valid")
});

/// Returns a finding for the first spaced arrow on each line.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .lines
        .iter()
        .filter_map(|line| {
            let hit = ASCII.captures(&line.text)?.get(1)?;
            Some(Hit {
                at: line.at(&line.text, hit.start()),
                message: format!(
                    "Arrow `{}` stands in for a word. Write the verb, a colon after a label, or `>` for a menu path.",
                    hit.as_str()
                ),
            })
        })
        .collect()
}
