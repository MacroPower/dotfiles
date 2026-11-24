//! Overview
//!
//! Reports a curly quote (U+2018 to U+201F), the ellipsis character
//! (U+2026), and the dash variants U+2010 to U+2012, U+2015, and the
//! minus sign U+2212. A curly quote and a straight one look alike in a
//! terminal, so the reader cannot see which one a line holds. A search
//! for the straight one also misses the curly one. The fix writes a
//! straight quote, three periods, or a hyphen. The rule sits at Required,
//! because it matches exact characters and the fix is mechanical.
//!
//! Expression
//!
//! The check reads the prose of each line of the markdown model, in
//! which code is blank. It reports the first of those characters on a
//! line. A Weir rule cannot do this check. Harper normalizes punctuation
//! before Weir sees it, so a pattern for a curly quote also matches a
//! plain ASCII quote. prose-lint-rs straightens a curly apostrophe in
//! the copy that Harper reads, and the native checks read the text as
//! the file holds it. Each finding names the character and its code
//! point, because a curly quote and a straight one look alike in a
//! terminal.
//!
//! Related Rules
//!
//! ProseDash reports the em dash and the en dash, so this check leaves
//! them out.
//!
//! Measurement
//!
//! The rule hit 3627 lines in the measure.py corpus, and 20 of 20 sampled
//! hits were typographic characters in prose, 19 curly apostrophes and
//! one curly double quote. The Rust book holds 3410 of the 3627 lines.

use crate::checks::Hit;
use crate::markdown::Markdown;

/// Returns whether the check reports the character `c`.
fn typographic(c: char) -> bool {
    matches!(
        c,
        '\u{2010}'..='\u{2012}' | '\u{2015}' | '\u{2018}'..='\u{201F}' | '\u{2026}' | '\u{2212}'
    )
}

/// Returns a finding for the first typographic character on each line.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .lines
        .iter()
        .filter_map(|line| {
            let (i, c) = line.text.char_indices().find(|&(_, c)| typographic(c))?;
            Some(Hit {
                at: line.at(&line.text, i),
                message: format!(
                    "Typographic character `{c}` (`U+{:04X}`). Use a straight quote, three periods, or a hyphen.",
                    u32::from(c)
                ),
            })
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    #[test]
    fn code() {
        // The case of the exported-modes test of pkgs/prose-lint.nix. The
        // ellipsis in the fence does not draw a finding.
        let text = "# Title\n\nThe build failed, e.g. on a cache miss.\nIt stalled \u{2026} twice.\n\n```sh\necho \"fast, e.g. really \u{2026} and more\"\n```\n";
        let region = Region::markdown(text);
        let hits = check(&Markdown::new(&region));
        assert_eq!(hits.len(), 1);
        assert_eq!(
            region.position(hits[0].at),
            crate::region::Position { line: 4, col: 12 }
        );
        assert_eq!(
            hits[0].message,
            "Typographic character `\u{2026}` (`U+2026`). Use a straight quote, three periods, or a hyphen."
        );
    }
}
