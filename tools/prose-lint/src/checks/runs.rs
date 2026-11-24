//! The runs of prose that both tiers of ProseWallOfText measure.
//!
//! A run is the prose between two breaks, such as a list, a table, a code
//! block, or a heading. [`runs`] reads the blocks of the markdown model
//! to find them. Blank lines between paragraphs continue a run, and so do
//! the paragraphs of a blockquote.
//!
//! These blocks end a run:
//!
//! - A code block, fenced or indented. A code block inside a list of one
//!   item ends the run at the code. The words of the item before the code
//!   join the run. The words after the code open the next run.
//! - A heading, written with one to six `#` marks or underlined with `=`
//!   or `-`, also inside a list item. CommonMark reads a line with seven
//!   or more marks as text, so its words join the run (`####### It starts
//!   the worker.`).
//! - A list of two or more items, and a table of two or more body rows.
//!   The items of a nested list count toward the list that holds them.
//! - A line of a paragraph that opens with `:::`, with the `: ` of a
//!   definition, or with `{{`, the start of a Hugo shortcode. The words of
//!   such a line do not count.
//!
//! One item or one body row adds structure without content, so its words
//! join the run. An HTML block and a thematic break neither end a run nor
//! add words. A single list item, a `<br>`, or a rule therefore never
//! clears a finding. Words are tokens that hold an ASCII letter. The model
//! blanks code spans, and `blank_urls` in `src/checks/sentences.rs` blanks
//! bare URLs, so neither counts as a word.

use std::sync::LazyLock;

use regex::Regex;

use crate::checks::sentences::{blank_urls, words};
use crate::markdown::{Block, BlockKind, Markdown};

/// A line that ends a run: the opener or the closer of a MyST directive,
/// a definition, or a Hugo shortcode.
static BREAK_LINE: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"^[ \t>]*(?::::|:[ \t]|\{\{)").expect("the pattern is valid"));

/// One run of prose.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Run {
    /// The index in the region of the first character of the first word.
    pub at: usize,
    /// The number of words of the run.
    pub words: usize,
}

/// The words of a run, or of a list or a table that can join one.
#[derive(Default)]
struct Count {
    at: Option<usize>,
    words: usize,
}

impl Count {
    /// Adds the words of the lines of `block`.
    fn add_block(&mut self, model: &Markdown, block: &Block) {
        for (i, text) in model.block_lines(block) {
            self.add_line(model, i, &text);
        }
    }

    /// Adds the words of the text `text` of line `i`.
    fn add_line(&mut self, model: &Markdown, i: usize, text: &str) {
        let text = &blank_urls(text);
        for (offset, word) in words(text) {
            if word.contains(|c: char| c.is_ascii_alphabetic()) {
                self.at
                    .get_or_insert_with(|| model.lines[i].at(text, offset));
                self.words += 1;
            }
        }
    }

    /// Adds the words of `other`.
    fn join(&mut self, other: Count) {
        if self.at.is_none() {
            self.at = other.at;
        }
        self.words += other.words;
    }
}

/// A list that the walk has not closed yet.
struct List {
    /// The index of the character after the last item that the list holds
    /// so far.
    end: usize,
    /// The index in [`Markdown::lines`] of the line after the last item
    /// that the list holds so far.
    last: usize,
    /// The number of items, nested items included.
    items: usize,
    /// The words of the paragraphs of the items before the first code
    /// block, once an item holds one.
    before: Option<Count>,
    /// The words of the paragraphs of the items, after the first code
    /// block when an item holds one.
    count: Count,
}

/// Returns each run of `model` that holds a word.
pub fn runs(model: &Markdown) -> Vec<Run> {
    let mut found = Vec::new();
    let mut run = Count::default();
    let mut list: Option<List> = None;
    for block in &model.blocks {
        if block.kind == BlockKind::BlockQuote {
            continue;
        }
        // A block inside an item of the list belongs to the list, and so
        // does an item after only blank lines below the list.
        let in_list = list.as_ref().is_some_and(|l| {
            block.chars.start < l.end
                || (block.kind == BlockKind::ListItem
                    && model.lines[l.last.min(block.lines.start)..block.lines.start]
                        .iter()
                        .all(|line| line.source.trim_matches([' ', '\t', '\r', '>']).is_empty()))
        });
        if let Some(l) = list.as_mut().filter(|_| in_list) {
            match block.kind {
                BlockKind::ListItem => {
                    l.items += 1;
                    l.end = l.end.max(block.chars.end);
                    l.last = l.last.max(block.lines.end);
                    continue;
                }
                BlockKind::Paragraph => {
                    l.count.add_block(model, block);
                    continue;
                }
                BlockKind::CodeBlock => {
                    l.before.get_or_insert_with(|| std::mem::take(&mut l.count));
                    continue;
                }
                // A heading or a table in an item ends the list. It then
                // ends the run or adds a table, as it does outside a list.
                BlockKind::Heading(_) | BlockKind::Table | BlockKind::BlockQuote => {}
            }
        }
        if let Some(l) = list.take() {
            settle(l, &mut run, &mut found);
        }
        match block.kind {
            BlockKind::ListItem => {
                list = Some(List {
                    end: block.chars.end,
                    last: block.lines.end,
                    items: 1,
                    before: None,
                    count: Count::default(),
                });
            }
            BlockKind::Paragraph => {
                for (i, text) in model.block_lines(block) {
                    if BREAK_LINE.is_match(&model.lines[i].source) {
                        close(&mut run, &mut found);
                    } else {
                        run.add_line(model, i, &text);
                    }
                }
            }
            BlockKind::Table => {
                // The header row and the delimiter row are not body rows.
                if block.lines.len() >= 4 {
                    close(&mut run, &mut found);
                } else {
                    run.add_block(model, block);
                }
            }
            BlockKind::Heading(_) | BlockKind::CodeBlock => close(&mut run, &mut found),
            BlockKind::BlockQuote => {}
        }
    }
    if let Some(l) = list.take() {
        settle(l, &mut run, &mut found);
    }
    close(&mut run, &mut found);
    found
}

/// Closes the run `run` into `found`, when it holds a word.
fn close(run: &mut Count, found: &mut Vec<Run>) {
    let done = std::mem::take(run);
    if let Some(at) = done.at {
        found.push(Run {
            at,
            words: done.words,
        });
    }
}

/// Ends the list `list`. A list of two or more items ends the run. A list
/// of one item that holds a code block ends the run at the code. The words
/// before the code join the run. The words after the code open the next
/// run. Any other list of one item adds its words to the run.
fn settle(list: List, run: &mut Count, found: &mut Vec<Run>) {
    if list.items >= 2 {
        close(run, found);
    } else if let Some(before) = list.before {
        run.join(before);
        close(run, found);
        *run = list.count;
    } else {
        run.join(list.count);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::region::Region;

    /// Returns the line, the column, and the words of each run of `text`.
    fn found(text: &str) -> Vec<(usize, usize, usize)> {
        let region = Region::markdown(text);
        runs(&Markdown::new(&region))
            .into_iter()
            .map(|run| {
                let at = region.position(run.at);
                (at.line, at.col, run.words)
            })
            .collect()
    }

    #[test]
    fn blocks() {
        struct Case {
            name: &'static str,
            text: &'static str,
            /// The line, the column, and the words of each run.
            want: Vec<(usize, usize, usize)>,
        }
        let cases = [
            Case {
                name: "paragraphs and a blockquote",
                text: "One two.\n\n> Three `x` four.\n>\n> Five https://example.com/a-b.\n",
                want: vec![(1, 1, 5)],
            },
            Case {
                name: "a list of three items",
                text: "One.\n\n- Two.\n- Three.\n\n- Four.\n\nFive.\n",
                want: vec![(1, 1, 1), (8, 1, 1)],
            },
            Case {
                name: "a list of one item with a nested item",
                text: "One.\n\n- Two.\n  - Three.\n\nFour.\n",
                want: vec![(1, 1, 1), (6, 1, 1)],
            },
            Case {
                name: "a list of one item, a table of one row, a rule, and HTML",
                text: "One.\n\n- Two.\n\n| a |\n| - |\n| b |\n\n***\n\n<br>\n\nThree.\n",
                want: vec![(1, 1, 5)],
            },
            Case {
                name: "code in a list of one item",
                text: "One.\n\n- Two.\n\n      make\n\nThree.\n",
                want: vec![(1, 1, 2), (7, 1, 1)],
            },
            Case {
                name: "text after code in a list of one item",
                text: "One.\n\n- Two three.\n\n  ```\n  x\n  ```\n\n  Four.\n\nFive.\n",
                want: vec![(1, 1, 3), (9, 3, 2)],
            },
            Case {
                name: "headings, a definition, and a shortcode",
                text: "One.\n\n## Two\n\nThree.\nFour\n: Five.\n\n{{< note >}}\n\nSix.\n",
                want: vec![(1, 1, 1), (5, 1, 2), (11, 1, 1)],
            },
        ];
        for case in cases {
            assert_eq!(found(case.text), case.want, "{}", case.name);
        }
    }
}
