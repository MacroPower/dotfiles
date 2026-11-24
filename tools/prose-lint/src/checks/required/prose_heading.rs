//! Overview
//!
//! Reports markdown headings below the title that do not name a topic:
//! a wrap-up (`Conclusion`, `Final Thoughts`, `Key Takeaways`, `TL;DR`),
//! or a heading that ends in a colon (`Setup:`). A wrap-up announces
//! where the document ends, not what the section holds, so a reader who
//! scans the headings learns nothing from it. The fix names the topic as a
//! short noun phrase, a noun and the words attached to it. A heading
//! already sets its section apart from the text, so a colon at its end
//! adds nothing, and the fix deletes it. The rule sits at Required,
//! because it matches a fixed list of headings and one trailing
//! character.
//!
//! Expression
//!
//! The check reads each heading block of the markdown model, at level
//! two and below. The model does not hold a heading in code or in
//! front matter. [`atx_level`] keeps only a heading that opens with `#`
//! marks outside a blockquote. A setext heading, which a line of `=` or
//! `-` marks underlines, does not count. A writer often means such a
//! `-` line as a rule, and the formatter rewrites a real one to `#`
//! marks. A heading in a blockquote titles a sidebar, so it does not
//! count either.
//!
//! [`heading_text`] reads the source of the first line of the heading
//! and drops the `#` marks around it. The check compares the whole
//! heading with a list of wrap-ups, in any letter case. A trailing
//! attribute list (`{#linux-security}`) is markup, so the check drops it
//! first. A heading that holds a code span prints in a double-backtick
//! span, so its own backticks stay inside the quote.
//!
//! False Positives
//!
//! `Summary`, `Overview`, and `Next Steps` pass, because human-written
//! docs and PR descriptions use them as section names.
//!
//! The check skips a level-one heading, because it is the document title.
//! It also skips a heading that is one code span, because a command
//! reference names the exact command.
//!
//! Related Rules
//!
//! The Required ProseHeading reports wrap-ups (`Conclusion`) and headings
//! that end in a colon (`Setup:`). The Recommended ProseHeading reports
//! headings that narrate (`How matching works`) or run long. The
//! Recommended triggers misread a heading more often, so a commit that
//! draws them passes on its second try.
//!
//! The Recommended ProseHeading skips each heading that this check
//! reports, so the two tiers never report the same heading. When a
//! heading that narrates (`How it works:`) or runs long ends in a colon,
//! it draws only the finding for the colon. The Recommended ProseHeading
//! reports the heading after the writer deletes the colon.
//!
//! Measurement
//!
//! The rule hit 16 lines in the measure.py docs corpus, and 16 of 16 were
//! wrap-ups or trailing colons (`Conclusion`, `Linux:`).

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::Hit;
use crate::markdown::{Block, BlockKind, Markdown};

/// Whole headings that announce a wrap-up.
pub const CLOSERS: &[&str] = &[
    "conclusion",
    "conclusions",
    "final thoughts",
    "closing thoughts",
    "concluding thoughts",
    "key takeaways",
    "takeaways",
    "wrapping up",
    "wrap-up",
    "in summary",
    "in closing",
    "tl;dr",
    "tldr",
];

static OPENING: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^#+[ \t]+").expect("the pattern is valid"));
static CLOSING: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[ \t]+#+[ \t]*$").expect("the pattern is valid"));
static ONE_SPAN: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^`[^`]+`$").expect("the pattern is valid"));
static ATTRIBUTES: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[ \t]*\{[#.][^}]*\}[ \t]*$").expect("the pattern is valid"));
static END_MARKS: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[.!:]+$").expect("the pattern is valid"));
static SPACES: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"[ \t]+").expect("the pattern is valid"));
static ATX: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^#{1,6}(?:[ \t]|$)").expect("the pattern is valid"));

/// A heading of level two or below.
pub struct Heading {
    /// The level of the heading, from 2 to 6.
    pub level: u8,
    /// The text of the heading, without its `#` marks and its attribute
    /// list.
    pub text: String,
    /// The index in the region of the first character of its line.
    pub at: usize,
}

/// Returns the heading of `block` when the block is a heading of level
/// two or below, and its text is not one code span.
pub fn heading_text(model: &Markdown, block: &Block) -> Option<Heading> {
    let level = atx_level(model, block).filter(|&level| level >= 2)?;
    let line = &model.lines[block.lines.start];
    let text = source(model, block);
    let text = OPENING.replace(&text, "");
    let text = CLOSING.replace(&text, "");
    let text = text.trim_end_matches([' ', '\t']);
    if ONE_SPAN.is_match(text) {
        return None;
    }
    Some(Heading {
        level,
        text: ATTRIBUTES.replace(text, "").into_owned(),
        at: line.start,
    })
}

/// Returns the source of the first line of `block`, from its first
/// character.
fn source(model: &Markdown, block: &Block) -> String {
    let line = &model.lines[block.lines.start];
    line.source
        .chars()
        .skip(block.chars.start - line.start)
        .collect()
}

/// Returns the level of `block` when the block is a heading that opens
/// with `#` marks outside a blockquote.
pub fn atx_level(model: &Markdown, block: &Block) -> Option<u8> {
    let BlockKind::Heading(level) = block.kind else {
        return None;
    };
    (ATX.is_match(&source(model, block)) && !model.inside(BlockKind::BlockQuote, block.chars.start))
        .then_some(level)
}

/// Returns the heading text in lowercase, without the marks at its end
/// and with each run of whitespace as one space.
pub fn name(text: &str) -> String {
    let lower = text.to_lowercase();
    SPACES
        .replace_all(&END_MARKS.replace(&lower, ""), " ")
        .into_owned()
}

/// Returns whether the Required ProseHeading reports the heading text.
pub fn reports(text: &str) -> bool {
    CLOSERS.contains(&name(text).as_str()) || text.ends_with(':')
}

/// Returns the heading text as a code span. A heading that holds a code
/// span gets a double-backtick span, so its own backticks stay inside.
pub fn quote(text: &str) -> String {
    if text.contains('`') {
        format!("`` {text} ``")
    } else {
        format!("`{text}`")
    }
}

/// Returns a finding for each wrap-up heading and each heading that
/// ends in a colon.
pub fn check(model: &Markdown) -> Vec<Hit> {
    model
        .blocks
        .iter()
        .filter_map(|block| heading_text(model, block))
        .filter_map(|h| {
            let message = if CLOSERS.contains(&name(&h.text).as_str()) {
                format!(
                    "Heading {} names no topic. Name the topic as a short noun phrase.",
                    quote(&h.text)
                )
            } else if h.text.ends_with(':') {
                format!(
                    "Heading {} ends in a colon. Delete the colon.",
                    quote(&h.text)
                )
            } else {
                return None;
            };
            Some(Hit { at: h.at, message })
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    #[test]
    fn headings() {
        struct Case {
            name: &'static str,
            text: &'static str,
            /// The line of each finding, counted from 1.
            want: Vec<usize>,
        }
        let cases = [
            Case {
                name: "wrap-up",
                text: "# Guide\n\n## Conclusion\n",
                want: vec![3],
            },
            Case {
                name: "colon and attribute list",
                text: "## Setup: {#setup}\n",
                want: vec![1],
            },
            Case {
                name: "title",
                text: "# Conclusion\n",
                want: vec![],
            },
            Case {
                name: "setext heading",
                text: "The option can be set as follows:\n---\n",
                want: vec![],
            },
            Case {
                name: "setext heading that opens with a hash",
                text: "#hashtag summary:\n---\n",
                want: vec![],
            },
            Case {
                name: "heading in a blockquote",
                text: "> ## Conclusion\n",
                want: vec![],
            },
            Case {
                name: "heading in a list item",
                text: "- ## Conclusion\n",
                want: vec![1],
            },
        ];
        for case in cases {
            let region = Region::markdown(case.text);
            let got: Vec<_> = check(&Markdown::new(&region))
                .into_iter()
                .map(|hit| region.position(hit.at).line)
                .collect();
            assert_eq!(got, case.want, "{}", case.name);
        }
    }
}
