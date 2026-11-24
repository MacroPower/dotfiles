//! Overview
//!
//! Reports a Unicode arrow or its HTML entity that stands in for a verb
//! in markdown prose (`client → proxy`, `Settings → Integrations`). The
//! reader must guess the verb that the arrow replaces. The message names
//! three fixes: the verb that the arrow stands for (`the client calls the
//! proxy`), a colon after a label (`Yes: Hugo builds the site`), and `>`
//! for a menu path. The rule sits at Required, because it matches exact
//! characters, and every sampled hit stood in for a word.
//!
//! Expression
//!
//! The check reads the prose of each line of the markdown model, in
//! which code, HTML, and link destinations are blank. `ARROW` matches
//! each arrow character and each HTML entity. `KEYNAME` matches an arrow
//! that names a key, and the check blanks every such arrow. It then
//! reports the first arrow that remains on the line.
//!
//! False Positives
//!
//! An arrow that names a key does not stand in for a verb, so it passes.
//! `KEYNAME` takes an arrow as a key name after a modifier glyph or a
//! `Ctrl+` style prefix (`⌘→`, `Ctrl+→`) and before the word `key` (`the
//! → key`). It also takes an arrow that sits alone in a table cell. The
//! up, down, and northeast arrows stay out, because docs use them as a
//! link icon or a UI glyph.
//!
//! Related Rules
//!
//! The Recommended ProseArrow reports the spaced ASCII arrows (`->`,
//! `=>`, `<-`). This tier reports only the Unicode arrows and their HTML
//! entities, so the two tiers never report the same arrow.
//!
//! Measurement
//!
//! The rule hit 47 lines in the measure.py corpus, and 20 of 20 sampled
//! hits stood in for a word. Eight followed a label (`Yes →`), six mapped
//! one value to another (`< → &lt;`), and six drew a flow (`Pod → Node`).

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::{Markdown, blank};

/// Each arrow character and each HTML entity of an arrow, as
/// alternatives of a pattern.
const ARROWS: &str = "\u{2192}|\u{2190}|\u{2194}|\u{21d2}|\u{21d0}|\u{21d4}|\u{27f6}|\u{27f5}|\u{27f7}|\u{27f9}|\u{27f8}|\u{27fa}|\u{21a6}|\u{27fc}|\u{2794}|\u{279c}|\u{279d}|\u{279e}|\u{27a1}|\u{2b05}|&rarr;|&larr;|&harr;|&rArr;|&lArr;|&hArr;";

static ARROW: LazyLock<Regex> = LazyLock::new(|| Regex::new(ARROWS).expect("the pattern is valid"));

/// An arrow that names a key: after a modifier glyph or a `Ctrl+` style
/// prefix, before the word `key`, or alone in a table cell.
static KEYNAME: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(&format!(
        "(?:\u{2318}|\u{2303}|\u{2325}|\u{21e7}|[A-Za-z]+[+-])+(?:{ARROWS})\
         |(?:{ARROWS})[ \t]*(?:arrow[ \t]+)?keys?(?:[^A-Za-z]|$)\
         |\\|[ \t]*(?:{ARROWS})[ \t]*(?:\\||$)"
    ))
    .expect("the pattern is valid")
});

/// Returns a finding for the first arrow on each line that does not
/// name a key.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .lines
        .iter()
        .filter_map(|line| {
            let text = blank(&line.text, &KEYNAME);
            let hit = ARROW.find(&text)?;
            Some(Hit {
                at: line.at(&text, hit.start()),
                message: format!(
                    "Arrow `{}` stands in for a word. Write the verb, a colon after a label, or `>` for a menu path.",
                    hit.as_str()
                ),
            })
        })
        .collect()
}
