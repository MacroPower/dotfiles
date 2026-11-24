//! Overview
//!
//! Reports a markdown heading that skips a level below the heading before
//! it, such as a `####` heading right after a `##` heading. Readers who
//! scan the headings read the outline from their levels. When the outline
//! skips a level, they cannot tell whether the deeper heading belongs to
//! the one above it or to a missing section. Screen readers list headings
//! by level, so the skip also breaks their outline. The fix moves the
//! heading up to the next level, or adds the missing heading above it.
//! The rule sits at Recommended, because a sidebar heading inside a
//! blockquote skips a level on purpose.
//!
//! Expression
//!
//! The check compares the level of each heading block of the markdown
//! model with the level of the heading before it. It reads the headings
//! that `atx_level` of the Required ProseHeading keeps, which open with
//! `#` marks outside a blockquote. A `title:` line in
//! YAML front matter counts as the level-one heading, and so does a `#`
//! heading. A file with neither takes its first heading's level as the
//! top. Each finding names the line of the heading that skips.
//!
//! False Positives
//!
//! A PR description or an included fragment that starts at `###` passes,
//! because its first heading sets the top level. Headings inside fenced
//! and indented code pass, because the model does not hold a heading in
//! code. A heading inside a blockquote passes and does not set the level
//! of the next heading, because a sidebar heading skips a level on
//! purpose. A setext heading does not count either, because a writer
//! often means its line of `-` marks as a rule.
//!
//! Related Rules
//!
//! The fix can add the missing heading, and ProseHeading then checks the
//! added heading.
//!
//! Measurement
//!
//! The rule hit 18 lines in the measure.py docs corpus, and 16 of 18
//! skipped a level. In one misfire, YAML comment lines inside an unfenced
//! Hugo shortcode opened with `#`, and the check took them for a
//! level-one heading. The other was a sidebar heading inside a
//! blockquote, which skips a level on purpose.

use crate::checks::Hit;
use crate::checks::required::prose_heading::atx_level;
use crate::markdown::Markdown;

/// Returns whether YAML front matter opens the region and holds a
/// `title:` line.
fn titled(model: &Markdown) -> bool {
    let mut lines = model.lines.iter().map(|l| l.source.trim_end());
    if lines.next() != Some("---") {
        return false;
    }
    lines.take_while(|l| *l != "---" && *l != "...").any(|l| {
        l.strip_prefix("title")
            .is_some_and(|rest| rest.trim_start().starts_with(':'))
    })
}

/// Returns a finding for each heading that skips a level below the
/// heading before it.
pub fn check(model: &Markdown) -> Vec<Hit> {
    let mut hits = Vec::new();
    let mut prev = u8::from(titled(model));
    for block in &model.blocks {
        let Some(level) = atx_level(model, block) else {
            continue;
        };
        if prev > 0 && level > prev + 1 {
            hits.push(Hit {
                at: model.lines[block.lines.start].start,
                message: format!(
                    "Heading level. A level-{level} heading follows a level-{prev} heading and skips a level. Make it `{}`, or add the missing heading above it.",
                    "#".repeat(usize::from(prev) + 1)
                ),
            });
        }
        prev = level;
    }
    hits
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    #[test]
    fn levels() {
        struct Case {
            name: &'static str,
            text: &'static str,
            /// The line of each finding, counted from 1.
            want: Vec<usize>,
        }
        let cases = [
            Case {
                name: "skip below a title",
                text: "# Title\n\n### Setup\n",
                want: vec![3],
            },
            Case {
                name: "title in front matter",
                text: "---\ntitle: Guide\n---\n\n### Setup\n",
                want: vec![5],
            },
            Case {
                name: "first heading sets the top",
                text: "### Setup\n\n#### Steps\n",
                want: vec![],
            },
            Case {
                name: "setext heading",
                text: "Title\n=====\n\n### Setup\n",
                want: vec![],
            },
            Case {
                name: "setext heading that opens with a hash",
                text: "# Title\n\n### Three\n\n#x\n---\n\n#### Four\n",
                want: vec![3],
            },
            Case {
                name: "heading in a blockquote",
                text: "## Setup\n\n> #### Sidebar\n\n### Steps\n",
                want: vec![],
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
